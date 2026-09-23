package observabilidade

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

type Metricas struct {
	Requisicoes atomic.Uint64
	Erros       atomic.Uint64
	Duplicatas  atomic.Uint64
}

func (m *Metricas) Middleware(proximo http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { m.Requisicoes.Add(1); proximo.ServeHTTP(w, r) })
}
func (m *Metricas) Handler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = fmt.Fprintf(w, "apostas_requisicoes_total %d\napostas_erros_total %d\napostas_duplicatas_total %d\n", m.Requisicoes.Load(), m.Erros.Load(), m.Duplicatas.Load())
}
