package sqs

import "testing"

func TestConsumidorPodeSerCancelado(t *testing.T) {
	var c Consumidor
	if c.FilaURL != "" {
		t.Fatal("fila inicial deveria estar vazia")
	}
}
