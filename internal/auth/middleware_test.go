package auth

import "testing"

func TestIssuerURL(t *testing.T) {
	a := "http://keycloak:8080/realms/jungle-gaming"
	if a == "" {
		t.Fatal("issuer vazio")
	}
}
