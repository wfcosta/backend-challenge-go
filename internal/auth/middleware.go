package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

type contextoChave string

const Identidade contextoChave = "identidade"

func Provedor(ctx context.Context) string {
	token, ok := ctx.Value(Identidade).(*oidc.IDToken)
	if !ok {
		return ""
	}
	var claims map[string]any
	if token.Claims(&claims) != nil {
		return ""
	}
	if valor, ok := claims["provider_id"].(string); ok && valor != "" {
		return valor
	}
	if valor, ok := claims["azp"].(string); ok {
		return valor
	}
	return ""
}

// TemPapel verifica papeis do realm e papeis de cliente presentes no token.
// O azp e usado apenas como compatibilidade com os clients locais exportados.
func TemPapel(ctx context.Context, papel string) bool {
	token, ok := ctx.Value(Identidade).(*oidc.IDToken)
	if !ok {
		return false
	}
	var claims struct {
		RealmAccess struct {
			Roles []string `json:"roles"`
		} `json:"realm_access"`
		ResourceAccess map[string]struct {
			Roles []string `json:"roles"`
		} `json:"resource_access"`
		Azp string `json:"azp"`
	}
	if token.Claims(&claims) != nil {
		return false
	}
	for _, role := range claims.RealmAccess.Roles {
		if role == papel {
			return true
		}
	}
	for _, acesso := range claims.ResourceAccess {
		for _, role := range acesso.Roles {
			if role == papel {
				return true
			}
		}
	}
	if papel == "internal:wallets" && claims.Azp == "wallet-internal" {
		return true
	}
	return papel == "provider:transactions" && (claims.Azp == "provider-a" || claims.Azp == "provider-b")
}

type Autenticador struct {
	issuer      string
	audience    string
	verificador *oidc.IDTokenVerifier
}

func NovoAutenticador(ctx context.Context, issuer, audience string) (*Autenticador, error) {
	provedor, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	return &Autenticador{issuer: issuer, audience: audience, verificador: provedor.Verifier(&oidc.Config{ClientID: audience})}, nil
}

func (a *Autenticador) Fechar() {}

func (a *Autenticador) Middleware(proximo http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cabecalho := r.Header.Get("Authorization")
		if !strings.HasPrefix(cabecalho, "Bearer ") {
			http.Error(w, "autenticacao obrigatoria", http.StatusUnauthorized)
			return
		}
		token, err := a.verificador.Verify(r.Context(), strings.TrimPrefix(cabecalho, "Bearer "))
		if err != nil {
			http.Error(w, "token invalido", http.StatusUnauthorized)
			return
		}
		proximo.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), Identidade, token)))
	})
}
