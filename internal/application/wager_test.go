package application

import (
	"github.com/wfcosta/backend-challenge-go/internal/domain"
	"testing"
)

func TestPayloadHashIsDeterministic(t *testing.T) {
	m, _ := domain.NewMoney("25.00", "BRL")
	a := WagerInput{ProviderID: "p", ExternalID: "e", WalletID: "w", Kind: "BET", Money: m}
	if PayloadHash(a) != PayloadHash(a) {
		t.Fatal("hash is not deterministic")
	}
	if err := ValidateWager(a); err != nil {
		t.Fatal(err)
	}
}

func TestLossRequiresZero(t *testing.T) {
	m, _ := domain.NewMoney("1.00", "BRL")
	if err := ValidateWager(WagerInput{ProviderID: "p", ExternalID: "e", WalletID: "w", Kind: "LOSS", Money: m}); err == nil {
		t.Fatal("expected error")
	}
}
