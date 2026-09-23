package integracao

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
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

	carteira, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8081/wallets/00000000-0000-0000-0000-000000000001", nil)
	if err != nil {
		t.Fatal(err)
	}
	carteira.Header.Set("Authorization", "Bearer "+accessToken)
	resposta, err = http.DefaultClient.Do(carteira)
	if err != nil {
		t.Fatal(err)
	}
	defer resposta.Body.Close()
	if resposta.StatusCode != http.StatusForbidden {
		t.Fatalf("carteira com token provider: status esperado 403, recebido %d", resposta.StatusCode)
	}
}

func TestIdempotenciaConcorrenteProcessaUmaVez(t *testing.T) {
	if os.Getenv("INTEGRATION") != "true" {
		t.Skip("defina INTEGRATION=true")
	}
	internal := obterToken(t, "wallet-internal", "internal-secret")
	provider := obterToken(t, "provider-a", "provider-a-secret")
	playerID := "00000000-0000-0000-0000-" + fmt.Sprintf("%012d", time.Now().UnixNano()%1000000000000)
	criar := `{"playerId":"` + playerID + `","initialBalance":{"amount":"100.00","currency":"BRL"}}`
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:8081/wallets", strings.NewReader(criar))
	req.Header.Set("Authorization", "Bearer "+internal)
	req.Header.Set("Content-Type", "application/json")
	resposta, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resposta.Body.Close()
	if resposta.StatusCode != http.StatusCreated {
		t.Fatalf("criação da carteira: %d", resposta.StatusCode)
	}
	var carteira struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resposta.Body).Decode(&carteira); err != nil {
		t.Fatal(err)
	}
	corpo := `{"providerId":"provider-a","externalTransactionId":"concorrente-` + playerID + `","walletId":"` + carteira.ID + `","playerId":"` + playerID + `","roundId":"round-1","gameId":"game-1","kind":"BET","money":{"amount":"80.00","currency":"BRL"}}`
	const total = 10
	type resultadoHTTP struct {
		status int
		corpo  string
	}
	resultados := make(chan resultadoHTTP, total)
	var grupo sync.WaitGroup
	for i := 0; i < total; i++ {
		grupo.Add(1)
		go func() {
			defer grupo.Done()
			operacao, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:8081/wagering/transactions", strings.NewReader(corpo))
			operacao.Header.Set("Authorization", "Bearer "+provider)
			operacao.Header.Set("Idempotency-Key", "concorrente-"+playerID)
			operacao.Header.Set("Content-Type", "application/json")
			res, err := http.DefaultClient.Do(operacao)
			if err != nil {
				resultados <- resultadoHTTP{}
				return
			}
			bytes, _ := io.ReadAll(res.Body)
			res.Body.Close()
			resultados <- resultadoHTTP{status: res.StatusCode, corpo: string(bytes)}
		}()
	}
	grupo.Wait()
	close(resultados)
	for resultado := range resultados {
		if resultado.status != http.StatusOK {
			t.Fatalf("status concorrente inesperado: %d (%s)", resultado.status, resultado.corpo)
		}
	}
	consulta, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:8081/wallets/"+carteira.ID, nil)
	consulta.Header.Set("Authorization", "Bearer "+internal)
	resposta, err = http.DefaultClient.Do(consulta)
	if err != nil {
		t.Fatal(err)
	}
	defer resposta.Body.Close()
	if resposta.StatusCode != http.StatusOK {
		t.Fatalf("consulta final da carteira: %d", resposta.StatusCode)
	}
	var final struct {
		Balance struct {
			Amount string `json:"amount"`
		} `json:"balance"`
	}
	if err := json.NewDecoder(resposta.Body).Decode(&final); err != nil {
		t.Fatal(err)
	}
	if final.Balance.Amount != "20.00" {
		t.Fatalf("saldo final esperado 20.00, recebido %s", final.Balance.Amount)
	}
}

func TestDuasApostasConcorrentesRespeitamSaldo(t *testing.T) {
	if os.Getenv("INTEGRATION") != "true" {
		t.Skip("defina INTEGRATION=true")
	}
	internal := obterToken(t, "wallet-internal", "internal-secret")
	provider := obterToken(t, "provider-a", "provider-a-secret")
	playerID := "00000000-0000-0000-0000-" + fmt.Sprintf("%012d", (time.Now().UnixNano()+1)%1000000000000)
	criar := `{"playerId":"` + playerID + `","initialBalance":{"amount":"100.00","currency":"BRL"}}`
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:8081/wallets", strings.NewReader(criar))
	req.Header.Set("Authorization", "Bearer "+internal)
	req.Header.Set("Content-Type", "application/json")
	resposta, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var carteira struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resposta.Body).Decode(&carteira); err != nil {
		resposta.Body.Close()
		t.Fatal(err)
	}
	resposta.Body.Close()
	if resposta.StatusCode != http.StatusCreated {
		t.Fatalf("criação da carteira: %d", resposta.StatusCode)
	}
	const total = 2
	resultados := make(chan int, total)
	var grupo sync.WaitGroup
	for i := 1; i <= total; i++ {
		grupo.Add(1)
		go func(numero int) {
			defer grupo.Done()
			corpo := `{"providerId":"provider-a","externalTransactionId":"saldo-` + playerID + `-` + fmt.Sprint(numero) + `","walletId":"` + carteira.ID + `","playerId":"` + playerID + `","roundId":"round-1","gameId":"game-1","kind":"BET","money":{"amount":"80.00","currency":"BRL"}}`
			operacao, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:8081/wagering/transactions", strings.NewReader(corpo))
			operacao.Header.Set("Authorization", "Bearer "+provider)
			operacao.Header.Set("Idempotency-Key", "saldo-"+playerID+"-"+fmt.Sprint(numero))
			operacao.Header.Set("Content-Type", "application/json")
			res, err := http.DefaultClient.Do(operacao)
			if err != nil {
				resultados <- 0
				return
			}
			res.Body.Close()
			resultados <- res.StatusCode
		}(i)
	}
	grupo.Wait()
	close(resultados)
	aceitas, rejeitadas := 0, 0
	for status := range resultados {
		if status == http.StatusOK {
			aceitas++
		} else if status == http.StatusUnprocessableEntity {
			rejeitadas++
		} else {
			t.Fatalf("status inesperado: %d", status)
		}
	}
	if aceitas != 1 || rejeitadas != 1 {
		t.Fatalf("esperava uma aceita e uma rejeitada; aceitas=%d rejeitadas=%d", aceitas, rejeitadas)
	}
}

func TestLedgerEReconcilacaoDaCarteira(t *testing.T) {
	if os.Getenv("INTEGRATION") != "true" {
		t.Skip("defina INTEGRATION=true")
	}
	token := obterToken(t, "wallet-internal", "internal-secret")
	playerID := "00000000-0000-0000-0000-" + fmt.Sprintf("%012d", (time.Now().UnixNano()+2)%1000000000000)
	corpo := `{"playerId":"` + playerID + `","initialBalance":{"amount":"25.00","currency":"BRL"}}`
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:8081/wallets", strings.NewReader(corpo))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resposta, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var carteira struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resposta.Body).Decode(&carteira); err != nil {
		resposta.Body.Close()
		t.Fatal(err)
	}
	resposta.Body.Close()
	if resposta.StatusCode != http.StatusCreated {
		t.Fatalf("criação da carteira: %d", resposta.StatusCode)
	}

	ledger, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:8081/wallets/"+carteira.ID+"/ledger", nil)
	ledger.Header.Set("Authorization", "Bearer "+token)
	resposta, err = http.DefaultClient.Do(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if resposta.StatusCode != http.StatusOK {
		resposta.Body.Close()
		t.Fatalf("ledger: %d", resposta.StatusCode)
	}
	resposta.Body.Close()

	reconciliacao, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:8081/wallets/"+carteira.ID+"/reconciliation", nil)
	reconciliacao.Header.Set("Authorization", "Bearer "+token)
	resposta, err = http.DefaultClient.Do(reconciliacao)
	if err != nil {
		t.Fatal(err)
	}
	defer resposta.Body.Close()
	if resposta.StatusCode != http.StatusOK {
		t.Fatalf("reconciliação: %d", resposta.StatusCode)
	}
	var resultado struct {
		Consistente bool `json:"consistent"`
	}
	if err := json.NewDecoder(resposta.Body).Decode(&resultado); err != nil {
		t.Fatal(err)
	}
	if !resultado.Consistente {
		t.Fatal("reconciliação deveria estar consistente")
	}
}

func TestFluxoBetEWin(t *testing.T) {
	if os.Getenv("INTEGRATION") != "true" {
		t.Skip("defina INTEGRATION=true")
	}
	internal := obterToken(t, "wallet-internal", "internal-secret")
	provider := obterToken(t, "provider-a", "provider-a-secret")
	playerID := "00000000-0000-0000-0000-" + fmt.Sprintf("%012d", (time.Now().UnixNano()+3)%1000000000000)
	criar := `{"playerId":"` + playerID + `","initialBalance":{"amount":"100.00","currency":"BRL"}}`
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:8081/wallets", strings.NewReader(criar))
	req.Header.Set("Authorization", "Bearer "+internal)
	req.Header.Set("Content-Type", "application/json")
	resposta, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var carteira struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resposta.Body).Decode(&carteira); err != nil {
		resposta.Body.Close()
		t.Fatal(err)
	}
	resposta.Body.Close()
	if resposta.StatusCode != http.StatusCreated {
		t.Fatalf("criação da carteira: %d", resposta.StatusCode)
	}

	postar := func(externo, tipo, valor string) map[string]any {
		corpo := `{"providerId":"provider-a","externalTransactionId":"` + externo + `","walletId":"` + carteira.ID + `","playerId":"` + playerID + `","roundId":"round-fluxo","gameId":"game-fluxo","kind":"` + tipo + `","money":{"amount":"` + valor + `","currency":"BRL"}}`
		operacao, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:8081/wagering/transactions", strings.NewReader(corpo))
		operacao.Header.Set("Authorization", "Bearer "+provider)
		operacao.Header.Set("Idempotency-Key", externo)
		operacao.Header.Set("Content-Type", "application/json")
		res, chamadaErr := http.DefaultClient.Do(operacao)
		if chamadaErr != nil {
			t.Fatal(chamadaErr)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d", tipo, res.StatusCode)
		}
		var retorno map[string]any
		if err := json.NewDecoder(res.Body).Decode(&retorno); err != nil {
			t.Fatal(err)
		}
		return retorno
	}
	bet := postar("fluxo-bet-"+playerID, "BET", "20.00")
	if bet["status"] != "PROCESSED" {
		t.Fatalf("BET não processado: %#v", bet)
	}
	win := postar("fluxo-win-"+playerID, "WIN", "30.00")
	if win["status"] != "PROCESSED" {
		t.Fatalf("WIN não processado: %#v", win)
	}

	consulta, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:8081/providers/provider-a/wagering/transactions/fluxo-win-"+playerID, nil)
	consulta.Header.Set("Authorization", "Bearer "+provider)
	resposta, err = http.DefaultClient.Do(consulta)
	if err != nil {
		t.Fatal(err)
	}
	defer resposta.Body.Close()
	if resposta.StatusCode != http.StatusOK {
		t.Fatalf("consulta WIN: %d", resposta.StatusCode)
	}
}

func TestFluxoRefundComReferencia(t *testing.T) {
	if os.Getenv("INTEGRATION") != "true" {
		t.Skip("defina INTEGRATION=true")
	}
	internal := obterToken(t, "wallet-internal", "internal-secret")
	provider := obterToken(t, "provider-a", "provider-a-secret")
	playerID := "00000000-0000-0000-0000-" + fmt.Sprintf("%012d", (time.Now().UnixNano()+4)%1000000000000)
	criar := `{"playerId":"` + playerID + `","initialBalance":{"amount":"100.00","currency":"BRL"}}`
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:8081/wallets", strings.NewReader(criar))
	req.Header.Set("Authorization", "Bearer "+internal)
	req.Header.Set("Content-Type", "application/json")
	resposta, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var carteira struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resposta.Body).Decode(&carteira); err != nil {
		resposta.Body.Close()
		t.Fatal(err)
	}
	resposta.Body.Close()
	if resposta.StatusCode != http.StatusCreated {
		t.Fatalf("criação da carteira: %d", resposta.StatusCode)
	}
	enviar := func(externo, tipo, valor, referencia string) int {
		corpo := `{"providerId":"provider-a","externalTransactionId":"` + externo + `","walletId":"` + carteira.ID + `","playerId":"` + playerID + `","roundId":"round-refund","gameId":"game-refund","kind":"` + tipo + `","referenceExternalId":"` + referencia + `","money":{"amount":"` + valor + `","currency":"BRL"}}`
		operacao, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:8081/wagering/transactions", strings.NewReader(corpo))
		operacao.Header.Set("Authorization", "Bearer "+provider)
		operacao.Header.Set("Idempotency-Key", externo)
		operacao.Header.Set("Content-Type", "application/json")
		res, chamadaErr := http.DefaultClient.Do(operacao)
		if chamadaErr != nil {
			t.Fatal(chamadaErr)
		}
		res.Body.Close()
		return res.StatusCode
	}
	original := "refund-original-" + playerID
	if status := enviar("refund-pendente-"+playerID, "REFUND", "20.00", "referencia-que-ainda-nao-existe"); status != http.StatusOK {
		t.Fatalf("REFUND pendente: %d", status)
	}
	if status := enviar(original, "BET", "20.00", ""); status != http.StatusOK {
		t.Fatalf("BET: %d", status)
	}
	if status := enviar("refund-"+playerID, "REFUND", "20.00", original); status != http.StatusOK {
		t.Fatalf("REFUND: %d", status)
	}
	if status := enviar("refund-duplicado-"+playerID, "REFUND", "20.00", original); status != http.StatusUnprocessableEntity {
		t.Fatalf("REFUND duplicado: status esperado 422, recebido %d", status)
	}
}

func TestFluxoRollbackDeWin(t *testing.T) {
	if os.Getenv("INTEGRATION") != "true" {
		t.Skip("defina INTEGRATION=true")
	}
	internal := obterToken(t, "wallet-internal", "internal-secret")
	provider := obterToken(t, "provider-a", "provider-a-secret")
	playerID := "00000000-0000-0000-0000-" + fmt.Sprintf("%012d", (time.Now().UnixNano()+5)%1000000000000)
	criar := `{"playerId":"` + playerID + `","initialBalance":{"amount":"100.00","currency":"BRL"}}`
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:8081/wallets", strings.NewReader(criar))
	req.Header.Set("Authorization", "Bearer "+internal)
	req.Header.Set("Content-Type", "application/json")
	resposta, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var carteira struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resposta.Body).Decode(&carteira); err != nil {
		resposta.Body.Close()
		t.Fatal(err)
	}
	resposta.Body.Close()
	if resposta.StatusCode != http.StatusCreated {
		t.Fatalf("criação da carteira: %d", resposta.StatusCode)
	}
	enviar := func(externo, tipo, valor, referencia string) int {
		corpo := `{"providerId":"provider-a","externalTransactionId":"` + externo + `","walletId":"` + carteira.ID + `","playerId":"` + playerID + `","roundId":"round-rollback","gameId":"game-rollback","kind":"` + tipo + `","referenceExternalId":"` + referencia + `","money":{"amount":"` + valor + `","currency":"BRL"}}`
		operacao, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:8081/wagering/transactions", strings.NewReader(corpo))
		operacao.Header.Set("Authorization", "Bearer "+provider)
		operacao.Header.Set("Idempotency-Key", externo)
		operacao.Header.Set("Content-Type", "application/json")
		res, chamadaErr := http.DefaultClient.Do(operacao)
		if chamadaErr != nil {
			t.Fatal(chamadaErr)
		}
		res.Body.Close()
		return res.StatusCode
	}
	original := "rollback-win-" + playerID
	if status := enviar(original, "WIN", "30.00", ""); status != http.StatusOK {
		t.Fatalf("WIN: %d", status)
	}
	if status := enviar("rollback-"+playerID, "ROLLBACK", "30.00", original); status != http.StatusOK {
		t.Fatalf("ROLLBACK: %d", status)
	}

	consulta, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:8081/wallets/"+carteira.ID, nil)
	consulta.Header.Set("Authorization", "Bearer "+internal)
	resposta, err = http.DefaultClient.Do(consulta)
	if err != nil {
		t.Fatal(err)
	}
	defer resposta.Body.Close()
	var saldo struct {
		Balance struct {
			Amount string `json:"amount"`
		} `json:"balance"`
	}
	if err := json.NewDecoder(resposta.Body).Decode(&saldo); err != nil {
		t.Fatal(err)
	}
	if saldo.Balance.Amount != "100.00" {
		t.Fatalf("saldo após rollback esperado 100.00, recebido %s", saldo.Balance.Amount)
	}
}

func obterToken(t *testing.T, cliente, segredo string) string {
	t.Helper()
	form := url.Values{"client_id": {cliente}, "client_secret": {segredo}, "grant_type": {"client_credentials"}}
	resposta, err := http.Post("http://127.0.0.1:8080/realms/jungle-gaming/protocol/openid-connect/token", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	defer resposta.Body.Close()
	var token map[string]any
	if err := json.NewDecoder(resposta.Body).Decode(&token); err != nil {
		t.Fatal(err)
	}
	valor, _ := token["access_token"].(string)
	if valor == "" {
		t.Fatalf("token não obtido para %s", cliente)
	}
	return valor
}
