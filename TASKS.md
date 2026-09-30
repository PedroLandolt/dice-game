# Dice Game Backend: Plano e Tarefas (versão de 2 dias)

## Objetivo

Backend em Go para o jogo de dados (par/ímpar). A Vertsa vende backends a casinos que fazem o seu próprio frontend, por isso **a API é o produto**. Quem a avalia usa o Postman e o README, não um frontend.

Prioridades, por esta ordem:

1. Correr com um comando: `docker compose up`.
2. Collection do Postman que cobre o happy path e cada proteção, e que se pode correr várias vezes.
3. API consistente: formato de erro único, códigos explícitos, request id.
4. Código simples que eu consiga explicar linha a linha.
5. Testes em Go que provam as regras e as proteções.

No fim de cada fase o projeto compila e os testes passam. **O MVP entregável fica pronto no fim da Fase 6.**

---

## Decisões de arquitetura (o que vou defender)

1. **O jogo e o dinheiro estão separados.** No iGaming B2B o saldo vive no operador (casino), não no fornecedor do jogo. Este modelo chama-se *seamless wallet*. O serviço de jogo só conhece uma interface de wallet. Aqui a implementação é local (Postgres, com tabelas próprias); em produção seria um cliente HTTP para a API de wallet do operador.
2. **Uma só lógica e dois transportes.** WebSocket (obrigatório no enunciado; a API do produto) e HTTP (testes automáticos no Postman e back-office) chamam o mesmo serviço.
3. **As proteções também estão na BD.** Índice único parcial = no máximo uma jogada ativa por cliente; `CHECK (balance >= 0)`; chaves únicas de idempotência. Com pedidos concorrentes ou várias instâncias, a BD recusa estados inválidos.
4. **Idempotência nos dois lados.** No jogo (`requestId` / `Idempotency-Key`) e na wallet (`txID`). Um retry nunca debita nem credita duas vezes.
5. **O clientId vem do token.** Se o do pedido for diferente, a resposta é 403 (evita IDOR).
6. **Dados preparados para volume.** O ledger da wallet é append-only e particionado por mês, com índices escolhidos para as queries reais.
7. **Nunca se mostra um resultado sem o dinheiro confirmado.** Antes do dado, se a wallet não responder, faz-se rollback e o jogador não ganha nem perde. Depois do dado, só se avança: o crédito é idempotente e acaba sempre por acontecer.

---

## Stack

- Go, versão estável mais recente, com o router da standard library (`net/http`)
- `github.com/jackc/pgx/v5` para Postgres
- `github.com/coder/websocket` para WebSocket
- `log/slog` para logs em JSON
- Postgres 16+
- Testes com o package `testing` da stdlib
- `hey` (ferramenta externa) para o load test

## Estrutura

```
dice-game/
  cmd/server/          main: config, BD, HTTP + WS, graceful shutdown
  internal/
    config/            env vars
    game/              regras, serviço de jogo, interface Wallet
    wallet/            implementação local da wallet (Postgres)
    storage/           acesso às jogadas no Postgres
    api/               HTTP e WebSocket: rotas, auth, erros, middleware
  db/init.sql          schema, partições, índices, seed
  postman/             collection + environment
  docs/aws.md
  Dockerfile
  docker-compose.yml
  Makefile
  README.md
  CLAUDE.md
  TASKS.md
```

---

## Regras do jogo

- Dado de 1 a 6 com `crypto/rand`. Par = 2, 4, 6; ímpar = 1, 3, 5.
- Ganhar: payout = 2 × aposta. Perder: payout = 0.
- Valores em cêntimos, `int64` (1000 = 10,00 €). A wallet tem moeda (`EUR`).
- Aposta mínima e máxima por env var.

## Fluxo

### Play

1. Se já existe uma jogada com este `requestId`:
   - com `amount` ou `type` diferentes → `IDEMPOTENCY_KEY_REUSED`;
   - `open` ou `closed` → devolve o mesmo resultado (o dado não é relançado);
   - `rejected` → devolve o mesmo erro (guardado em `error_code`);
   - `pending` → `PLAY_IN_PROGRESS`.
2. Grava a jogada como `pending`. O índice único parcial sobre `pending`/`open` impede uma segunda jogada ativa, mesmo em pedidos concorrentes.
3. Chama `wallet.Debit(clientId, amount, txID = "<playId>:debit")`.
   - Recusa definitiva (saldo insuficiente): a jogada passa a `rejected` e o erro é devolvido.
   - Sem resposta clara (timeout, rede): chama `wallet.Rollback(txID)`, a jogada passa a `rejected` e devolve `WALLET_UNAVAILABLE`. Se o rollback também falhar, a jogada fica `pending` e a limpeza trata dela.
4. Lança o dado, calcula o resultado e o payout, e marca a jogada como `open`.
5. Devolve o número, win/lose, o payout e o saldo.

### EndPlay

1. Busca a jogada `open` do cliente. Se não existir, devolve `NO_OPEN_PLAY`.
2. Se payout > 0, chama `wallet.Credit(clientId, payout, txID = "<playId>:credit")`. É idempotente, por isso um retry é seguro.
3. Marca a jogada como `closed` e devolve o saldo final.

### Limpeza (goroutine no serviço, a cada poucos segundos)

- `pending` com mais de ~10 s: rollback e `rejected`.
- `open` com mais de N minutos: credita o payout e fecha (auto-settle).

Cada chamada à wallet leva um `context.WithTimeout`, para uma wallet lenta não prender o servidor.

## Proteções

| Situação | Resposta | Onde é garantido |
|---|---|---|
| Nova jogada com outra ativa | 409 `PLAY_ALREADY_OPEN` | índice único parcial |
| Aposta maior que o saldo | 422 `INSUFFICIENT_FUNDS` | `UPDATE` condicional + `CHECK` na wallet |
| Aposta <= 0, abaixo do mínimo ou acima do máximo | 400 `INVALID_BET_AMOUNT` | serviço |
| Tipo diferente de `even`/`odd` | 400 `INVALID_BET_TYPE` | serviço |
| Campos desconhecidos ou JSON inválido | 400 `INVALID_REQUEST` | decode |
| EndPlay sem jogada aberta | 409 `NO_OPEN_PLAY` | serviço |
| Sem token ou token inválido | 401 `UNAUTHORIZED` | middleware |
| clientId diferente do token | 403 `FORBIDDEN` | middleware |
| Retry do mesmo pedido | mesma resposta, sem mexer outra vez no saldo | `requestId` + `txID` únicos |
| Mesma key, body diferente | 422 `IDEMPOTENCY_KEY_REUSED` | serviço |
| Retry com a jogada ainda em curso | 409 `PLAY_IN_PROGRESS` | serviço |
| Pedidos concorrentes | só um passa | constraints na BD |
| Wallet lenta ou em baixo | 503 `WALLET_UNAVAILABLE`, rollback, saldo intacto | `context.WithTimeout` + `Rollback` idempotente |
| Jogada presa ou esquecida | rollback ou auto-settle | goroutine de limpeza |
| Body ou mensagem WS enorme | recusado | `MaxBytesReader` / limite WS |
| Ligações lentas de propósito (Slowloris) | cortadas | timeouts no `http.Server` |
| Erro interno | 500 sem detalhes; o detalhe fica só no log, com o request id | handler de erros |

---

## API HTTP

Todas as rotas `/v1` exigem `Authorization: Bearer <token>`. Todas as respostas têm o header `X-Request-Id` (reutiliza o do pedido, se vier um, ou gera um novo).

| Método | Path | Body | Resposta |
|---|---|---|---|
| GET | `/v1/clients/{clientId}/wallet` | — | `{ clientId, balance, currency, openPlay }` |
| POST | `/v1/clients/{clientId}/play` | `{ amount, type }` + header `Idempotency-Key` | `{ playId, rolled, result, payout, balance }` |
| POST | `/v1/clients/{clientId}/end-play` | — | `{ playId, credited, balance }` |
| GET | `/healthz` | — | 200 |
| POST | `/dev/reset` | — | repõe o seed. **Só existe com `DEV_MODE=true`** |

Formato de erro:

```json
{ "error": { "code": "INSUFFICIENT_FUNDS", "message": "bet exceeds available balance", "requestId": "..." } }
```

## Protocolo WebSocket

`GET /v1/ws` com `Authorization: Bearer <token>`.

Cliente para servidor:

```json
{ "type": "wallet",   "requestId": "a1" }
{ "type": "play",     "requestId": "a2", "data": { "amount": 500, "type": "even" } }
{ "type": "end_play", "requestId": "a3" }
```

Servidor para cliente:

```json
{ "type": "wallet_result",   "requestId": "a1", "data": { "balance": 10000, "currency": "EUR" } }
{ "type": "play_result",     "requestId": "a2", "data": { "rolled": 4, "result": "win", "payout": 1000, "balance": 9500 } }
{ "type": "end_play_result", "requestId": "a3", "data": { "credited": 1000, "balance": 10500 } }
{ "type": "error",           "requestId": "a2", "error": { "code": "PLAY_ALREADY_OPEN", "message": "..." } }
```

---

## Base de dados (nomes sugeridos)

### Lado do jogo

- **api_tokens**: `token_hash` (SHA-256), `player_id`
- **plays**: `id`, `player_id`, `request_id`, `amount`, `bet_type`, `rolled`, `won`, `payout`, `status ('pending','open','closed','rejected')`, `error_code`, `created_at`, `closed_at`
  - `UNIQUE (player_id, request_id)` para a idempotência do jogo
  - `CREATE UNIQUE INDEX ... ON plays (player_id) WHERE status IN ('pending','open')` para garantir uma jogada ativa
  - **Sem partições**: estes índices únicos não incluem a data, e o Postgres não os permite numa tabela particionada.

### Lado da wallet (simula o operador)

- **wallets**: `player_id`, `balance BIGINT NOT NULL CHECK (balance >= 0)`, `currency`
- **wallet_transactions**: `tx_id` (PK), `player_id`, `kind ('debit','credit','rollback')`, `amount`, `balance_after`, `created_at`
  - Serve para a idempotência: se o `tx_id` já existe, devolve o resultado guardado.
  - Tabela pequena; em produção limpa-se ao fim de N dias (é o que o Stripe faz com as idempotency keys).
- **ledger_entries**: `player_id`, `tx_id`, `kind ('debit','credit','rollback','adjustment')`, `amount`, `balance_after`, `created_at`
  - `PARTITION BY RANGE (created_at)`: partições mensais (mês atual e seguintes) mais uma `DEFAULT`.
  - Índice `(player_id, created_at DESC)` para o histórico do jogador.
  - Índice BRIN em `created_at` para relatórios por intervalo de datas.
  - Append-only, sem índices únicos, por isso particiona sem problemas.

Seed (EUR, tokens de dev fixos usados no environment do Postman):

| player_id | Saldo | Papel |
|---|---|---|
| `gandalf` | 100,00 € | happy path |
| `luffy` | 5,00 € | `INSUFFICIENT_FUNDS` |
| `dante` | 100,00 € | concorrência e spam |
| `mr-robot` | 100,00 € | ataques (token dele no `clientId` de outro → 403) |

---

## Como trabalhar com o Claude Code

1. Plan mode no início de cada fase.
2. Ele propõe os nomes das structs e das funções; eu escolho ou mudo.
3. Ele implementa e corre o build e os testes.
4. Antes de aceitar: leio o nome e a assinatura de cada função, tento adivinhar o que faz e só depois leio o corpo. As dúvidas ficam no chat.
5. No fim da fase:
   - explico-lhe o fluxo por palavras minhas e ele corrige;
   - faço eu uma alteração pequena (uma ou duas linhas) para praticar;
   - pergunto o que se pode apagar ou simplificar.
6. Marco a tarefa e faço o commit.

---

# TAREFAS

## Fase 0: Setup (quarta)

- [ ] `go mod init`, estrutura de pastas, `.gitignore`.
- [ ] `docker-compose.yml` com Postgres (a montar `db/init.sql`) e healthcheck.
- [ ] `Makefile`: `up`, `down`, `run`, `test`.
- [ ] `internal/config` com valores por defeito para dev.
- [ ] `cmd/server` arranca e responde a `/healthz`.

**Pronto quando:** `curl localhost:8080/healthz` devolve 200.

## Fase 1: Regras do jogo (quarta)

- [ ] Tipos do domínio e erros do domínio.
- [ ] Lançar o dado com `crypto/rand`.
- [ ] Função pura: número + tipo → ganhou? + payout.
- [ ] Validação da aposta.
- [ ] Testes table-driven (números 1 a 6 × even/odd, payout, validação).

**Pronto quando:** `make test` está verde.

## Fase 2: Base de dados (quarta)

- [ ] `db/init.sql` com as tabelas, constraints, índices, partições do ledger e seed.
- [ ] Utilizador da app (`DATABASE_URL`) sem `UPDATE`/`DELETE`/`TRUNCATE` em `ledger_entries`: o append-only é garantido pela BD, não só por convenção.
- [ ] Confirmar no `psql` que as partições existem (`\d+ ledger_entries`) e que um insert vai parar à partição certa.

## Fase 3: Wallet (quarta)

- [ ] Interface `Wallet` definida em `internal/game` (Balance, Debit, Credit, Rollback, com `txID`).
- [ ] `internal/wallet`: implementação Postgres. Cada operação é uma transação que:
  - [ ] verifica o `tx_id` e, se já existe, devolve o resultado guardado;
  - [ ] faz o `UPDATE` condicional ao saldo;
  - [ ] insere em `wallet_transactions` e em `ledger_entries`.
- [ ] Testes de integração (saltados sem `DATABASE_URL`):
  - [ ] debit e credit alteram o saldo e o ledger
  - [ ] debit acima do saldo é recusado
  - [ ] o mesmo `txID` duas vezes só altera o saldo uma vez
  - [ ] rollback depois de um debit devolve o valor uma só vez
  - [ ] rollback antes do debit faz com que um debit atrasado seja recusado

## Fase 4: Serviço de jogo + storage das jogadas (quarta à noite / quinta de manhã)

- [ ] `internal/storage`:
  - [ ] criar jogada `pending`
  - [ ] marcar como `open`/`rejected`/`closed`
  - [ ] buscar a jogada ativa
  - [ ] buscar por `request_id`
- [ ] Converter as violações de constraint em erros do domínio.
- [ ] Config: `MIN_BET` (5), `MAX_BET` (2000) e o timeout da wallet.
- [ ] Serviço em `internal/game`: saldo, jogar e terminar, com o fluxo acima. Timeout em cada chamada à wallet.
- [ ] Respostas de idempotência do Play (mesmo resultado, `IDEMPOTENCY_KEY_REUSED`, `PLAY_IN_PROGRESS`).
- [ ] Goroutine de limpeza (`pending` antigas → rollback; `open` antigas → auto-settle), arrancada pelo `main`.
- [ ] Testes unitários do serviço com a wallet e o storage fake, incluindo:
  - [ ] a wallet recusa → a jogada fica `rejected` e é possível jogar outra vez
  - [ ] timeout → rollback → `rejected` → `WALLET_UNAVAILABLE` → é possível jogar outra vez
  - [ ] mesma key → mesmo resultado
  - [ ] mesma key com outro body → `IDEMPOTENCY_KEY_REUSED`
- [ ] Teste de integração: **20 goroutines a fazer Play ao mesmo tempo para o mesmo jogador → exatamente 1 tem sucesso e o saldo só é debitado uma vez.**

## Fase 5: API HTTP + Postman (quinta de manhã)

- [ ] Rotas com `http.NewServeMux`.
- [ ] Middleware de request id (`X-Request-Id` no log, na resposta e nos erros).
- [ ] Middleware de auth (Bearer → hash → jogador → `context`) e verificação do clientId (403).
- [ ] Middleware de log (com a duração do pedido) e de recover. Nunca regista o header `Authorization` nem tokens.
- [ ] Handlers Wallet, Play (`Idempotency-Key` obrigatório) e EndPlay.
- [ ] `DEV_MODE` no config (default `false`); `/dev/reset` registado só quando está a `true`. Repõe os saldos com um movimento `adjustment` no ledger, sem apagar histórico.
- [ ] Um único sítio que converte os erros do domínio em HTTP.
- [ ] `MaxBytesReader` e `DisallowUnknownFields`.
- [ ] `http.Server` com timeouts e graceful shutdown.
- [ ] Testes dos handlers com `httptest`.
- [ ] Collection do Postman (formato v2.1) + environment, confirmada a importar e a correr no Insomnia:
  - [ ] o primeiro pedido chama `/dev/reset`, para a collection se poder correr várias vezes
  - [ ] happy path: wallet → play → end-play → wallet
  - [ ] os testes leem o `result` e verificam a conta do saldo, sem assumir win ou lose
  - [ ] um pedido por cada proteção da tabela
  - [ ] `Idempotency-Key` com `{{$guid}}`; um pedido repetido com a mesma key prova a idempotência

**Pronto quando:** a collection passa duas vezes seguidas contra o `docker compose up`.

## Fase 6: WebSocket ← MVP entregável

- [ ] `/v1/ws` com o mesmo middleware de auth.
- [ ] Auth compatível com browsers (a API de WebSocket não envia `Authorization`): token na query string ou em `Sec-WebSocket-Protocol`, sempre sobre TLS.
- [ ] Origin check, limite de tamanho por mensagem, ping/pong.
- [ ] Loop: decode do envelope → switch pelo `type` → serviço → resposta com o mesmo `requestId`.
- [ ] Só uma goroutine escreve no socket.
- [ ] Teste com um cliente WS em `httptest` a jogar uma ronda.
- [ ] Pedidos WS no Postman. Se não der para os exportar na collection, documentar exemplos no README.

## Fase 7: Docker (quinta)

- [ ] Dockerfile multi-stage: build em `golang`, runtime em `distroless/static`, non-root.
- [ ] Serviço `api` no compose, dependente do Postgres saudável, com `DEV_MODE=true`.

**Pronto quando:** um clone limpo + `docker compose up` + Postman funciona sem mais nada.

## Fase 8: Medir e documentar (quinta à noite)

- [ ] Load test com `hey` contra o Wallet (por exemplo, 2000 pedidos, 50 em simultâneo) e registar o p50/p99 no README.
- [ ] README:
  - [ ] como correr
  - [ ] a API HTTP e o protocolo WS
  - [ ] a tabela de proteções
  - [ ] as decisões de arquitetura
  - [ ] o modelo de dados (porque é que o ledger é particionado e as jogadas não)
- [ ] Secção **"Como escalaria isto"**, a responder ao que ele perguntou na entrevista:
  - [ ] **Concorrência**: o que está feito (constraints, idempotência, o teste das 20 goroutines).
  - [ ] **Latência**: porque é que WS é melhor aqui, os timeouts na wallet e os números do load test.
  - [ ] **Pub/sub com NATS**: quando entraria (várias instâncias com saldo em tempo real; outros sistemas a reagir às jogadas, como relatórios, jackpots e antifraude; jogos multiplayer) e porque não faz sentido forçá-lo num jogo single-player.
  - [ ] **Redis (ElastiCache)** para estado partilhado entre instâncias: rate limit e cache (tokens, config dos operadores). Não para pub/sub.
  - [ ] **Dados**: criação automática de partições (`pg_partman`), arquivo das partições antigas para o S3 por causa da regulação, read replicas para relatórios.
  - [ ] **Retenção de 10 anos**: `plays` quente (janela de idempotência) + `plays_archive` particionada, com a mudança num só `DELETE ... RETURNING` → `INSERT`.
  - [ ] **Migrações**: sem ferramenta por ser um exercício; em produção, `goose` com migrações versionadas.
- [ ] Secção "O que faria a seguir", uma frase por item:
  - [ ] wallet real do operador por HTTP, com rollback
  - [ ] transactional outbox
  - [ ] rate limiting (se o extra não for feito)
  - [ ] RTP configurável (2x = RTP 100%)
  - [ ] RNG auditável / provably fair
  - [ ] métricas
  - [ ] CI
- [ ] `docs/aws.md`, uma página: ALB (suporta WS) → ECS Fargate em 2 AZs → RDS Postgres Multi-AZ, Secrets Manager, ECR, CloudWatch, S3 para o arquivo do ledger, ElastiCache (Redis) para cache e rate limit, e onde entraria o NATS.
- [ ] `govulncheck ./...` sem vulnerabilidades.
- [ ] Reler o código todo e conseguir explicar cada função.

## Extras (só se sobrar tempo, por esta ordem)

- [ ] **Aposta num número** (d6), com pagamento por RTP (ex.: 97% → 5,82×, em basis points).
- [ ] **d20**: `RollDie(sides int)` e aposta num número de 1 a 20.
- [ ] **Mock do operador**: um segundo container Go com a sua própria BD e a API de wallet; o jogo passa a usar um cliente HTTP que implementa a mesma interface `Wallet`. Tratar o timeout com retry usando o mesmo `txID`.
- [ ] **Rate limit** por jogador com `golang.org/x/time/rate` → 429 `RATE_LIMITED`.
- [ ] **Pasta "caos" na collection**: spam, retries com a mesma key, e `DEV_WALLET_DELAY` (só com `DEV_MODE`) para mostrar o timeout e o rollback ao vivo.
- [ ] **EXPLAIN ANALYZE**: meter 1 milhão de linhas no ledger com `generate_series` e mostrar no README o plano com e sem índice, e o partition pruning.
- [ ] **NATS**: publicar `balance_updated` depois do commit; a ligação WS subscreve `wallet.balance.<clientId>`.
- [ ] Workflow de GitHub Actions (vet + `test -race` com Postgres + `govulncheck`), só se for mandar o repo.
- [ ] **Ideia em bruto, por polir: dois dados em cadeia.** Lançar dois dados custa o dobro; se saírem iguais, acumula e lança outra vez (reação em cadeia). Falta definir as regras e o RTP.
