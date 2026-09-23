package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wfcosta/backend-challenge-go/internal/application"
	"github.com/wfcosta/backend-challenge-go/internal/domain"
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

func (s *Store) CreateWallet(ctx context.Context, playerID string, money domain.Money) (Wallet, error) {
	var w Wallet
	var minor int64
	err := s.pool.QueryRow(ctx, "INSERT INTO wallets(player_id,currency,balance_minor,version) VALUES($1,$2,$3,1) RETURNING id, player_id, balance_minor, version",
		playerID, money.Currency(), money.Minor()).Scan(&w.ID, &w.Player, &minor, &w.Version)
	if err != nil {
		return Wallet{}, err
	}
	normalized, err := domain.NewMoney(fmt.Sprintf("%d.%02d", minor/100, minor%100), money.Currency())
	if err != nil {
		return Wallet{}, err
	}
	w.Balance = normalized
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

func (s *Store) ListLedger(ctx context.Context, walletID string, limit int) ([]Lancamento, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, "SELECT id,transaction_id,direction,amount_minor,balance_before_minor,balance_after_minor FROM ledger_entries WHERE wallet_id=$1 ORDER BY created_at,id LIMIT $2", walletID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Lancamento
	for rows.Next() {
		var item Lancamento
		var amount, before, after int64
		var currency string
		if err := rows.Scan(&item.ID, &item.TransactionID, &item.Direcao, &amount, &before, &after); err != nil {
			return nil, err
		}
		err = s.pool.QueryRow(ctx, "SELECT currency FROM wallets WHERE id=$1", walletID).Scan(&currency)
		if err != nil {
			return nil, err
		}
		item.Dinheiro, err = domain.NewMoney(fmt.Sprintf("%d.%02d", amount/100, amount%100), currency)
		if err != nil {
			return nil, err
		}
		item.SaldoAnterior, err = domain.NewMoney(fmt.Sprintf("%d.%02d", before/100, before%100), currency)
		if err != nil {
			return nil, err
		}
		item.SaldoPosterior, err = domain.NewMoney(fmt.Sprintf("%d.%02d", after/100, after%100), currency)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
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
	reconstruido, err := domain.NewMoney(fmt.Sprintf("%d.%02d", calculado/100, calculado%100), moeda)
	if err != nil {
		return ResultadoConciliacao{}, err
	}
	diferenca := domain.NewInternalMoney(armazenado-calculado, moeda)
	return ResultadoConciliacao{CarteiraID: walletID, SaldoArmazenado: saldo, SaldoCalculado: reconstruido, Diferenca: diferenca, Consistente: diferenca.IsZero(), LancamentosVerificados: quantidade}, nil
}

func (s *Store) ProcessarAposta(ctx context.Context, input application.EntradaAposta, idempotencyKey string) (ResultadoAposta, error) {
	if err := application.ValidarAposta(input); err != nil {
		return ResultadoAposta{}, err
	}
	hash := application.HashPayload(input)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ResultadoAposta{}, err
	}
	defer tx.Rollback(ctx)

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
	err = tx.QueryRow(ctx, "INSERT INTO wagering_transactions(provider_id,external_transaction_id,idempotency_key,payload_hash,wallet_id,player_id,round_id,game_id,kind,amount_minor,currency,status,result_balance_minor,result_wallet_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id", input.ProviderID, input.ExternalID, idempotencyKey, hash, input.WalletID, input.PlayerID, input.RoundID, input.GameID, input.Kind, amount, input.Money.Currency(), status, next, nextVersion).Scan(&transactionID)
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
	}
	if err = tx.Commit(ctx); err != nil {
		return ResultadoAposta{}, err
	}
	resultMoney, _ := domain.NewMoney(fmt.Sprintf("%d.%02d", next/100, next%100), walletCurrency)
	return ResultadoAposta{ID: transactionID, Status: status, Balance: resultMoney}, nil
}
