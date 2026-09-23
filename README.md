# Backend Challenge Go

Serviço de processamento distribuído de apostas. Os requisitos estão em spec.md, o plano técnico em spec tecnica.md e a execução incremental em plan.md.

## Subir o ambiente
Pré-requisitos: Docker com Compose e Go 1.27+.
```bash
cp .env.example .env
docker compose up --build
```
Serviços: API em localhost:8081, Keycloak em localhost:8080 (admin/admin), PostgreSQL em localhost:5432 e LocalStack em localhost:4566.
```bash
curl http://localhost:8081/health/live
curl http://localhost:8081/health/ready
```
O realm jungle-gaming é importado de deploy/keycloak/realm-export.json. Os clients locais são provider-a, provider-b e wallet-internal. Substitua os segredos em ambientes reais.

## Desenvolvimento
```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
docker compose down
```
Testes unitários usam mocks das portas; testes de integração devem usar PostgreSQL, Keycloak e LocalStack reais.

## Documentação
spec.md contém requisitos funcionais; spec tecnica.md contém arquitetura técnica; plan.md contém tarefas/BDD; ARCHITECTURE.md contém decisões e diagramas.
