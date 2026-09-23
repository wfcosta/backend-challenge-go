package application

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/wfcosta/backend-challenge-go/internal/domain"
)

var ErroConflitoIdempotencia = errors.New("conflito de payload da idempotencia")

type EntradaAposta struct {
	ProviderID          string
	ExternalID          string
	PlayerID            string
	WalletID            string
	RoundID             string
	GameID              string
	Kind                string
	Money               domain.Money
	ReferenceExternalID string
}

func HashPayload(input EntradaAposta) string {
	b, _ := json.Marshal([]string{input.ProviderID, input.ExternalID, input.PlayerID, input.WalletID, input.RoundID, input.GameID, input.Kind, input.Money.String(), input.Money.Currency(), input.ReferenceExternalID})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func ValidarAposta(input EntradaAposta) error {
	switch input.Kind {
	case "BET", "WIN", "REFUND", "ROLLBACK":
		if input.Money.IsZero() {
			return fmt.Errorf("%w: a operacao exige valor positivo", domain.ErrInvalidMoney)
		}
	case "LOSS":
		if !input.Money.IsZero() {
			return fmt.Errorf("%w: LOSS exige valor zero", domain.ErrInvalidMoney)
		}
	default:
		return fmt.Errorf("tipo de aposta nao suportado: %q", input.Kind)
	}
	if input.ProviderID == "" || input.ExternalID == "" || input.WalletID == "" {
		return errors.New("identidade da aposta ausente")
	}
	if (input.Kind == "REFUND" || input.Kind == "ROLLBACK") && input.ReferenceExternalID == "" {
		return errors.New("referencia obrigatoria para reversao")
	}
	return nil
}
