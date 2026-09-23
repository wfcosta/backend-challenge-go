package domain

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

var (
	ErrInvalidMoney     = errors.New("invalid money")
	ErrCurrencyMismatch = errors.New("currency mismatch")
	ErrMoneyOverflow    = errors.New("money overflow")
)

type Money struct {
	minor    int64
	currency string
}

func NewMoney(amount, currency string) (Money, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if len(currency) != 3 {
		return Money{}, fmt.Errorf("%w: invalid currency", ErrInvalidMoney)
	}
	if amount == "" || strings.ContainsAny(amount, "eE") || strings.ContainsAny(amount, "NnIi") {
		return Money{}, fmt.Errorf("%w: invalid amount", ErrInvalidMoney)
	}
	if strings.HasPrefix(amount, "-") {
		return Money{}, fmt.Errorf("%w: negative external amount", ErrInvalidMoney)
	}
	if strings.HasPrefix(amount, "+") {
		amount = amount[1:]
	}
	parts := strings.Split(amount, ".")
	if len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && len(parts[1]) != 2) {
		return Money{}, fmt.Errorf("%w: amount must have two decimal places", ErrInvalidMoney)
	}
	if len(parts) == 1 {
		parts = append(parts, "00")
	}
	if _, err := strconv.ParseUint(parts[0], 10, 63); err != nil {
		return Money{}, fmt.Errorf("%w: %v", ErrMoneyOverflow, err)
	}
	units, err := strconv.ParseInt(parts[0]+parts[1], 10, 64)
	if err != nil || units < 0 {
		return Money{}, fmt.Errorf("%w: amount overflow", ErrMoneyOverflow)
	}
	return Money{minor: units, currency: currency}, nil
}

func ZeroMoney(currency string) (Money, error) { return NewMoney("0.00", currency) }
func NewInternalMoney(minor int64, currency string) Money {
	return Money{minor: minor, currency: strings.ToUpper(currency)}
}
func (m Money) Currency() string { return m.currency }
func (m Money) Minor() int64     { return m.minor }
func (m Money) IsZero() bool     { return m.minor == 0 }
func (m Money) String() string {
	return fmt.Sprintf("%d.%02d", m.minor/100, m.minor%100)
}
func (m Money) Add(other Money) (Money, error) {
	if err := m.compatible(other); err != nil {
		return Money{}, err
	}
	if other.minor > math.MaxInt64-m.minor {
		return Money{}, ErrMoneyOverflow
	}
	return Money{minor: m.minor + other.minor, currency: m.currency}, nil
}
func (m Money) Sub(other Money) (Money, error) {
	if err := m.compatible(other); err != nil {
		return Money{}, err
	}
	if other.minor > m.minor {
		return Money{}, ErrMoneyOverflow
	}
	return Money{minor: m.minor - other.minor, currency: m.currency}, nil
}
func (m Money) Negate() Money { return Money{minor: -m.minor, currency: m.currency} }
func (m Money) Compare(other Money) (int, error) {
	if err := m.compatible(other); err != nil {
		return 0, err
	}
	if m.minor < other.minor {
		return -1, nil
	}
	if m.minor > other.minor {
		return 1, nil
	}
	return 0, nil
}
func (m Money) compatible(other Money) error {
	if m.currency == "" || m.currency != other.currency {
		return ErrCurrencyMismatch
	}
	return nil
}
