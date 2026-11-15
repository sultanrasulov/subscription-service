# Subscription Service — Design

- **Status:** approved
- **Date:** 2026-11-13
- **Scope:** the whole service (test assignment for the Effective Mobile Golang Developer role)

This document records what the service does, the rules it follows, and why each decision was made. It deliberately contains no code.

Once the service is implemented, the source of truth is:
- the code and its tests, for behavior;
- the generated Swagger specification, for the HTTP contract.

This document then remains the record of intent and decisions. Step-by-step implementation plans are written separately, one per phase, when that phase starts (§12).

## 1. Goal and scope

### 1.1 Requirements (from the assignment)

A REST service that aggregates data about users' online subscriptions.

1. CRUDL on subscription records. A record has:
   - a service name;
   - a monthly price in whole rubles;
   - a user ID (UUID);
   - a start month (`MM-YYYY`);
   - an optional end month.
2. An endpoint that returns the total cost of all subscriptions over a chosen period, filtered by user ID and service name.
3. PostgreSQL with migrations.
4. Logging.
5. Configuration in `.env` or `.yaml`.
6. Swagger documentation.
7. The service runs with `docker compose`.

The assignment also says:
- user existence is not checked;
- kopecks are not used.

### 1.2 Success criteria

- From a clean clone, `docker compose up --build` starts the service and its database with no other steps.
- The service is reachable on `http://localhost:8080`, and Swagger UI on `http://localhost:8080/swagger/index.html`. If 8080 is taken, `APP_PORT` moves the host port (§10.3).
- All six endpoints behave as specified in §4, including every documented error case.
- CI is green: lint, unit tests, integration tests, and a smoke test against the composed stack.

### 1.3 Non-goals

Each of these is out of scope on purpose. §13 lists when to revisit them.

- Authentication or authorization.
- Checking that users exist.
- Currencies other than rubles. Fractional prices and proration are also out.
- Price history and discounts.
- Idempotency keys.
- Soft delete.
- Optimistic locking (ETag / If-Match). PUT is last-write-wins.
- A uniqueness rule. Duplicate and overlapping records are allowed.

## 2. Domain model

There is one bounded context, `subscription`. The model is small on purpose. Value objects carry every validation rule, including the one rule that spans two fields (end not before start), and the aggregate keeps identity and consistency.

### 2.1 Ubiquitous language

| Term | Meaning |
|---|---|
| Subscription | One record: a user pays a monthly price for a service during a subscription period |
| Subscription period | When a subscription is active: from a start month, optionally until an end month (inclusive) |
| Open-ended subscription | A subscription whose period has no end month. It is active until it is ended or deleted |
| Month | A calendar month and year (`MonthYear`). The service works at month granularity only |
| Calculation period | The inclusive range of months `from…to` over which the total cost is calculated. Both ends are required |
| Total cost | The sum, over matching subscriptions, of price × the number of months the subscription period overlaps the calculation period |

### 2.2 Value objects

Value objects are immutable, and their fields are not accessible from outside the domain package. Outside the package, a value object can be obtained in only two ways: through its validating constructor, or as the Go zero value. The aggregate rejects zero values that are invalid. A zero price is valid and means 0 rubles.

| Value object | Rules | Behavior |
|---|---|---|
| `MonthYear` | month 1–12; year 2000–2100 | Ordered comparison. The domain never reads the clock |
| `ServiceName` | Trims the input and collapses internal whitespace to single spaces. The result must be 1–100 characters (Unicode runes). Case is preserved | Matching is case-insensitive and happens in the store (§5.4) |
| `Price` | Whole rubles, 0 to 2 147 483 647 (the Postgres `INTEGER` maximum) | Exposes the amount; no arithmetic |
| `UserID` | A UUID that is not the nil UUID | The same rule applies to the request body and to the filter, so it lives in one place. It also keeps a user ID from being passed where a subscription ID is expected |
| `SubscriptionPeriod` | A start month, an optional end month, and end not before start | Held entirely by value, with no pointers, so nobody can change it after validation, and two periods compare equal by value. Read-only accessors: the start, and the end as "present or absent". No methods depend on the current date |
| `CalculationPeriod` | `from ≤ to`, both required | The period over which the total cost is calculated |

`SubscriptionPeriod` and `CalculationPeriod` are deliberately separate types. A subscription may have no end, while a calculation period must have one. A single type would allow a calculation period without `to`, and that rule would then need a separate check.

This fixes a defect in the earlier draft. Its period type kept the caller's pointer to the end month and handed its own pointer back out, so the end could be changed after validation and the invariant broken. The draft type also had about fifteen methods the assignment does not need, several of them based on the current date. None of these are carried over.

Why the year range is 2000–2100:
- It rejects placeholder dates that clients send when a date was never set. The common ones are `01-1970` (Unix epoch) and `01-1900` (SQL Server and Excel epochs).
- It rejects typos such as `2205` or `0225`.
- It caps the months per record at 1 212, which bounds the total (§5.4).

The range is a business assumption: online subscriptions do not predate 2000. Widening it changes one domain constant and the matching database constraint.

### 2.3 Aggregate: `Subscription`

**State:** an ID, a `UserID`, a `ServiceName`, a `Price` and a `SubscriptionPeriod`.

**Invariants:**
- The ID is not the nil UUID.
- No value object is an invalid zero value.

The period's own rule (end not before start) is already guaranteed by `SubscriptionPeriod`.

**Operations:**
- **Create** takes all fields, including an ID supplied by the caller (§6), and checks the invariants. The same constructor also loads records from the database. Both paths run the same checks, so a corrupted row fails loudly instead of loading silently.
- **Update** is a full replacement of the service name, the price and the period, matching PUT semantics. A period without an end makes the subscription open-ended. The user ID is not part of Update: it is set once at creation and never changes.
- **Read accessors** exist for every field.

**Business usage:**
- A trial period is a separate record with price 0.
- A price change is a new record: end the old one, then create the new one.

**Outside the domain:** the API keeps the flat `start_date` and `end_date` fields, and the table keeps two date columns. The adapters build the `SubscriptionPeriod` from them.

### 2.4 Queries

- **Filter:** an optional `UserID` and an optional `ServiceName`. Filters that are set are combined with AND. List and total cost use the same filter semantics.
- **List query:** a filter plus a page, given as limit and offset. The HTTP adapter validates the page (§4.3).
- **Total-cost query** (`TotalCostQuery`): a filter plus a `CalculationPeriod`.

### 2.5 Ports

The domain owns these interfaces, and the Postgres adapter implements them.

| Port | Operations | Errors |
|---|---|---|
| Repository | create, get by ID, update, delete by ID, list a page with the total number of matches | not found (get, update, delete) |
| TotalCostReader | total cost for a total-cost query, as a whole number of rubles | — |

`TotalCostReader` is the read side for the total, a light form of CQRS. The `Reader` suffix says so: the port does not calculate anything in the domain; it reads a sum that the database computes. A single database query computes the sum (§5.4). Integration tests pin down the rule in §2.7.

### 2.6 Errors

The domain defines two error categories:
- **not found** → HTTP 404;
- **invalid data** → HTTP 400.

Each specific validation failure belongs to the "invalid data" category and has its own identity. Examples: year out of range, invalid month, empty or too-long service name, price out of range, nil user ID, end before start, inverted calculation period. The HTTP adapter can therefore treat them all as 400 and still turn each one into a field-level message. For example, "end before start" is reported on `end_date`.

### 2.7 Total-cost rule

For each subscription that matches the filter:

```
effective_end = end ?? to
months        = max(0, min(effective_end, to) − max(start, from) + 1)   // in whole months
total         = Σ price × months
```

- Both ends of the calculation period are inclusive. An open-ended subscription counts until `to`.
- No matching subscriptions → total 0.

Examples for the period `01-2025…12-2025`:

| Subscription period | Months counted |
|---|---|
| `03-2025…05-2025` | 3 |
| `10-2024…02-2025` | 2 |
| `11-2025`, open-ended | 2 |
| `01-2020…12-2030` | 12 |
| `01-2024…12-2024` | 0 |

The database evaluates this rule, not Go code (decision #3). Integration tests over these edge cases (§11) are its executable specification.

**Revisit trigger:** proration of partial months, discounts, or price history. Any of them moves the calculation into the domain, where its natural home is an overlap operation between `SubscriptionPeriod` and `CalculationPeriod`.

## 3. Architecture

### 3.1 Layout

```
subscription-service/
├── cmd/subscription-service/      # entry point and composition root (manual wiring, no DI framework)
├── internal/
│   ├── subscription/              # bounded context
│   │   ├── domain/                # aggregate, value objects, queries, ports, errors
│   │   ├── application/           # use cases
│   │   └── adapters/
│   │       ├── httpapi/           # driving: HTTP/JSON ↔ use cases; Swagger annotations
│   │       └── postgres/          # driven: implements the Repository and TotalCostReader ports
│   └── platform/                  # technical infrastructure; knows nothing about subscriptions
│       ├── config/                # environment → typed, validated configuration
│       ├── logger/                # structured logging (JSON or text) with request IDs
│       ├── postgres/              # connection pool and migration runner
│       ├── problem/               # RFC 9457 problem+json: the one implementation of the error format
│       └── httpserver/            # server, middleware, /health, /ready, Swagger, graceful shutdown
├── migrations/                    # versioned SQL migrations, embedded into the binary
├── docs/
│   ├── design.md                  # this document
│   └── swagger/                   # generated specification, embedded into the binary, not committed
├── .github/workflows/             # CI
├── compose.yaml                   # the stack a reviewer runs
├── compose.dev.yaml               # local development overlay, used only through the Makefile
└── .env.example, .golangci.yml, Dockerfile, Makefile, README.md
```

Each adapter states its role (driving/inbound or driven/outbound) in its package documentation. Inbound and outbound adapters get separate directories once one side has a second adapter.

### 3.2 Dependency rules

```
httpapi → application → domain ← postgres
adapters → platform
platform ↛ subscription
```

golangci-lint `depguard` enforces these rules:

| Package | Must not depend on |
|---|---|
| domain | HTTP, JSON, SQL and database drivers; `application`, `adapters`, `platform` |
| application | HTTP, JSON, database drivers; `adapters`, `platform` |
| adapters/httpapi | `adapters/postgres`, database drivers |
| adapters/postgres | `adapters/httpapi`, HTTP |
| platform | anything under `internal/subscription` |

The domain depends only on the standard library and a UUID library.

### 3.3 Composition root

The entry point wires everything by hand, in this order:
1. configuration;
2. logger;
3. Postgres pool;
4. migrations;
5. the Postgres adapters (repository and total-cost reader);
6. the application service;
7. the HTTP adapter;
8. the HTTP server, which receives the API routes, the pool as its readiness check, and the embedded Swagger specification.

The graph has about eight nodes, so a DI framework would add a code generator and no value.

### 3.4 Startup and shutdown

1. Load and validate the config. Invalid or missing values → log every problem and exit with an error.
2. Connect to Postgres and ping. Failure → exit with an error. Compose starts the app only after the database is healthy.
3. Apply migrations (§5.3). Failure → exit with an error; the HTTP server never starts.
4. Serve HTTP with the configured timeouts (§7).
5. On SIGINT or SIGTERM:
   - stop accepting connections;
   - wait up to the configured shutdown timeout for in-flight requests;
   - close the pool;
   - exit cleanly.

## 4. HTTP API

The base path is `/api/v1`. Bodies are JSON. Dates in requests and responses are `MM-YYYY` strings. The JSON below is illustrative; once implemented, the Swagger specification is authoritative.

### 4.1 Endpoints

| Method | Path | Success | Errors |
|---|---|---|---|
| POST | `/api/v1/subscriptions` | 201 + `Location` header + the subscription | 400, 413, 415 |
| GET | `/api/v1/subscriptions/{id}` | 200 + the subscription | 400 (malformed id), 404 |
| PUT | `/api/v1/subscriptions/{id}` | 200 + the subscription | 400, 404, 413, 415 |
| DELETE | `/api/v1/subscriptions/{id}` | 204 | 400, 404 |
| GET | `/api/v1/subscriptions` (filters + page) | 200 + a page of subscriptions | 400 |
| GET | `/api/v1/subscriptions/total-cost` (period + filters) | 200 + the total | 400 |

Any endpoint can also return 500 on an unexpected failure, with no internal details in the response.

The total-cost path does not collide with the `{id}` path. The standard router in Go 1.22+ prefers the literal segment.

### 4.2 Representations

**Create request:**
```json
{
  "service_name": "Yandex Plus",
  "price": 400,
  "user_id": "60601fee-2bf1-4721-ae6f-7636e79a0cba",
  "start_date": "07-2025",
  "end_date": "12-2025"
}
```

**Update request:** the same fields without `user_id`.

**Subscription response:** the create fields plus `id`.
- `end_date` is omitted when the subscription is open-ended, instead of being `null`. There are three reasons:
  - Swagger 2.0 cannot express nullable fields, but it describes an optional field exactly.
  - It matches the request side, where an omitted `end_date` means open-ended.
  - The Google JSON Style Guide recommends dropping empty properties rather than writing `null`.
- Creation and update times are not exposed.

**List response:**
```json
{"items": [ ... ], "limit": 20, "offset": 0, "total": 57}
```
- `items` is always an array, `[]` when nothing matches, never `null`.
- `limit` and `offset` are the values actually applied, defaults included.
- `total` is the number of all matching records, so a client can tell how many pages there are.

**Total-cost response:** `{"total_cost": 4800}`. It has no currency field and does not echo the parameters. No matches → 0.

### 4.3 Input rules

**Content type.** POST and PUT require a `Content-Type` whose media type is `application/json`; parameters such as `charset=utf-8` are accepted. A different or missing content type → 415. The README notes that `curl -d` without `-H 'Content-Type: application/json'` sends a form, not JSON.

**Body size.** A body over 1 MiB → 413.

**Body structure.** These fail with a single error and no field list:
- malformed JSON;
- a body that is not a JSON object (for example an array, a string, or an empty body);
- trailing data after the object.

**Field errors.** Otherwise, validation reports **every** field error in one response, ordered by the schema's field order (`service_name`, `price`, `user_id`, `start_date`, `end_date`), followed by unknown and duplicate fields. The same mistakes therefore always produce the same response, whatever order the client used.

| Situation | Message |
|---|---|
| The same key appears twice | `duplicate field` |
| An unknown key, including `user_id` on PUT (the user of a subscription can never change) | `unknown field` |
| A required field is missing **or is `null`** | `is required` |
| A value of the wrong JSON type, such as `"price": "abc"` or `"price": 400.0` | a type message for that field |
| A value object rule is broken, then end before start | the rule's message for that field |

The duplicate-key rule needs explanation. RFC 8259 leaves the behavior undefined, and I-JSON (RFC 7493) forbids duplicates. Parsers that pick different copies of a duplicated key can be used to bypass validation, so a strict API rejects them.

The null rule also needs explanation. In `encoding/json`, a JSON `null` decoded into a number is ignored without an error: the field keeps its previous value, which is 0 in a freshly declared struct. So `"price": null` must be caught explicitly. Otherwise it would pass as a price of 0, which breaks "price is required, an explicit 0 is valid".

**Field rules:**

| Field | Required | Rule | Example message |
|---|---|---|---|
| `service_name` | yes | string; normalized, 1–100 characters | `must not be empty` |
| `price` | yes | JSON integer, 0…2147483647; `0` is valid | `must be an integer between 0 and 2147483647` |
| `user_id` | yes (create only) | UUID, not the nil UUID | `must be a valid UUID` |
| `start_date` | yes | `MM-YYYY`, month 01–12, year 2000–2100 | `must be in MM-YYYY format` |
| `end_date` | no | same format and range; not before `start_date`; omitted or `null` → open-ended | `must not be before start_date` |

**Path:** a malformed `{id}` → 400 for field `id`.

**Query parameters** (list and total cost):
- An absent parameter means no filter.
- A parameter that is present but empty (`?user_id=`) → 400.
- A repeated parameter → 400.
- Unknown parameters are ignored, because proxies and tracking tools add them (`utm_*`, `_=`).
- `user_id` follows the `UserID` rules: a valid, non-nil UUID.
- `service_name` is normalized like stored names and matched against them case-insensitively.
- Total cost only: `from` and `to` are required, use `MM-YYYY` with years 2000–2100 (the same `MonthYear` rules as stored dates), and must satisfy `from ≤ to`. Otherwise → 400, reported on field `to` for an inverted period.

**Pagination** (list only):
- `limit` defaults to 20 and must be an integer from 1 to 100. Anything else → 400; values above 100 are not silently capped.
- `offset` defaults to 0 and must be an integer ≥ 0. Anything else → 400.
- Results are ordered by ID. IDs are UUID v7, so the order is stable across pages and roughly follows creation time. It is strictly monotonic only within one process; IDs created in the same millisecond by different processes may interleave.

### 4.4 Errors

Every error response is RFC 9457 `application/problem+json`, including responses produced by the router itself. Validation errors carry a list of field errors:

```json
{
  "type": "about:blank",
  "title": "Bad Request",
  "status": 400,
  "detail": "request validation failed",
  "errors": [
    {"field": "price", "message": "must be an integer between 0 and 2147483647"},
    {"field": "start_date", "message": "must be in MM-YYYY format"}
  ]
}
```

| Cause | Status | Detail |
|---|---|---|
| Field validation, including the domain's "invalid data" errors | 400 | request validation failed + field errors |
| Malformed JSON, a body that is not an object, trailing data | 400 | a description of the structural problem |
| Body over 1 MiB | 413 | request body too large |
| Content type other than JSON, or missing | 415 | Content-Type must be application/json |
| Subscription not found | 404 | subscription not found |
| No route matches | 404 | not found |
| The route exists, but not for this method | 405 | method not allowed; the router's `Allow` header is kept |
| The request ran past its deadline | 503 | request timed out |
| Anything else | 500 | internal server error; the real error is logged once, with the request ID |

**Request deadline.** The server's write timeout closes the connection, but it does not cancel the handler's context. A hung database query would keep running after the client is gone. Middleware therefore gives every request a context with a deadline (`HTTP_REQUEST_TIMEOUT`). That context reaches pgx, so a hung query is canceled, and the client receives a 503 problem+json instead of a dropped connection. 503 is also what the standard library's timeout handler uses. 504 is meant for gateways waiting on an upstream server.

**Client disconnects.** When the client goes away, the request context is canceled. That is not a server failure, so it is logged at WARN level and is not counted as a 500.

**One implementation of the format.** Two places write problem+json:
- the HTTP server: router errors and panics;
- the HTTP adapter: validation errors, "not found" and other internal errors.

Both use `internal/platform/problem`, so there is a single definition of the format, including field errors. The package is domain-neutral, which lets `platform` own it. It is separate from `httpserver`, so the adapter does not depend on the server package.

The standard router answers unmatched routes and wrong methods in plain text. A thin wrapper detects these cases, because the router reports that no pattern matched, and rewrites them as problem+json. A 404 returned by our own handlers ("subscription not found") is left untouched.

### 4.5 Operational endpoints

| Path | Purpose | Behavior |
|---|---|---|
| `GET /health` | Liveness: the process is up and serving | Always 200. Does not touch the database, so a database outage never makes an orchestrator restart healthy instances |
| `GET /ready` | Readiness: the service can handle requests | Pings Postgres with a 1 s timeout. 200 or 503. The compose healthcheck and the CI smoke test use it |
| `GET /swagger/doc.json` | Swagger specification | Serves the embedded specification; 404 if it was not generated |
| `GET /swagger/` | Swagger UI | Served without a CDN. `/swagger` and `/swagger/` redirect to `/swagger/index.html` |

`/health` and `/ready` are not documented in Swagger.

## 5. Persistence

### 5.1 Table `subscriptions`

| Column | Type | Null | Rule |
|---|---|---|---|
| `id` | UUID | no | Primary key. No database default: the application is the only source of IDs, so a missing ID fails loudly |
| `user_id` | UUID | no | |
| `service_name` | text, collation `pg_c_utf8` | no | 1–100 characters (§5.2) |
| `price` | integer | no | ≥ 0 |
| `start_date` | date | no | The 1st of a month, within 2000-01 … 2100-12 |
| `end_date` | date | yes | Same rule as `start_date`; not before `start_date` |
| `created_at` | timestamptz | no | Set by the database on insert |
| `updated_at` | timestamptz | no | Set on insert; the update statement itself sets it to the current time (no trigger) |

- A month is stored as a date on the 1st of that month, so values order and compare correctly. This fixes the earlier draft, which stored `MM-YYYY` strings.
- Database constraints repeat the domain rules as a last line of defense.
- `created_at` and `updated_at` exist for operations only. Neither the domain nor the API uses them.
- **Indexes:** on `user_id`, and on the lower-cased `service_name` for case-insensitive matching.

### 5.2 Case-insensitive matching and collation

Lower-casing Cyrillic depends on the collation that applies to an expression. The default database of the official Alpine image uses the libc `en_US.utf8` locale, which happens to lower-case Cyrillic. A database created with the `C` locale does not, so `lower('ЯНДЕКС')` stays upper-case there. Correctness must not depend on how somebody created the database.

The rule therefore lives in the schema and the queries, not in database initialization:
- The `service_name` column uses the built-in `pg_c_utf8` collation (PostgreSQL 17+). It lower-cases Unicode correctly, independent of the operating system.
- The filter value is lower-cased under the **same explicit collation**. A collation on the column applies only to the column's side of the comparison. The parameter would otherwise be lower-cased under the database default. This was verified on a UTF8 database with the `C` locale: with the collation only on the column, 0 rows matched; with it on both sides, the expected rows matched through an index scan.
- Compose needs no locale settings at database initialization. The tests deliberately set the `C` locale with UTF8 encoding to prove that the result does not depend on the locale (see **Tests** below).

**Tests.** All integration tests run on a database initialized with the `C` locale and UTF8 encoding, the least forgiving setup. Both settings must be explicit, because `C` alone yields `SQL_ASCII` (verified). Every integration test therefore also shows that results do not depend on the database locale.

The case-insensitivity test checks two things:
- **The row is found:** «ЯНДЕКС ПЛЮС» finds «Яндекс Плюс».
- **The query can use the index on the lower-cased name.** On a table of a few rows the planner always prefers a sequential scan, so a plain plan check would fail for the wrong reason. The test therefore disables sequential scans for its own transaction only, then checks that the plan uses that index.

Neither check is enough alone. The index is used even when the value was lower-cased under the wrong collation, and then nothing is found (verified).

`pg_c_utf8` exists only in UTF8 databases. On a database with any other encoding, the first migration fails immediately with a clear error. This is intended: the service cannot match names correctly there, so it must not start.

### 5.3 Migrations

- **Tool:** golang-migrate with the pgx v5 driver.
- **Embedding:** migration files are embedded into the binary. The service applies pending migrations at startup, before HTTP starts, so the schema always matches the code it was built with.
- **Concurrency:** golang-migrate takes a Postgres advisory lock, so concurrent starts are safe. "Nothing to apply" counts as success.
- **Transactions:** every migration file wraps its statements in an explicit `BEGIN … COMMIT`, so a failure does not leave the schema half-changed.
  - The pgx driver currently sends a whole file as one simple-protocol query, which Postgres runs as one implicit transaction. That is a side effect of the driver, not a guarantee: enabling golang-migrate's multi-statement mode would remove it. The explicit transaction does not depend on it.
  - A statement that cannot run inside a transaction, such as `CREATE INDEX CONCURRENTLY`, gets a migration of its own without the wrapper.
- **Failures:** golang-migrate marks a failed version as dirty, and recovery is a manual `force`, documented in the README.
- **Down migrations** never run automatically. An integration test checks up → down → up. The README documents manual rollback.
- Integration tests apply the same embedded migrations.

**Revisit trigger:** several replicas, a backup before migrating, or a separate role for schema changes. Then migrations move to a one-shot `migrate up` subcommand run from the same image.

### 5.4 Queries

- **Shared filter.**
  - List and total cost build their filter conditions in one place.
  - A condition is added only for filters that are set, and values are always bound parameters. This keeps the query planner able to use the indexes, which the "parameter is null or column equals parameter" pattern can defeat under a generic plan.
  - The service name condition compares both sides lower-cased under the `pg_c_utf8` collation (§5.2).
- **List.** Matching rows, ordered by ID, with limit and offset. The total comes from a separate count with the same filter. A window count over the page is not used: when the offset is past the end, no rows come back and the total would be lost.
- **Total cost.** One query that:
  - keeps only subscriptions whose period overlaps the calculation period: the start is not after `to`, and the end is absent or not before `from`;
  - for each one, counts the months from the later of (start, `from`) to the earlier of (end or `to`, `to`), inclusive;
  - multiplies the price by that count and sums the results; no rows → 0.
- **Overflow safety.**
  - The price is widened to a 64-bit integer before multiplying. A 32-bit product would overflow at the maximum price × 2 months.
  - The database sums with arbitrary precision (`SUM` over 64-bit integers returns `numeric`). The result is cast back to a 64-bit integer, which fails with an error if it does not fit. The API then returns 500 and logs the error, instead of losing precision silently. This was verified: the cast reports `bigint out of range`.
  - In practice: one record contributes at most 2 147 483 647 × 1 212 ≈ 2.6·10¹², so reaching the 64-bit limit takes about 3.5 million maximum-price records in a single query.

## 6. Application layer

One application service, with one method per use case. It depends on the two domain ports and a logger.

| Use case | Flow |
|---|---|
| Create | Generate a UUID v7 → create the aggregate → save → log `subscription created` |
| Get | Load by ID |
| Update | Load → apply the full replacement → save → log `subscription updated` |
| Delete | Delete by ID → log `subscription deleted` |
| List | Query the repository for a page and the total |
| Total cost | Query the total-cost reader |

- Inputs arrive already as value objects. Parsing and validating raw input is the HTTP adapter's job.
- The service generates IDs directly with the UUID library. There is no ID-generator abstraction, because there is nothing to swap.
- Errors are wrapped with context and returned, not logged here. They are logged once, at the HTTP boundary (§8).

## 7. Configuration

- The service reads only process environment variables, using the `caarlos0/env` library. It does not read files itself.
- Parsing reports every missing or malformed variable at once. A validation step then checks meaning: the log level is a known value, the port is in range, durations and pool size are positive, the request timeout is shorter than the write timeout, and the shutdown timeout is not shorter than the request timeout.
- **docker compose** supplies the values through `env_file`: first the committed `.env.example`, then an optional local `.env`, which is gitignored and overrides the example.
- **`make run`** loads the same two files in the same order. Variables already set in the shell win over both files, as with Compose interpolation and dotenv tools, so `POSTGRES_PORT=55432 make db` and `POSTGRES_PORT=55432 make run` agree on the port.

| Variable | Default | Notes |
|---|---|---|
| `HTTP_ADDR` | `:8080` | |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` | Required against slow-header attacks (Slowloris); linters flag a server without it (gosec G112) |
| `HTTP_READ_TIMEOUT` | `10s` | |
| `HTTP_WRITE_TIMEOUT` | `10s` | |
| `HTTP_IDLE_TIMEOUT` | `60s` | |
| `HTTP_REQUEST_TIMEOUT` | `8s` | Deadline for each request's context (§4.4). Must be shorter than the write timeout, so the client receives a problem+json response instead of a dropped connection |
| `HTTP_SHUTDOWN_TIMEOUT` | `10s` | How long graceful shutdown waits for in-flight requests. Must be at least the request timeout, so shutdown never cuts off a request that is still within its deadline. Docker's stop grace period must be longer (§10.3) |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | `json` | `json` or `text`, matched exactly. JSON is for machines (containers, log collectors), so compose needs no setting; text is for reading logs locally and goes in a developer's `.env` |
| `POSTGRES_HOST` | required | `localhost` in `.env.example`; compose overrides it with the database service name |
| `POSTGRES_PORT` | `5432` | Also the host port that the development overlay publishes (§10.3) |
| `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` | required | The same names the Postgres image reads, so one env file configures both containers without duplicated values |
| `POSTGRES_SSLMODE` | `disable` | |
| `POSTGRES_MAX_CONNS` | `10` | |
| `POSTGRES_CONNECT_TIMEOUT` | `5s` | Bounds opening every new connection and the ping at startup. Without it pgx waits for the operating system's TCP timeout, minutes long. Passed to pgx as a duration, so sub-second values work. The other pool settings (minimum connections, connection lifetime and idle time) keep pgx's defaults (§13) |

Compose interpolation (`${VAR}` in a compose file) reads only the shell and `.env`, not `env_file`. Sharing the image's variable names is what lets a clean clone start without copying `.env.example` to `.env`. The app's other `POSTGRES_*` names are not variables the image interprets.

`APP_PORT` (default 8080) is a compose-only variable: the host port that `docker compose up` publishes the app on. The service does not read it. Interpolation reads it from the shell or `.env`, for example `APP_PORT=18080 docker compose up`, so `.env.example` lists it only as a comment.

The connection string is built so that passwords containing reserved URL characters work.

## 8. Logging

- **Format:** structured `log/slog` records to stdout, JSON by default or text with `LOG_FORMAT=text` for local reading (§7). The level comes from `LOG_LEVEL`. Both formats carry the same fields, including the request ID.
- **Request ID:** an incoming `X-Request-ID` is reused if it is at most 64 printable characters; otherwise a new one is generated. The ID is returned in the response header and attached to every log record written while handling the request.
- **Request log:** one `request completed` line per request, with method, path, status, duration and request ID. A request abandoned by the client is recorded with status **499**, nginx's "client closed request" convention. Nothing is sent to the client, so the log would otherwise show a misleading 200 or 0.
- **Errors are logged exactly once:**
  - unexpected errors (500) at ERROR level, by the HTTP error writer, with the full error chain;
  - client errors (4xx) only through the request log line;
  - requests whose context was canceled because the client disconnected, at WARN level; they are not server errors;
  - requests that ran past their deadline (503), at WARN level, with the operation that was cut off;
  - panics by the recovery middleware, at ERROR level with the stack trace; the client receives a problem+json 500.
- **Business events** at INFO level: subscription created, updated and deleted, with the subscription ID and user ID.
- Startup and shutdown are logged, including the migration versions applied (from → to).

## 9. API documentation (Swagger)

- **Approach:** code-first with swaggo v1.
  - The source of truth is annotations on the HTTP handlers, plus general API info on the entry point.
  - The output is **Swagger 2.0**.
  - The swag generator version is pinned in `go.mod` as a tool dependency.
  - Swagger 2.0 cannot give each response its own content type. Error responses are therefore documented as `application/json` with the problem schema, while the server actually sends `application/problem+json`. This is a known limitation of the format and is accepted.
- **Generation:** `make swagger` produces the JSON and YAML specification. `make run`, `make build` and the Docker build all generate first. Only the specification is generated, never Go code, so building, testing, linting and IDEs work on a clean clone without generating anything.
- **Not committed:** the generated files are gitignored. A placeholder file keeps the directory in git, so embedding works on a clean clone.
- **Delivery:** the specification is embedded into the binary. The HTTP server receives it as an abstract file system and does not know where it came from, which also lets tests use an in-memory file system.
- **Missing specification:** a warning at startup ("swagger spec not embedded, run make swagger"), and `doc.json` returns 404. The service keeps running.
- **Always on:** Swagger UI has no feature flag.

## 10. Build, run, CI

### 10.1 Makefile

Make is the developer entry point. Go code still compiles without it, and the reviewer only needs `docker compose up --build`.

| Target | Purpose |
|---|---|
| `help` (default) | List the targets |
| `swagger` | Generate the Swagger specification |
| `build` | Generate the specification, then build a static, stripped binary |
| `db` | Start only Postgres, with its port published on the loopback interface (§10.3) |
| `run` | Generate the specification, load the env files, run the service on the host. Works from a clean clone after `make db` |
| `test` | All tests with the race detector. Integration tests need Docker. The race detector needs cgo and a C compiler, so only `build` disables cgo |
| `test-unit` | Unit tests only. Integration tests skip themselves in short mode |
| `lint` | golangci-lint |
| `up`, `down`, `logs` | Start the compose stack and wait until it is healthy; stop it; follow its logs |

### 10.2 Dockerfile

- **Builder stage:** the Go Alpine image matching `go.mod`, plus `make`, which the image does not include. It builds through `make build`, with cache mounts for the module and build caches, so only the modules the build needs are downloaded and both caches persist between builds. If Swagger generation fails, the image build fails, so a missing specification cannot slip through.
- **Runtime stage:** Alpine, a non-root user with the fixed numeric UID and GID 65532, and the single static binary, owned by root. Migrations and the Swagger specification are inside the binary. It exposes port 8080. BusyBox `wget` in the image serves the healthcheck.
- **Build context:** `.dockerignore` is an allow-list. Only the files the build reads enter the context, without tests, so a stray file in the working tree (a `.env`, a dump, a stale specification) cannot reach the image.
- **Signals:** the binary is started in exec form, so it is the container's main process and receives SIGTERM directly. A shell-form command would wrap it in `/bin/sh -c`, which does not forward the signal, and graceful shutdown would never run.
- **Toolchain check:** the builder fails unless its Go version equals the `toolchain` line in `go.mod`.
  - `GOTOOLCHAIN=local` alone is not enough: it ignores the `toolchain` line and checks only the `go` line (verified).
  - Dependabot does not bump `toolchain` (dependabot-core #13520). A Dependabot pull request for a new `golang` image therefore fails until the same pull request updates `toolchain`, so the drift is visible instead of silent (§10.5).

### 10.3 Compose files

`compose.yaml` is the preferred file name in the Compose Specification. It describes the stack a reviewer runs.

- **`postgres`:**
  - the PostgreSQL Alpine image, version 17 or later (`pg_c_utf8`, §5.2);
  - the shared env files (§7);
  - a `pg_isready` healthcheck;
  - a named data volume;
  - port **not published**, so a reviewer with a local Postgres on 5432 cannot hit "port is already allocated".
- **`app`:**
  - built from the Dockerfile;
  - the shared env files, with `POSTGRES_HOST` and `POSTGRES_PORT` overridden to the database service and its in-network port 5432. `POSTGRES_PORT` has two roles: the host port that `make db` publishes, and the port inside the compose network. A developer's `.env` may change the first, and the override keeps the second. `HTTP_ADDR` has the same two roles (the host address for `make run`, the in-container address), so the `app` service also fixes it to `:8080`, the port that is published and health-checked;
  - starts only after the database is healthy;
  - publishes its port on the loopback interface only, on the host at `APP_PORT` (8080 by default), so a taken 8080 needs no edit to `compose.yaml`. Inside the container it stays 8080, which the healthcheck uses;
  - has a healthcheck on `/ready`;
  - has a stop grace period of 15 s. `docker compose stop` sends SIGTERM and kills the container after the grace period, which is 10 s by default. The grace period must cover the 10 s shutdown timeout plus closing the pool.
- **Network:** both services share Compose's default network, `<project>_default`, so stacks started with different `-p` never share one. No `network_mode: host`: it would put the database port on the host, break service names and ignore the loopback binding (#70).

`compose.dev.yaml` is a committed development overlay:
- It publishes the database port on `127.0.0.1` at `POSTGRES_PORT`.
- Only `make db` uses it, by naming both files explicitly. Its name is deliberately not `compose.override.yaml`, which Compose would merge automatically and so change the reviewer's run.
- `make run` therefore works from a clean clone, and a plain `docker compose up` stays unchanged.

### 10.4 CI (GitHub Actions)

The workflow runs on every push and on pull requests to `main`.

| Job | Purpose |
|---|---|
| lint | golangci-lint at the version in `.golangci-lint-version`. It must be built with a Go version ≥ the module's `go` directive, so a Go update moves this pin too |
| test | All tests, including integration tests on the runner's Docker |
| smoke | Start the compose stack and wait until it is healthy. Check that `/health` and `/ready` return 200 and that `/swagger/doc.json` is a Swagger 2.0 document. From phase 3, also require at least one documented path. Phase 1 has no endpoints yet, and `/health` and `/ready` are not in Swagger, so that check would fail there. Print the logs on failure; tear down |

Every action is pinned to a full commit SHA, with its version in a comment. GitHub's security hardening guide names the full SHA as the only immutable reference to an action, and Dependabot updates both the SHA and the comment.

### 10.5 Dependency updates

- **Dependabot** opens weekly update pull requests for:
  - Go modules (`gomod`);
  - the Dockerfile images (`docker`);
  - the compose images (`docker-compose`);
  - the GitHub Actions (`github-actions`).

  Minor and patch updates are grouped. The commit messages are `chore(deps): …` (#61). Each update pull request passes the same CI, including smoke, before it is merged.
- **One version, one file.** Every exact version lives in exactly one of these files:
  - `go.mod`;
  - the `Dockerfile`;
  - `compose.yaml`;
  - `.golangci-lint-version`;
  - `.github/workflows/ci.yml`.

  Nothing else repeats it: not documents, plans, Makefiles or Go constants. Documents name the file, never the version. The integration tests read the database image from `compose.yaml`, and the Makefile and CI read the linter version from `.golangci-lint-version`.
- **Updated by hand:**
  - the linter version, because Dependabot has no ecosystem for `.golangci-lint-version`;
  - the `toolchain` line, which Dependabot does not bump (§10.2). The toolchain check makes a Dependabot pull request for the `golang` image fail until `toolchain` is updated in the same pull request.
- **Postgres major versions are excluded** from automatic updates. A new major needs `pg_upgrade` for the data volume, and the collation rules (§5.2) need 17 or later, so a major upgrade is a design decision.

## 11. Testing strategy

| Level | Scope | Tools |
|---|---|---|
| Domain unit tests | Every value object and the aggregate: valid and invalid cases, normalization, boundaries (years 1999/2000 and 2100/2101, months 0/13, prices −1/0/max/max+1, names of 100/101 characters, Cyrillic character counting), the nil user ID, the subscription period (end before start, equal start and end, open-ended, value equality, and that it cannot be changed after construction), invalid zero values rejected by the aggregate | table-driven Go tests |
| HTTP adapter tests | Every endpoint, through an in-process HTTP recorder, with the real application service over hand-written in-memory fakes of the ports. Covers: status codes, `Location`, response shapes (`items` never `null`, `end_date` omitted), every rule in §4.3, including the decoding traps (`null` in a required field, duplicate keys, `400.0`, a non-object body, trailing data), 413 and 415, 503 when a port call outlives the request deadline, problem+json bodies, router 404/405 as problem+json with `Allow`, all field errors in one response in schema order | standard library |
| Postgres integration tests | Repository CRUDL and not-found paths; filters; case-insensitive Cyrillic matching, including the index check (§5.2); ordering, pagination and the total; database constraints; migrations up → down → up. **Total cost** for every §2.7 case: inside, overlapping the start, overlapping the end, covering, open-ended, single-month period, touching boundaries, disjoint, price 0, no rows, maximum price × 1 212 months without overflow | testcontainers with the image from `compose.yaml` (§10.5), initialized with the `C` locale and UTF8 encoding (§5.2); one container per test package; each test creates its own database from `template0` (UTF8, `C` locale) and drops it at the end, so tests run in parallel |
| Smoke | The composed stack starts; health, readiness and Swagger respond | CI (§10.4) |

- Fakes are hand-written; there is no mock generator.
- Integration tests skip themselves in short mode.

## 12. Delivery plan and workflow

Each phase is one short-lived branch that merges into `main` when CI is green (GitHub Flow).

Branch names follow `type/short-description`:
- **Format:** lowercase, words separated by hyphens, 2–4 words.
- **Types:** `feature/`, `fix/`, `docs/`, `chore/`, `refactor/`. They are close to the Conventional Commits types (`feature/` ↔ `feat:`; the others share the name). There is no `test/` type, because tests ship with the code they cover.
- **No ticket ID:** there is no issue tracker.
- **Merging:** each phase lands through one pull request, merged with a merge commit, never squashed or rebased. Every commit, including the test-first steps, stays in `main`, and phase boundaries remain visible in the history.
- **Plans:** each phase starts with its own implementation plan.
  - It is written when the phase begins, not up front, so it builds on the code that exists by then.
  - The plan is the first commit on the phase's branch (`docs/superpowers/plans/`). It is approved before any code is written, and it ships in the same pull request as the code.
  - The table below is the roadmap; the plans hold the detail.
- **Phase 0** has no CI yet, so it merges once the spec is approved.

| Phase | Branch | Content | Done when |
|---|---|---|---|
| 0. Design | `docs/design` | This spec and `CLAUDE.md` | The spec is approved |
| 1. Walking skeleton | `feature/walking-skeleton` | Module and layout, Makefile, linter with depguard, configuration, logging, Postgres pool, first migration, HTTP server with middleware and timeouts, `/health` and `/ready`, problem+json for router errors, graceful shutdown, Swagger generation and UI (no endpoints yet), Dockerfile, compose files, CI, Dependabot | From a clean clone, `docker compose up --build` starts the stack, migrations are applied, `/health` and `/ready` return 200, Swagger UI opens, CI is green |
| 2. Domain model | `feature/domain-model` | Value objects, aggregate, queries, ports, errors, written test-first | Domain tests pass; the domain depends only on the standard library and the UUID library |
| 3. CRUDL | `feature/subscription-crudl` | Application service, Postgres repository, HTTP adapter with strict body decoding, Swagger annotations, delivered in slices: Create + Get, then List, Update, Delete | All five endpoints work through curl and Swagger UI; handler and repository tests pass; the smoke test now also requires at least one documented path |
| 4. Total cost | `feature/total-cost` | The total-cost query, its use case, the endpoint with parameter validation | Integration tests pass for every §2.7 case |
| 5. Finish | `chore/final-review` | Check Swagger against real behavior, README, final review, final check of `CLAUDE.md` | From a clean clone, `docker compose up --build` gives a working service, and the whole README walkthrough succeeds |

- **Language:** everything in the repository is English: code, comments, commits, README, this document and the Swagger text.
- **`CLAUDE.md`:** the instructions Claude Code loads in every session. It is written in phase 0 and extended in every phase that adds commands or conventions, so it is never stale while work is in progress.
- **Module path:** `github.com/sultanrasulov/subscription-service`.
- **Versions:** Go 1.27 or later; PostgreSQL 17 or later (`pg_c_utf8`).
  - This document states only minimums.
  - Each exact version lives in one file only (§10.5), where Dependabot or a single manual edit updates it without making this document stale.
- **Commits:** Conventional Commits (`feat:`, `fix:`, `refactor:`, `test:`, `docs:`, `chore:`), in English.

## 13. Revisit triggers

| Change | Response |
|---|---|
| Proration, discounts, price history | Move the total-cost calculation into the domain, as an overlap between `SubscriptionPeriod` and `CalculationPeriod` |
| "At most one active subscription per user and service" | A database exclusion constraint on overlapping periods, a domain conflict error, HTTP 409 |
| Retried POSTs create duplicates in practice | `Idempotency-Key` header on POST |
| Several replicas or a backup before migrating | Migrations as a one-shot `migrate up` subcommand of the same image |
| A second adapter on one side (gRPC, cache, queue) | Split adapters into inbound and outbound groups |
| Dozens of providers or several bounded contexts | Reconsider generated dependency injection (goforj/wire) |
| `encoding/json/v2` becomes a stable part of Go | Switch request decoding to it. It rejects duplicate keys and encodes nil slices as `[]` by itself. In Go 1.27 it is still a default-on experiment, so the stable v1 package is used |
| Deployment to Kubernetes | Point the liveness probe at `/health` and the readiness probe at `/ready` |
| The database sits behind PgBouncer, a load balancer or failover | Make the connection lifetime, idle time and minimum connections configurable, so connections are recycled toward the current server; until then pgx's defaults apply (1 h, 30 min, 0) |
| Dependabot starts bumping `toolchain` (dependabot-core #13520) | It would then bump `toolchain` in a `gomod` pull request and the `golang` image in a `docker` pull request, and each would fail the toolchain check (§10.2) on its own. Combine both updates in one pull request, by hand or with a Dependabot multi-ecosystem group |

## Appendix A. Decision log

While this document is a draft, every decision is written in its final form where it is first stated. Once the document is committed, numbers become permanent: they are never reused or shifted, and a decision that is later amended, replaced or withdrawn keeps its number and names the decision that changed it.

| # | Topic | Decision |
|---|---|---|
| 1 | Approach | DDD, one bounded context `subscription` |
| 2 | Total-cost rule | price × overlapping months, inclusive; open-ended counts until `to` (§2.7) |
| 3 | Where the sum is computed | One database query behind the `TotalCostReader` port (input: `TotalCostQuery`), plus integration tests |
| 4 | Earlier draft | Fresh start; only the value-object ideas are carried over, with fixes |
| 5 | Serialization | Only in adapters. The HTTP adapter parses `MM-YYYY`, the domain validates, depguard enforces the boundary |
| 6 | `from` / `to` | Both required, `MM-YYYY`, inclusive, `from ≤ to`, else 400 |
| 7 | Filters | Optional, AND, full name case-insensitive, the same for list and total cost; absent = no filter, empty = 400 |
| 8 | Layout | `domain` / `application` / `adapters/{httpapi,postgres}` + `platform` (§3.1) |
| 9 | Update | PUT, full replacement; omitted `end_date` → open-ended |
| 10 | Delivery | Six phases: walking skeleton first, then vertical slices (§12) |
| 11 | `user_id` | Set once at creation, never changes, rejected in the PUT body |
| 12 | Price | Value object, 0…2 147 483 647, required, integer only. A trial is a 0-price record; a price change is a new record |
| 13 | Service name | Trim and collapse whitespace, preserve case, 1–100 characters |
| 14 | Years | 2000–2100, for every month, including `from` and `to` |
| 15 | Duplicates | Allowed; every record counts |
| 16 | IDs and timestamps | UUID v7 generated in the application layer; one constructor; no database default; timestamps only in the database, `updated_at` set by the update statement; list ordered by ID |
| 17 | Paths | `/api/v1/subscriptions`, `/api/v1/subscriptions/total-cost` |
| 18 | Pagination | `limit` 1–100, default 20; `offset` ≥ 0, default 0; anything else → 400 |
| 19 | Error format | RFC 9457 problem+json with field errors |
| 20 | Total-cost response | `{"total_cost": N}` only; no matches → 0 |
| 21 | Status codes | 201 + `Location` / 200 / 204 / 400 / 404 / 405 / 413 / 415 / 503 (request deadline); 500 without internals |
| 22 | Deletion | Physical |
| 23 | Schema | Month stored as a date on the 1st, database constraints, indexes on `user_id` and lower-cased `service_name` |
| 24 | Migrations | Embedded, applied at startup (golang-migrate, pgx v5); explicit `BEGIN … COMMIT` in every file |
| 25 | Database access | Hand-written SQL on pgx v5 |
| 26 | Collation | `pg_c_utf8` on the `service_name` column and on the filter value in queries; compose needs no initdb locale settings; tests deliberately run on a UTF8 database with the `C` locale to prove the result does not depend on the locale; a non-UTF8 database fails the first migration by design |
| 27 | Configuration | Environment variables, supplied from env files; Go does not read `.env` itself |
| 28 | Env files | `.env.example` committed; compose and `make run` read `.env.example`, then an optional `.env`. *(Shell precedence for `make run` added by #68.)* |
| 29 | Router | Standard library router |
| 30 | Swagger | Code-first swaggo, specification only (no generated Go code), not committed, embedded, Swagger 2.0, always on |
| 31 | Logging | slog JSON, request middleware, each error logged once. *(Format made selectable by #65.)* |
| 32 | Versions | Minimums only: Go 1.27, PostgreSQL 17 (`pg_c_utf8`). Exact versions live only in `go.mod`, `Dockerfile` and `compose.yaml`. Alpine build image with `make`, Alpine runtime. *(List of version files amended by #63.)* |
| 33 | Tests | Domain unit tests, HTTP tests, testcontainers |
| 34 | CI | Lint + tests + compose smoke (`/health`, `/ready`, Swagger spec) |
| 35 | Spec | `docs/design.md`, no code: intent, rules and decisions |
| 36 | Branches | One short-lived branch per phase, named `type/short-description` (lowercase, hyphens, no ticket ID). One pull request per phase, merged into `main` with a merge commit (no squash) when CI is green. Phase 0, which has no CI yet, merges after the spec is approved. *(Phase 0 clause amended by #62.)* |
| 37 | Module | `github.com/sultanrasulov/subscription-service` |
| 38 | Language | Everything in the repository in English |
| 39 | Dependency injection | Manual wiring in the entry point, no framework |
| 40 | Makefile | Developer entry point; the Dockerfile builds through `make build` |
| 41 | List response | `items` (always an array), `limit`, `offset`, `total`; the total comes from a separate count with the same filter |
| 42 | Open-ended in responses | `end_date` omitted; on input, `null` equals absence |
| 43 | Body size | Over 1 MiB → 413 |
| 44 | User ID | `UserID` value object; the nil UUID is rejected in bodies and filters |
| 45 | Body decoding | Strict token-by-token decoding with the stable `encoding/json`; structural problems → one error; otherwise all field errors in schema order; duplicate keys rejected; `null` in a required field = missing |
| 46 | Query parameters | Repeated → 400; unknown → ignored |
| 47 | SQL filter | Conditions only for filters that are set; one builder for list and total cost; bound parameters only |
| 48 | Overflow | Price widened to 64 bits; total cast back to 64 bits so overflow fails loudly → 500 |
| 49 | Configuration library | `caarlos0/env` plus a validation step; database variables named `POSTGRES_*` like the image's |
| 50 | Compose files | `compose.yaml` with the database port unpublished; committed `compose.dev.yaml` used only by `make db`. *(The app's database overrides amended by #64.)* |
| 51 | App healthcheck | On `/ready`; the smoke test waits for health |
| 52 | Router errors | 404 and 405 from the router rewritten as problem+json, `Allow` kept |
| 53 | Health endpoints | `/health` liveness without dependencies; `/ready` readiness with a database ping |
| 54 | Content type | POST/PUT without a JSON content type → 415 |
| 55 | Server timeouts | All HTTP server timeouts configurable, with defaults |
| 56 | Subscription period | The aggregate holds a `SubscriptionPeriod` value object (start, optional end, end not before start); held by value; separate from `CalculationPeriod` |
| 57 | problem+json | One implementation in `internal/platform/problem`, used by both the HTTP server (router errors, panics) and the HTTP adapter (validation, not found, internal errors) |
| 58 | Request deadline | Middleware gives every request context a deadline (`HTTP_REQUEST_TIMEOUT`, default 8s, shorter than the write timeout); a timeout (deadline exceeded) → 503 problem+json; a client disconnect (context canceled) is logged at WARN with status 499, not as a 500 |
| 59 | Integration test database | All integration tests share one database initialized with the `C` locale and UTF8 encoding; the index check disables sequential scans for its own transaction and also asserts that the row is found. *(Isolation amended by #67.)* |
| 60 | Graceful shutdown in Docker | The binary runs in exec form and receives SIGTERM; the app's stop grace period is 15 s; config validation requires the shutdown timeout to be at least the request timeout |
| 61 | Commit messages | Conventional Commits (`feat:`, `fix:`, `refactor:`, `test:`, `docs:`, `chore:`), in English |
| 62 | Planning | One implementation plan per phase, written when the phase starts; it is the first commit on the phase's branch and is approved before any code |
| 63 | Dependency updates | Dependabot, weekly, minor and patch grouped, `chore(deps)` commits; each exact version in one file (`go.mod`, `Dockerfile`, `compose.yaml`, `.golangci-lint-version`, `ci.yml`); actions pinned by commit SHA; the builder checks that its Go version equals `toolchain`; Postgres majors excluded (§10.2, §10.4, §10.5) |
| 64 | Compose database overrides | The `app` service overrides both `POSTGRES_HOST` and `POSTGRES_PORT` to the in-network values, because `POSTGRES_PORT` also sets the host port for `make db` (§10.3). *(HTTP_ADDR added by #69.)* |
| 65 | Log format | `LOG_FORMAT`: `json` (default) or `text`, matched exactly; JSON for compose and log collectors, text for local reading (§7, §8) |
| 66 | Database connect timeout | `POSTGRES_CONNECT_TIMEOUT`, default 5 s, passed to pgx as a duration; bounds every new connection and the startup ping. Other pool settings keep pgx defaults until a §13 trigger |
| 67 | Integration test isolation | One container per test package; each test creates its own database (`TEMPLATE template0`, UTF8, `C` locale) and drops it at the end; tests run in parallel |
| 68 | `make run` precedence | Variables already set in the shell win over `.env.example` and `.env`, as with Compose interpolation and dotenv tools (§7) |
| 69 | App listen address in compose | The `app` service fixes `HTTP_ADDR` to `:8080`, the published and health-checked port, because a developer's `.env` may move it for `make run` (§10.3) |
| 70 | Image and port hardening | `.dockerignore` is an allow-list without tests; the runtime user has the numeric UID and GID 65532, so `runAsNonRoot` can verify it; the app's port is published on 127.0.0.1 only, because a published port bypasses the host firewall (§10.2, §10.3) |
| 71 | App host port | `APP_PORT` (default 8080) sets the host port that compose publishes the app on, so a taken 8080 needs no edit to `compose.yaml`; compose-only, read by interpolation from the shell or `.env`; inside the container the app and its healthcheck keep 8080 (§7, §10.3) |
