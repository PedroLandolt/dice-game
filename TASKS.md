# Dice Game Backend: Plan and Tasks (two-day version)

## Goal

Go backend for the even/odd dice game. Vertsa sells backends to casinos that build their own frontends, so **the API is the product**. Whoever evaluates it uses Postman and the README, not a frontend.

Priorities, in this order:

1. Runs with one command: `docker compose up`.
2. A Postman collection that covers the happy path and every protection, and can be run any number of times.
3. A consistent API: one error format, explicit codes, request id.
4. Simple code that I can explain line by line.
5. Go tests that prove the rules and the protections.

At the end of every phase the project builds and the tests pass. **The deliverable MVP is ready at the end of Phase 6.**

---

## Architecture decisions (what I will defend)

1. **The game and the money are separate.** In B2B iGaming the balance lives with the operator (the casino), not with the game provider. This model is called a *seamless wallet*. The game service only knows a wallet interface. Here the implementation is local (Postgres, with its own tables); in production it would be an HTTP client for the operator's wallet API.
2. **One logic, two transports.** WebSocket (the preference of the brief and of the interviewer; the product API) and HTTP (automated tests in Postman and back office) call the same service.
3. **The protections are in the database too.** A partial unique index allows at most one active play per client; `CHECK (balance >= 0)`; unique idempotency keys. With concurrent requests or several instances, the database refuses invalid states.
4. **Idempotency on both sides.** In the game (`requestId` / `Idempotency-Key`) and in the wallet (`txID`). A retry never debits or credits twice.
5. **The clientId comes from the token.** If the one in the request is different, the response is 403 (prevents IDOR).
6. **Data ready for volume.** The wallet ledger is append-only and partitioned by month, with indexes chosen for the real queries.
7. **A result is never shown without the money confirmed.** Before the roll, if the wallet does not answer, the debit is rolled back and the player neither wins nor loses. After the roll the flow only moves forward: the credit is idempotent and always ends up happening.

---

## Stack

- Go, latest stable version, with the standard library router (`net/http`)
- `github.com/jackc/pgx/v5` for Postgres
- `github.com/coder/websocket` for WebSocket
- `log/slog` for JSON logs
- Postgres 16+
- Tests with the standard library `testing` package
- `hey` (external tool) for the load test

## Structure

```
dice-game/
  cmd/server/          main: config, database, HTTP + WS, graceful shutdown
  cmd/play/            terminal WebSocket client for playing and demos
  internal/
    config/            env vars
    game/              rules, game service, Wallet interface
    wallet/            local wallet implementation (Postgres)
    storage/           access to plays in Postgres
    api/               HTTP and WebSocket: routes, auth, errors, middleware
  db/init.sql          schema, partitions, indexes, seed
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

## Game rules

- A die from 1 to 6 with `crypto/rand`. Even = 2, 4, 6; odd = 1, 3, 5.
- Win: payout = 2 x bet. Lose: payout = 0.
- Amounts in cents, `int64` (1000 = 10.00 EUR). The wallet has a currency (`EUR`).
- Minimum and maximum bet from env vars.

## Flow

### Play

1. If a play with this `requestId` already exists:
   - with a different `amount` or `type` → `IDEMPOTENCY_KEY_REUSED`;
   - `open` or `closed` → return the same result (the die is not rolled again);
   - `rejected` → return the same error (stored in `error_code`);
   - `pending` → `PLAY_IN_PROGRESS`.
2. Store the play as `pending`. The partial unique index on `pending`/`open` prevents a second active play, even with concurrent requests.
3. Call `wallet.Debit(clientId, amount, txID = "<playId>:debit")`.
   - Definitive refusal (insufficient balance): the play becomes `rejected` and the error is returned.
   - No clear answer (timeout, network): call `wallet.Rollback(txID)`, the play becomes `rejected` and `WALLET_UNAVAILABLE` is returned. If the rollback also fails, the play stays `pending` and the cleanup handles it.
4. Roll the die, compute the result and the payout, and mark the play as `open`.
5. Return the number, win/lose, the payout and the balance.

### EndPlay

1. Find the client's `open` play. If there is none, return `NO_OPEN_PLAY`.
2. If payout > 0, call `wallet.Credit(clientId, payout, txID = "<playId>:credit")`. It is idempotent, so a retry is safe.
3. Mark the play as `closed` and return the final balance.

### Cleanup (goroutine in the service, every few seconds)

- `pending` for more than ~10 s: rollback and `rejected`.
- `open` for more than N minutes: credit the payout and close (auto-settle).

Every wallet call gets a `context.WithTimeout`, so a slow wallet cannot hold the server.

## Protections

| Situation | Response | Where it is enforced |
|---|---|---|
| New play while another is active | 409 `PLAY_ALREADY_OPEN` | partial unique index |
| Bet larger than the balance | 422 `INSUFFICIENT_FUNDS` | conditional `UPDATE` + `CHECK` in the wallet |
| Bet <= 0, below the minimum or above the maximum | 400 `INVALID_BET_AMOUNT` | service |
| Type other than `even`/`odd` | 400 `INVALID_BET_TYPE` | service |
| Unknown fields or invalid JSON | 400 `INVALID_REQUEST` | decode |
| EndPlay without an open play | 409 `NO_OPEN_PLAY` | service |
| No token or invalid token | 401 `UNAUTHORIZED` | middleware |
| clientId different from the token | 403 `FORBIDDEN` | middleware |
| Retry of the same request | same response, balance untouched | unique `requestId` + `txID` |
| Same key, different body | 422 `IDEMPOTENCY_KEY_REUSED` | service |
| Retry while the play is still running | 409 `PLAY_IN_PROGRESS` | service |
| Concurrent requests | only one gets through | database constraints |
| Slow or down wallet | 503 `WALLET_UNAVAILABLE`, rollback, balance intact | `context.WithTimeout` + idempotent `Rollback` |
| Stuck or forgotten play | rollback or auto-settle | cleanup goroutine |
| Huge body or WS message | refused | `MaxBytesReader` / WS limit |
| Deliberately slow connections (Slowloris) | cut | `http.Server` timeouts |
| Internal error | 500 without details; the detail only goes to the log, with the request id | error handler |

---

## HTTP API

Every `/v1` route requires `Authorization: Bearer <token>`. Every response has the `X-Request-Id` header (reuses the request's one, if present, or generates a new one).

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/v1/clients/{clientId}/wallet` | — | `{ clientId, balance, currency, openPlay }` |
| POST | `/v1/clients/{clientId}/play` | `{ amount, type }` + `Idempotency-Key` header | `{ playId, rolled, result, payout, balance }` |
| POST | `/v1/clients/{clientId}/end-play` | — | `{ playId, credited, balance }` |
| GET | `/healthz` | — | 200 |
| POST | `/dev/reset` | — | restores the seed. **Only exists with `DEV_MODE=true`** |

Error format:

```json
{ "error": { "code": "INSUFFICIENT_FUNDS", "message": "bet exceeds available balance", "requestId": "..." } }
```

## WebSocket protocol

`GET /v1/ws` with `Authorization: Bearer <token>`.

Client to server:

```json
{ "type": "wallet",   "requestId": "a1" }
{ "type": "play",     "requestId": "a2", "data": { "amount": 500, "type": "even" } }
{ "type": "end_play", "requestId": "a3" }
```

Server to client:

```json
{ "type": "wallet_result",   "requestId": "a1", "data": { "balance": 10000, "currency": "EUR" } }
{ "type": "play_result",     "requestId": "a2", "data": { "rolled": 4, "result": "win", "payout": 1000, "balance": 9500 } }
{ "type": "end_play_result", "requestId": "a3", "data": { "credited": 1000, "balance": 10500 } }
{ "type": "error",           "requestId": "a2", "error": { "code": "PLAY_ALREADY_OPEN", "message": "..." } }
```

---

## Database (suggested names)

### Game side

- **api_tokens**: `token_hash` (SHA-256), `player_id`
- **plays**: `id`, `player_id`, `request_id`, `amount`, `bet_type`, `rolled`, `won`, `payout`, `status ('pending','open','closed','rejected')`, `error_code`, `created_at`, `closed_at`
  - `UNIQUE (player_id, request_id)` for game idempotency
  - `CREATE UNIQUE INDEX ... ON plays (player_id) WHERE status IN ('pending','open')` to guarantee one active play
  - **No partitions**: these unique indexes do not include the date, and Postgres does not allow them on a partitioned table.

### Wallet side (simulates the operator)

- **wallets**: `player_id`, `balance BIGINT NOT NULL CHECK (balance >= 0)`, `currency`
- **wallet_transactions**: `tx_id` (PK), `player_id`, `kind ('debit','credit','rollback')`, `amount`, `balance_after`, `created_at`
  - Used for idempotency: if the `tx_id` already exists, return the stored result.
  - Small table; in production it is cleaned after N days (as Stripe does with idempotency keys).
- **ledger_entries**: `player_id`, `tx_id`, `kind ('debit','credit','rollback','adjustment')`, `amount`, `balance_after`, `created_at`
  - `PARTITION BY RANGE (created_at)`: monthly partitions (current and following months) plus a `DEFAULT`.
  - Index `(player_id, created_at DESC)` for the player's history.
  - BRIN index on `created_at` for reports over date ranges.
  - Append-only, no unique indexes, so it partitions without problems.

Seed (EUR, fixed dev tokens used in the Postman environment):

| player_id | Balance | Role |
|---|---|---|
| `gandalf` | 100.00 EUR | happy path |
| `luffy` | 5.00 EUR | `INSUFFICIENT_FUNDS` |
| `dante` | 100.00 EUR | concurrency and spam |
| `mr-robot` | 100.00 EUR | attacks (his token on another `clientId` → 403) |

---

## Working with Claude Code

1. Plan mode at the start of every phase.
2. It proposes the names of the structs and functions; I choose or change them.
3. It implements and runs the build and the tests.
4. Before accepting: I read the name and signature of each function, try to guess what it does, and only then read the body. Questions stay in the chat.
5. At the end of the phase:
   - I explain the flow in my own words and it corrects me;
   - I make a small change myself (one or two lines) to practise;
   - I ask what can be deleted or simplified.
6. I tick the task and make the commit.

---

# TASKS

## Phase 0: Setup (Wednesday)

- [ ] `go mod init`, folder structure, `.gitignore`.
- [ ] `docker-compose.yml` with Postgres (mounting `db/init.sql`) and a healthcheck.
- [ ] `Makefile`: `up`, `down`, `run`, `test`.
- [ ] `internal/config` with defaults for dev.
- [ ] `cmd/server` starts and answers `/healthz`.

**Done when:** `curl localhost:8080/healthz` returns 200.

## Phase 1: Game rules (Wednesday)

- [ ] Domain types and domain errors.
- [ ] Roll the die with `crypto/rand`.
- [ ] Pure function: number + type → won? + payout.
- [ ] Bet validation.
- [ ] Table-driven tests (numbers 1 to 6 x even/odd, payout, validation).

**Done when:** `make test` is green.

## Phase 2: Database (Wednesday)

- [ ] `db/init.sql` with the tables, constraints, indexes, ledger partitions and seed.
- [ ] Application user (`DATABASE_URL`) without `UPDATE`/`DELETE`/`TRUNCATE` on `ledger_entries`: append-only is enforced by the database, not only by convention.
- [ ] Confirm in `psql` that the partitions exist (`\d+ ledger_entries`) and that an insert lands in the right partition.

## Phase 3: Wallet (Wednesday)

- [ ] `Wallet` interface defined in `internal/game` (Balance, Debit, Credit, Rollback, with `txID`).
- [ ] `internal/wallet`: Postgres implementation. Each operation is a transaction that:
  - [ ] checks the `tx_id` and, if it already exists, returns the stored result;
  - [ ] does the balance-conditional `UPDATE`;
  - [ ] inserts into `wallet_transactions` and `ledger_entries`.
- [ ] Integration tests (skipped without `DATABASE_URL`):
  - [ ] debit and credit change the balance and the ledger
  - [ ] a debit above the balance is refused
  - [ ] the same `txID` twice only changes the balance once
  - [ ] a rollback after a debit returns the amount only once
  - [ ] a rollback before the debit makes a late debit be refused

## Phase 4: Game service + plays storage (Wednesday night / Thursday morning)

- [ ] `internal/storage`:
  - [ ] create a `pending` play
  - [ ] mark as `open`/`rejected`/`closed`
  - [ ] find the active play
  - [ ] find by `request_id`
- [ ] Convert constraint violations into domain errors.
- [ ] Config: `MIN_BET` (5), `MAX_BET` (2000) and the wallet timeout.
- [ ] Service in `internal/game`: balance, play and end, with the flow above. Timeout on every wallet call.
- [ ] Play idempotency responses (same result, `IDEMPOTENCY_KEY_REUSED`, `PLAY_IN_PROGRESS`).
- [ ] Cleanup goroutine (old `pending` → rollback; old `open` → auto-settle), started by `main`.
- [ ] Service unit tests with a fake wallet and storage, including:
  - [ ] the wallet refuses → the play stays `rejected` and it is possible to play again
  - [ ] timeout → rollback → `rejected` → `WALLET_UNAVAILABLE` → it is possible to play again
  - [ ] same key → same result
  - [ ] same key with another body → `IDEMPOTENCY_KEY_REUSED`
- [ ] Integration test: **20 goroutines playing at the same time for the same player → exactly 1 succeeds and the balance is debited only once.**

## Phase 5: HTTP API + Postman (Thursday morning)

- [ ] Routes with `http.NewServeMux`.
- [ ] Request id middleware (`X-Request-Id` in the log, the response and the errors).
- [ ] Auth middleware (Bearer → hash → player → `context`) and clientId check (403).
- [ ] Log middleware (with the request duration) and recover. Never logs the `Authorization` header or tokens.
- [ ] Wallet, Play (`Idempotency-Key` required) and EndPlay handlers.
- [ ] `DEV_MODE` in the config (default `false`); `/dev/reset` registered only when it is `true`. Restores the balances with an `adjustment` movement in the ledger, without deleting history.
- [ ] A single place that converts domain errors into HTTP.
- [ ] `MaxBytesReader` and `DisallowUnknownFields`.
- [ ] `http.Server` with timeouts and graceful shutdown.
- [ ] Handler tests with `httptest`.
- [ ] Postman collection (v2.1 format) + environment:
  - [ ] the first request calls `/dev/reset`, so the collection can run any number of times
  - [ ] happy path: wallet → play → end-play → wallet
  - [ ] the tests read the `result` and check the balance maths, without assuming win or lose
  - [ ] one request per protection in the table
  - [ ] `Idempotency-Key` with `{{$guid}}`; a request repeated with the same key proves idempotency

**Done when:** the collection passes twice in a row against `docker compose up`.

## Phase 6: WebSocket ← deliverable MVP

- [ ] `/v1/ws` with the same auth.
- [ ] Browser-compatible auth (the browser WebSocket API does not send `Authorization`): token in `Sec-WebSocket-Protocol`, always over TLS.
- [ ] Origin check, size limit per message, ping/pong.
- [ ] Loop: decode the envelope → switch on `type` → service → reply with the same `requestId`.
- [ ] Only one goroutine writes messages to the socket.
- [ ] Test with a WS client in `httptest` playing a round.
- [ ] WS requests in Postman. If they cannot be exported in the collection, document examples in the README.

## Phase 7: Docker (Thursday)

- [ ] Multi-stage Dockerfile: build on `golang`, runtime on `distroless/static`, non-root.
- [ ] `api` service in compose, depending on a healthy Postgres, with `DEV_MODE=true`.

**Done when:** a clean clone + `docker compose up` + Postman works with nothing else.

## Phase 8: Measure and document (Thursday night)

- [ ] Load test with `hey` against Wallet (for example 2000 requests, 50 concurrent) and record p50/p99 in the README.
- [ ] README:
  - [ ] how to run it
  - [ ] the HTTP API and the WS protocol
  - [ ] the protections table
  - [ ] the architecture decisions
  - [ ] the data model (why the ledger is partitioned and the plays are not)
- [ ] **"How I would scale this"** section, answering what was asked in the interview:
  - [ ] **Concurrency**: what is done (constraints, idempotency, the 20-goroutine test).
  - [ ] **Latency**: why WS is better here, the wallet timeouts and the load test numbers.
  - [ ] **Pub/sub with NATS**: when it would come in (several instances with real-time balance; other systems reacting to plays, such as reporting, jackpots and anti-fraud; multiplayer games) and why forcing it into a single-player game makes no sense.
  - [ ] **Redis (ElastiCache)** for state shared between instances: rate limit and cache (tokens, operator config). Not for pub/sub.
  - [ ] **Data**: automatic partition creation (`pg_partman`), archiving old partitions to S3 for regulation, read replicas for reports.
  - [ ] **10-year retention**: a hot `plays` table (idempotency window) + a partitioned `plays_archive`, moved with a single `DELETE ... RETURNING` → `INSERT`.
  - [ ] **Migrations**: no tool because this is an exercise; in production, `goose` with versioned migrations.
- [ ] "Possible extensions" section, one sentence per idea.
- [ ] `docs/aws.md`, one page: ALB (supports WS) → ECS Fargate in 2 AZs → RDS Postgres Multi-AZ, Secrets Manager, ECR, CloudWatch, S3 for the ledger archive, ElastiCache (Redis) for cache and rate limit, and where NATS would come in.
- [ ] `govulncheck ./...` with no vulnerabilities.
- [ ] Re-read all the code and be able to explain every function.

## Extras (only if there is time, in this order)

- [ ] **Bet on a number** (d6), paid by RTP (e.g. 97% → 5.82x, in basis points).
- [ ] **d20**: `RollDie(sides int)` and a bet on a number from 1 to 20.
- [ ] **Operator mock**: a second Go container with its own database and wallet API; the game uses an HTTP client that implements the same `Wallet` interface. Handle the timeout with a retry using the same `txID`.
- [ ] **Rate limit** per player with `golang.org/x/time/rate` → 429 `RATE_LIMITED`.
- [ ] **"Chaos" folder in the collection**: spam, retries with the same key, and `DEV_WALLET_DELAY` (only with `DEV_MODE`) to show the timeout and the rollback live.
- [ ] **EXPLAIN ANALYZE**: put 1 million rows in the ledger with `generate_series` and show in the README the plan with and without the index, and the partition pruning.
- [ ] **NATS**: publish `balance_updated` after the commit; the WS connection subscribes to `wallet.balance.<clientId>`.
- [ ] GitHub Actions workflow (vet + `test -race` with Postgres + `govulncheck` + the collection against `docker compose`).
- [ ] **Rough idea, still to polish: two dice in a chain.** Rolling two dice costs double; if they match, it accumulates and rolls again (a chain reaction). The rules and the RTP are still to be defined.
