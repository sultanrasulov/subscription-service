# Phase 1: Walking Skeleton — Implementation Plan

> **Execution:** one task at a time, in order. Each task is reviewed and committed before the next one starts (CLAUDE.md, Workflow). Checkboxes (`- [ ]`) mark the "Done when" results.

- **Status:** approved. It becomes historical when phase 1 merges. From then on the code and `docs/design.md` are the source of truth.
- **Spec:** `docs/design.md`.
- **Branch:** `feature/walking-skeleton`.
- **First commit:** `docs: add walking skeleton plan and version policy`. A decision goes into the design before any code depends on it (CLAUDE.md, Workflow), so this commit also carries the design and CLAUDE.md changes the plan relies on:
  - design §10.2: the toolchain check;
  - design §10.3: the `POSTGRES_PORT` overrides;
  - design §10.4: the linter version file and actions pinned by SHA;
  - design §10.5: the dependency policy;
  - design §11: the integration tests use the image from `compose.yaml`;
  - design §12: the version files, and Dependabot in the phase 1 row;
  - design §13: the revisit trigger for Dependabot bumping `toolchain`;
  - decisions #63 and #64, with #32 and #50 marked as amended;
  - CLAUDE.md: the list of files that hold versions, and the gotcha that `toolchain` and the `golang` image change together.

## What this plan is

This plan records intent:
- what each task must achieve;
- which rules apply, as references to the design rather than copies;
- what each task hands to the next one;
- when each task is done;
- the pitfalls already found and verified.

It contains no file contents, code, test code or version numbers. Those live only in the files themselves, and they are reviewed there, task by task.

## Goal

A thin end-to-end path through every layer, with no business endpoints yet (design §12). From a clean clone:
- `docker compose up --build` starts Postgres and the service;
- the first migration is applied;
- `/health` and `/ready` return 200;
- Swagger UI opens;
- CI is green.

## Global rules

- **Order:** tasks run bottom up. Each layer is tested before the next one depends on it.
- **TDD:** every behavior in "Done when" is covered by a test that was seen to fail before the code existed. Configuration-only tasks are verified by the commands in their "Done when".
- **Commits:** one commit per task, in English, using only the Conventional Commits types of #61: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`. Add a scope only when the task touches one package.
- **Versions:** take the latest stable release when a task adds a dependency, an image or an action.
  - Check that the version exists and works with the rest. For example, the golangci-lint release binary must be built with a Go version at least the module's.
  - Each exact version lives in one file only (design §10.5), and nothing else repeats it.
  - The Go `toolchain` line and the build image tag must be equal. The builder checks this and fails otherwise (design §10.2), so drift is never silent.
- **Layers:** the depguard rules from design §3.2 are active from Task 1. `platform` never imports `internal/subscription`.
- **Logging:** only slog `*Context` methods (CLAUDE.md), and never `context.Background()` on a request path.
- **Modules:** run `go mod tidy` after adding imports. `go get` on a module root does not record checksums for subpackages imported later (verified).
- **New dependencies:** only the libraries the design lists. Anything else needs approval first (CLAUDE.md).

## Review focus

These failure modes are implied by the spec but not named in it. Each is covered by the task in brackets.

1. A Postgres password with URL-reserved characters such as `p@ss:/w?rd#%` still connects (Task 2).
2. An unreachable database fails startup quickly instead of hanging on a TCP connect (Task 5).
3. After migrations the pool can still close. An unclosed migrator holds a connection, and `pool.Close()` then blocks forever (Task 5).
4. A hostile `X-Request-ID` (newline, space, non-ASCII, longer than 64) is replaced, never echoed into headers or logs (Task 6).
5. A panic after the response headers were sent does not write a second response (Task 6).

Also covered:
- a database that is not UTF8 (Task 5);
- version drift after a Dependabot update (Tasks 5, 10 and 11);
- a missing Swagger spec (Task 7);
- SIGTERM in the container (Task 10);
- `POSTGRES_PORT` having two roles (Tasks 9 and 10);
- a clean clone without `.env` (Task 10).

---

### Task 1: Module, tooling and lint baseline
Commit: `chore: scaffold module, tooling and linter`

**Goal:** an empty module that builds, lints and generates a Swagger 2.0 spec. The layer rules are enforced before any code exists.

**Scope:** `go.mod`, `.gitignore`, `.golangci.yml`, `.golangci-lint-version`, `Makefile`, `cmd/subscription-service`, `docs/embed.go`, `docs/swagger/.gitkeep`.

**Rules:**
- Module path `github.com/sultanrasulov/subscription-service`. `go.mod` holds the minimum version in `go` and the pinned patch in `toolchain`.
- `.gitignore` covers `bin/`, `.env` and the generated Swagger files, keeping the `.gitkeep` placeholder.
- Linters, a strict baseline enabled while the code base is empty and they cost nothing:
  - the standard set, with `errcheck` also checking type assertions;
  - **correctness and safety:** bodyclose, errorlint, exhaustive (a `default` counts as exhaustive), gosec, nilerr, noctx, unparam;
  - **architecture and conventions:**
    - depguard with one rule per row of design §3.2, including layers that do not exist yet. The domain rule is an allow-list (standard library and the UUID library only, `list-mode: strict`), and its deny entries keep the §3.2 explanations for the standard packages it forbids;
    - nolintlint: name the linter and give a reason;
    - sloglint: `*Context` methods only;
  - **readability:** gocognit, goconst (not in tests), gocritic, godot, misspell (US), nakedret, revive, unconvert, whitespace.

  Tests are excluded from gosec, noctx, gocognit and unparam. Issue limits are off, and findings on one line are not merged, so no finding is hidden. Formatters: gofmt and goimports.
- Not enabled, on purpose:
  - lll, because Go style has no fixed line length;
  - dupl and prealloc, which are mostly noise at this size;
  - gocyclo, which duplicates gocognit;
  - the govet shadow check, which is noisy on `err`.
- `.golangci-lint-version` is the only place the linter version is written, as one line with the `v` prefix (`vX.Y.Z`). The Makefile reads it, and CI reads it in Task 11.
- Make targets follow design §10.1. `lint` installs the version from the file once into `bin/` under a versioned name (`bin/golangci-lint-vX.Y.Z`), without a global install. Later runs need no network, and a new version in the file is a new file name, so make installs it.
- The entry point carries the swag general info: title, version, description and `@BasePath /api/v1`.
- The generated Swagger directory is embedded (design §9).

**Provides:** the Make targets `help`, `swagger`, `build`, `test`, `test-unit` and `lint`; an embedded file system for the Swagger directory.

**Done when:**
- [ ] `make lint test build` pass.
- [ ] A deliberate, uncommitted layer violation fails lint with the depguard message.
- [ ] The generated spec is Swagger 2.0, with basePath `/api/v1` and no paths.
- [ ] `git status` shows no generated files, no `bin/` and no `.env`.
- [ ] A second `make lint` works without network (`GOPROXY=off`), and changing `.golangci-lint-version` installs the new version.

**Known pitfalls (verified):**
- `go mod init` writes the local patch version, so set the `go` line explicitly.
- `go get -tool` leaves `go.sum` without the checksums of the tool's test dependencies. Run `go mod tidy`; `go mod tidy -diff` must be clean.
- A deny-only depguard rule lets any third-party package through. The domain rule must be an allow-list to match design §3.2.
- godot does not flag swag annotations written as gofmt code blocks (`//<tab>@…`), so it needs no exclusion.
- `go:embed` needs the `all:` prefix to accept a directory that holds only a dotfile placeholder.
- swag fails on a directory that does not exist yet, so the parse list starts with the entry point only. Phase 3 adds the HTTP adapter.
- `go install …@VERSION` needs the `v` prefix. golangci-lint-action accepts the file with or without it.
- `go run …@VERSION` contacts the module proxy on every run (a deprecation check), even with the module cached. When the proxy was unreachable, lint failed, and `GOPROXY=off` did not help. Installing once into `bin/` removes the network from every later run.
- golangci-lint's docs recommend a binary install over `go install`. Locally `go install …@VERSION` is used anyway: it builds with the project's toolchain (design §10.4 needs Go ≥ the `go` directive), checks sources against the checksum database and needs only Go. CI uses the action's binary, as the docs recommend.

### Task 2: Configuration and `.env.example`
Commit: `feat(config): load and validate configuration from environment`

**Goal:** a typed, validated configuration that reports every problem at once and never prints a secret.

**Scope:** `internal/platform/config`, `.env.example`.

**Rules:**
- **One file per section**, each next to its test file:
  - `config.go`: `Config`, `Load` and the error renaming;
  - `http.go`, `log.go`, `postgres.go`: each section's type, its validation and its derived values;
  - `secret.go`: the `Secret` type.

  A new section is a new pair of files.
- Variables and defaults are exactly design §7, and `.env.example` lists all of them. Tags carry the full variable names, so a search for a name finds its field.
- The environment is passed in as a map, so tests need no process environment and can run in parallel.
- Parse errors and missing or empty variables are reported together. Then each section validates its own values, and all problems are joined. Each message starts with the variable's name and shows the value received (secrets excepted):
  - ports in 1–65535;
  - durations and the pool size above zero;
  - `POSTGRES_SSLMODE` is a libpq mode;
  - request timeout shorter than write timeout;
  - shutdown timeout not shorter than request timeout.
- Non-obvious fields carry a short "why" comment pointing at the design.
- `LOG_FORMAT` is its own type with exactly two values, `json` (default) and `text` (decision #65). Like the level, it is checked while parsing, so an unknown value is a parse error.
- `POSTGRES_CONNECT_TIMEOUT` (default 5 s, decision #66) is a duration above zero. Other pool settings are not configurable: they keep pgx's defaults (design §13).
- `POSTGRES_PASSWORD` is a `Secret`. fmt (`%v`, `%+v`, `%#v`, `%s`, `%q`), slog and encoding/json show `[REDACTED]`, even when a whole config struct is printed; go vet rejects the other verbs for this type. `Secret` deliberately does not implement `fmt.Formatter`, which would only silence vet while `%p` still bypasses it. Only `Reveal` returns the value, and only the connection string calls it (CLAUDE.md, "Never log secrets").
- The connection string escapes the user and the password.
- `.env.example` has no quotes or spaces, so compose and the shell read it the same way. Its header says it holds local defaults only.

**Provides:** the config with HTTP, logging and Postgres sections; a Postgres connection string; the `Secret` type.

**Done when:** tests cover
- [ ] the defaults and explicit values of each section;
- [ ] every parse problem, and every validation problem, reported at once, each named by its variable;
- [ ] each invalid value, with the received value in the message, including an unknown or wrongly cased log format;
- [ ] an empty value for a variable with a default means the default;
- [ ] the password never appearing in an error, in fmt output, in slog output or in JSON;
- [ ] `Load(nil)` treating the environment as empty instead of reading the process environment;
- [ ] field names unique across all sections, so a parse error always names the right variable;
- [ ] a user and a password with reserved characters surviving a round trip through the connection string and pgx's parser.

**Known pitfalls (verified):**
- The env library's parse error names the Go struct field, not the variable. Rewrite those messages to name the variable.
- `required` alone accepts an empty value such as `POSTGRES_HOST=`. Required strings also need `notEmpty`.
- The env library tries `encoding.TextUnmarshaler` before custom parsers, so `slog.Level` parses through its own `UnmarshalText`, and an unknown level is already a parse error. `LogFormat` uses the same mechanism.
- For a variable with a default, the env library treats an empty value as unset and uses the default. This holds for every such variable.
- Given a nil map, the env library reads the process environment. `Load` turns nil into an empty map.
- go vet checks the verbs of a helper only when its format and args reach `fmt.Sprintf` unchanged. `problem` is written that way, so vet checks every call.

### Task 3: Logger with request IDs
Commit: `feat(logger): add structured logger with request ID`

**Goal:** structured logs that carry the request ID automatically, as JSON or text (design §8, decision #65).

**Scope:** `internal/platform/logger`.

**Rules:**
- `log/slog`, with the JSON or the text handler chosen by the configured format. Any format other than text is written as JSON, the default.
- The constructor takes the logging section of the configuration (`config.Log`: level and format), so `main` passes it as one value.
- The request ID comes from the context. It is a top-level `request_id` field in both formats, even after `With` and `WithGroup`. An empty ID is treated as no ID.

**Provides:** a logger constructor taking a writer and the logging configuration; a way to store a request ID in a context and read it back.

**Done when:** tests cover
- [ ] each format writes its own output (JSON, or `key=value` text);
- [ ] the field is present when the context has an ID, and absent when it has none or an empty one, in both formats;
- [ ] the field stays top-level after `With` and `WithGroup`, while attrs added after a group stay inside it, with several attrs in one `With` and nested groups;
- [ ] the zero `config.Log` writes JSON;
- [ ] the level filters records;
- [ ] the handler passes the standard library's `testing/slogtest` suite twice: as is, and through a context with a request ID, with the ID checked at the top level.

**Known pitfalls (verified):**
- Adding the field to the record (`Record.AddAttrs`) puts it inside any group opened with `WithGroup`. The handler therefore records its `WithAttrs` and `WithGroup` calls and, when a group is open, adds the field to the base handler first and replays them. Without a group it adds the field to the record directly.
- slogtest logs without a context, so it reaches only the path without a request ID. A second run through a context with an ID covers the replay. Verified with mutations: a replay that drops groups fails the second run (`WithGroup`, `multi-With`, empty and nested-empty groups). A replay that keeps only the last attr of each `With` passes slogtest, because its `With` calls carry one attr each, but fails the test with two attrs in one `With`.
- The replay costs allocations per record (about 15 in a benchmark, against none without a group). It runs only for loggers with a group, and the service does not use groups on hot paths.

### Task 4: problem+json writer
Commit: `feat(problem): add RFC 9457 problem details writer`

**Goal:** the one implementation of the error format (design §4.4, decision #57).

**Scope:** `internal/platform/problem`.

**Rules:**
- Content type `application/problem+json`, type `about:blank`, and the title taken from the status code.
- Optional field errors keep their order. An empty list is omitted.
- The type name does not repeat the package name.
- Like `http.Error`, it deletes a stale `Content-Length` and sets `X-Content-Type-Options: nosniff`. It keeps the other headers set before the call, such as `Allow`, and leaves `Content-Encoding` alone.

**Provides:** a problem-details type with field errors, which phase 3 documents in Swagger; a writer that takes a status, a detail and field errors.

**Done when:** tests cover
- [ ] the body and the header;
- [ ] the omitted empty list, and an omitted empty detail;
- [ ] the order of field errors;
- [ ] headers set before the call, such as `Allow`, are kept;
- [ ] a stale `Content-Length` is removed and `nosniff` is set, and on a real server the client reads the whole body;
- [ ] the members appear in RFC 9457 order (type, title, status, detail, errors).

**Known pitfall (verified):** a `Content-Length` set before the call cuts the body off. On a real server the client gets the status, an empty body and `unexpected EOF`. `http.Error` deletes it for the same reason (`net/http/server.go`).

### Task 5: Schema migration, pool and migration runner
Commit: `feat(postgres): add schema migration, pool and migration runner`

**Goal:** the full schema exists in the database, and the service connects and migrates safely.

**Scope:** `migrations/`, `compose.yaml` (the `postgres` service only), `internal/platform/postgres`, `internal/platform/postgres/pgtest`.

**Rules:**
- The first migration follows design §5.1–§5.3:
  - the table;
  - the `service_name` column with collation `pg_c_utf8`;
  - named CHECK constraints for every rule in §5.1;
  - the two indexes;
  - `BEGIN … COMMIT`, and the files embedded in the binary.
- The pool takes its connect timeout from `POSTGRES_CONNECT_TIMEOUT` and sets it as a duration on the connection config, so every new connection is bounded, including those the pool opens later.
- The pool pings on creation under its own deadline (the same timeout). The connect timeout bounds opening a connection, not a query on one, and the context from `main` has no deadline.
- On a failed ping the pool is closed. The connection string never appears in an error.
- Migrations run on the same pool, not on a second connection string:
  - "no change" is success;
  - a dirty version is an error that points to manual `migrate force`;
  - versions are logged from → to.
- `compose.yaml` holds the database service (design §10.3). It is the single source of the database image.
- The test helper `pgtest`:
  - finds `compose.yaml` by walking up from the working directory to the directory that holds `go.mod`;
  - takes the image from the single line `image: postgres:…` by a line search. No YAML library, so no new dependency; the project owns the file's shape;
  - fails with an error naming `compose.yaml` if the file or the line is missing;
  - starts one container per test package with `C` locale and UTF8 encoding (design §5.2);
  - each integration test creates its own database in that container (`CREATE DATABASE … TEMPLATE template0`) and drops it at the end, so tests are independent and run in parallel;
  - is skipped under `-short`;
  - is reused in phases 3 and 4.

**Provides:** embedded migrations; pool creation from the Postgres config; the migration runner; `pgtest`, which returns a Postgres config and a stop function that reports errors.

**Done when:** tests cover
- [ ] connect and ping, and the pool configuration (max connections, connect timeout) checked without a network;
- [ ] a server that accepts the TCP connection and never answers fails within the timeout instead of hanging (a local listener, no Postgres);
- [ ] a server that completes the handshake and then never answers a query fails within the timeout (a fake backend on `pgproto3`, no Postgres): only the ping's own deadline covers this case;
- [ ] a wrong password and a closed port fail without leaking the password;
- [ ] an empty database migrates to version 1, a second run reports no change, and down → up works;
- [ ] the pool closes after migrating;
- [ ] collation and indexes are in place;
- [ ] every CHECK rejects its invalid row with SQLSTATE 23514 and its own name, and a boundary row is accepted (100 Cyrillic characters, price 0, 2000-01 and 2100-12);
- [ ] a non-UTF8 database fails the migration;
- [ ] a failed migration leaves the version dirty, and the next run refuses with an error pointing to `migrate force`;
- [ ] `pgtest` reads the image from `compose.yaml` (a unit test that also runs under `-short`).

**Known pitfalls (verified):**
- The migration driver keeps one dedicated pool connection while the migrator lives. Close the migrator on every path, or `pool.Close()` blocks forever. If building the migrator fails, close the `*sql.DB` built from the pool. Closing it twice is safe, and it does not close the pool.
- testcontainers' ready-made connection string does not escape the password. Build the config from host and port instead.
- A closed port fails at once with or without a timeout. Assert the timeout on the pool config, or with a listener that accepts and stays silent, never through an outside network.
- pgx without a connect timeout waits for the operating system's TCP timeout, which takes minutes.
- `connect_timeout` in a connection string takes whole seconds. Setting the duration on the config avoids that limit.
- The silent-server test passes with either protection, connect timeout or ping deadline, because the handshake itself stalls. Only a server that finishes the handshake and stalls on the query tests the ping deadline. Without the deadline, that test hangs until the test timeout (verified).
- After a canceled query, pgx closes the connection in the background: it sends a cancel request on a new connection and Terminate on the old one, then waits for the server to close each, for up to 15 seconds, and `pool.Close()` waits for that. A fake server must therefore close every connection when it is done with it, including one whose message it cannot read. pgx v5.13 sends a cancel request that `pgproto3.Backend` rejects as "cancel request too short". A real Postgres closes them at once.
- On first start the image runs a temporary server on the Unix socket only (`listen_addresses=''` in its entrypoint). `pg_isready` over the socket reports it ready just before it shuts down, so a dependent service could start against a database that is not yet on TCP. The healthcheck uses TCP (`-h 127.0.0.1`); five fresh-volume starts each accepted a TCP connection as soon as the service was healthy (verified).
- testcontainers' `Run` returns the container together with an error, for example when the wait strategy fails. Terminate it on that path too, because Ryuk may be disabled in CI. `TerminateContainer` accepts a nil container.
- `errcheck` with `check-type-assertions` flags `ln.Addr().(*net.TCPAddr)`; check the `ok`. gosec flags `os.ReadFile` on a built path; read the fixed name through `os.DirFS(root)` instead.
- A `SQL_ASCII` database can be created in the same container (`TEMPLATE template0`, `LOCALE 'C'`). The migration then fails, because `pg_c_utf8` does not exist for that encoding.

### Task 6: HTTP middleware
Commit: `feat(httpserver): add request ID, access log, recover and deadline middleware`

**Goal:** every request gets a request ID, exactly one log line, panic safety and a deadline (design §4.4, §8).

**Scope:** `internal/platform/httpserver`.

**Rules:**
- The order is request ID → access log → recover → deadline → router, fixed in one function that Task 7 calls. The request ID is outermost, so every line of the request carries it; the access log sits outside the recovery, so it logs the 500 the recovery writes; the deadline is innermost, so it bounds only the handler's work. A client that goes away cancels the context (`context.Canceled`), while an expired deadline gives `context.DeadlineExceeded`, so the access log tells them apart in any order.
- An incoming request ID is kept only if it has 1–64 visible ASCII characters; otherwise a UUID v7 is generated. The ID goes into the response header and the context.
- One `request completed` line per request, with method, path, status, duration and request ID. A client disconnect is logged as status 499 at WARN.
- A panic is logged at ERROR with its stack, and becomes a 500 problem+json only if no header was sent yet. `http.ErrAbortHandler` is re-panicked.
- The deadline middleware only sets the request deadline. Turning an expired deadline into a 503 belongs to the HTTP adapter in phase 3.

**Provides:** the middleware chain that Task 7 uses.

**Done when:** tests cover
- [ ] a valid ID is kept, up to 64 bytes, and each invalid kind is replaced by a UUID v7;
- [ ] the default status is 200;
- [ ] a disconnect is logged as 499, both with a canceled context and on a real server whose client closes the connection;
- [ ] a panic before and after headers were sent;
- [ ] the abort panic is re-panicked;
- [ ] the deadline is set;
- [ ] `http.ResponseController` still reaches `Flush` through the wrapped writer (`Unwrap`);
- [ ] a flush before a panic keeps the response as sent and logs its status;
- [ ] a request aborted with `http.ErrAbortHandler` is logged once;
- [ ] the chain applies the order above: one request shows the ID, the deadline, the 500 problem and the same request ID on the panic line and the access line.

**Known pitfalls (verified):**
- The access log and the panic recovery each wrap the writer with their own recorder. Neither depends on the other's type, and the access log still sees the 500 that the recovery writes.
- net/http cancels the request context when the client closes the connection, and the access log runs before the handler chain returns, so the cancellation it sees comes from the client. A real-server test confirms it.
- misspell runs with the US locale: write "canceled", not "cancelled".
- A flush through `http.ResponseController` bypasses `WriteHeader`, because the controller unwraps the writer. The recorder implements `FlushError`, which the controller looks for first, and counts the flush as the header. Without it, a panic after a flush sent 200 with a 500 body and "superfluous WriteHeader" (verified on a real server).
- Code after `next.ServeHTTP` does not run when a panic passes through, and the recovery re-panics `http.ErrAbortHandler`. The access log therefore writes its line in a defer.
- `uuid.NewString` is `Must(NewRandom())`, and `NewV7` starts with `NewRandom`, so a uuid fallback would panic on the same failure. The fallback is the clock.
- The order of the deadline and the access log does not affect telling 499 from an expired deadline: `context.Canceled` and `context.DeadlineExceeded` differ in any order (a mutation that swapped them passed every test).

### Task 7: Health, readiness, Swagger and router-error routes
Commit: `feat(httpserver): add health, readiness, swagger and router error routes`

**Goal:** the operational endpoints and the error format for router responses (design §4.4, §4.5, §9).

**Scope:** `internal/platform/httpserver`.

**Rules:**
- `/health` is liveness: always 200, no database.
- `/ready` pings the database with a 1 s timeout: 200, or 503 problem+json and a WARN.
- Swagger:
  - the spec is read once at startup from the embedded file system;
  - if it is missing, one WARN is logged and the service keeps running, and `doc.json` returns 404 problem+json;
  - the UI is served without a CDN.
- The router's own 404 and 405 become problem+json, keeping `Allow`. Handlers' own 404s and redirects are not touched.
- One mux, with a registration hook through which phase 3 adds the API.

**Provides:** an HTTP handler built from a logger, a readiness check, the Swagger file system, the request timeout and the registration hook.

**Done when:** tests cover
- [ ] health while the readiness check fails;
- [ ] readiness ok, failing, and slower than 1 s;
- [ ] an unknown path → 404 problem;
- [ ] a wrong method → 405 problem with `Allow`;
- [ ] a handler's own 404 is left alone;
- [ ] the spec present and missing;
- [ ] the redirect chain `/swagger` → `/swagger/` → `index.html`;
- [ ] the middleware is applied.

**Known pitfalls (verified):**
- The standard router reports an empty pattern for both 404 and 405, and sets `Allow` on 405.
- The `/swagger` redirect reports a non-empty pattern.
- The Swagger UI library cannot serve our spec itself, because it expects generated Go code we do not have. `doc.json` has its own route, which wins as the more specific pattern.

### Task 8: Server with graceful shutdown
Commit: `feat(httpserver): add server with graceful shutdown`

**Goal:** a server with the configured timeouts that drains in-flight requests on shutdown (design §3.4, §7).

**Scope:** `internal/platform/httpserver`.

**Rules:**
- All four server timeouts come from the config, and the server's own error log goes through slog.
- Shutdown starts when the context is canceled and is bounded by the shutdown timeout. If it overruns, the server is closed and an error is returned. A normal close is not an error.
- The listener is passed in, so tests can use a random port.

**Provides:** a server constructor; a serve function that takes a context, a listener and the shutdown timeout.

**Done when:** tests cover
- [ ] the timeouts are applied;
- [ ] after cancellation an in-flight request completes, serve returns cleanly, and new connections are refused;
- [ ] an overrunning shutdown returns an error.

### Task 9: Composition root and local run
Commit: `feat: wire the service in main and run it locally`

**Goal:** the service runs on the host against a Postgres in Docker.

**Scope:** `cmd/subscription-service`, `compose.dev.yaml`, the Make targets `db` and `run`.

**Rules:**
- The wiring follows design §3.3–§3.4: config → logger → pool → migrations → handler → listen → serve.
- On config errors, every problem is printed and the process exits non-zero.
- Signals: SIGINT and SIGTERM.
- The log shows `server started`, `shutting down` and `server stopped`. The pool closes last.
- `compose.dev.yaml` publishes the database on loopback at `POSTGRES_PORT`, defaulting to 5432 inside the file. Only `make db` uses it (design §10.3).
- `make run` loads `.env.example`, then an optional `.env` (design §7).

**Done when** (manual):
- [ ] after `make db` and `make run`, every route answers as specified;
- [ ] Ctrl-C exits 0 with the shutdown lines;
- [ ] without a database, and with an invalid `LOG_LEVEL`, the process exits quickly with a clear error.

**Known pitfall (verified):** compose interpolation (`${…}` in a compose file) reads only the shell and `.env`, never `env_file`. That is why `compose.dev.yaml` needs its own default for `POSTGRES_PORT`.

### Task 10: Container image and full stack
Commit: `feat: add Dockerfile and full compose stack`

**Goal:** `docker compose up --build` from a clean clone gives a healthy stack (design §1.2, §10.2–§10.3).

**Scope:** `Dockerfile`, `.dockerignore`, the `app` service in `compose.yaml`, the Make targets `up`, `down` and `logs`.

**Rules:**
- Builder stage:
  - the Go Alpine image plus `make`, with `GOTOOLCHAIN=local`;
  - before building, it fails unless its Go version equals the `toolchain` line in `go.mod`, using a one-line check against `go env GOVERSION` (design §10.2);
  - it builds through `make build`, so it also generates the spec.
- Runtime stage: Alpine, a non-root user, and an exec-form entrypoint.
- The Docker context excludes stale generated specs and the plans.
- The app:
  - starts after the database is healthy;
  - has a healthcheck on `/ready`;
  - has a 15 s stop grace period;
  - overrides both `POSTGRES_HOST` and `POSTGRES_PORT` to the in-network values (design §10.3, #64).

**Done when**, from a fresh clone in a temporary directory, without `.env`:
- [ ] the stack comes up healthy;
- [ ] `/health`, `/ready`, `doc.json` (Swagger 2.0) and the UI answer;
- [ ] the log shows the migration to version 1;
- [ ] stopping the app logs the shutdown lines and exits 0, not 137;
- [ ] a deliberate, uncommitted mismatch between `toolchain` and the image fails the image build.

**Known pitfalls (verified):**
- The Go Alpine image has no `make`.
- `GOTOOLCHAIN=local` ignores the `toolchain` line: a newer `toolchain` still builds, and only a newer `go` line fails. Dependabot does not bump `toolchain` (dependabot-core #13520). Without the explicit check, an image update would leave `toolchain` behind silently.
- A shell-form entrypoint swallows SIGTERM.

### Task 11: CI and Dependabot
Commit: `chore: add CI workflow and Dependabot`

**Goal:** every push and pull request is checked automatically, and dependencies stay current without stale copies (design §10.4–§10.5).

**Scope:** `.github/workflows/ci.yml`, `.github/dependabot.yml`, `CLAUDE.md` (the "available from phase 1" note only).

**Rules:**
- CI runs on push and on pull requests to `main`, with read-only permissions. Jobs:
  - `lint`: the official golangci-lint action, reading `.golangci-lint-version`;
  - `test`: `go mod tidy -diff` must be clean, then `make test`, with the Go version taken from `go.mod`;
  - `smoke`: compose up, then health, readiness, Swagger 2.0 and the UI; logs on failure; always tear down.
- Every action, `actions/*` included, is pinned to a full commit SHA, with its version in a comment (design §10.4).
- Dependabot follows design §10.5:
  - weekly updates for `gomod`, `docker`, `docker-compose` and `github-actions`;
  - minor and patch updates grouped;
  - `commit-message` with `prefix: chore` and `include: scope`, which gives `chore(deps): …` (#61);
  - no Postgres major updates.
- `CLAUDE.md`: delete the "These targets are available from phase 1" note. The commands now exist.

**Done when:**
- [ ] `make lint test` pass locally;
- [ ] after the push (ask first), the three CI jobs are green;
- [ ] the pull request is merged with a merge commit after approval.

**Known pitfalls (verified):**
- golangci-lint-action looks for the version in its `version` input, then in `go.mod`, then in `version-file`, and accepts `.golangci-lint-version`.
- Dependabot supports the `gomod`, `docker`, `docker-compose` and `github-actions` ecosystems.
- Without an explicit `commit-message`, Dependabot picks a prefix from the repository history, which may not be one of the #61 types.
