package worker

import "testing"

func TestIntervaloPadraoDoPublicador(t *testing.T) {
	p := PublicadorOutbox{}
	if p.Intervalo != 0 {
		t.Fatal("intervalo inicial deve permitir configuracao padrao")
	}
}
