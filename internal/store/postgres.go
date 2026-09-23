package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
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
