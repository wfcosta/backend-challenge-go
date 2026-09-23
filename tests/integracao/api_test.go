package integracao

import (
	"net/http"
	"os"
	"testing"
)

func TestAPIComCompose(t *testing.T) {
	if os.Getenv("INTEGRATION") != "true" {
		t.Skip("defina INTEGRATION=true")
	}
	base := "http://127.0.0.1:8081"
	resposta, err := http.Get(base + "/health/live")
	if err != nil {
		t.Fatal(err)
	}
	resposta.Body.Close()
	if resposta.StatusCode != http.StatusOK {
		t.Fatalf("liveness: %d", resposta.StatusCode)
	}
	resposta, err = http.Get(base + "/wallets/00000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	resposta.Body.Close()
	if resposta.StatusCode != http.StatusUnauthorized {
		t.Fatalf("auth esperada: %d", resposta.StatusCode)
	}
}
