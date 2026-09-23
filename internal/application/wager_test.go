package application

import (
	"github.com/wfcosta/backend-challenge-go/internal/domain"
	"testing"
)

func TestPayloadHashIsDeterministic(t *testing.T) {
	m, _ := domain.NewMoney("25.00", "BRL")
	a := EntradaAposta{ProviderID: "p", ExternalID: "e", WalletID: "w", Kind: "BET", Money: m}
	if HashPayload(a) != HashPayload(a) {
		t.Fatal("hash is not deterministic")
	}
	if err := ValidarAposta(a); err != nil {
		t.Fatal(err)
	}
}

func TestLossRequiresZero(t *testing.T) {
	m, _ := domain.NewMoney("1.00", "BRL")
	if err := ValidarAposta(EntradaAposta{ProviderID: "p", ExternalID: "e", WalletID: "w", Kind: "LOSS", Money: m}); err == nil {
		t.Fatal("expected error")
	}
}
