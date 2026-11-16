# Subscription Service

## Overview
A REST service that stores users' online subscriptions and reports their total cost over a period (Effective Mobile Golang Developer test assignment).
The approved design is `docs/design.md`. Read the relevant section before changing behavior. It is referenced, not imported, because of its size.
Approach: Domain-Driven Design, kept small. See "Domain modeling".
Stack: Go, PostgreSQL (17 or later, which the `pg_c_utf8` collation requires), Docker Compose. Each exact version lives in one file only: `go.mod`, the `Dockerfile`, `compose.yaml`, `.golangci-lint-version`, `.github/workflows/ci.yml` or `.pre-commit-config.yaml`. Never repeat a version anywhere else, including here and in plans (design §10.5).
- HTTP: the standard library `net/http` `ServeMux`, not chi, gin or echo.
- Database: pgx v5 with hand-written SQL. No ORM, query builder or sqlc.
- Migrations: golang-migrate, embedded with `go:embed` and applied at startup.
- Logging: `log/slog`, not zap or zerolog. JSON handler by default; `LOG_FORMAT=text` for reading logs locally.
- Config: `caarlos0/env`, from environment variables only. No viper, and no reading of `.env` in Go.
- JSON: `encoding/json` (v1), not `encoding/json/v2` until v2 is a stable part of Go (design §13).
- IDs: `github.com/google/uuid`, version 7.
- Swagger: swaggo annotations produce Swagger 2.0. Generated files are never committed.
- Tests: the standard `testing` package, hand-written fakes (no mock generator), and testcontainers-go for Postgres.
- Dependency injection: manual wiring in `cmd/subscription-service`. No Wire and no DI framework.

## Commands
- `docker compose up --build`: the full stack, as a reviewer runs it.
- `make db`, then `make run`: Postgres in Docker, the service on the host.
- `make test`: all tests with `-race`. Integration tests need Docker running.
- `make test-unit`: `go test -short ./...`, without Docker.
- One test: `go test ./<package> -run 'TestName/subtest' -v`.
- One integration test: `go test ./internal/subscription/adapters/postgres -run 'TestName' -count=1 -v`. `-count=1` bypasses the test cache.
- `make lint`, `make swagger`, `make up` / `make down` / `make logs`.
- `make pre-commit-install` once per clone: every commit then runs `.pre-commit-config.yaml` (whitespace, YAML, `go mod tidy -diff`, `make lint`, no commit to `main`). `make pre-commit-run` checks every file. pre-commit itself is installed once per machine, for example with `pipx install pre-commit`.

## Architecture
- `internal/subscription/domain`: the aggregate, value objects, ports and errors. Standard library, uuid and `internal/shared` only; no HTTP, JSON or SQL; never reads the clock.
- `internal/subscription/application`: one service, one method per use case. It receives value objects and returns domain objects.
- `internal/subscription/adapters/httpapi`: parses and validates raw input, calls the application, and maps errors to problem+json.
- `internal/subscription/adapters/postgres`: the only code that talks to the database. It implements the domain ports.
- `internal/shared/valueobject`: value objects every bounded context may use, such as `ID[K]`, the typed identifier. Only value objects: no serialization, no helpers. Standard library and uuid only. A new identifier is an unexported kind, an alias and a constructor (see `subscription_id.go`).
- `internal/platform/*`: domain-neutral config, logger, Postgres pool and migrations, problem+json, and the HTTP server.
- `migrations/`: SQL migrations.

Dependencies flow `httpapi → application → domain ← postgres`, and the domain may use `internal/shared`. Adapters may use `platform`. `platform` never imports `internal/subscription` or `internal/shared`. The application and the adapters never import `internal/shared` either: they create identifiers through the domain's constructors, such as `NewUserID`. depguard enforces this in CI. Details: design §3.

## Domain modeling
- One bounded context: `subscription`.
- Use the ubiquitous language of design §2.1 in code, tests and the API, with one term per concept. For example, it is "total cost", never "spending". `SubscriptionPeriod` and `CalculationPeriod` are different concepts.
- `Subscription` is the only aggregate. Its state changes only through its own constructor and methods.
- Invariants and validation live in value objects and the aggregate, not in handlers or the application service. Database constraints only repeat them.
- The one deliberate exception is the total-cost calculation. The database performs it behind the `TotalCostReader` read port (design §2.7, decision #3).
- One repository, for the one aggregate. The total-cost read side is a separate port.
- Not used here, on purpose: domain events, specifications, factories, generic repositories, a separate read store. Add one only when the design records the need.

## Principles
- Simple over clever: no new interface or abstraction without a real second use. Ports exist because adapters implement them. There is no ID-generator or clock interface.
- Make invalid states unrepresentable. Value objects validate in their constructor, keep their fields unexported, and hold no pointers.
- Fail explicitly. Wrap errors with context (`%w`), never swallow one, and never panic in a production code path.
- Comments explain why, not what, and stand on their own. They never cite the design or this file. They may cite an external, immutable source for detail, such as RFC 9457 §4.2.1 or an upstream issue. Messages shown at runtime cite nothing.
- Fix lint findings at the cause. `//nolint` only with a reason on the same line.

## Conventions
- Errors: domain errors wrap `ErrInvalid` or `ErrNotFound`. Mapping errors to HTTP status codes lives only in `httpapi`, and the problem+json format only in `internal/platform/problem` (design §4.4).
- Logging: each error is logged once, at the HTTP boundary. The application layer returns errors and logs only business events. Always use the slog `*Context` methods, so `request_id` is attached. Never log secrets.
- Money: whole rubles as integers, never floats.
- Months: `MonthYear` in the domain, stored as a date on the 1st of the month, sent as `MM-YYYY` in the API.
- A new environment variable goes into the config struct (with validation) and into `.env.example` in the same change. A compose-only variable, such as `APP_PORT`, is read by interpolation from the shell or `.env`, so it goes into `.env.example` as a commented line only.

## API
- REST + JSON under `/api/v1`. Errors are RFC 9457 problem+json.
- A handler change updates its swag annotations in the same change.
- Request bodies go only through the strict decoder (design §4.3).
- Don't change the public contract (paths, fields, status codes) without updating the design first.

## Database
- Schema changes go only into new migrations. Never edit a migration that has reached `main`.
- Every migration wraps its statements in `BEGIN … COMMIT`. A statement that cannot run in a transaction gets a migration of its own.
- Always pass the request context to pgx. Never use `context.Background()` on a request path.

## Testing
- TDD: write the failing test first, then the code.
- Domain: table-driven unit tests.
- HTTP adapter: `httptest`, with the real application service over hand-written fakes.
- Postgres adapter: integration tests on testcontainers, against a UTF8 database with the `C` locale.
- Integration tests skip themselves under `-short`.
- Tests sit next to the code (`foo_test.go`).
- Every bug fix ships with a regression test that fails without the fix.
- Never delete, skip or weaken a failing test to make it pass. Fix the code or ask.

## Workflow
1. Unclear requirement → ask. Don't guess and build.
2. Follow the current phase's plan in `docs/superpowers/plans/`. Each phase starts with its plan as the first commit on its branch, and no code is written before the plan is approved. Propose any change outside the plan first.
3. Work in small steps, and run the single relevant test after each one.
4. Before saying "done", make sure lint and all tests pass.
5. Summarize what changed and what is left.

- One short-lived branch per phase, named `type/short-description`. The types are `feature/`, `fix/`, `docs/`, `chore/` and `refactor/`. Names are lowercase with hyphens, with no ticket ID. Branch names per phase: design §12. Each phase lands through one pull request, merged with a merge commit only when CI is green. Never squash.
- Commits follow Conventional Commits (`feat:`, `fix:`, `refactor:`, `test:`, `docs:`, `chore:`) and are written in English, like everything in the repository.
- A change to a decision updates `docs/design.md` and its decision log in the same commit.
- A change that adds a command or a convention updates this file in the same commit.

## Ask first / Don't
- Ask before:
  - adding a dependency the design does not list;
  - changing the API contract or a decision in the design;
  - editing CI, the Dockerfile or the compose files outside the plan.
- Don't commit `.env`, generated Swagger files, keys or credentials.
- Don't weaken depguard rules to make an import compile. A layer violation is a design error.
- Don't refactor or reformat outside the task.

## Gotchas
- Case-insensitive name matching needs the `pg_c_utf8` collation on both sides of the comparison, not only on the column (design §5.2).
- Plain `json.Unmarshal` ignores `null` for a value field (a number, string or bool) without an error: the field keeps its value, which is the zero value in a fresh struct. It sets interfaces, maps, pointers and slices to nil instead. It also silently keeps the last of duplicate keys. Use the strict decoder (design §4.3).
- `/swagger/doc.json` returns 404 after a plain `go run` without `make swagger`. That is expected, not a bug: the specification is generated and embedded at build time (design §9).
- Never create `compose.override.yaml`. Compose merges it automatically and would change a reviewer's `docker compose up`. Local development uses `compose.dev.yaml` through `make db` (design §10.3).
- A failed migration leaves its version marked dirty. Editing the applied file does not help. Recovery is a manual `migrate force` (design §5.3).
- Encoding a nil slice produces `null`, so list responses must return an initialized slice (design §4.2).
- `r.URL.Query()` silently drops a pair with an invalid escape (`%zz`) or a `;` separator, so a filter can vanish and a list return every user's subscriptions. Parse with `url.ParseQuery(r.URL.RawQuery)` and answer any error with 400 (design §4.3).
- A Go update changes `toolchain` in `go.mod` and the `golang` image in the `Dockerfile` together; the builder fails otherwise. A Dependabot PR for the image stays red until `toolchain` is bumped in the same PR (design §10.2).
