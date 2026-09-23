package eventos

import "testing"

func TestEnvelopePossuiIdentidadeEstavel(t *testing.T) {
	e := NovoEnvelope("WagerTransactionProcessed", "tx-1", "corr-1", map[string]string{"status": "PROCESSED"})
	if e.IdEvento == "" || e.Tipo == "" || e.Versao != 1 {
		t.Fatal("envelope invalido")
	}
	if _, err := e.JSON(); err != nil {
		t.Fatal(err)
	}
}
