# Phase 4: Total Cost — Implementation Plan

> **Execution:** one task at a time, in order. Each task is reviewed and committed before the next one starts (CLAUDE.md, Workflow). Checkboxes (`- [ ]`) mark the "Done when" results.

- **Status:** approved. It becomes historical when phase 4 merges. From then on the code and `docs/design.md` are the source of truth.
- **Spec:** `docs/design.md` §2.4, §2.5, §2.7, §3.3, §4.1–§4.4, §5.4, §6, §9, §10.4, §11 and the phase 4 row of §12; the phase 3 plan's "Hands over to phase 4"; decisions #78, #79, #84 and #85.
- **Branch:** `feature/total-cost`. Pull request title: `feat: total cost (phase 4)`.
- **First commit:** `docs: add total cost plan`. A decision goes into the design before any code depends on it, so this commit also carries:
  - design §4.1: `PUT` and `DELETE` on the total-cost path reach `{id}` and get 400 on `id`, and why not 405;
  - design §4.3: the order of total-cost parameter errors and the messages of `from` and `to`;
  - design §5.4: integers above 2^53 − 1 in JSON, accepted;
  - design §10.4 and §11: the smoke job checks total cost; the fake runs the same cases as the integration tests;
  - decisions #84 and #85, with #82 marked as amended;
  - CLAUDE.md, API: a parameter's Swagger example is `x-example`.

## What this plan is

Like the plans of phases 2 and 3, it records intent: what each task must achieve, which rules apply, what it hands to the next task, when it is done, and the pitfalls already found and verified. **Provides** lists the exact signatures later tasks build on. It contains no function bodies or test code.

## Goal

`GET /api/v1/subscriptions/total-cost?from=MM-YYYY&to=MM-YYYY[&user_id=…][&service_name=…]` → `{"total_cost": N}` (design §2.7, §4.1, §4.2): the Postgres query, its use case, and the endpoint with parameter validation.

Done when integration tests pass for every case of §2.7 and §11, the endpoint works through curl and Swagger UI, and `make lint test`, smoke and CI are green.

Out of scope: the README (phase 5); indexes on the dates; new dependencies; changes to the domain, where `TotalCostQuery`, `CalculationPeriod` and the `TotalCostReader` port exist since phase 2.

## Decisions

| # | Decision | Why |
|---|---|---|
| 1 | A separate type `postgres.TotalCostReader` over the same pool | §3.3 names two Postgres adapters. One type implements one port (a light CQRS), so the read side can move to a replica or a cache without touching the repository |
| 2 | `where(f domain.Filter, args []any, conditions ...string) (string, []any)` | The filter is still built in one place. The caller's arguments come first and the filter numbers its `$n` after them; the SQL of list and count does not change |
| 3 | The fake `domaintest.TotalCostReader` reads the same store and applies the rule in Go | Fakes keep the port's contract (CLAUDE.md). An HTTP test then catches swapped `from` and `to` or a lost filter; a stub that returns a fixed number cannot |
| 4 | One table of cases, `domaintest.TotalCostCases`, run by both the fake and Postgres | A contract test (Software Engineering at Google, ch. 13): the fake is checked against the real implementation. A new case is written once and checks both |
| 5 | One `MM-YYYY` parser for the body and the query: `parseMonth` | One rule, one place. The body tests do not change, which shows the behavior did not |
| 6 | `PUT` and `DELETE` on `/total-cost` → 400 on `id`, pinned by a test | Design §4.1 |
| 7 | The smoke job requires the total cost of the created subscription: `2400` for `01-2025…12-2025` | No test covers `main.go`; only the smoke job sees a reader that is not wired. It is also the one run of the query in the built image |
| 8 | No test of a 64-bit overflow | It needs about 3.5 million rows. The `numeric → bigint` cast was verified by hand (`bigint out of range` → 500); the 32-bit risk is covered by the maximum price × 1 212 case |
| 9 | A parameter's Swagger example is `extensions(x-example=…)`, not `example(…)` | Swagger 2.0 has no `example` field on a non-body parameter (`additionalProperties: false`, only `^x-` extensions); Swagger UI reads `x-example` for such a parameter |

## Global rules

- **TDD:** every behavior in "Done when" is covered by a test that was seen to fail before the code existed. One commit per task, in English, Conventional Commits.
- **Layers:** `httpapi → application → domain ← postgres`; identifiers are created only through the domain's constructors.
- **Status by stage (#78):**
  - parsing the request → 400;
  - calling the application: an expired deadline → 503 and one WARN; a canceled request → nothing; anything else → 500 and one ERROR;
  - total cost never returns `ErrNotFound`.
- **Logging:** total cost is a read, so it logs no business event.
- **Comments:** they explain why and never cite the design.

## Review focus

These failure modes are implied by the spec but easy to miss. Each is covered by the task in brackets.

1. `{id}` takes over `total-cost` (Task 3).
2. Off by one at the edges: both ends inclusive, across a year boundary (Task 1).
3. An open-ended subscription counts until `to`, and one that starts after `to` does not count at all (Task 1).
4. A 32-bit overflow of `price × months`; `NULL` instead of 0 for no rows (Task 1).
5. A malformed query silently drops a filter, and the total covers every user (Task 3).
6. An inverted-period error alongside an invalid month, so one mistake gives two errors (Task 3).
7. The fake and the database disagree on the rule (Tasks 1 and 2: one table of cases; Task 3: smoke).
8. The specification stops being valid Swagger 2.0 (Task 3: `x-example`, not `example`).

## Hands over to phase 5

- README: a curl example for total cost and the §2.7 rule in plain words; `make db` before `make run`, because `compose.yaml` does not publish the database port.
- Check Swagger against real behavior, including 400 for `PUT /total-cost` and the `Allow` of a 405.
- Validate `doc.json` once against the Swagger 2.0 schema, with no new dependency in the repository: the smoke job checks only `.swagger == "2.0"`.
- The final check of CLAUDE.md.
- Before merging: `gh pr view`. The pull request's head is the last commit of Task 3, and its title is `feat: total cost (phase 4)`.

---

### Task 1: Total cost in Postgres
Commit: `feat(postgres): read the total cost of subscriptions`

**Goal:** the database computes the total cost of design §2.7 in one query (design §5.4).

**Scope:** `internal/subscription/adapters/postgres` (`total_cost.go`, `list.go`, tests); `internal/subscription/domain/domaintest/total_cost_cases.go`.

**Provides:**
- `func NewTotalCostReader(pool *pgxpool.Pool) *TotalCostReader`
- `func (r *TotalCostReader) TotalCost(ctx context.Context, q domain.TotalCostQuery) (int64, error)`
- `var _ domain.TotalCostReader = (*TotalCostReader)(nil)`
- `where(f domain.Filter, args []any, conditions ...string) (string, []any)`; `List` and the count call `where(q.Filter, nil)`.
- In `domaintest`:
  ```go
  type TotalCostCase struct {
      Name               string
      Price              domain.Price
      SubscriptionPeriod domain.SubscriptionPeriod
      CalculationPeriod  domain.CalculationPeriod
      Want               int64
  }
  func TotalCostCases(t testing.TB) []TotalCostCase
  ```
  The values are built through the domain's constructors, and an error calls `t.Fatal`, as `pgtest.NewDatabase` does. depguard allows it: `testing` is part of `$gostd`.

**Rules:**
- **One query:**
  - `WHERE` holds the overlap first, `start_date <= $to AND (end_date IS NULL OR end_date >= $from)`, then the filter's conditions;
  - months = index(`LEAST(COALESCE(end_date, $to), $to)`) − index(`GREATEST(start_date, $from)`) + 1, where index = `EXTRACT(YEAR …) * 12 + EXTRACT(MONTH …)`;
  - the sum is `COALESCE(SUM(price::bigint * months), 0)::bigint`.
- **The overlap is in `WHERE`, not a `max(0, …)` in `SELECT`:** rows outside the period are never read, and a negative month count cannot occur.
- **Dates** go through the existing `firstDay`; values are always bound parameters.
- **The error** is `read total cost: %w`, a verb phrase like `list subscriptions`.
- **`Want`** is written as months × price, such as `3 * 400`, never as the rule's formula, or the table would repeat the logic it checks.

**Cases of `TotalCostCases`.** Unless stated otherwise, the calculation period is `01-2025…12-2025` and the price is 400:

| Case | Subscription period | Want |
|---|---|---|
| inside | 03-2025…05-2025 | `3 * 400` |
| overlapping the start, across a year | 10-2024…02-2025 | `2 * 400` |
| overlapping the end | 11-2025…03-2026 | `2 * 400` |
| covering | 01-2020…12-2030 | `12 * 400` |
| open-ended, starting inside | from 11-2025 | `2 * 400` |
| open-ended, starting before `from` | from 06-2024 | `12 * 400` |
| open-ended, starting after `to` | from 01-2026 | 0 |
| touching `from` | 06-2024…01-2025 | `1 * 400` |
| touching `to` | 12-2025…03-2026 | `1 * 400` |
| disjoint, before | 01-2024…12-2024 | 0 |
| disjoint, after | 01-2026…02-2026 | 0 |
| a one-month subscription | 07-2025…07-2025 | `1 * 400` |
| a one-month calculation period, `07-2025…07-2025` | 01-2025…12-2025 | `1 * 400` |
| price 0 | 03-2025…05-2025, price 0 | 0 |
| the maximum: both periods `01-2000…12-2100` | price 2 147 483 647 | `1212 * 2147483647` = 2 602 750 180 164 |

**Done when:** tests cover
- [ ] every case of `TotalCostCases`;
- [ ] the sum of several subscriptions of one user;
- [ ] no rows → 0;
- [ ] the filters: a Cyrillic service name in another case; user and service together; the zero filter summing every user;
- [ ] the existing list tests, the index check included, green without changes.

**Test layout:**
- Each case gets a user of its own and filters by it. One database serves the whole table, the subtests run in parallel, and every case also checks the user filter.
- The zero filter and "no rows" get databases of their own.

**Known pitfalls (verified on the image from `compose.yaml`, `C` locale):**
- `SUM` over no rows is `NULL`, and `NULL` does not scan into an `int64`: hence `COALESCE`.
- `EXTRACT` returns `numeric` since PostgreSQL 14. `price::bigint` is written anyway: should the month count ever become `integer`, `integer × integer` fails at the maximum price × 2 with `integer out of range`.
- `LEAST` and `GREATEST` ignore `NULL`. `COALESCE(end_date, $to)` stays: the query reads like the §2.7 rule.
- pgx sends parameters untyped. With the period first and the filter after it, Postgres infers `{date, date, uuid, text}`, so no `::date` casts are needed.
- The Cyrillic filter works after the period's parameters: `lower($4 COLLATE pg_c_utf8)` finds «Яндекс Плюс» by «ЯНДЕКС ПЛЮС».

### Task 2: The use case and the fake
Commit: `feat(application): calculate the total cost`

**Goal:** the application exposes total cost, and the layers above have a fake to test it over (design §6).

**Scope:** `internal/subscription/application`; `internal/subscription/domain/domaintest`; `cmd/subscription-service/main.go`; `newEnv` in the httpapi tests, for the new signature; the fakes line of CLAUDE.md, Testing.

**Provides:**
- `func NewService(repo domain.Repository, totals domain.TotalCostReader, log *slog.Logger) *Service`
- `func (s *Service) TotalCost(ctx context.Context, q domain.TotalCostQuery) (int64, error)`
- In `domaintest`: `func NewTotalCostReader(store *Repository) *TotalCostReader` and `var _ domain.TotalCostReader = (*TotalCostReader)(nil)`.

**Rules:**
- **The service** calls the reader and wraps its error as `calculate total cost: %w`. It logs no event.
- **The fake:**
  - goes through `store.begin(ctx)`, so the store's `Fail` and `Block` act on both ports, as one database would;
  - filters with the existing `matchesFilter`;
  - counts months by the index `year * 12 + month`.
- **`main.go`:** `subscriptionpostgres.NewTotalCostReader(pool)` → `application.NewService`.
- **CLAUDE.md, Testing:** "The total-cost fake reads the same in-memory store; `domaintest.TotalCostCases` holds the rule's cases, and both the fake's tests and the Postgres integration tests run them."

**Done when:** tests cover
- [ ] the fake: every case of `TotalCostCases`, the filter, no rows → 0, `Fail`, `Block`;
- [ ] the service: it returns the reader's number; its error satisfies `errors.Is` and names the operation; nothing is logged;
- [ ] every existing test green with the new `NewService` signature.

### Task 3: The endpoint and the smoke check
Commit: `feat(httpapi): calculate the total cost`

**Goal:** the endpoint of design §4.1, with the parameter rules of §4.3, its Swagger annotations, and the smoke check of §10.4.

**Scope:**
- `internal/subscription/adapters/httpapi`, laid out like list: the handler in `subscriptions.go`, parsing in `query.go` and `request.go`, the response in `response.go`, tests in a new `total_cost_test.go`;
- the smoke job in `.github/workflows/ci.yml`.

**Provides:**
- the route `GET /api/v1/subscriptions/total-cost` → `(a *API) totalCost`;
- `readTotalCost(values url.Values) (domain.TotalCostQuery, []problem.FieldError)`;
- `(p *params) month(name string) (domain.MonthYear, bool)`: the parameter is required, and an absent one → `is required`;
- `parseMonth(s string) (domain.MonthYear, string, bool)`: the one parser behind `body.month` and `params.month`. It returns the month, the message and ok, in the shape of `pathID`;
- `totalCostResponse` with one field, `TotalCost int64`, tagged `json:"total_cost" example:"4800"`.

**Rules:**
- **Parameters:** strict `parseQuery`, the existing `filter()`, errors in the order `user_id`, `service_name`, `from`, `to`.
- **An inverted period** is checked only when both months are valid, and reported on `to` as `must not be before from`.
- **The error of `NewTotalCostQuery`** cannot occur after a valid period. It is still handled, not ignored: `p.fail("from", monthMessage(err))`, as `body.period` handles the unreachable error of `NewOpenEndedSubscriptionPeriod`.
- **`fail`** is called with the operation `calculate total cost`.
- **Swagger:**
  - `from` and `to` are required, with `extensions(x-example=01-2025)` and `extensions(x-example=12-2025)`; the `MM-YYYY` format is described in the parameter's text, because swag emits no `pattern` for a parameter;
  - no `example(…)` on a parameter (decision 9);
  - responses 200, 400, 500 and 503; no 404;
  - `@Description`: both ends inclusive, an open-ended subscription counts until `to`, no match → 0.
- **Smoke:** after create → get, `curl -fsS ".../total-cost?from=01-2025&to=12-2025&user_id=60601fee-…" | jq -e '.total_cost == 2400'`.

**Done when:** tests and checks cover
- [ ] routing: `GET …/total-cost` reaches total cost, not `{id}`; `PUT` and `DELETE` on the same path → 400 on `id`;
- [ ] the response is exactly `{"total_cost": N}` with a JSON content type, and 0 when nothing matches;
- [ ] the period and the filters: both ends inclusive; the user filter; the service filter in another case;
- [ ] parameter errors:
  - `from` and `to` absent, empty or repeated;
  - the format: `2025-01`, `1-2025`, `01-25`;
  - month `00` or `13`; year `1999` or `2101`;
  - an inverted period → an error on `to`;
  - no inverted-period error when either month is invalid;
- [ ] every error in one response, in a fixed order, whatever order the client used (10 tries each, as for list);
- [ ] a malformed query → 400; unknown parameters ignored;
- [ ] `Block` → 503 and a WARN naming the operation; `Fail` → 500 and an ERROR; a canceled request → nothing;
- [ ] the body tests (create, update) green without changes after `parseMonth` is extracted;
- [ ] `make swagger`: the path is there; `from` and `to` in `doc.json` have `x-example` and no `example`; Swagger UI fills the examples in "Try it out";
- [ ] the smoke job green.

**Known pitfalls (verified on go1.27 and the pinned swag version):**

| Case | Result |
|---|---|
| `GET`, `HEAD /total-cost` | total cost: the literal segment wins over `{id}` |
| `PUT`, `DELETE /total-cost` | the `{id}` handler with `id=total-cost` → 400 |
| `POST`, `PATCH /total-cost` | 405, `Allow: DELETE, GET, HEAD, PUT` |
| `/total-cost/` | 404 |
| `/Total-Cost` | `{id}` → 400: paths are case-sensitive |
| a pattern for the path with no method | panic: it conflicts with `GET /{id}` |
| explicit `PUT` and `DELETE` routes on the path | they register, but their `Allow` differs from the router's. Rejected |
| swag `example(01-2025)` on a parameter | `"example": "01-2025"`, a field the Swagger 2.0 schema forbids there |
| swag `extensions(x-example=01-2025)` | `"x-example": "01-2025"`, allowed by the schema (`^x-`) |
