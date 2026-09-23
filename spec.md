# Spec funcional — desafio backend Go

## Objetivo
Serviço distribuído para processar apostas por HTTP e SQS, mantendo saldo e ledger corretos sob concorrência, entrega at-least-once, reinícios e falhas.

## Stack e decisões
- Go Modules, Uber Fx, PostgreSQL com pgx/SQL explícito, Keycloak OIDC e LocalStack.
- Money em int64 de centavos, moeda ISO 4217 e JSON decimal string com duas casas; jamais float.
- Lock pessimista por carteira com SELECT FOR UPDATE; sem lock global.
- Inbox/outbox na mesma transação; publisher com SKIP LOCKED e lease recuperável.
- UUID, UTC/RFC3339 e erros tipados. Decisões diferentes devem ser justificadas em ARCHITECTURE.md.

## Domínio
- Wallet: id, playerId, currency, balance, version, timestamps. (playerId,currency) único; saldo não negativo; versão inicia em 1 e só aumenta com mudança.
- WagerTransaction: ids interno/externo, providerId, idempotencyKey, payloadHash, player/wallet/round/game, kind, money, referências, status, failureCode, resultado original e timestamps.
- Ledger: id, walletId, transactionId, direção, money, saldo anterior/posterior e timestamp; append-only, único por carteira/transação.
- Inbox: consumerName, messageId, hash, recebimento/conclusão; único por consumidor/mensagem.
- Outbox: eventId, aggregateId, tipo, payload imutável, ocorrência, tentativas, próximo envio, lease e publicação.

Money deve validar escala fixa, overflow, moeda, vazio, negativo externo, notação científica, NaN e Infinity. Implementar parsing, zero, soma, subtração, negação, comparação e serialização.

Tipos: BET debita valor positivo e saldo suficiente; WIN credita valor positivo; LOSS exige 0.00 e não altera saldo/versão; REFUND credita integralmente uma BET processada; ROLLBACK desfaz integralmente BET, WIN ou REFUND; OPENING é somente interno. Reversões exigem referência e mesma identidade, carteira, moeda, rodada e valor. Impedir dupla reversão e usar código específico para reversão sem saldo.

Estados: PENDING, PENDING_REFERENCE, PROCESSED, REJECTED e FAILED. Terminais não mudam. Pendências devem sobreviver a reinício e ter worker com backoff/TTL.

## Atomicidade
Na mesma transação SQL: deduplicar; bloquear carteira; validar referência/saldo; atualizar saldo/versão; inserir ledger; atualizar transação e resultado original; inserir outbox; e concluir inbox no SQS. Só remover/publicar depois do commit. Constraints devem impor unicidade, saldo não negativo, ledger imutável, balanceAfter coerente e uma abertura por carteira.

## API
- POST /wallets: cria carteira única por jogador/moeda. Saldo positivo cria OPENING, ledger e eventos no mesmo commit; saldo zero não cria movimento; duplicidade 409.
- GET /wallets/{walletId}: saldo/versão.
- GET /wallets/{walletId}/ledger?cursor=&limit=: cursor opaco e ordem estável.
- GET /wagering/transactions/{transactionId} e GET /providers/{providerId}/wagering/transactions/{externalTransactionId}: respeitar autorização.
- POST /wagering/transactions: exige Bearer e Idempotency-Key; validar provider contra claim do token. Corpo inclui providerId, externalTransactionId, playerId, walletId, roundId, gameId, kind, money e referência quando necessária.
- Hash SHA-256 de JSON canônico dos campos de negócio, excluindo idempotency key/transporte. Mesma chave/hash retorna resultado original e idempotentReplay=true; hash diferente ou mesmo provider/id externo com outra chave retorna 409.
- POST /wallets/{walletId}/reconciliation: comparar saldo armazenado com soma do ledger, sem alterar dados; divergência em log/métrica.
- GET /health/live e /health/ready (processo; PostgreSQL/SQS).
- Códigos: 400 inválido, 401 não autenticado, 403 proibido, 404 inexistente, 409 conflito, 422 rejeição, 202 pendente, 503 transitório.

## Autenticação
Keycloak OIDC/client_credentials. Carteiras são internas; provider só consulta e envia operações do providerId presente nas claims. Nunca confiar apenas no corpo. Testar token ausente, inválido, expirado, roles e isolamento entre providers.

## SQS
Provisionar wager-transactions.fifo e wager-transactions-dlq.fifo com redrive. Usar messageId e hash na inbox e data.idempotencyKey na operação. Remover após commit; rejeição terminal pode ser removida; transitório faz retry; inválido/permanente/tentativas esgotadas vai à DLQ. Documentar visibility timeout, backoff, tentativas, MessageGroupId/DeduplicationId e SIGTERM seguro.

## Eventos/outbox
Publicar WagerTransactionProcessed, WagerTransactionRejected, WagerTransactionPendingReference e WalletBalanceChanged. Envelope: eventId, eventType, aggregateId, correlationId, causationId opcional, occurredAt, version e data. BalanceChanged inclui walletId, transactionId, direction, money, balanceBefore/After e walletVersion. LOSS não gera mudança de saldo. Suportar múltiplos publishers, lease, retry, recuperação e republicação com mesmo eventId.

## Arquitetura
Domínio independente de Fx/HTTP/SQS/DB. Sugestão: internal/domain, application, adapters/http, adapters/sqs, repository, events e config. Fx deve compor dependências e lifecycle deve iniciar/parar servidor, consumidores, publisher e worker de referências ordenadamente. Documentar decisões em ARCHITECTURE.md.

## Observabilidade
Logs JSON com correlationId, messageId, transactionId, walletId e providerId; nunca tokens/credenciais/payload completo. Métricas de status, duplicatas, conflitos, retries, DLQ, concorrência, latência, atraso da outbox e reconciliação.

## Testes e aceite
- Unitários: Money, overflow/moedas, carteira, estados, zero por tipo, reversões, idempotência, abertura e eventos.
- Integração real com PostgreSQL, Keycloak e LocalStack: migrations, constraints, ledger, atomicidade, inbox/outbox, retry, DLQ, auth e lifecycle.
- Mesma BET 50 vezes: um débito.
- Duas BET de 80 em saldo 100: uma processada, uma rejeitada, saldo 20 e um ledger.
- Carteiras distintas em paralelo e ao menos três processos.
- Interrupção após commit, publishers concorrentes, referência atrasada, reinício e cruzamento HTTP/SQS.
- Executar go test ./..., go test -race ./..., go vet ./... e docker compose up --build.
- Eliminatórios: sem auth, acesso cruzado, float, saldo negativo/duplicado, idempotência em memória, lock global, publicação pré-commit, ledger não auditável ou testes sem infraestrutura real.

## Ordem de implementação
1. Bootstrap/Fx/Compose. 2. Money e domínio. 3. Migrations/repositórios. 4. Carteira/ledger. 5. Transações/idempotência. 6. Reversões/referências. 7. HTTP/auth. 8. SQS/inbox. 9. Outbox. 10. Observabilidade/shutdown. 11. Testes de falha/concorrência. 12. README e ARCHITECTURE.
