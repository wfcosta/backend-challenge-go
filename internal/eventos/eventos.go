package eventos

import (
	"encoding/json"
	"github.com/google/uuid"
	"time"
)

type Envelope struct {
	IdEvento   string    `json:"eventId"`
	Tipo       string    `json:"eventType"`
	IdAgregado string    `json:"aggregateId"`
	Correlacao string    `json:"correlationId"`
	Causacao   string    `json:"causationId,omitempty"`
	OcorridoEm time.Time `json:"occurredAt"`
	Versao     int       `json:"version"`
	Dados      any       `json:"data"`
}

func NovoEnvelope(tipo, agregado, correlacao string, dados any) Envelope {
	return Envelope{IdEvento: uuid.NewString(), Tipo: tipo, IdAgregado: agregado, Correlacao: correlacao, OcorridoEm: time.Now().UTC(), Versao: 1, Dados: dados}
}

func (e Envelope) JSON() ([]byte, error) { return json.Marshal(e) }
