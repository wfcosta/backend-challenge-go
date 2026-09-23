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
	_, _ = t.Banco.Exec(ctx, "UPDATE wagering_transactions SET status='REJECTED',failure_code='REFERENCE_EXPIRED',updated_at=now() WHERE status='PENDING_REFERENCE' AND reference_expires_at <= now()")
	rows, err := t.Banco.Query(ctx, "SELECT id,reference_attempts FROM wagering_transactions WHERE status='PENDING_REFERENCE' AND reference_next_attempt_at <= now() AND (reference_locked_until IS NULL OR reference_locked_until < now()) ORDER BY reference_next_attempt_at LIMIT 50")
	if err != nil {
		slog.Error("falha ao buscar referencias pendentes", "erro", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var tentativas int
		if err := rows.Scan(&id, &tentativas); err != nil {
			continue
		}
		reservado, err := t.Banco.Exec(ctx, "UPDATE wagering_transactions SET reference_locked_until=now()+INTERVAL '30 seconds' WHERE id=$1 AND status='PENDING_REFERENCE' AND (reference_locked_until IS NULL OR reference_locked_until < now())", id)
		if err != nil || reservado.RowsAffected() != 1 {
			continue
		}
		if err := t.Resolutor.Resolver(ctx, id); err != nil {
			slog.Warn("referencia ainda nao resolvida", "transactionId", id, "erro", err)
			tentativas++
			if tentativas >= 5 {
				_, _ = t.Banco.Exec(ctx, "UPDATE wagering_transactions SET status='REJECTED',failure_code='REFERENCE_RETRY_EXHAUSTED',reference_locked_until=NULL,updated_at=now() WHERE id=$1 AND status='PENDING_REFERENCE'", id)
				continue
			}
			segundos := 1 << min(tentativas, 8)
			_, _ = t.Banco.Exec(ctx, "UPDATE wagering_transactions SET reference_attempts=$1,reference_locked_until=NULL,reference_next_attempt_at=now()+($2 * INTERVAL '1 second'),updated_at=now() WHERE id=$3 AND status='PENDING_REFERENCE'", tentativas, segundos, id)
			continue
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
