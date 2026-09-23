package sqs

import "testing"

func TestClientePodeUsarEndpointLocal(t *testing.T) {
	if _, err := NovoCliente(t.Context(), "us-east-1", "http://localhost:4566"); err != nil {
		t.Fatal(err)
	}
}
