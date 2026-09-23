# Especificação técnica — plataforma de apostas distribuídas

> Documento de planejamento para implementação incremental com Codex. A `spec.md` é a fonte funcional; este documento define como construir, testar e operar.

## Como reproduzir a implementação

```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
docker compose config --quiet
docker compose up --build -d
INTEGRATION=true go test ./tests/integracao -v
docker compose down
```

As migrations em `migrations/` são aplicadas pelo serviço `migrate`. O CI executa a mesma validação em ambiente limpo.

## 1. Resultado esperado
Entregar um projeto Go executável por Docker Compose, com API HTTP autenticada, consumidor SQS, PostgreSQL, Keycloak, LocalStack, migrations, transactional outbox, workers, observabilidade, testes unitários, testes de integração e README operacional.

## 2. Estrutura do repositório
```text
.github/workflows/ci.yml
cmd/api/main.go
internal/config/                 configuração e validação
internal/domain/                 Money, Wallet, Transaction, Ledger, erros
internal/application/            casos de uso e portas
internal/adapters/http/           handlers, middleware, DTOs
internal/adapters/sqs/            produtor, consumidor, inbox
internal/adapters/oidc/           JWKS/OIDC e autorização
internal/repository/postgres/     queries, transações e migrations
internal/events/                  contratos e outbox
internal/workers/                 referência, outbox e SQS
migrations/
deploy/keycloak/realm-export.json
docker-compose.yml
Dockerfile
.env.example
README.md
ARCHITECTURE.md
spec.md
spec tecnica.md
```

Regras: domínio não importa Fx, HTTP, AWS ou PostgreSQL; application depende de interfaces; adapters implementam portas; cmd apenas monta Fx. Cada função de I/O recebe context.Context.

## 3. Stack e versões
- Go 1.23+ definido em go.mod e Dockerfile.
- Uber Fx, pgx/v5, router net/http ou chi, AWS SDK v2 SQS, OIDC/JWKS, Prometheus e slog/zerolog.
- PostgreSQL 16, Keycloak 25+, LocalStack e Docker Compose.
- golang-migrate ou ferramenta equivalente para migrations up/down.
- Testes com testing, httptest, mocks gerados/manualizados e testcontainers-go quando necessário.

Fixar versões no go.mod, imagens por tag estável e comandos reprodutíveis. Nunca depender de serviços instalados na máquina host, exceto Docker e Go para testes locais.

## 4. Configuração
Validar configuração na inicialização e falhar cedo. Variáveis mínimas: APP_ENV, HTTP_ADDR, DATABASE_URL, DB_MAX_CONNS, OIDC_ISSUER_URL, OIDC_AUDIENCE, OIDC_JWKS_URL, SQS_ENDPOINT, AWS_REGION, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, SQS_QUEUE_URL, SQS_DLQ_URL, OUTBOX_INTERVAL, REFERENCE_INTERVAL, MAX_REFERENCE_ATTEMPTS e SHUTDOWN_TIMEOUT.

`.env.example` deve conter somente valores locais. Segredos reais nunca entram no repositório. Configuração deve permitir executar API, worker e publisher no mesmo processo em desenvolvimento, mas também múltiplas réplicas.

## 5. Docker Compose
Serviços obrigatórios:
- postgres: volume persistente, healthcheck pg_isready, usuário/banco configuráveis.
- keycloak: modo dev local, volume opcional, import automático de `deploy/keycloak/realm-export.json`, healthcheck e dependência do banco.
- localstack: serviço SQS habilitado, volume e healthcheck.
- aws-init: cria filas FIFO, DLQ, redrive policy e, opcionalmente, destino de eventos.
- app: build multi-stage, migrations antes de iniciar workers, healthchecks e dependência saudável dos serviços.

Usar rede interna, nomes DNS dos serviços e portas documentadas. Não usar `depends_on` como substituto de retry: a aplicação deve tentar conexões e readiness novamente.

Keycloak deve importar realm `jungle-gaming`, clients `wallet-internal` e `provider-a`/`provider-b`, roles `wallet:read`, `wallet:write`, `provider:transactions` e `internal:wallets`. O README deve mostrar como obter token client_credentials via curl e como configurar dois providers com claims distintas.

## 6. Banco e migrations
Migrations devem criar tabelas de wallets, wagering_transactions, ledger_entries, inbox_messages, outbox_events e controle de migrations. Usar BIGINT para centavos, CHAR/VARCHAR para moeda, UUID, timestamptz e CHECK constraints.

Constraints mínimas: moeda válida, amount não negativo na entrada, wallet balance >= 0, balance_after = balance_before +/- amount, unicidade player/moeda, provider/external id, idempotency key, ledger wallet/transaction e abertura única. Revogar UPDATE/DELETE do ledger para o usuário da aplicação ou usar triggers que rejeitem alteração.

Índices: wallet player/moeda, transactions provider/external, idempotency, status/referência pendente, ledger wallet/created/id, outbox pending/next_attempt e inbox consumer/message.

Transações devem ser explícitas. Para operação financeira: BEGIN, inserções/locks/atualizações, COMMIT; rollback em qualquer erro. Usar SELECT FOR UPDATE na carteira e lock ordenado quando mais de uma linha puder ser acessada.

## 7. Domínio e aplicação
Money é imutável e serializa somente string decimal. Wallet expõe debit/credit; não permitir alteração direta de saldo. Transaction expõe transições válidas. Ledger só pode ser construído pelo caso de uso.

Casos de uso: CreateWallet, GetWallet, ListLedger, SubmitWager, GetTransaction, GetProviderTransaction, ReconcileWallet, ConsumeWagerMessage, ResolvePendingReference e PublishOutbox. HTTP e SQS devem chamar SubmitWager com o mesmo DTO normalizado e o mesmo algoritmo de hash.

Definir erros estáveis: invalid_money, insufficient_balance, reversal_insufficient_balance, duplicate_idempotency, idempotency_payload_conflict, reference_not_found, reference_not_processed, currency_mismatch, wallet_not_found, provider_forbidden e infrastructure_unavailable.

## 8. HTTP e segurança
Middleware: request ID/correlation ID, recuperação segura, limite de body, timeout, JSON content type, autenticação OIDC, autorização e logging sanitizado. Validar UUID, tamanho de strings, enum, moeda, valor, header e campos obrigatórios.

JWKS deve ser cacheado com renovação; validar issuer, audience, assinatura, expiração e algoritmo permitido. O providerId efetivo vem de claim configurada; corpo divergente retorna 403. Endpoints internos exigem role interna e não podem ser acessados por clients de provider.

Respostas de erro devem ter código, mensagem segura, correlationId e, quando aplicável, failureCode. Não expor SQL, token, stack trace ou payload financeiro completo.

## 9. SQS e workers
Consumer usa long polling, visibility timeout maior que o tempo máximo esperado, contexto cancelável e concorrência configurável. Registrar inbox antes de processar e concluir na mesma transação; mensagem só é deletada após commit.

Mensagens inválidas vão para DLQ conforme política. Erros transitórios não marcam operação como rejeitada. Duplicatas de messageId/hash igual são ack idempotente; hash diferente é falha de contrato.

Worker de referências consulta operações PENDING_REFERENCE com lease, backoff exponencial com jitter e máximo de tentativas/TTL configurável. Referência ausente expirada vira REJECTED com evento; referência pendente permanece; referência rejeitada não deve ser aplicada.

Publisher outbox faz claim atômico, publica com eventId estável, marca publicado depois do sucesso, libera leases expirados e mantém tentativas. Falha entre publish e mark gera republicação aceitável pelo contrato.

## 10. Testes unitários
Testar sem banco e sem rede: parsing/format Money, escala, overflow, moedas, Wallet debit/credit, transições, regras de cada operação, reversões, canonical hash, paginação, mapeamento de erros e construção de eventos.

Mocks devem existir para WalletRepository, TransactionRepository, UnitOfWork, EventPublisher, SQSClient, Clock, IDGenerator e OIDCVerifier. Testes devem validar chamadas, rollback, não publicação antes do commit, timeout/cancelamento e não reaplicação em replay.

Usar table-driven tests, subtests, nomes descritivos, cobertura de borda e `go test ./internal/...`. Não perseguir percentual artificial: priorizar invariantes e caminhos de falha.

## 11. Testes de integração
Separar duas camadas:
- Integração de adapters com mocks controlados: httptest para handlers, servidor OIDC/JWKS fake, SQS fake e publisher fake; validar contratos, middleware, serialização e retries.
- Integração de sistema com serviços reais: Docker Compose ou testcontainers para PostgreSQL, LocalStack e Keycloak. Validar migrations, constraints, auth real, SQS, inbox/outbox e lifecycle.

Os testes reais devem poder rodar com `INTEGRATION=true go test ./...` e aguardar healthchecks. Usar banco/realm/filas isolados por suíte; limpar por migration ou schema dedicado. Não substituir PostgreSQL, SQS ou IdP por mocks na suíte que comprova o desafio.

Casos obrigatórios: abertura zero/positiva, duplicate wallet, BET/WIN/LOSS, REFUND/ROLLBACK, referência atrasada, idempotência após restart, conflito de payload, provider isolation, ledger imutável, reconciliação, DLQ, outbox concorrente e readiness.

## 12. Concorrência e falhas
Executar ao menos três processos/containers independentes com conexões próprias. Verificar 50 reenvios da mesma operação, duas apostas de 80 em saldo 100, carteiras distintas em paralelo, HTTP cruzado com SQS e ausência de lost update.

Adicionar pontos de injeção de falha testáveis: antes do commit, depois do commit, antes do delete SQS, depois do publish e antes de marcar outbox. Cada cenário deve demonstrar recuperação sem duplicação.

Comandos: `go test ./...`, `go test -race ./...`, `go vet ./...`, `gofmt -w .`, `docker compose up --build`, `INTEGRATION=true go test ./...`.

## 13. README obrigatório
O README deve conter: visão geral; pré-requisitos; clone e configuração; árvore do projeto; variáveis; `docker compose up --build`; URLs/portas; credenciais locais não produtivas; import/configuração do realm; criação de token para provider e interno; exemplos curl de wallet, BET, replay, ledger, reconciliação e health.

Também documentar migrations up/down, criação/verificação das filas, execução local sem Docker quando suportada, testes unitários, integração mock, integração real, race, vet, logs/métricas, troubleshooting de Keycloak/PostgreSQL/LocalStack, shutdown e limpeza dos volumes.

## 14. Fases de desenvolvimento com Codex
Fase 1: scaffold, configuração, Compose, healthchecks e README inicial.
Fase 2: domínio Money/Wallet/Transaction, erros e testes unitários.
Fase 3: migrations, constraints, repositórios e testes SQL.
Fase 4: casos de uso financeiros, idempotência e ledger.
Fase 5: HTTP, OIDC, Keycloak realm e autorização.
Fase 6: SQS, inbox, worker de referências e DLQ.
Fase 7: outbox, publisher e eventos.
Fase 8: integração real, concorrência, falhas, observabilidade e documentação final.

Cada fase deve terminar com testes passando, diff revisável e atualização do README/ARCHITECTURE. O Codex deve implementar uma fase por vez, consultar esta spec, não alterar invariantes sem registrar decisão e executar os comandos de verificação antes de concluir.

## 15. Definition of Done
Projeto sobe em checkout limpo com Compose; realm e filas são provisionados automaticamente; endpoints exigem autenticação; dinheiro não usa float; saldo/ledger são atomicamente consistentes; idempotência sobrevive a restart; HTTP/SQS compartilham o caso de uso; outbox é recuperável; testes unitários, mocks e integração real passam; `-race`/`vet` passam; README permite reprodução por outra pessoa.
