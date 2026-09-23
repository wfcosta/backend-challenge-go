package sqs

import "testing"

func TestPublicadorExigeFila(t *testing.T) {
	p := Publicador{}
	if p.FilaURL != "" {
		t.Fatal("fila deveria iniciar vazia")
	}
}
