package integracao

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
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
	resposta, err = http.Get(base + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	if resposta.StatusCode != http.StatusOK {
		resposta.Body.Close()
		t.Fatalf("metricas: %d", resposta.StatusCode)
	}
	resposta.Body.Close()
	resposta, err = http.Get(base + "/wallets/00000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	resposta.Body.Close()
	if resposta.StatusCode != http.StatusUnauthorized {
		t.Fatalf("auth esperada: %d", resposta.StatusCode)
	}
}

func TestProviderNaoAcessaOutroProvider(t *testing.T) {
	if os.Getenv("INTEGRATION") != "true" {
		t.Skip("defina INTEGRATION=true")
	}
	form := url.Values{"client_id": {"provider-a"}, "client_secret": {"provider-a-secret"}, "grant_type": {"client_credentials"}}
	resposta, err := http.Post("http://127.0.0.1:8080/realms/jungle-gaming/protocol/openid-connect/token", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	var token map[string]any
	if err := json.NewDecoder(resposta.Body).Decode(&token); err != nil {
		t.Fatal(err)
	}
	resposta.Body.Close()
	accessToken, _ := token["access_token"].(string)
	if accessToken == "" {
		t.Fatal("token nao obtido")
	}
	corpo := `{"providerId":"provider-b","externalTransactionId":"isolamento-1","walletId":"00000000-0000-0000-0000-000000000001","playerId":"00000000-0000-0000-0000-000000000002","roundId":"r","gameId":"g","kind":"LOSS","money":{"amount":"1.00","currency":"BRL"}}`
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:8081/wagering/transactions", strings.NewReader(corpo))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Idempotency-Key", "provider-b:isolamento-1")
	req.Header.Set("Content-Type", "application/json")
	resposta, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resposta.Body.Close()
	if resposta.StatusCode != http.StatusForbidden {
		t.Fatalf("status esperado 403, recebido %d", resposta.StatusCode)
	}

	consulta, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8081/providers/provider-b/wagering/transactions/isolamento-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	consulta.Header.Set("Authorization", "Bearer "+accessToken)
	resposta, err = http.DefaultClient.Do(consulta)
	if err != nil {
		t.Fatal(err)
	}
	defer resposta.Body.Close()
	if resposta.StatusCode != http.StatusForbidden {
		t.Fatalf("consulta: status esperado 403, recebido %d", resposta.StatusCode)
	}
}
