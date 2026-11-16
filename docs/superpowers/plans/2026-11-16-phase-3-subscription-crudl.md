# Phase 3: Subscription CRUDL — Implementation Plan

> **Execution:** one task at a time, in order. Each task is reviewed and committed before the next one starts (CLAUDE.md, Workflow). Checkboxes (`- [ ]`) mark the "Done when" results.

- **Status:** approved. It becomes historical when phase 3 merges. From then on the code and `docs/design.md` are the source of truth.
- **Spec:** `docs/design.md` §2.5, §3, §4 (without total cost), §5, §6, §8, §9, §10.4, §11 and the phase 3 row of §12; the phase 2 plan's "Hands over to phase 3"; decisions #77–#83.
- **Branch:** `feature/subscription-crudl`. Pull request title: `feat: subscription CRUDL (phase 3)`.
- **First commit:** `docs: add subscription CRUDL plan`. A decision goes into the design before any code depends on it, so this commit also carries:
  - design §2.5: `Delete` returns the deleted subscription;
  - design §3.1: `platform/jsonbody` in the layout;
  - design §4.3: canonical UUIDs; strict query parsing, its messages and the fixed order of parameter errors; unknown and then duplicate fields in alphabetical order, a duplicate's value not validated;
  - design §4.4: the status follows the stage the error came from; a disconnect is logged only by the request log line;
  - design §5.1: `id` and `user_id` are not the nil UUID; whitespace normalization deliberately not repeated in the schema;
  - design §10.4: the smoke job requires a documented path and runs create → get;
  - decisions #77–#83, with #23, #34 and #45 marked as amended;
  - CLAUDE.md, Gotchas: `r.URL.Query()` drops malformed pairs silently.

## What this plan is

Like the phase 2 plan, it records intent: what each task must achieve, which rules apply, what it hands to the next task, when it is done, and the pitfalls already found and verified. **Provides** lists the exact exported signatures, because they are what later tasks and phase 4 build on. It contains no function bodies or test code.

## Goal

The five subscription endpoints of design §4.1, without total cost (design §12):
- `POST /api/v1/subscriptions`, `GET /api/v1/subscriptions/{id}`, `GET /api/v1/subscriptions`, `PUT /api/v1/subscriptions/{id}`, `DELETE /api/v1/subscriptions/{id}`;
- the application service, the Postgres repository, the HTTP adapter with strict body decoding, and their Swagger annotations.

Done when all five endpoints work through curl and Swagger UI, the handler and repository tests pass, and the smoke job requires a documented path and passes create → get.

Out of scope: total cost (phase 4), so `NewService` takes only the `Repository` for now; the README (phase 5). No new dependencies: pgx, uuid and swag are already in `go.mod`.

**Order: ports first, endpoints in slices.** The repository and the application service are built whole (Tasks 2–4); the endpoints arrive in slices (Tasks 6–9): create and get, then list, update, delete. A slice through every layer is not possible: `domain.Repository` has five methods, and the composition root does not compile until an adapter implements all of them. Each endpoint slice still works end to end.

## Global rules

- **TDD:** every behavior in "Done when" is covered by a test that was seen to fail before the code existed. One commit per task, in English, Conventional Commits.
- **Layers:**
  - `httpapi → application → domain ← postgres`; domain-neutral HTTP mechanics live in `platform` (`problem`, `jsonbody`);
  - identifiers are created only through the domain's constructors (depguard);
  - `httpapi` depends on the concrete `*application.Service`, with no interface: there is no second implementation.
- **Status by stage (#78):**
  - parsing the request: the domain's "invalid data" errors → 400 with field errors;
  - calling the application:
    - an error that wraps `ErrNotFound` → 404 `subscription not found`;
    - `r.Context().Err()` is `context.DeadlineExceeded` → 503 `request timed out`, and one WARN naming the operation;
    - `r.Context().Err()` is `context.Canceled` → nothing written and nothing logged: the request log line already records it at WARN with status 499 (design §8, "exactly once");
    - anything else, including `ErrInvalid` → 500 `internal server error`, and one ERROR with the full error chain.
- **Logging:**
  - the application logs the business events `subscription created`, `subscription updated` and `subscription deleted` at INFO with `subscription_id` and `user_id`;
  - it never logs errors: it wraps them with `%w` and the operation's name;
  - only the slog `*Context` methods.
- **Postgres:** the request context always reaches pgx; a month is a date on the 1st, in UTC; only bound parameters.
- **Tests:**
  - application: unit tests over `domaintest`;
  - httpapi: `httptest`, the real application service over `domaintest`;
  - postgres: testcontainers, one container per package, one database per test, skipped under `-short`.
- **Comments:** they explain why and never cite the design.
- **CLAUDE.md:** a new package's line is added in the task that creates the package, not before.

## Review focus

These failure modes are implied by the spec but not named in its tests. Each is covered by the task in brackets.

1. A malformed escape in the query silently drops a filter, and a list returns other users' subscriptions (Task 7).
2. A stored row that breaks an invariant gives 500, not 400 (Tasks 2 and 6).
3. A deadline in the middle of a database call gives a 503 problem; a client that went away is logged once, as 499 (Task 6).
4. Non-canonical UUIDs are rejected the same way in the path, the body and the query (Tasks 6 and 7).
5. The order of field errors does not depend on the order of the client's keys, including unknown and duplicate fields (Tasks 5 and 6).

## Hands over to phase 4

1. The filter conditions builder of Task 3 serves the total-cost query.
2. The query parsing of Task 7 serves `from`, `to` and the filters of total cost.
3. `NewService` gains the `TotalCostReader`.
4. The literal `total-cost` path must win over `{id}` (design §4.1).

---

### Task 1: Schema rules for control characters and nil IDs
Commit: `feat(migrations): repeat control-character and nil-ID rules in the schema`

**Goal:** the schema repeats the domain rules it does not repeat yet (decisions #75 and #83).

**Scope:** `migrations/000002_*`; `TestSchema_RejectsInvalidRows` in `internal/platform/postgres`.

**Rules:**
- The up migration, inside `BEGIN … COMMIT`, adds three CHECK constraints: `service_name !~ '[[:cntrl:]]'`, `id <> '00000000-0000-0000-0000-000000000000'` and the same for `user_id`. The down migration drops all three.
- The first migration is not edited.

**Done when:** tests cover
- [ ] ESC, DEL, a nil `id` and a nil `user_id` rejected by the database;
- [ ] Cyrillic and a no-break space inside a name accepted;
- [ ] a name of only a no-break space accepted: the boundary is deliberate, the domain owns normalization;
- [ ] the existing up → down → up test covering the new version.

**Known pitfalls (verified):**
- Postgres's `[[:cntrl:]]` matches exactly the code points `unicode.IsControl` does, under both `pg_c_utf8` and the `C` collation.
- `ADD CONSTRAINT … CHECK` validates the existing rows and fails if one breaks the rule, which leaves the version dirty. It is safe here because nothing has written rows before this phase; a later CHECK on live data needs the data cleaned first, or `NOT VALID` followed by `VALIDATE CONSTRAINT`.

### Task 2: The subscription repository
Commit: `feat(postgres): add the subscription repository`

**Goal:** store, load, update and delete subscriptions (design §5.1, §5.4).

**Scope:** `internal/subscription/adapters/postgres`; `internal/platform/postgres/pgtest`; the tests of `internal/platform/postgres`; `internal/subscription/domain/repository.go`.

**Provides:**
- `func NewDatabase(t testing.TB, server config.Postgres, encoding string) config.Postgres` in `pgtest`: creates a database from `template0` with the given encoding and the `C` locale, drops it in cleanup, and skips under `-short`. The `platform/postgres` tests move to it and `freshDB` is deleted.
- `func NewRepository(pool *pgxpool.Pool) *Repository`, with `Create`, `Get`, `Update` and `Delete`.
- The port changes to `Delete(ctx context.Context, id SubscriptionID) (Subscription, error)` (#81).
- `List` and the `var _ domain.Repository` assertion come in Task 3.

**Rules:**
- A row becomes a subscription only through the domain's constructors. A constructor's error is wrapped with `%w` and the row's ID.
- `pgx.ErrNoRows` and zero affected rows → an error that wraps `ErrNotFound`, with the ID.
- `Update` writes the service name, the price and both dates, and sets `updated_at = now()` in the statement itself. It never writes `user_id`: the user of a subscription never changes (#11).
- `Delete` is `DELETE … RETURNING`, in a transaction that rolls back when the deleted row cannot be built into a subscription, so an error always means nothing was deleted.
- The test packages that need a container share `pgtest.Main(m *testing.M, server *config.Postgres) int`; each `TestMain` is `os.Exit(pgtest.Main(m, &server))`.

**Done when:** tests cover
- [ ] a round trip of a closed and an open-ended period, a price of 0 and a Cyrillic name;
- [ ] not found from `Get`, `Update` and `Delete`;
- [ ] `Delete` returning the deleted subscription, and `Get` then giving not found;
- [ ] `Update` moving `updated_at` and leaving `user_id` unchanged;
- [ ] a corrupted row: a direct `INSERT` with the name `"\u00a0"`, a name of only a no-break space, makes `Get` return an error for which `errors.Is(err, domain.ErrInvalid)` holds. The chain is kept; the HTTP adapter turns it into a 500.
- [ ] `Delete` of such a row returns that error and leaves the row in place.

**Known pitfalls (verified):**
- `pgtest` cannot import `platform/postgres`: the internal test of `package postgres` imports `pgtest`, so that would be an import cycle in tests. The adapter's tests open the pool and apply the migrations themselves.
- The adapter package is named `postgres`, like the platform package; the composition root imports one of them under an alias.

### Task 3: Listing by filter and page
Commit: `feat(postgres): list subscriptions by filter and page`

**Goal:** `List` with filters and pagination, and the repository complete (design §5.2, §5.4).

**Scope:** `internal/subscription/adapters/postgres`.

**Rules:**
- One builder turns a `domain.Filter` into conditions; total cost uses it in phase 4.
- A condition is added only for a filter that is set (`IsZero`), with bound parameters only.
- The service name compares `lower(service_name) = lower($n COLLATE pg_c_utf8)`.
- `ORDER BY id`, `LIMIT` and `OFFSET`, and a separate `count(*)` with the same filter.

**Provides:** `List` and `var _ domain.Repository = (*Repository)(nil)`.

**Done when:** tests cover
- [ ] each filter alone, both together, and none;
- [ ] «ЯНДЕКС ПЛЮС» finding «Яндекс Плюс»;
- [ ] with `SET LOCAL enable_seqscan = off` in the test's transaction, a plan that uses the index on `lower(service_name)`, **and** the row found;
- [ ] the order by ID;
- [ ] pages, and an offset past the end giving no items with the right total.

**Known pitfall (verified):** an index check without a result check proves nothing. With the collation on only one side of the comparison the index is used and no row is found.

### Task 4: The application service
Commit: `feat(application): add the subscription use cases`

**Goal:** one method per use case (design §6), and the fake the tests of this and later tasks share.

**Scope:** `internal/subscription/application`; `internal/subscription/domain/domaintest`; CLAUDE.md, Testing: where the fakes live.

**Provides:**
- `domaintest`:
  - `func NewRepository() *Repository`: in memory, holding values, behind a mutex;
  - `func (r *Repository) Fail(err error)`: every later call returns `err`;
  - `func (r *Repository) Block()`: every later call waits for `ctx.Done()` and returns its error.
- `func NewService(repo domain.Repository, log *slog.Logger) *Service`:
  - `Create(ctx context.Context, userID domain.UserID, name domain.ServiceName, price domain.Price, period domain.SubscriptionPeriod) (domain.Subscription, error)`;
  - `Get(ctx context.Context, id domain.SubscriptionID) (domain.Subscription, error)`;
  - `Update(ctx context.Context, id domain.SubscriptionID, name domain.ServiceName, price domain.Price, period domain.SubscriptionPeriod) (domain.Subscription, error)`: load, `Update`, save; last write wins;
  - `Delete(ctx context.Context, id domain.SubscriptionID) error`: the event takes the user ID from the subscription `Delete` returned;
  - `List(ctx context.Context, q domain.ListQuery) (items []domain.Subscription, total int, err error)`.

**Done when:** tests cover
- [ ] `Create` giving a version 7 UUID and storing the subscription;
- [ ] each event logged exactly once, with both IDs (a recording slog handler);
- [ ] not found passing through (`errors.Is`);
- [ ] `Fail` and `Block` passing through;
- [ ] a failing `uuid.NewV7`: a wrapped error, no panic, nothing stored.

**Known pitfalls (verified):**
- `uuid.NewV7` reads the package's global random source. The test replaces it with `uuid.SetRand`, restores it with `uuid.SetRand(nil)` in cleanup, and does not call `t.Parallel()`, because the source is a global variable without synchronization; a comment says why.
- The fake matches names with `strings.EqualFold`, which is close to Postgres's `lower()` but not the same; the integration test of Task 3 is the proof.
- `Subscription` has pointer receivers, so the fake copies a stored value into a local variable before it uses a method.

### Task 5: The JSON body reader
Commit: `feat(jsonbody): add strict JSON object reader`

**Goal:** the domain-neutral half of strict body decoding (#80, design §4.3).

**Scope:** `internal/platform/jsonbody`; CLAUDE.md, Architecture: `jsonbody` in the `platform/*` line.

**Provides:**
- `func Read(w http.ResponseWriter, r *http.Request, maxBytes int64) (Object, error)`;
- `type Object struct { Fields map[string]json.RawMessage; Duplicates []string }`: a duplicated key keeps its first value, and `Duplicates` is sorted;
- `type Error struct { Status int; Detail string }`, returned as `*Error`.

**Rules:**
- The media type, read with `mime.ParseMediaType`, must be `application/json`; parameters such as `charset` are allowed. Anything else, or no header → 415.
- The body is read through `http.MaxBytesReader` → 413 past the limit.
- Token by token. A body that is not an object (an empty body included), malformed JSON, or data after the object → 400 with one detail.
- The package knows nothing about subscriptions.

**Done when:** tests cover
- [ ] every structural case of design §4.3;
- [ ] a `charset` parameter accepted;
- [ ] exactly 1 MiB accepted, one byte more → 413;
- [ ] duplicates.

**Pitfall (to verify in the task):** the size error can surface from inside the JSON decoder; it is recognized with `errors.As` and `*http.MaxBytesError`.

### Task 6: Create and get
Commit: `feat(httpapi): serve create and get`

**Goal:** the first endpoint slice works through curl.

**Scope:** `internal/subscription/adapters/httpapi`; `cmd/subscription-service`; `Makefile` (`SWAG_DIRS`); `.github/workflows/ci.yml` (smoke).

**Provides:**
- `func New(svc *application.Service, log *slog.Logger) *API`;
- `func (a *API) Register(mux *http.ServeMux)`;
- `POST /api/v1/subscriptions` and `GET /api/v1/subscriptions/{id}`.

**Rules:**
- **Field errors:** all in one response, in the schema's order (`service_name`, `price`, `user_id`, `start_date`, `end_date`), then unknown fields and then duplicate fields, each in alphabetical order. A duplicate's value is not validated. `null` equals absence; for `end_date` absence means open-ended.
- **Messages**, built from the domain's constants:
  - `is required`, `unknown field`, `duplicate field`;
  - `service_name`: `must be a string`, and one message per domain error;
  - `price`: `must be an integer between 0 and 2147483647`, for a wrong type, for `400.0` or `4e2`, and for the range alike;
  - `user_id`: `must be a valid UUID` or `must not be the nil UUID`;
  - dates: `must be in MM-YYYY format`, `month must be between 01 and 12`, `year must be between 2000 and 2100`;
  - `end_date`: `must not be before start_date`.
- **UUIDs (#77):** only the 36-character `8-4-4-4-12` form, hex in either case; responses in lower case.
- **Response:** 201, `Location: /api/v1/subscriptions/{id}` and the subscription; an open-ended subscription has no `end_date`.
- **Wiring:** `Register` goes into `httpserver.Options` in the composition root.
- **Swagger:** annotations on the handlers; errors described with `problem.Details`; `SWAG_DIRS` gains the httpapi and `platform/problem` directories.
- **CI smoke (#82):** `.paths | length > 0`; POST → 201, then GET through `Location` → 200 with the same fields.

**Done when:** tests cover
- [ ] every trap of design §4.3: `null`, `400.0`, `4e2`, `"abc"`, a duplicate, an array, data after the object, an empty body, 413, 415;
- [ ] the order of field errors independent of the order of the keys;
- [ ] `{…}`, `urn:uuid:` and 32 hex digits → 400 in the path and in the body; the nil UUID → 400;
- [ ] 404 as a problem;
- [ ] `Block` → 503 and exactly one WARN;
- [ ] `Fail(domain.ErrInvalid)` → 500 and exactly one ERROR;
- [ ] a request canceled by the client: nothing logged by the adapter;
- [ ] both paths in the generated specification;
- [ ] a curl walk through `make db` and `make run`.

**Pitfall (to verify in the task):** that swag resolves `problem.Details` from another package.

### Task 7: List
Commit: `feat(httpapi): list subscriptions`

**Goal:** the list endpoint (design §4.2, §4.3).

**Scope:** `internal/subscription/adapters/httpapi`.

**Rules:**
- `url.ParseQuery(r.URL.RawQuery)`; any error → 400 `query string is malformed` (#79).
- A present but empty value → `must not be empty`; a repeated parameter → `must not be repeated`; unknown parameters are ignored.
- `limit` outside its range or not an integer → `must be an integer between 1 and 100`; `offset` → `must be a non-negative integer`.
- Every parameter error in one response, in a fixed order: `user_id`, `service_name`, `limit`, `offset`. A malformed query string is a single error with no field list, like a malformed body.
- `user_id` in canonical form only; `service_name` through `NewServiceName`.
- `limit` from 1 to 100, 20 by default; `offset` ≥ 0, 0 by default.
- The response is `{items, limit, offset, total}`: the applied `limit` and `offset`, and `items` always an array.

**Done when:** tests cover
- [ ] `user_id=%zz` and a `;` → 400, not every user's subscriptions;
- [ ] an empty and a repeated parameter → 400;
- [ ] `limit` 0, 101 and `abc` → 400;
- [ ] two invalid parameters, such as `?offset=-1&limit=0`, → both errors in the fixed order, whatever order the client used;
- [ ] the defaults applied and returned;
- [ ] `items: []` when nothing matches;
- [ ] the path in the generated specification.

### Task 8: Update
Commit: `feat(httpapi): update subscriptions`

**Goal:** PUT as a full replacement (design §2.3, §4.1).

**Scope:** `internal/subscription/adapters/httpapi`.

**Rules:** the body schema has no `user_id`, so one in the body → `unknown field`; the body traps of Task 6 apply.

**Done when:** tests cover
- [ ] 200 with the replaced subscription;
- [ ] a closed period replaced by an open-ended one;
- [ ] `user_id` in the body → `unknown field`;
- [ ] 404 for a missing subscription;
- [ ] the path in the generated specification.

### Task 9: Delete and the final run
Commit: `feat(httpapi): delete subscriptions`

**Goal:** the last endpoint, and the phase checked as a reviewer runs it.

**Scope:** `internal/subscription/adapters/httpapi`.

**Done when:**
- [ ] DELETE → 204 with no body, or 404;
- [ ] the path in the generated specification;
- [ ] on `docker compose up --build`, all five endpoints work through curl and through Swagger UI;
- [ ] `make lint test` is green;
- [ ] CI is green on the pull request.
