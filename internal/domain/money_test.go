package domain

import (
	"errors"
	"testing"
)

func TestMoneyParsingAndArithmetic(t *testing.T) {
	m, err := NewMoney("25.00", "brl")
	if err != nil || m.String() != "25.00" || m.Currency() != "BRL" {
		t.Fatalf("money=%v err=%v", m, err)
	}
	other, _ := NewMoney("5.50", "BRL")
	sum, err := m.Add(other)
	if err != nil || sum.String() != "30.50" {
		t.Fatalf("sum=%v err=%v", sum, err)
	}
	diff, err := m.Sub(other)
	if err != nil || diff.String() != "19.50" {
		t.Fatalf("diff=%v err=%v", diff, err)
	}
}

func TestMoneyRejectsInvalidExternalValues(t *testing.T) {
	for _, value := range []string{"", "-1.00", "1.0", "1.000", "1e2", "NaN", "Infinity"} {
		if _, err := NewMoney(value, "BRL"); !errors.Is(err, ErrInvalidMoney) && !errors.Is(err, ErrMoneyOverflow) {
			t.Errorf("value %q accepted: %v", value, err)
		}
	}
}

func TestMoneyRejectsDifferentCurrencies(t *testing.T) {
	brl, _ := NewMoney("1.00", "BRL")
	usd, _ := NewMoney("1.00", "USD")
	if _, err := brl.Add(usd); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("err=%v", err)
	}
}
