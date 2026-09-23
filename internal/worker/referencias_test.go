package worker

import "testing"

func TestIntervaloReferencias(t *testing.T) {
	trabalhador := TrabalhadorReferencias{}
	if trabalhador.Intervalo != 0 {
		t.Fatal("intervalo inicial inesperado")
	}
}
