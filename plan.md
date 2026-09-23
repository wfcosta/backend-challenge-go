# Plano de implementação — desafio backend Go

## Objetivo do plano

Desenvolver o desafio de forma incremental, orientada a testes e com evidência verificável em cada etapa. A implementação só será considerada concluída quando o código, testes, documentação, Docker Compose, autenticação, mensageria e fluxos financeiros estiverem validados conforme `spec.md` e `spec tecnica.md`.

## Regras de execução

1. Cada tarefa deve começar com testes que expressem o comportamento esperado.
2. Executar o ciclo TDD: Red (teste falhando), Green (menor implementação possível) e Refactor (melhoria sem quebrar testes).
3. Nenhuma tarefa é concluída sem critérios de aceite e comandos de verificação.
4. Toda decisão arquitetural relevante deve ser registrada em `ARCHITECTURE.md`.
5. Contratos HTTP, eventos, schema, mensagens SQS e configurações devem ser documentados e testados.
6. Não avançar para a próxima fase se houver teste falhando, race condition, `go vet` quebrado ou requisito eliminatório pendente.
7. Ao finalizar cada fase, atualizar o README e registrar limitações ou decisões.

## Fase 0 — Repositório e planejamento

### Tarefa 0.1 — Criar repositório no GitHub

Objetivo: criar o repositório `wfcosta/backend-challenge-go` no GitHub e conectá-lo ao workspace local.

Atividades:
- Criar repositório público, sem adicionar README automático conflitante.
- Inicializar branch principal, configurar remote e primeiro commit.
- Adicionar `.gitignore` para Go, IDE, Docker, `.env` e artefatos de teste.
- Adicionar `spec.md`, `spec tecnica.md`, este `plan.md` e um README inicial.
- Configurar proteção da branch principal quando possível.

Pronto quando: o repositório remoto existe, o clone limpo contém os documentos e `git status` está limpo.

### Tarefa 0.2 — Criar ARCHITECTURE.md

Documentar tecnologias, decisões e trade-offs:
- Go: tipagem, concorrência, simplicidade operacional e suporte ao race detector.
- Uber Fx: composição, lifecycle, shutdown e injeção sem acoplar o domínio.
- PostgreSQL: transações, locks, constraints, auditoria e consistência.
- pgx: SQL explícito e controle de transações/locks.
- SQS/LocalStack: entrega at-least-once, retry e DLQ próximos do ambiente real.
- Keycloak/OIDC: autenticação externa, claims, roles e client_credentials.
- Docker Compose: ambiente reproduzível local.
- `int64` em centavos: precisão e ausência de ponto flutuante.
- Outbox/inbox: atomicidade entre banco e mensageria.

Incluir diagramas Mermaid para:
1. arquitetura de componentes;
2. fluxo de uma operação HTTP;
3. fluxo de uma mensagem SQS;
4. transação financeira com lock e ledger;
5. fluxo de outbox/publicação/retry;
6. autenticação e autorização;
7. shutdown com Fx;
8. estados de uma WagerTransaction;
9. fluxo de referência pendente;
10. sequência de concorrência entre duas apostas.

Pronto quando: outro desenvolvedor consegue explicar componentes, limites, dependências, fluxos, falhas e garantias lendo o documento.

## Fase 1 — Bootstrap do projeto Go

### Tarefa 1.1 — Criar módulo e comandos
- Criar `go.mod`, `cmd/api/main.go` e estrutura `internal`.
- Configurar `gofmt`, `go vet`, logger, config e versão da aplicação.
- Implementar parsing/validação de configuração.
- Criar composição Fx mínima com lifecycle, sinal de shutdown e health básico.

BDD:
```gherkin
Feature: inicialização da aplicação
  Scenario: inicia com configuração válida
    Given todas as variáveis obrigatórias estão definidas
    When a aplicação é inicializada
    Then os módulos são construídos
    And o processo permanece disponível

  Scenario: rejeita configuração inválida
    Given DATABASE_URL está ausente
    When a aplicação é inicializada
    Then ela falha com erro descritivo
    And nenhum worker é iniciado
```

Pronto quando: testes de configuração/Fx passam e `go test ./...` executa sem infraestrutura externa.

### Tarefa 1.2 — Dockerfile e Compose inicial
- Criar build multi-stage.
- Criar serviços PostgreSQL, Keycloak, LocalStack, init de SQS e app.
- Adicionar healthchecks, volumes, rede e `.env.example`.
- Criar script idempotente de filas e redrive.

BDD: com `docker compose up --build`, PostgreSQL, Keycloak, LocalStack e app ficam saudáveis; subir novamente não duplica filas nem falha.

## Fase 2 — Domínio com TDD

### Tarefa 2.1 — Money
Escrever primeiro testes para parsing válido, duas casas, zero, soma, subtração, negação, comparação, moeda incompatível, negativo externo, escala inválida, notação científica e overflow. Implementar depois o value object imutável.

### Tarefa 2.2 — Wallet e Ledger
Testar criação/reidratação, débito com saldo suficiente, rejeição por saldo, crédito, moeda incompatível, versão, saldo zero e invariantes do ledger. Implementar entidades encapsuladas e erros classificáveis.

### Tarefa 2.3 — WagerTransaction
Testar máquina de estados, estados terminais, operações externas versus OPENING, failureCode, replay e referência pendente.

### Tarefa 2.4 — Regras financeiras
BDD mínimo:
```gherkin
Feature: processamento financeiro
  Scenario: BET debita saldo
    Given carteira BRL com saldo 100.00
    When uma BET BRL de 25.00 é processada
    Then o saldo é 75.00
    And existe um único débito no ledger
    And a transação fica PROCESSED

  Scenario: BET sem saldo é rejeitada
    Given carteira com saldo 10.00
    When uma BET de 20.00 é processada
    Then a transação fica REJECTED
    And o saldo permanece 10.00
    And nenhum ledger é criado

  Scenario: LOSS não altera saldo
    Given carteira com saldo 100.00
    When LOSS com 0.00 é processada
    Then a transação fica PROCESSED
    And não existe WalletBalanceChanged

  Scenario: moeda incompatível é rejeitada
    Given carteira BRL
    When uma operação USD é enviada
    Then a operação é rejeitada sem efeito financeiro
```

Pronto quando: domínio não depende de banco/Fx/HTTP e todos os testes unitários passam.

## Fase 3 — Banco e persistência

### Tarefa 3.1 — Migrations e schema
- Criar migrations up/down para wallets, transactions, ledger, inbox e outbox.
- Adicionar constraints, índices, triggers de imutabilidade e usuário de aplicação.
- Testar migrations em banco limpo e rollback.

### Tarefa 3.2 — Repositórios e Unit of Work
- Criar interfaces de repositório.
- Implementar transação explícita com pgx.
- Implementar lock por carteira com `FOR UPDATE`.
- Implementar claim de inbox/outbox com `SKIP LOCKED` e lease.

BDD: duas conexões simultâneas nunca produzem saldo negativo, lost update ou dois lançamentos para a mesma operação.

Pronto quando: testes de integração PostgreSQL validam constraints, locks, rollback e imutabilidade do ledger.

## Fase 4 — Casos de uso financeiros

### Tarefa 4.1 — CreateWallet
Test-first para saldo zero, saldo positivo com OPENING, eventos atômicos e conflito jogador/moeda.

### Tarefa 4.2 — SubmitWager e idempotência
BDD:
```gherkin
Feature: idempotência persistente
  Scenario: replay idêntico
    Given operação já processada com uma chave
    When a mesma chave e payload são reenviados
    Then o resultado original é retornado
    And idempotentReplay é true
    And nenhum novo ledger é criado

  Scenario: mesma chave com payload diferente
    Given uma chave já registrada
    When a chave é reutilizada com outro payload
    Then a resposta é 409
    And nenhum efeito financeiro ocorre

  Scenario: mesma operação com outra chave
    Given provider e externalTransactionId já processados
    When outra idempotency key é usada
    Then a resposta é 409
```

### Tarefa 4.3 — Reversões e referências
Testar REFUND, ROLLBACK, referência inexistente, referência pendente, referência rejeitada, valor divergente, provider divergente, dupla reversão e reversão sem saldo.

### Tarefa 4.4 — Reconciliação
Testar soma do ledger, diferença, visão consistente, divergência em métrica/log e ausência de mutação.

Pronto quando: casos de uso passam com mocks unitários e PostgreSQL real, preservando resultado original e atomicidade.

## Fase 5 — HTTP e autenticação

### Tarefa 5.1 — DTOs, handlers e erros
Escrever testes httptest para todos os endpoints, JSON válido/inválido, limites, status codes, cursor e correlationId. Implementar handlers finos que chamam application.

### Tarefa 5.2 — OIDC/Keycloak
- Configurar realm exportado, clients, roles e usuários/clients de teste.
- Implementar descoberta/JWKS cacheado, issuer, audience, expiração e algoritmo.
- Implementar autorização de provider e endpoints internos.

BDD: token ausente/inválido/expirado retorna 401; role insuficiente retorna 403; provider A não lê/escreve dados do provider B; request autorizada chega ao caso de uso.

## Fase 6 — SQS, inbox e workers

### Tarefa 6.1 — Consumidor
Testar parsing, messageId, hash, ack após commit, reentrega, erro transitório, erro permanente e DLQ.

### Tarefa 6.2 — Worker de referência
Testar backoff, lease, retomada após restart, resolução quando referência chega e rejeição após TTL.

### Tarefa 6.3 — Shutdown
Testar SIGTERM: para polling, aguarda trabalho dentro do timeout ou libera mensagem, encerra workers e fecha dependências em ordem.

## Fase 7 — Outbox e eventos

### Tarefa 7.1 — Contratos
Testar envelope, tipos, versões, timestamps, payload tipado, snapshot imutável e regra de LOSS.

### Tarefa 7.2 — Publisher
Testar publicação após commit, múltiplos publishers, lease expirado, retry, falha antes/depois de publicar e eventId estável.

## Fase 8 — Observabilidade

Implementar logs JSON sanitizados, métricas de status/duplicatas/retries/DLQ/concorrência/outbox/reconciliação e endpoints de liveness/readiness. Testar campos obrigatórios e ausência de token/payload sensível.

## Fase 9 — Integração completa e cenários de aceite

Executar com Compose e infraestrutura real:
- wallet zero e positiva;
- BET, WIN, LOSS, REFUND e ROLLBACK;
- 50 reenvios paralelos da mesma aposta;
- duas apostas de 80.00 em carteira de 100.00;
- três instâncias independentes;
- carteiras diferentes em paralelo;
- HTTP e SQS para a mesma operação;
- interrupção após commit antes do delete SQS;
- dois publishers concorrentes;
- referência antes da operação original;
- restart com pendência e idempotência preservadas;
- provider isolation;
- reconciliação;
- DLQ e readiness.

## Fase 10 — README e entrega

README final deve conter pré-requisitos, clone, `.env`, portas, Compose, Keycloak, realm, clients, roles, obtenção de tokens, exemplos curl de todos os endpoints, SQS, migrations, testes unitários, mocks, integração real, race, vet, logs, troubleshooting, reset de volumes e shutdown.

Atualizar ARCHITECTURE.md com decisões reais e limitações. Adicionar CI executando format check, vet, unitários, integração, race quando suportado e build Docker.

## Checklist final de execução

```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
docker compose up --build -d
docker compose ps
INTEGRATION=true go test ./...
curl /health/live
curl /health/ready
# executar coleção completa autenticada de endpoints
docker compose down
```

Antes de marcar o projeto como pronto, conferir: nenhum teste ignorado sem justificativa; nenhum mock substitui a integração real; nenhum endpoint de negócio sem auth; nenhuma operação financeira sem ledger/outbox transacional; nenhum saldo negativo/duplicado; documentação reproduzível a partir de checkout limpo.

## Execução reproduzível

```bash
docker compose up --build -d
INTEGRATION=true go test ./tests/integracao -v
go test -race ./...
go vet ./...
docker compose down
```

Cada tarefa deve ter teste, implementação, validação local e documentação atualizada. O CI executa o mesmo ciclo em ambiente limpo.
