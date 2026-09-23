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
	var expiradas []string
	rowsExpiradas, err := t.Banco.Query(ctx, "UPDATE wagering_transactions SET status='REJECTED',failure_code='REFERENCE_EXPIRED',updated_at=now() WHERE status='PENDING_REFERENCE' AND reference_expires_at <= now() RETURNING id")
	if err == nil {
		for rowsExpiradas.Next() {
			var id string
			if rowsExpiradas.Scan(&id) == nil {
				expiradas = append(expiradas, id)
			}
		}
		rowsExpiradas.Close()
	}
	for _, id := range expiradas {
		t.publicarRejeicao(ctx, id, "REFERENCE_EXPIRED")
	}
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
				t.publicarRejeicao(ctx, id, "REFERENCE_RETRY_EXHAUSTED")
				continue
			}
			segundos := 1 << min(tentativas, 8)
			_, _ = t.Banco.Exec(ctx, "UPDATE wagering_transactions SET reference_attempts=$1,reference_locked_until=NULL,reference_next_attempt_at=now()+($2 * INTERVAL '1 second'),updated_at=now() WHERE id=$3 AND status='PENDING_REFERENCE'", tentativas, segundos, id)
			continue
		}
	}
}

func (t TrabalhadorReferencias) publicarRejeicao(ctx context.Context, id, codigo string) {
	_, _ = t.Banco.Exec(ctx, `
			INSERT INTO outbox_events(event_id, aggregate_id, event_type, payload)
			SELECT gen_random_uuid(), id, 'WagerTransactionRejected',
			       jsonb_build_object('transactionId', id, 'status', 'REJECTED', 'failureCode', $2)
			FROM wagering_transactions
			WHERE id=$1
			  AND NOT EXISTS (SELECT 1 FROM outbox_events WHERE outbox_events.aggregate_id=wagering_transactions.id AND event_type='WagerTransactionRejected')`, id, codigo)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
