package store

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wfcosta/backend-challenge-go/internal/application"
	"github.com/wfcosta/backend-challenge-go/internal/domain"
	"github.com/wfcosta/backend-challenge-go/internal/eventos"
)

var ErrWalletExists = errors.New("wallet already exists")
var ErrWalletNotFound = errors.New("wallet not found")

type Wallet struct {
	ID      string
	Player  string
	Balance domain.Money
	Version int64
}

type ResultadoAposta struct {
	ID          string
	Status      string
	Balance     domain.Money
	Replay      bool
	FailureCode string
}

func (s *Store) BuscarTransacao(ctx context.Context, id string) (ResultadoAposta, error) {
	var resultado ResultadoAposta
	var minor int64
	var currency string
	err := s.pool.QueryRow(ctx, "SELECT id,status,result_balance_minor,currency,COALESCE(failure_code,'') FROM wagering_transactions WHERE id=$1", id).Scan(&resultado.ID, &resultado.Status, &minor, &currency, &resultado.FailureCode)
	if err != nil {
		return ResultadoAposta{}, errors.New("transacao nao encontrada")
	}
	resultado.Balance, err = domain.NewMoney(fmt.Sprintf("%d.%02d", minor/100, minor%100), currency)
	if err != nil {
		return ResultadoAposta{}, err
	}
	return resultado, nil
}

func (s *Store) BuscarTransacaoExterna(ctx context.Context, provedor, externo string) (ResultadoAposta, error) {
	var resultado ResultadoAposta
	var minor int64
	var currency string
	err := s.pool.QueryRow(ctx, "SELECT id,status,result_balance_minor,currency,COALESCE(failure_code,'') FROM wagering_transactions WHERE provider_id=$1 AND external_transaction_id=$2", provedor, externo).Scan(&resultado.ID, &resultado.Status, &minor, &currency, &resultado.FailureCode)
	if err != nil {
		return ResultadoAposta{}, errors.New("transacao nao encontrada")
	}
	resultado.Balance, err = domain.NewMoney(fmt.Sprintf("%d.%02d", minor/100, minor%100), currency)
	if err != nil {
		return ResultadoAposta{}, err
	}
	return resultado, nil
}

type Lancamento struct {
	ID             string
	TransactionID  string
	Direcao        string
	Dinheiro       domain.Money
	SaldoAnterior  domain.Money
	SaldoPosterior domain.Money
}

type ResultadoConciliacao struct {
	CarteiraID             string
	SaldoArmazenado        domain.Money
	SaldoCalculado         domain.Money
	Diferenca              domain.Money
	Consistente            bool
	LancamentosVerificados int
}

type Store struct{ pool *pgxpool.Pool }

func New(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }
func (s *Store) Pool() *pgxpool.Pool            { return s.pool }

func (s *Store) Registrar(ctx context.Context, consumidor, mensagemID, corpo string) (bool, error) {
	hash := sha256.Sum256([]byte(corpo))
	valor := hex.EncodeToString(hash[:])
	var inserido bool
	err := s.pool.QueryRow(ctx, "INSERT INTO inbox_messages(consumer_name,message_id,payload_hash) VALUES($1,$2,$3) ON CONFLICT DO NOTHING RETURNING true", consumidor, mensagemID, valor).Scan(&inserido)
	if err == nil {
		return inserido, nil
	}
	var concluida *time.Time
	var hashArmazenado string
	if err = s.pool.QueryRow(ctx, "SELECT payload_hash,completed_at FROM inbox_messages WHERE consumer_name=$1 AND message_id=$2", consumidor, mensagemID).Scan(&hashArmazenado, &concluida); err != nil {
		return false, err
	}
	if hashArmazenado != valor {
		return false, errors.New("mensagem SQS duplicada com payload divergente")
	}
	return concluida == nil, nil
}

func (s *Store) Concluir(ctx context.Context, consumidor, mensagemID string) error {
	_, err := s.pool.Exec(ctx, "UPDATE inbox_messages SET completed_at=now() WHERE consumer_name=$1 AND message_id=$2", consumidor, mensagemID)
	return err
}

func (s *Store) CreateWallet(ctx context.Context, playerID string, money domain.Money) (Wallet, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Wallet{}, err
	}
	defer tx.Rollback(ctx)
	var w Wallet
	var minor int64
	err = tx.QueryRow(ctx, "INSERT INTO wallets(player_id,currency,balance_minor,version) VALUES($1,$2,$3,1) RETURNING id, player_id, balance_minor, version",
		playerID, money.Currency(), money.Minor()).Scan(&w.ID, &w.Player, &minor, &w.Version)
	if err != nil {
		return Wallet{}, err
	}
	if minor > 0 {
		var transactionID string
		err = tx.QueryRow(ctx, "INSERT INTO wagering_transactions(wallet_id,player_id,kind,amount_minor,currency,status,result_balance_minor,result_wallet_version) VALUES($1,$2,'OPENING',$3,$4,'PROCESSED',$3,1) RETURNING id", w.ID, playerID, minor, money.Currency()).Scan(&transactionID)
		if err != nil {
			return Wallet{}, err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO ledger_entries(wallet_id,transaction_id,direction,amount_minor,balance_before_minor,balance_after_minor) VALUES($1,$2,'CREDIT',$3,0,$3)", w.ID, transactionID, minor); err != nil {
			return Wallet{}, err
		}
		if err = inserirEvento(ctx, tx, eventos.NovoEnvelope("WagerTransactionProcessed", transactionID, transactionID, map[string]any{"transactionId": transactionID, "status": "PROCESSED"})); err != nil {
			return Wallet{}, err
		}
		if err = inserirEvento(ctx, tx, eventos.NovoEnvelope("WalletBalanceChanged", w.ID, transactionID, map[string]any{"walletId": w.ID, "transactionId": transactionID, "direction": "CREDIT", "money": map[string]string{"amount": money.String(), "currency": money.Currency()}, "balanceBefore": map[string]string{"amount": "0.00", "currency": money.Currency()}, "balanceAfter": map[string]string{"amount": money.String(), "currency": money.Currency()}, "walletVersion": 1})); err != nil {
			return Wallet{}, err
		}
	}
	normalized, err := domain.NewMoney(fmt.Sprintf("%d.%02d", minor/100, minor%100), money.Currency())
	if err != nil {
		return Wallet{}, err
	}
	w.Balance = normalized
	if err := tx.Commit(ctx); err != nil {
		return Wallet{}, err
	}
	return w, nil
}

func (s *Store) GetWallet(ctx context.Context, id string) (Wallet, error) {
	var w Wallet
	var minor int64
	var currency string
	err := s.pool.QueryRow(ctx, "SELECT id, player_id, balance_minor, currency, version FROM wallets WHERE id=$1", id).
		Scan(&w.ID, &w.Player, &minor, &currency, &w.Version)
	if err != nil {
		return Wallet{}, ErrWalletNotFound
	}
	m, err := domain.NewMoney(fmt.Sprintf("%d.%02d", minor/100, minor%100), currency)
	if err != nil {
		return Wallet{}, err
	}
	w.Balance = m
	return w, nil
}

func (s *Store) ListLedger(ctx context.Context, walletID, cursor string, limit int) ([]Lancamento, string, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	cursorID := ""
	if cursor != "" {
		valor, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return nil, "", errors.New("cursor invalido")
		}
		cursorID = string(valor)
	}
	query := "SELECT id,transaction_id,direction,amount_minor,balance_before_minor,balance_after_minor FROM ledger_entries WHERE wallet_id=$1 AND ($2='' OR id::text>$2) ORDER BY id LIMIT $3"
	rows, err := s.pool.Query(ctx, query, walletID, cursorID, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var result []Lancamento
	for rows.Next() {
		var item Lancamento
		var amount, before, after int64
		var currency string
		if len(result) >= limit {
			break
		}
		if err := rows.Scan(&item.ID, &item.TransactionID, &item.Direcao, &amount, &before, &after); err != nil {
			return nil, "", err
		}
		err = s.pool.QueryRow(ctx, "SELECT currency FROM wallets WHERE id=$1", walletID).Scan(&currency)
		if err != nil {
			return nil, "", err
		}
		item.Dinheiro, err = domain.NewMoney(fmt.Sprintf("%d.%02d", amount/100, amount%100), currency)
		if err != nil {
			return nil, "", err
		}
		item.SaldoAnterior, err = domain.NewMoney(fmt.Sprintf("%d.%02d", before/100, before%100), currency)
		if err != nil {
			return nil, "", err
		}
		item.SaldoPosterior, err = domain.NewMoney(fmt.Sprintf("%d.%02d", after/100, after%100), currency)
		if err != nil {
			return nil, "", err
		}
		result = append(result, item)
	}
	next := ""
	if len(result) == limit {
		next = base64.RawURLEncoding.EncodeToString([]byte(result[len(result)-1].ID))
	}
	return result, next, rows.Err()
}

func (s *Store) ConciliarCarteira(ctx context.Context, walletID string) (ResultadoConciliacao, error) {
	var moeda string
	var armazenado int64
	if err := s.pool.QueryRow(ctx, "SELECT currency,balance_minor FROM wallets WHERE id=$1", walletID).Scan(&moeda, &armazenado); err != nil {
		return ResultadoConciliacao{}, ErrWalletNotFound
	}
	var calculado int64
	var quantidade int
	if err := s.pool.QueryRow(ctx, "SELECT COALESCE(SUM(CASE WHEN direction='CREDIT' THEN amount_minor ELSE -amount_minor END),0),COUNT(*) FROM ledger_entries WHERE wallet_id=$1", walletID).Scan(&calculado, &quantidade); err != nil {
		return ResultadoConciliacao{}, err
	}
	saldo, err := domain.NewMoney(fmt.Sprintf("%d.%02d", armazenado/100, armazenado%100), moeda)
	if err != nil {
		return ResultadoConciliacao{}, err
	}
	reconstruido := domain.NewInternalMoney(calculado, moeda)
	diferenca := domain.NewInternalMoney(armazenado-calculado, moeda)
	return ResultadoConciliacao{CarteiraID: walletID, SaldoArmazenado: saldo, SaldoCalculado: reconstruido, Diferenca: diferenca, Consistente: diferenca.IsZero(), LancamentosVerificados: quantidade}, nil
}

func (s *Store) ProcessarAposta(ctx context.Context, input application.EntradaAposta, idempotencyKey string) (ResultadoAposta, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ResultadoAposta{}, err
	}
	defer tx.Rollback(ctx)
	resultado, err := s.processarApostaNaTransacao(ctx, tx, input, idempotencyKey)
	if err != nil {
		return ResultadoAposta{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ResultadoAposta{}, err
	}
	return resultado, nil
}

func (s *Store) processarApostaNaTransacao(ctx context.Context, tx pgx.Tx, input application.EntradaAposta, idempotencyKey string) (ResultadoAposta, error) {
	if err := application.ValidarAposta(input); err != nil {
		return ResultadoAposta{}, err
	}
	hash := application.HashPayload(input)
	var err error
	// Serializa a mesma chave antes de consultar e inserir, evitando corrida
	// entre duas primeiras tentativas que ainda não possuem uma linha existente.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", idempotencyKey); err != nil {
		return ResultadoAposta{}, err
	}

	var existing ResultadoAposta
	var minor int64
	var currency string
	var storedHash string
	err = tx.QueryRow(ctx, "SELECT id,status,result_balance_minor,currency,payload_hash,COALESCE(failure_code,'') FROM wagering_transactions WHERE idempotency_key=$1 FOR UPDATE", idempotencyKey).
		Scan(&existing.ID, &existing.Status, &minor, &currency, &storedHash, &existing.FailureCode)
	if err == nil {
		if storedHash != hash {
			return ResultadoAposta{}, application.ErroConflitoIdempotencia
		}
		existing.Replay = true
		existing.Balance, _ = domain.NewMoney(fmt.Sprintf("%d.%02d", minor/100, minor%100), currency)
		return existing, nil
	}

	var walletCurrency string
	var balance, version int64
	err = tx.QueryRow(ctx, "SELECT currency,balance_minor,version FROM wallets WHERE id=$1 FOR UPDATE", input.WalletID).
		Scan(&walletCurrency, &balance, &version)
	if err != nil {
		return ResultadoAposta{}, ErrWalletNotFound
	}
	if walletCurrency != input.Money.Currency() {
		return ResultadoAposta{}, domain.ErrCurrencyMismatch
	}
	amount := input.Money.Minor()
	next := balance
	direction := ""
	if input.Kind == "REFUND" || input.Kind == "ROLLBACK" {
		var refKind, refWallet, refCurrency, refStatus, refPlayer, refRound, refGame string
		var refAmount int64
		err = tx.QueryRow(ctx, "SELECT kind,wallet_id,currency,amount_minor,status,player_id,round_id,game_id FROM wagering_transactions WHERE provider_id=$1 AND external_transaction_id=$2 FOR UPDATE", input.ProviderID, input.ReferenceExternalID).Scan(&refKind, &refWallet, &refCurrency, &refAmount, &refStatus, &refPlayer, &refRound, &refGame)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				var pendenteID string
				err = tx.QueryRow(ctx, "INSERT INTO wagering_transactions(provider_id,external_transaction_id,idempotency_key,payload_hash,wallet_id,player_id,round_id,game_id,kind,amount_minor,currency,reference_external_id,status,result_balance_minor,result_wallet_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'PENDING_REFERENCE',$13,$14) RETURNING id", input.ProviderID, input.ExternalID, idempotencyKey, hash, input.WalletID, input.PlayerID, input.RoundID, input.GameID, input.Kind, amount, input.Money.Currency(), input.ReferenceExternalID, balance, version).Scan(&pendenteID)
				if err != nil {
					return ResultadoAposta{}, err
				}
				if err = inserirEvento(ctx, tx, eventos.NovoEnvelope("WagerTransactionPendingReference", pendenteID, pendenteID, map[string]any{"transactionId": pendenteID, "referenceExternalTransactionId": input.ReferenceExternalID})); err != nil {
					return ResultadoAposta{}, err
				}
				return ResultadoAposta{ID: pendenteID, Status: "PENDING_REFERENCE", Balance: domain.NewInternalMoney(balance, walletCurrency)}, nil
			}
			return ResultadoAposta{}, errors.New("referencia nao encontrada")
		}
		if refStatus != "PROCESSED" || refWallet != input.WalletID || refPlayer != input.PlayerID || refRound != input.RoundID || refGame != input.GameID || refCurrency != input.Money.Currency() || refAmount != amount {
			return ResultadoAposta{}, errors.New("referencia invalida")
		}
		var duplicadas int
		_ = tx.QueryRow(ctx, "SELECT COUNT(*) FROM wagering_transactions WHERE provider_id=$1 AND reference_external_id=$2 AND kind=$3 AND status='PROCESSED'", input.ProviderID, input.ReferenceExternalID, input.Kind).Scan(&duplicadas)
		if duplicadas > 0 {
			return ResultadoAposta{}, errors.New("reversao duplicada")
		}
		if input.Kind == "REFUND" && refKind != "BET" {
			return ResultadoAposta{}, errors.New("REFUND exige referencia BET")
		}
		if input.Kind == "REFUND" || refKind == "BET" {
			next = balance + amount
			direction = "CREDIT"
		} else {
			if amount > balance {
				return ResultadoAposta{}, errors.New("saldo insuficiente para reversao")
			}
			next = balance - amount
			direction = "DEBIT"
		}
	}
	if input.Kind == "BET" {
		if amount > balance {
			return ResultadoAposta{}, errors.New("saldo insuficiente")
		}
		next = balance - amount
		direction = "DEBIT"
	}
	if input.Kind == "WIN" {
		next = balance + amount
		direction = "CREDIT"
	}
	nextVersion := version
	if direction != "" {
		nextVersion++
	}
	var transactionID string
	status := "PROCESSED"
	err = tx.QueryRow(ctx, "INSERT INTO wagering_transactions(provider_id,external_transaction_id,idempotency_key,payload_hash,wallet_id,player_id,round_id,game_id,kind,amount_minor,currency,reference_external_id,status,result_balance_minor,result_wallet_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING id", input.ProviderID, input.ExternalID, idempotencyKey, hash, input.WalletID, input.PlayerID, input.RoundID, input.GameID, input.Kind, amount, input.Money.Currency(), input.ReferenceExternalID, status, next, nextVersion).Scan(&transactionID)
	if err != nil {
		return ResultadoAposta{}, err
	}
	if direction != "" {
		if _, err = tx.Exec(ctx, "UPDATE wallets SET balance_minor=$1,version=$2,updated_at=now() WHERE id=$3", next, nextVersion, input.WalletID); err != nil {
			return ResultadoAposta{}, err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO ledger_entries(wallet_id,transaction_id,direction,amount_minor,balance_before_minor,balance_after_minor) VALUES($1,$2,$3,$4,$5,$6)", input.WalletID, transactionID, direction, amount, balance, next); err != nil {
			return ResultadoAposta{}, err
		}
		if err = inserirEvento(ctx, tx, eventos.NovoEnvelope("WalletBalanceChanged", input.WalletID, transactionID, map[string]any{"walletId": input.WalletID, "transactionId": transactionID, "direction": direction, "money": map[string]string{"amount": input.Money.String(), "currency": input.Money.Currency()}, "balanceBefore": map[string]string{"amount": fmt.Sprintf("%d.%02d", balance/100, balance%100), "currency": walletCurrency}, "balanceAfter": map[string]string{"amount": fmt.Sprintf("%d.%02d", next/100, next%100), "currency": walletCurrency}, "walletVersion": nextVersion})); err != nil {
			return ResultadoAposta{}, err
		}
	}
	if err = inserirEvento(ctx, tx, eventos.NovoEnvelope("WagerTransactionProcessed", transactionID, transactionID, map[string]any{"transactionId": transactionID, "status": status, "kind": input.Kind})); err != nil {
		return ResultadoAposta{}, err
	}
	resultMoney, _ := domain.NewMoney(fmt.Sprintf("%d.%02d", next/100, next%100), walletCurrency)
	return ResultadoAposta{ID: transactionID, Status: status, Balance: resultMoney}, nil
}

// Resolver tenta concluir uma reversão que chegou antes da operação original.
func (s *Store) Resolver(ctx context.Context, transactionID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var provider, reference, kind, walletID, currency, status, player, round, game string
	var amount int64
	if err = tx.QueryRow(ctx, "SELECT provider_id,reference_external_id,kind,wallet_id,currency,amount_minor,status,player_id,round_id,game_id FROM wagering_transactions WHERE id=$1 FOR UPDATE", transactionID).
		Scan(&provider, &reference, &kind, &walletID, &currency, &amount, &status, &player, &round, &game); err != nil {
		return err
	}
	if status != "PENDING_REFERENCE" {
		return nil
	}
	var originalKind, originalWallet, originalCurrency, originalStatus, originalPlayer, originalRound, originalGame string
	var originalAmount int64
	if err = tx.QueryRow(ctx, "SELECT kind,wallet_id,currency,amount_minor,status,player_id,round_id,game_id FROM wagering_transactions WHERE provider_id=$1 AND external_transaction_id=$2 FOR UPDATE", provider, reference).
		Scan(&originalKind, &originalWallet, &originalCurrency, &originalAmount, &originalStatus, &originalPlayer, &originalRound, &originalGame); err != nil {
		return err
	}
	if originalStatus != "PROCESSED" || originalWallet != walletID || originalPlayer != player || originalRound != round || originalGame != game || originalCurrency != currency || originalAmount != amount || (kind == "REFUND" && originalKind != "BET") {
		return errors.New("referencia invalida")
	}
	var balance, version int64
	if err = tx.QueryRow(ctx, "SELECT balance_minor,version FROM wallets WHERE id=$1 FOR UPDATE", walletID).Scan(&balance, &version); err != nil {
		return err
	}
	next := balance + amount
	direction := "CREDIT"
	if kind == "ROLLBACK" {
		if amount > balance {
			return errors.New("saldo insuficiente para reversao")
		}
		next = balance - amount
		direction = "DEBIT"
	}
	nextVersion := version + 1
	if _, err = tx.Exec(ctx, "UPDATE wagering_transactions SET status='PROCESSED',result_balance_minor=$1,result_wallet_version=$2,updated_at=now() WHERE id=$3", next, nextVersion, transactionID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE wallets SET balance_minor=$1,version=$2,updated_at=now() WHERE id=$3", next, nextVersion, walletID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO ledger_entries(wallet_id,transaction_id,direction,amount_minor,balance_before_minor,balance_after_minor) VALUES($1,$2,$3,$4,$5,$6)", walletID, transactionID, direction, amount, balance, next); err != nil {
		return err
	}
	if err = inserirEvento(ctx, tx, eventos.NovoEnvelope("WalletBalanceChanged", walletID, transactionID, map[string]any{"walletId": walletID, "transactionId": transactionID, "direction": direction, "money": map[string]string{"amount": fmt.Sprintf("%d.%02d", amount/100, amount%100), "currency": currency}, "balanceBefore": map[string]string{"amount": fmt.Sprintf("%d.%02d", balance/100, balance%100), "currency": currency}, "balanceAfter": map[string]string{"amount": fmt.Sprintf("%d.%02d", next/100, next%100), "currency": currency}, "walletVersion": nextVersion})); err != nil {
		return err
	}
	if err = inserirEvento(ctx, tx, eventos.NovoEnvelope("WagerTransactionProcessed", transactionID, transactionID, map[string]any{"transactionId": transactionID, "status": "PROCESSED", "kind": kind})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Tratar(ctx context.Context, _ string, corpo string) error {
	entrada, chave, err := entradaDoCorpo(corpo)
	if err != nil {
		return err
	}
	_, err = s.ProcessarAposta(ctx, entrada, chave)
	return err
}

// TratarComInbox executa inbox, processamento financeiro e conclusão no mesmo commit.
func (s *Store) TratarComInbox(ctx context.Context, consumidor, mensagemID, corpo string) error {
	entrada, chave, err := entradaDoCorpo(corpo)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	hash := sha256.Sum256([]byte(corpo))
	valor := hex.EncodeToString(hash[:])
	var novo bool
	err = tx.QueryRow(ctx, "INSERT INTO inbox_messages(consumer_name,message_id,payload_hash) VALUES($1,$2,$3) ON CONFLICT DO NOTHING RETURNING true", consumidor, mensagemID, valor).Scan(&novo)
	if err != nil {
		var concluida *time.Time
		var existente string
		if err = tx.QueryRow(ctx, "SELECT payload_hash,completed_at FROM inbox_messages WHERE consumer_name=$1 AND message_id=$2 FOR UPDATE", consumidor, mensagemID).Scan(&existente, &concluida); err != nil {
			return err
		}
		if existente != valor {
			return errors.New("mensagem SQS duplicada com payload divergente")
		}
		if concluida != nil {
			return tx.Commit(ctx)
		}
	}
	if _, err = s.processarApostaNaTransacao(ctx, tx, entrada, chave); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE inbox_messages SET completed_at=now() WHERE consumer_name=$1 AND message_id=$2", consumidor, mensagemID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func entradaDoCorpo(corpo string) (application.EntradaAposta, string, error) {
	var envelope map[string]any
	if err := json.Unmarshal([]byte(corpo), &envelope); err != nil {
		return application.EntradaAposta{}, "", err
	}
	dados, ok := envelope["data"].(map[string]any)
	if !ok {
		dados = envelope
	}
	dinheiro, _ := dados["money"].(map[string]any)
	amount, _ := dinheiro["amount"].(string)
	currency, _ := dinheiro["currency"].(string)
	money, err := domain.NewMoney(amount, currency)
	if err != nil {
		return application.EntradaAposta{}, "", err
	}
	chave, _ := dados["idempotencyKey"].(string)
	entrada := application.EntradaAposta{}
	entrada.ProviderID, _ = dados["providerId"].(string)
	entrada.ExternalID, _ = dados["externalTransactionId"].(string)
	entrada.PlayerID, _ = dados["playerId"].(string)
	entrada.WalletID, _ = dados["walletId"].(string)
	entrada.RoundID, _ = dados["roundId"].(string)
	entrada.GameID, _ = dados["gameId"].(string)
	entrada.Kind, _ = dados["kind"].(string)
	entrada.Money = money
	if chave == "" {
		return application.EntradaAposta{}, "", errors.New("idempotency key ausente")
	}
	return entrada, chave, nil
}

func inserirEvento(ctx context.Context, tx pgx.Tx, envelope eventos.Envelope) error {
	payload, err := envelope.JSON()
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO outbox_events(event_id,aggregate_id,event_type,payload,occurred_at) VALUES($1,$2,$3,$4,$5)", envelope.IdEvento, envelope.IdAgregado, envelope.Tipo, payload, envelope.OcorridoEm)
	return err
}
