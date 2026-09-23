package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type EventoPendente struct {
	ID      string
	Tipo    string
	Payload json.RawMessage
}

type TransporteEventos interface {
	Publicar(context.Context, EventoPendente) error
}

type PublicadorOutbox struct {
	Banco      *pgxpool.Pool
	Transporte TransporteEventos
	Intervalo  time.Duration
}

func (p *PublicadorOutbox) Executar(ctx context.Context) {
	intervalo := p.Intervalo
	if intervalo <= 0 {
		intervalo = time.Second
	}
	ticker := time.NewTicker(intervalo)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.publicarLote(ctx)
		}
	}
}

func (p *PublicadorOutbox) publicarLote(ctx context.Context) {
	rows, err := p.Banco.Query(ctx, "SELECT event_id,event_type,payload FROM outbox_events WHERE published_at IS NULL AND next_attempt_at <= now() AND (locked_until IS NULL OR locked_until < now()) ORDER BY occurred_at,event_id LIMIT 50")
	if err != nil {
		slog.Error("falha ao buscar outbox", "erro", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var evento EventoPendente
		if err := rows.Scan(&evento.ID, &evento.Tipo, &evento.Payload); err != nil {
			slog.Error("falha ao ler outbox", "erro", err)
			continue
		}
		reservado, err := p.Banco.Exec(ctx, "UPDATE outbox_events SET locked_until=now()+INTERVAL '30 seconds' WHERE event_id=$1 AND published_at IS NULL AND (locked_until IS NULL OR locked_until < now())", evento.ID)
		if err != nil || reservado.RowsAffected() != 1 {
			continue
		}
		if err := p.Transporte.Publicar(ctx, evento); err != nil {
			slog.Error("falha ao publicar evento", "eventId", evento.ID, "erro", err)
			_, _ = p.Banco.Exec(ctx, "UPDATE outbox_events SET attempts=attempts+1,locked_until=NULL,next_attempt_at=now()+LEAST((2^LEAST(attempts,10))*INTERVAL '1 second',INTERVAL '15 minutes') WHERE event_id=$1", evento.ID)
			continue
		}
		_, _ = p.Banco.Exec(ctx, "UPDATE outbox_events SET published_at=now(),locked_until=NULL WHERE event_id=$1 AND published_at IS NULL", evento.ID)
	}
}
