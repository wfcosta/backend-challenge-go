# Arquitetura

## Componentes

```mermaid
flowchart LR
  P[Provider] --> API[HTTP API]
  API --> APP[Application]
  SQS[SQS FIFO] --> APP
  APP --> DB[(PostgreSQL)]
  APP --> OUT[Outbox]
  OUT --> PUB[Publisher]
  API -. OIDC .-> KC[Keycloak]
  APP --> DLQ[DLQ]
```

Domínio não conhece HTTP, SQS, Fx ou PostgreSQL. A aplicação concentra garantias financeiras; adapters traduzem protocolos; PostgreSQL é a fonte de verdade.

## Tecnologias

- Go: tipagem, concorrência, baixo consumo e race detector.
- Uber Fx: composição e lifecycle explícitos.
- PostgreSQL/pgx: transações, constraints e locks verificáveis.
- SQS/LocalStack: entrega at-least-once, retry e DLQ.
- Keycloak/OIDC: IdP externo, claims e client credentials.
- Docker Compose: ambiente reproduzível.
- int64 em centavos: precisão sem floating point.

## Fluxo financeiro

```mermaid
sequenceDiagram
  participant P as Provider
  participant A as API
  participant D as PostgreSQL
  P->>A: operação e idempotency key
  A->>D: BEGIN e deduplicação
  A->>D: lock wallet e validações
  A->>D: wallet, ledger, transaction, outbox
  D-->>A: COMMIT
  A-->>P: resultado persistido
```

## Outbox

```mermaid
sequenceDiagram
  participant D as DB
  participant W as Publisher
  participant B as Broker
  D->>W: claim pending event
  W->>B: publish eventId
  B-->>W: sucesso
  W->>D: mark published
```

## Estados

```mermaid
stateDiagram-v2
  [*] --> PENDING
  PENDING --> PROCESSED
  PENDING --> REJECTED
  PENDING --> FAILED
  PENDING --> PENDING_REFERENCE
  PENDING_REFERENCE --> PROCESSED
  PENDING_REFERENCE --> REJECTED
```

Keycloak emite tokens OIDC. A API valida assinatura, issuer, audience e expiração. O provider autorizado vem de claim; roles internas protegem carteiras e reconciliação. Locks por linha e constraints impedem lost update, saldo negativo e duplicidade.
