package observabilidade

import "testing"

func TestMetricasContamRequisicoes(t *testing.T) {
	var m Metricas
	m.Requisicoes.Add(1)
	if m.Requisicoes.Load() != 1 {
		t.Fatal("contador invalido")
	}
}
