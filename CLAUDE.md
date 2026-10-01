# Project rules

Context: technical exercise for a Go backend role (iGaming). I am learning Go and must be able to explain every line of this code in an interview. The plan and the tasks are in `TASKS.md`.

## Code

- Simple, idiomatic Go that someone starting with Go can follow. Always prefer the standard library.
- Dependencies: only those listed in `TASKS.md`. For any other one, ask first and explain why.
- **No comments in the code.** None. The code must explain itself through names and structure.
- Short functions with a single responsibility. No abstractions "for the future", generics, reflection, builders, factories or other enterprise patterns.
- Interfaces only where two real implementations exist (for example the real one and the test fake), defined in the package that uses them.
- Structs only with the fields they need.
- Errors: return them with context (`fmt.Errorf("...: %w", err)`); domain errors as `var ErrX = errors.New("...")`; never `panic` for control flow.
- Money is always `int64` in cents. Never `float`.
- Randomness always from `crypto/rand`.
- `context.Context` as the first argument of everything that does I/O.
- `gofmt`, `go vet` and `golangci-lint` must pass.

## Simple and deliberate

Code is read far more often than it is written; every line should be there for a reason.

- Less is better. Before adding code, ask whether the task really needs it.
- No `Manager`, `Helper`, `Util`, generic `Handler`, `utils`/`common`/`helpers` packages, or wrappers that only call another function.
- No dead code: unused config options, functions that are never called, `TODO`s, ignored parameters.
- No defensive checks for impossible situations.
- Logs only at meaningful points (start and end of a request, errors), not at every step.
- One style across the whole project: the same way of handling errors, decoding JSON and writing responses, always.
- Error messages in lowercase, without final punctuation (Go convention).
- Small diffs. If a change goes beyond ~150 lines, split it into steps.
- Documentation (README, docs) in a technical, direct tone: no emojis, no marketing phrases, no empty sections.
- At the end of each phase, list what can be deleted or simplified without losing functionality.

## Database and flow

- The whole schema is in `db/init.sql`; there is no migration tool. Postgres only runs this file when the volume is created, so after changing the schema you need `docker compose down -v && docker compose up -d`. Always tell me when that is needed.
- Do not change the order of the Play flow (`pending` → wallet debit → die → `open`) or of EndPlay without discussing it with me first.
- Whenever the game service, the wallet or the plays storage changes, explain in the chat what happens if the process crashes between each step.
- Integration tests skip (`t.Skip`) when `DATABASE_URL` is not set.

## Names

- Never rename identifiers I wrote.
- If I already wrote the structs and the signatures, implement only the bodies.
- If you need new types, structs, functions, methods, constants or package variables, propose the names in a short list and **wait for my choice** before implementing. Trivial local variables (`err`, `ctx`, `i`, `tx`) need no approval.

## Way of working

- One task from `TASKS.md` at a time.
- Before writing code, say in a few lines what you are going to do and which files you will touch.
- At the end of each task, run `go build ./...`, `go vet ./...` and the relevant tests, and show the result.
- Explanations stay in the chat, never in the code. Explain the less obvious parts without me having to ask.
- Do not commit or push. I do that.
- Do not mark tasks as done in `TASKS.md`. I do that.
- Do not create files or folders outside the structure in `TASKS.md` without asking.
- If a task looks badly thought out or there is a simpler way, say so before implementing.
- Always show me the changes (code, TASKS.md, CLAUDE.md) before applying them.
- Industry-standard practices only; no invented solutions.
