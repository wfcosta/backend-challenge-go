package application

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/wfcosta/backend-challenge-go/internal/domain"
)

var ErrIdempotencyConflict = errors.New("idempotency payload conflict")

type WagerInput struct {
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

func PayloadHash(input WagerInput) string {
	b, _ := json.Marshal([]string{input.ProviderID, input.ExternalID, input.PlayerID, input.WalletID, input.RoundID, input.GameID, input.Kind, input.Money.String(), input.Money.Currency(), input.ReferenceExternalID})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func ValidateWager(input WagerInput) error {
	switch input.Kind {
	case "BET", "WIN":
		if input.Money.IsZero() {
			return fmt.Errorf("%w: operation requires positive amount", domain.ErrInvalidMoney)
		}
	case "LOSS":
		if !input.Money.IsZero() {
			return fmt.Errorf("%w: loss requires zero amount", domain.ErrInvalidMoney)
		}
	default:
		return fmt.Errorf("unsupported wager kind %q", input.Kind)
	}
	if input.ProviderID == "" || input.ExternalID == "" || input.WalletID == "" {
		return errors.New("missing wager identity")
	}
	return nil
}
