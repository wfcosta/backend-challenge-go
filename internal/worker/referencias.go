package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ResolutorReferencias interface {
	Resolver(context.Context, string) error
}

type TrabalhadorReferencias struct {
	Banco     *pgxpool.Pool
	Resolutor ResolutorReferencias
	Intervalo time.Duration
}

func (t TrabalhadorReferencias) Executar(ctx context.Context) {
	intervalo := t.Intervalo
	if intervalo <= 0 {
		intervalo = 2 * time.Second
	}
	ticker := time.NewTicker(intervalo)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.tentar(ctx)
		}
	}
}

func (t TrabalhadorReferencias) tentar(ctx context.Context) {
	rows, err := t.Banco.Query(ctx, "SELECT id FROM wagering_transactions WHERE status='PENDING_REFERENCE' AND updated_at < now()-INTERVAL '1 second' ORDER BY updated_at LIMIT 50")
	if err != nil {
		slog.Error("falha ao buscar referencias pendentes", "erro", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		if err := t.Resolutor.Resolver(ctx, id); err != nil {
			slog.Warn("referencia ainda nao resolvida", "transactionId", id, "erro", err)
			continue
		}
	}
}
