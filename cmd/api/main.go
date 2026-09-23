package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wfcosta/backend-challenge-go/internal/application"
	"github.com/wfcosta/backend-challenge-go/internal/domain"
	"github.com/wfcosta/backend-challenge-go/internal/store"
)

func main() {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8081"
	}
	mux := http.NewServeMux()
	databaseURL := os.Getenv("DATABASE_URL")
	var db *store.Store
	if databaseURL != "" {
		var err error
		db, err = store.New(context.Background(), databaseURL)
		if err != nil {
			slog.Error("database unavailable", "error", err)
			os.Exit(1)
		}
		defer db.Close()
		mux.HandleFunc("POST /wallets", func(w http.ResponseWriter, r *http.Request) {
			var input map[string]any
			if json.NewDecoder(r.Body).Decode(&input) != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
				return
			}
			playerID, _ := input["playerId"].(string)
			balance, _ := input["initialBalance"].(map[string]any)
			amount, _ := balance["amount"].(string)
			currency, _ := balance["currency"].(string)
			money, err := domain.NewMoney(amount, currency)
			if playerID == "" || err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
				return
			}
			wallet, err := db.CreateWallet(r.Context(), playerID, money)
			if err != nil {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{"id": wallet.ID, "playerId": wallet.Player, "balance": moneyJSON(wallet.Balance), "version": wallet.Version})
		})
		mux.HandleFunc("GET /wallets/{id}", func(w http.ResponseWriter, r *http.Request) {
			wallet, err := db.GetWallet(r.Context(), r.PathValue("id"))
			if err != nil {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"id": wallet.ID, "playerId": wallet.Player, "balance": moneyJSON(wallet.Balance), "version": wallet.Version})
		})
		mux.HandleFunc("GET /wallets/{id}/ledger", func(w http.ResponseWriter, r *http.Request) {
			limite := 50
			if lancamentos, err := db.ListLedger(r.Context(), r.PathValue("id"), limite); err != nil {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			} else {
				resposta := make([]map[string]any, 0, len(lancamentos))
				for _, item := range lancamentos {
					resposta = append(resposta, map[string]any{
						"id": item.ID, "transactionId": item.TransactionID, "direction": item.Direcao,
						"money": moneyJSON(item.Dinheiro), "balanceBefore": moneyJSON(item.SaldoAnterior),
						"balanceAfter": moneyJSON(item.SaldoPosterior),
					})
				}
				writeJSON(w, http.StatusOK, resposta)
			}
		})
		mux.HandleFunc("POST /wallets/{id}/reconciliation", func(w http.ResponseWriter, r *http.Request) {
			result, err := db.ConciliarCarteira(r.Context(), r.PathValue("id"))
			if err != nil {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"walletId":          result.CarteiraID,
				"storedBalance":     moneyJSON(result.SaldoArmazenado),
				"calculatedBalance": moneyJSON(result.SaldoCalculado),
				"difference":        moneyJSON(result.Diferenca),
				"consistent":        result.Consistente,
				"checkedEntries":    result.LancamentosVerificados,
			})
		})
		mux.HandleFunc("POST /wagering/transactions", func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Idempotency-Key")
			if key == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing Idempotency-Key"})
				return
			}
			var raw map[string]any
			if json.NewDecoder(r.Body).Decode(&raw) != nil {
				writeJSON(w, 400, map[string]string{"error": "invalid request"})
				return
			}
			providerID, _ := raw["providerId"].(string)
			externalID, _ := raw["externalTransactionId"].(string)
			playerID, _ := raw["playerId"].(string)
			walletID, _ := raw["walletId"].(string)
			roundID, _ := raw["roundId"].(string)
			gameID, _ := raw["gameId"].(string)
			kind, _ := raw["kind"].(string)
			dinheiro, _ := raw["money"].(map[string]any)
			amount, _ := dinheiro["amount"].(string)
			currency, _ := dinheiro["currency"].(string)
			money, err := domain.NewMoney(amount, currency)
			if err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			result, err := db.ProcessarAposta(r.Context(), application.EntradaAposta{ProviderID: providerID, ExternalID: externalID, PlayerID: playerID, WalletID: walletID, RoundID: roundID, GameID: gameID, Kind: kind, Money: money}, key)
			if err != nil {
				if errors.Is(err, application.ErroConflitoIdempotencia) {
					writeJSON(w, 409, map[string]string{"error": err.Error()})
					return
				}
				writeJSON(w, 422, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, 200, map[string]any{"transactionId": result.ID, "status": result.Status, "balance": moneyJSON(result.Balance), "idempotentReplay": result.Replay})
		})
	}
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if db != nil {
			if err := db.Ping(r.Context()); err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server stopped", "error", err)
			os.Exit(1)
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func moneyJSON(m domain.Money) map[string]string {
	return map[string]string{"amount": m.String(), "currency": m.Currency()}
}
