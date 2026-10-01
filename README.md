# Dice Game Backend

Backend for an even/odd dice game, written in Go with PostgreSQL. Players connect over WebSocket (the primary API) or HTTP, and both transports call the same game service. Balances live in a wallet that is kept separate from the game, as in B2B iGaming, where the operator (the casino) owns the player's money and the game provider only calls its wallet API.

## Quick start

### Requirements

| Tool | Needed for |
|---|---|
| [Docker](https://docs.docker.com/get-docker/) with Compose | running the API and the database (the only hard requirement) |
| [Postman](https://www.postman.com/downloads/) (free account) | testing the API by hand or running the collection |
| Go 1.27+ | optional: the terminal client and the Go tests |
| Node.js | optional: running the collection from the command line with Newman |

Ports `8080` (API) and `5432` (Postgres) must be free.

### Run

```bash
git clone https://github.com/PedroLandolt/dice-game.git
cd dice-game
docker compose up
```

The first start builds the API image and creates the database, which takes about a minute. The API is ready when the log shows `"msg":"server starting"`. To check it from another terminal:

```bash
curl -i http://localhost:8080/healthz   # HTTP/1.1 200 OK
```

The API listens on `http://localhost:8080` and the WebSocket endpoint is `ws://localhost:8080/v1/ws`. On the first start Postgres runs `db/init.sql`, which creates the schema and four test players. The compose file sets `DEV_MODE=true`, which enables `POST /dev/reset`.

| To | Run |
|---|---|
| stop | `Ctrl+C`, or `docker compose down` from another terminal |
| restart keeping the data | `docker compose up` |
| start again from a clean database | `docker compose down -v`, then `docker compose up` |
| restore the test balances without restarting | `curl -X POST http://localhost:8080/dev/reset` |

### Test accounts

| Player | Token | Balance | Use it for |
|---|---|---|---|
| `gandalf` | `dev-gandalf` | 100.00 EUR | happy path |
| `luffy` | `dev-luffy` | 5.00 EUR | insufficient funds |
| `dante` | `dev-dante` | 100.00 EUR | concurrency and repeated requests |
| `mr-robot` | `dev-mr-robot` | 100.00 EUR | attacks, such as using his token on another client's id |

Amounts are integer cents everywhere in the API: `500` is 5.00 EUR.

### Play from the terminal (WebSocket)

Requires Go 1.27 or newer.

```bash
make play                           # as gandalf
go run ./cmd/play -token dev-luffy  # as another player
```

```text
commands: wallet | play <cents> <even|odd> | end | quit
balance 100.00 EUR
play 1250 odd
rolled 5: win, payout 25.00 | balance 87.50
end
credited 25.00 | balance 112.50
```

### Postman

**Import** (once):

1. Open Postman and click **Import** (top left).
2. Drop both files from the `postman/` folder: `dice-game.postman_collection.json` and `dice-game.postman_environment.json`.
3. In the environment selector (top right, "No environment" by default) choose **Dice Game (local)**. It holds the base URL and the test tokens; without it every request fails.

**Run the whole collection** (what it proves):

1. In the left sidebar, hover **Dice Game API**, click **...** and then **Run collection**.
2. Click **Run Dice Game API**. All requests should pass. Run it again to see that it is repeatable: the "0. Setup" folder settles open plays and resets the balances first.

| Folder | Contents |
|---|---|
| 0. Setup | ends any open play and resets the balances |
| 1. Happy path | Wallet, Play, EndPlay and Wallet again; the tests check the balance maths without assuming a win or a loss, and a repeated Play proves idempotency |
| 2. Protections | one request per protection in the [Protections](#protections) table |
| 3. Play yourself | Wallet, Play and End play with no assertions, for playing by hand |

**Play by hand:**

1. Open **3. Play yourself**, then **Play**, and click **Send**. The response shows the number rolled, the result and the balance after the bet.
2. Open **End play** and click **Send** to collect the payout. Only then can a new play start.
3. To change the bet, open the environment (Environments in the left sidebar, then **Dice Game (local)**), set `betAmount` (cents, 5 to 2000) and `betType` (`even` or `odd`), and save.

The same run from the command line:

```bash
npx newman run postman/dice-game.postman_collection.json -e postman/dice-game.postman_environment.json
```

The collection file format cannot hold WebSocket requests; see [WebSocket in Postman](#websocket-in-postman).

## WebSocket API

### Connecting

`GET /v1/ws`, upgraded to WebSocket. Authenticate with one of:

- the header `Authorization: Bearer <token>`, for Postman and server-side clients;
- the subprotocols `dice.v1, bearer.<token>`, for browsers, whose WebSocket API cannot set headers: `new WebSocket(url, ["dice.v1", "bearer.dev-gandalf"])`. The server selects `dice.v1` and never echoes the token.

Tokens are not accepted in the query string, because URLs end up in proxy and load balancer logs. A missing or unknown token gets an HTTP 401 before the upgrade. Browser connections must come from the same host or from an origin listed in `WS_ALLOWED_ORIGINS`.

### Messages

Every message is a JSON envelope. Client to server:

```json
{ "type": "wallet",   "requestId": "w1" }
{ "type": "play",     "requestId": "p1", "data": { "amount": 500, "type": "even" } }
{ "type": "end_play", "requestId": "e1" }
```

Server to client. Each reply carries the `requestId` of the message it answers:

```json
{ "type": "wallet_result",   "requestId": "w1", "data": { "clientId": "gandalf", "balance": 10000, "currency": "EUR", "openPlay": null } }
{ "type": "play_result",     "requestId": "p1", "data": { "playId": "5f0c...", "rolled": 4, "result": "win", "payout": 1000, "balance": 9500 } }
{ "type": "end_play_result", "requestId": "e1", "data": { "playId": "5f0c...", "credited": 1000, "balance": 10500 } }
{ "type": "error",           "requestId": "p1", "error": { "code": "PLAY_ALREADY_OPEN", "message": "another play is still active" } }
```

- On connect, the server pushes a `wallet_result` without a `requestId`. It includes any play left open, for example after a dropped connection.
- The `requestId` of a `play` is its idempotency key. Resending the same message returns the original result without rolling or debiting again. The same `requestId` with a different amount or type gets `IDEMPOTENCY_KEY_REUSED`.
- Errors do not close the connection. Messages are processed one at a time, in order.
- A message larger than 1 KiB closes the connection with status 1009. The server sends a ping every 30 seconds and drops clients that do not answer.

### WebSocket in Postman

1. New, then WebSocket.
2. URL `ws://localhost:8080/v1/ws`.
3. Headers: `Authorization` with the value `Bearer dev-gandalf`.
4. Connect. The first message is the pushed wallet state.
5. Send the messages above. Use a new `requestId` for each new play.

## HTTP API

The same operations over HTTP, used by the Postman collection and suitable for back-office tools.

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/v1/clients/{clientId}/wallet` | | `{ clientId, balance, currency, openPlay }` |
| POST | `/v1/clients/{clientId}/play` | `{ "amount": 500, "type": "even" }` | `{ playId, rolled, result, payout, balance }` |
| POST | `/v1/clients/{clientId}/end-play` | | `{ playId, credited, balance }` |
| GET | `/healthz` | | 200 |
| POST | `/dev/reset` | | 204, only with `DEV_MODE=true` |

- Every `/v1` route requires `Authorization: Bearer <token>`. The `clientId` in the path must be the token's player, otherwise the response is 403.
- `POST /play` requires an `Idempotency-Key` header (1 to 128 characters). Repeating a request with the same key returns the original response.
- Every response has an `X-Request-Id` header. A valid id sent by the client (up to 64 characters of `A-Z a-z 0-9 _ -`) is reused; otherwise a new one is generated.
- Request bodies are limited to 1 KiB and unknown JSON fields are rejected.

Every error has the same shape, over HTTP and WebSocket alike:

```json
{ "error": { "code": "INSUFFICIENT_FUNDS", "message": "bet exceeds available balance", "requestId": "3KQ..." } }
```

| HTTP | Code | When |
|---|---|---|
| 400 | `INVALID_BET_AMOUNT` | amount is not between `MIN_BET` and `MAX_BET` |
| 400 | `INVALID_BET_TYPE` | type is not `even` or `odd` |
| 400 | `INVALID_REQUEST` | malformed JSON, unknown field, body too large, missing `Idempotency-Key` |
| 401 | `UNAUTHORIZED` | missing or unknown token |
| 403 | `FORBIDDEN` | `clientId` is not the token's player |
| 404 | `NOT_FOUND` | unknown route |
| 409 | `PLAY_ALREADY_OPEN` | a new play while another is still active |
| 409 | `PLAY_IN_PROGRESS` | a retry while the original request is still running |
| 409 | `NO_OPEN_PLAY` | EndPlay with nothing to end |
| 422 | `INSUFFICIENT_FUNDS` | the bet exceeds the balance |
| 422 | `IDEMPOTENCY_KEY_REUSED` | the same key with a different amount or type |
| 503 | `WALLET_UNAVAILABLE` | the wallet did not answer in time; the bet was rolled back |
| 500 | `INTERNAL_ERROR` | anything else; details are logged with the request id, never returned |

## Game rules

- One six-sided die, drawn with `crypto/rand`, which uses rejection sampling and has no modulo bias.
- The player bets on even (2, 4, 6) or odd (1, 3, 5). A win pays twice the bet; a loss pays nothing.
- Bets go from 5 to 2000 cents by default (`MIN_BET`, `MAX_BET`).
- Play debits the bet and returns the result. EndPlay credits the payout and closes the play. A new play is refused until the previous one is ended, and plays left open for 5 minutes are settled automatically.
- Paying 2x on a 1 in 2 chance gives a return to player (RTP) of 100%, so the house has no edge. A real game would pay slightly less, for example 1.94x for an RTP of 97%.

## Protections

| Situation | Response | Enforced by |
|---|---|---|
| New play while another is active | 409 `PLAY_ALREADY_OPEN` | partial unique index on active plays |
| Bet above the balance | 422 `INSUFFICIENT_FUNDS` | row lock on the wallet and `CHECK (balance >= 0)` |
| Bet of zero, negative, below the minimum or above the maximum | 400 `INVALID_BET_AMOUNT` | service, plus `CHECK (amount > 0)` |
| Type other than `even` or `odd` | 400 `INVALID_BET_TYPE` | service, plus `CHECK` constraint |
| Malformed JSON or unknown fields | 400 `INVALID_REQUEST` | strict decoder |
| EndPlay without an open play | 409 `NO_OPEN_PLAY` | service |
| Missing or invalid token | 401 `UNAUTHORIZED` | auth before any handler or upgrade |
| `clientId` of another player | 403 `FORBIDDEN` | player id taken from the token, never from the request |
| Retry of the same request | same response, balance untouched | unique `(player_id, request_id)` and wallet `tx_id` |
| Same key with a different body | 422 `IDEMPOTENCY_KEY_REUSED` | service |
| Retry while the first attempt is running | 409 `PLAY_IN_PROGRESS` | service and unique index |
| Concurrent plays for one player | exactly one succeeds | database constraints (tested with 20 goroutines) |
| Slow or unavailable wallet | 503 `WALLET_UNAVAILABLE`, bet rolled back | timeout on every wallet call and idempotent rollback |
| Play stuck or forgotten | rolled back or settled | background cleanup |
| Huge body or WebSocket message | rejected | 1 KiB limit on both |
| Slow clients holding connections (Slowloris) | disconnected | `http.Server` timeouts |
| Cross-site WebSocket connection | 403 | origin check |
| Rollback of another player's transaction | no effect | wallet transactions keyed by `(player_id, tx_id, kind)` |
| Internal error | 500 without details | single error mapping; details only in logs |

## Architecture

```text
   WebSocket  /v1/ws                     HTTP  /v1/clients/{clientId}/...
            \                                   /
             internal/api        auth, error mapping, request id, logs
                          |
             internal/game       rules, play flow, idempotency, cleanup
                /                                  \
     game.Wallet interface                  game.PlayStore interface
               |                                     |
     internal/wallet                        internal/storage
     wallets, wallet_transactions,          plays, api_tokens
     ledger_entries
```

### Design decisions

1. **The game and the money are separate.** In B2B iGaming the balance lives with the operator (the casino), not with the game provider; this is called a seamless wallet. The game only knows the `Wallet` interface: `Balance`, `Debit`, `Credit` and `Rollback`, each identified by a transaction id. Here it is backed by local Postgres tables. In production it would be an HTTP client for the operator's wallet API, with no change to the game code.
2. **One service, two transports.** WebSocket and HTTP decode a request, call the same `game.Service` and map errors with the same table, so the rules and the error codes cannot drift apart.
3. **Protections live in the database too.** A partial unique index allows one active play per player, `CHECK (balance >= 0)` makes a negative balance impossible, and unique keys make retries safe. Concurrent requests or several API instances cannot produce an invalid state.
4. **Idempotency on both sides.** The game keys plays by `(player_id, request_id)`. The wallet keys movements by transaction id (`<playId>:debit`, `<playId>:credit`). A retry never debits or credits twice.
5. **The player comes from the token.** Tokens are stored as SHA-256 hashes, and the player id is never taken from the request body or path. Registration, login and KYC belong to the operator: in production the operator issues a session token when the game is launched and the provider validates it. The four fixed tokens stand in for those sessions.
6. **No result is shown unless the money is settled.** Before the die is rolled, a wallet that does not answer is rolled back and the player neither wins nor loses. After the roll the flow only moves forward: the credit is idempotent and eventually happens.

### Play and EndPlay

**Play**

1. Look up the `requestId`. A known play returns its original result, its original error, `PLAY_IN_PROGRESS` or `IDEMPOTENCY_KEY_REUSED`.
2. Insert the play as `pending`. The partial unique index rejects a second active play, even under concurrency.
3. Debit the wallet with a timeout, using the transaction id `<playId>:debit`.
   - Insufficient funds: the play becomes `rejected` and the error is returned.
   - No clear answer (timeout or network error): roll the debit back, mark the play `rejected` and return `WALLET_UNAVAILABLE`. If the rollback also fails, the play stays `pending` for the cleanup.
4. Roll the die, compute the payout and mark the play `open`, only if it is still `pending`.
5. Return the number, the result, the payout and the balance.

**EndPlay** finds the open play, credits the payout with the transaction id `<playId>:credit` and marks the play `closed`.

**If the process crashes**

| After | State left | Who finishes it |
|---|---|---|
| inserting the play | `pending`, no debit | cleanup: rollback (records a marker) and `rejected` |
| the debit | `pending`, debited | cleanup: rollback (refunds the bet) and `rejected`; the player never saw a result |
| the rollback | `pending`, refunded | cleanup repeats the rollback (idempotent) and marks `rejected` |
| marking `open` | `open` with a result | a retry returns the same result; EndPlay or the cleanup pays it |
| the credit | `open`, credited | a retried EndPlay credits again (idempotent) and closes; or the cleanup does |

### Cleanup

A goroutine runs every 5 seconds:

- plays `pending` for more than 10 seconds are rolled back and marked `rejected`;
- plays `open` for more than 5 minutes are credited and marked `closed`.

Every step is idempotent, so running it twice, or on several instances at once, is safe. The 10 second threshold is far above the 2 second wallet timeout, so the cleanup never touches a request that is still running, and marking a play `open` only succeeds while it is still `pending`.

### Wallet operations

Each wallet operation is one database transaction: lock the player's wallet row (`SELECT ... FOR UPDATE`), look up the transaction id, update the balance, and insert into `wallet_transactions` and `ledger_entries`. A crash before the commit leaves nothing behind; after the commit, a retry finds the transaction id and returns the stored result.

A rollback can arrive before the debit it cancels (the debit timed out on our side but is still travelling). In that case the rollback records a marker, and the late debit is refused.

Wallet calls use `context.WithTimeout`. The steps that undo or record money (rollback, marking a play) use `context.WithoutCancel`, so a client that disconnects mid-request cannot interrupt them.

## Data model

All of it is in [`db/init.sql`](db/init.sql).

**Game side**

- `api_tokens`: `token_hash` (SHA-256) and `player_id`.
- `plays`: one row per bet, with `status` (`pending`, `open`, `closed`, `rejected`), the roll, the payout, the balance after the debit and the error code of a rejected play.

**Wallet side** (stands in for the operator)

- `wallets`: balance per player, `CHECK (balance >= 0)`.
- `wallet_transactions`: one row per `(player_id, tx_id, kind)`, used for idempotency.
- `ledger_entries`: every money movement, append-only.

There are no foreign keys between the two sides on purpose: in production the wallet is another company's system.

### Why the ledger is partitioned and the plays are not

The ledger only grows, and regulation usually requires keeping money movements for years (often 10). It is partitioned by month: queries by date only read the months they need (partition pruning), each partition's indexes stay small, and an old month can be detached (an instant metadata change) and archived. `init.sql` creates the current month, the next five and a default partition; production would use `pg_partman` to keep creating them.

`plays` needs `UNIQUE (player_id, request_id)` and the unique index on active plays. On a partitioned table every unique index must include the partition key, which would break both guarantees. At scale, plays would be kept as a small hot table and moved to a partitioned archive (see [How I would scale this](#how-i-would-scale-this)).

### Indexes

Each index serves a query the code runs; there is none "just in case", since every index costs a write on every bet.

| Index | Query |
|---|---|
| `plays_player_request_unique (player_id, request_id)` | idempotency lookup |
| `plays_one_active_per_player (player_id) WHERE status IN ('pending','open')` | open play lookup and the one-active-play rule |
| `plays_active_created_at (created_at) WHERE status IN ('pending','open')` | cleanup; only active plays, so it stays small |
| `ledger_entries_player_created (player_id, created_at DESC)` | a player's history |
| `ledger_entries_created_brin` (BRIN on `created_at`) | reports by date range; a few KB for millions of rows |

### Integrity rules

- **The ledger adds up.** A `CHECK` ties the sign to the kind (debit negative; credit and rollback positive), and the seed writes an opening `adjustment`, so `SUM(amount)` per player always equals the wallet balance. The wallet tests assert this after every operation.
- **The ledger is append-only by permission.** The application connects as `dice_app`, which can only `SELECT` and `INSERT` on `ledger_entries` and has no `DELETE` on any table. The schema owner is a different role.
- **Timestamps come from the database.** `TIMESTAMPTZ DEFAULT now()`: one clock for every instance, stored in UTC. `now()` is the transaction time, so the debit, its wallet transaction and its ledger entry share the same instant.
- **Constraint names are explicit**, and the storage layer maps them to domain errors by name, never by parsing messages.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `DATABASE_URL` | required | Postgres connection string |
| `HTTP_ADDR` | `:8080` | listen address |
| `MIN_BET` | `5` | minimum bet in cents |
| `MAX_BET` | `2000` | maximum bet in cents |
| `WALLET_TIMEOUT` | `2s` | timeout of each wallet call |
| `DEV_MODE` | `false` | `true` enables `POST /dev/reset` |
| `WS_ALLOWED_ORIGINS` | empty | comma-separated origins allowed to open a WebSocket from a browser, e.g. `casino.example.com,*.casino.example.com`; empty means same host only |

The passwords in `docker-compose.yml` and the `dev-*` tokens are for local development only.

## Development

```text
cmd/server/       main: config, database, HTTP and WebSocket server, graceful shutdown
cmd/play/         terminal WebSocket client
internal/config/  environment variables
internal/game/    rules, service, Wallet and PlayStore interfaces
internal/wallet/  Postgres wallet
internal/storage/ Postgres plays and tokens
internal/api/     HTTP and WebSocket handlers, auth, errors, middleware
db/init.sql       schema, partitions, indexes, roles and seed
postman/          collection and environment
```

| Command | What it does |
|---|---|
| `make up` | build and start the API and Postgres |
| `make db` | start only Postgres, to run the API with `make run` |
| `make run` | run the API locally with `DEV_MODE=true` |
| `make play` | terminal WebSocket client |
| `make test` | all tests, including integration tests against the local database |
| `make test-race` | all tests with the race detector, inside the `golang` container (the race detector needs cgo) |
| `make down` | stop the containers |

Tests:

- **Unit**: game rules, the play service with a fake wallet and store (wallet refusal, timeout and rollback, retries, cleanup), and the HTTP and WebSocket handlers with `httptest`.
- **Integration**: wallet (idempotency, rollback before and after the debit, the ledger total, another player's transaction id), plays storage and constraints, and 20 goroutines playing at once for the same player (exactly one succeeds and the balance is debited once). They are skipped when `DATABASE_URL` is not set; `make test` sets it to the local database.

Static checks: `gofmt`, `go vet`, `golangci-lint run` and `govulncheck ./...`.

## Performance

`hey`, 2000 requests, 50 concurrent, against `GET /v1/clients/gandalf/wallet` on the `docker compose` stack (Docker Desktop on Windows, 24 cores):

| | Run 1 | Run 2 |
|---|---|---|
| Requests per second | ~10,400 | ~11,800 |
| p50 | 2.3 ms | 2.6 ms |
| p99 | 73 ms | 47 ms |
| Responses | 2000 x 200 | 2000 x 200 |

Each request runs three queries: the token, the balance and the open play. The p99 moves between runs; the first run includes warming up the connection pool. These are numbers from a development machine, useful as a baseline rather than as a production figure.

```bash
go run github.com/rakyll/hey@latest -n 2000 -c 50 \
  -H "Authorization: Bearer dev-gandalf" \
  http://localhost:8080/v1/clients/gandalf/wallet
```

## How I would scale this

### Concurrency

Already in place: the database constraints, a row lock per wallet (players never block each other), idempotency on both sides, and a test with 20 concurrent plays. Running several API instances does not change correctness. The cleanup would read stale plays with `FOR UPDATE SKIP LOCKED`, so instances split the work instead of repeating it.

### Latency

WebSocket pays the handshake and the token lookup once per session instead of once per request, and lets the server push state. The wallet timeout bounds the worst case of every bet. The next steps are measuring p99 per message type, sizing the connection pool, and keeping the API and the database in the same availability zone.

### Events: NATS

Pub/sub becomes useful when there are several instances and a balance change must reach the instance holding the player's WebSocket, when other systems react to plays (reporting, jackpots, anti-fraud), or for multiplayer games. Events would be published after the commit through a transactional outbox, so none is lost. This single-player request/response game has no consumer for them yet, so NATS is not forced in.

Redis (ElastiCache) would hold shared state between instances: rate limits and a cache of tokens and operator configuration. Not balances, whose source of truth is the database, and not pub/sub, so the two workloads do not affect each other's latency.

### Data

- `pg_partman` keeps creating monthly ledger partitions ahead of time; the default partition is monitored and stays empty.
- Old ledger months are detached (an instant metadata change) and archived to S3 for the retention period.
- Ten years of plays: `plays` keeps only the idempotency window (for example 30 days), and one statement moves older rows to a partitioned `plays_archive`:

  ```sql
  WITH moved AS (
      DELETE FROM plays
      WHERE status IN ('closed', 'rejected') AND created_at < now() - INTERVAL '30 days'
      RETURNING *
  )
  INSERT INTO plays_archive SELECT * FROM moved;
  ```

- `wallet_transactions` exists for idempotency and can expire after some days; the ledger is the legal record.
- Read replicas serve reports and back-office queries.
- Versioned migrations (for example `goose`) replace the single `init.sql`, which only runs on an empty database.

### Infrastructure

See [docs/aws.md](docs/aws.md).

## Known limitations

- A request with the wrong method on an existing path returns 404 `NOT_FOUND` instead of 405: a catch-all route keeps every error in the JSON format.
- A WebSocket connection from a browser origin that is not allowed gets a plain-text 403 from the WebSocket library, not the JSON error body.
- Graceful shutdown waits for HTTP requests but not for open WebSocket connections, which close when the process exits; clients reconnect, and the money is safe through idempotency and the cleanup.
- `POST /dev/reset` restores balances but leaves open plays as they are.
- There is no rate limiting, and the test tokens never expire.

## Possible extensions

Ideas for the game:

- Bets on a specific number, paid according to a target RTP (for example 5.82x on a d6 for 97%), using basis points to keep integer money.
- Other dice, such as a d20 with bets from 1 to 20.
- Two dice for a higher stake, with a chain bonus when both match.
- Payouts and limits configured per operator.
- Provably fair results: a hashed server seed published in advance, combined with a client seed and a nonce.

Ideas for the platform:

- A mock operator: a second service with its own database and wallet API, reached through an HTTP client that implements the same `Wallet` interface, retrying with the same transaction id, and issuing session tokens that expire.
- Rate limiting per player (429).
- Balance updates pushed to open WebSockets through NATS, using a transactional outbox.
- Metrics and tracing: p99 per message type, wallet errors, open plays.
- A "chaos" folder in the collection with an artificial wallet delay, to show the timeout and the rollback live.
- `EXPLAIN ANALYZE` on a ledger with millions of rows, to show partition pruning and the indexes at work.

## About this project

Written as a technical exercise for a backend role. It was developed with AI assistance (Claude Code) under the working rules in [CLAUDE.md](CLAUDE.md): standard library first, no unnecessary dependencies, small reviewed steps and tests for every rule. I reviewed every change and can explain each design decision. The original plan, in Portuguese, is in [TASKS.md](TASKS.md).

Licensed under the [MIT License](LICENSE).
