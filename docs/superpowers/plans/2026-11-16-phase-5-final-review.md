# Phase 5: Final Review — Implementation Plan

> **Execution:** one task at a time, in order. Each task is reviewed and committed
> before the next one starts (CLAUDE.md, Workflow). Checkboxes (`- [ ]`) mark the
> "Done when" results.

- **Status:** approved. It becomes historical when phase 5 merges. From then on the
  code, the generated Swagger specification and `docs/design.md` are the source of truth.
- **Spec:** `docs/design.md` §1.2, §1.3, §2.7, §3.1, §4, §5.3, §7, §9, §10, §11, §13
  and the phase 5 row of §12; the phase 4 plan's "Hands over to phase 5";
  decisions #86–#90.
- **Branch:** `chore/final-review`. Pull request title: `chore: final review (phase 5)`.
- **First commit:** `docs: add final review plan`. A decision goes into the design
  before any code depends on it, so this commit also carries:
  - design §5.3: recovery from a dirty version and manual rollback (#87);
  - design §9: required response fields and field descriptions (#88);
  - design §10.4 and §11: the smoke test requires exactly the documented operations
    (#89, amends #86);
  - design §13: a govulncheck job in CI as a revisit trigger (#90);
  - CLAUDE.md, API: a response field that is always present carries
    `validate:"required"`; a request or response field's comment is its Swagger
    description.

## What this plan is

Like the plans of phases 2–4, it records intent: what each task must achieve, which
rules apply, when it is done, and the pitfalls already found and verified. It contains
no function bodies, test code or README text.

## Goal

From a clean clone, `docker compose up --build` gives a working service, the Swagger
specification describes exactly what the service does, and the whole README
walkthrough succeeds with the outputs it shows.

Out of scope: changes to the API contract or behavior, except review fixes the user
approves; new dependencies; new make targets; new CI jobs.

## Decisions

| # | Decision | Why |
|---|---|---|
| 1 | Recovery and rollback use golang-migrate's own CLI, `go run -tags pgx5 github.com/golang-migrate/migrate/v4/cmd/migrate`, against the database `make db` publishes (#87) | Its version is the one in `go.mod`: no second pin (§10.5), no new dependency, `go.mod` and `go.sum` unchanged (verified). The tool owns the semantics of `force`. Rejected: the `migrate/migrate` image (a second version), `UPDATE schema_migrations` through psql (repeats the tool's internals), a make target (an interface for a rare manual step) |
| 2 | A dirty version N is forced to N−1, never N; for N = 1 it is `-1` (no version), never `0` | Every migration runs in `BEGIN … COMMIT`, so a failed one leaves the schema at N−1 (verified: the failed migration's column is absent) |
| 3 | Fields always present in a response get `validate:"required"`; formats and units go into field comments, which swag turns into `description`. No `--requiredByDefault`, no `pattern` (#88) | Request structs already use the tag only for swag; no validator library exists. With the flag, a new `omitempty` field without a marker would be documented as required, which is false; a forgotten tag only gives a weaker, true statement. A `pattern` would be a second copy of the `MM-YYYY` rule, which lives in the parser |
| 4 | The smoke test requires `basePath` `/api/v1` and exactly the six documented operations (#89). Validation against the Swagger 2.0 schema stays a one-off check | swag exits 0 without a warning when a handler loses its `@Router`, and the operation disappears from the specification (verified). The specification exists only in the built image, which only smoke sees. An automated schema validation would need a new dependency |
| 5 | The final review covers the whole repository, by a reviewer with a fresh context. Each finding is confirmed by a probe or a failing test before it is fixed, and fixed only once the user approves it | The author shares the code's blind spots, and phase 5 has almost no diff of its own to review |
| 6 | govulncheck runs once in the review, as `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`; a CI job is a §13 revisit trigger (#90) | A command, not a pinned file, so §10.5 holds. A job would add a pinned action and fail unrelated pull requests whenever a new advisory appears; Dependabot already updates weekly |
| 7 | Order: Swagger → review and its fixes → README → CLAUDE.md, the design and the finish | A review fix may change behavior or a message; a README written after it needs no second pass |
| 8 | The README is for users: how to run, call and develop the service. It links to the design for every "why". Its endpoint table holds only method, path and purpose; statuses, messages and defaults stay in Swagger, the design and `.env.example` | One source per fact. A third copy of the variable table or of the status codes would drift |
| 9 | The walkthrough uses only `curl`, with every URL in single quotes, and shows response bodies only. IDs are copied from the responses by hand. Its user ID is a fixed UUID of its own, not the one in the Swagger examples, and it deletes what it creates | A reader may have no `jq`. Unquoted, zsh treats `?` as a glob (`no matches found`), and every shell runs a command cut at `&` in the background. Headers such as `Date` and `X-Request-Id` change on every run. Duplicates are allowed, so records left behind, by an earlier run or by "Try it out" with the example values, would change the total the README shows |
| 10 | Every stack this phase starts runs with its own `COMPOSE_PROJECT_NAME`, `APP_PORT`, `POSTGRES_PORT` and, for `make run`, `HTTP_ADDR` | `compose.yaml` names the project `subscription-service`, so a clone elsewhere would join the developer's `make db` project, recreate its postgres and write to its volume; the default ports may be taken by the developer's own stack |
| 11 | The README has a CI badge | The repository is public (verified); the badge URL names only the workflow |

## Global rules

- **Behavior** changes only through a review finding the user approved: one
  `fix(<scope>): …` commit each, with a regression test seen to fail first.
- **Comments:** field comments on request and response structs are API documentation
  read by swag, the one exception to "comments explain why". Every other comment
  still explains why and never cites the design.
- **Versions:** no exact version in the README, the plan or the design. Minimums are
  allowed: Go per `go.mod`, PostgreSQL 17 or later, Docker Compose 2.24 or later.
- **Isolation:** no check touches the developer's `subscription-service` project
  (Decision 10).

## Review focus

These failure modes are implied by the spec but easy to miss. Each is covered by the
task in brackets.

1. A handler loses its `@Router`, and its operation silently leaves the specification (Task 1).
2. `required` lands on a field that can be absent: `end_date`, `detail`, `errors` (Task 1).
3. An annotation edit makes the specification invalid Swagger 2.0, or a status is
   documented that the server never sends, or the other way round (Task 1).
4. A README command fails on a clean clone or prints something else, including on a
   second run or after "Try it out" with the example values (Task 3).
5. `curl -d` without `-H 'Content-Type: application/json'` gets 415 (Task 3).
6. `make run` while the compose stack's `app` holds the same port (Task 3).
7. `force N` instead of `N−1`, or `force 0` instead of `force -- -1`; a rollback in
   the wrong order, with an old build started before `down` (Task 3).
8. An exact version slips into the README (Task 3).
9. A check touches the developer's database or ports (all tasks).

## After the merge (the user, optional)

- A `v1.0.0` tag, matching `@version 1.0` in the specification.
- The repository's description and topics on GitHub.
- Conventional titles for pull requests #1, #4, #5 and #7. This changes the GitHub
  UI only; their merge commits keep the old titles.

---

### Task 1: The Swagger specification matches the behavior
Commits, in this order:
1. `test(smoke): require every documented operation`
2. `docs(api): complete the Swagger specification`

**Scope:**
- `test/smoke/smoke_test.go`;
- `internal/subscription/adapters/httpapi`: `response.go`, `request.go`, `subscriptions.go`;
- `internal/platform/problem/problem.go`;
- `cmd/subscription-service/main.go`.

**Provides:**
- `TestSmoke_OperationalRoutes` also requires:
  - `basePath == "/api/v1"`;
  - the set of `METHOD path` in `paths` equals exactly:
    `POST /subscriptions`, `GET /subscriptions`, `GET /subscriptions/total-cost`,
    `GET /subscriptions/{id}`, `PUT /subscriptions/{id}`, `DELETE /subscriptions/{id}`.
- `validate:"required"` on:
  - `subscriptionResponse`: `id`, `service_name`, `price`, `user_id`, `start_date`;
  - `listResponse`: `items`, `limit`, `offset`, `total`;
  - `totalCostResponse`: `total_cost`;
  - `problem.Details`: `type`, `title`, `status`;
  - `problem.FieldError`: `field`, `message`.
- Field comments, which become `description`, in requests and responses:
  - `price`: the monthly price in whole rubles;
  - `start_date`: the first month, `MM-YYYY`;
  - `end_date`: the last month, `MM-YYYY`, inclusive; omitted for an open-ended subscription;
  - `total_cost`: in whole rubles.
- `@Produce json` on DELETE.
- `@description` in `main.go`: months are `MM-YYYY`, money is whole rubles, and errors
  are `application/problem+json`, documented as `application/json` because of a
  Swagger 2.0 limit.

**Rules:**
- **Operations.** Only the method keys of a path item count: `get`, `put`, `post`,
  `delete`, `options`, `head`, `patch`. A path item's `parameters`, `$ref` and `x-`
  keys are not operations.
- **Failure message.** A failure names both the missing and the unexpected operations.
- **RED for the smoke change is a mutation.** A stack built with one `@Router` line
  removed fails the test and names that operation; the restored stack passes.
- **Every documented status is reproduced once on an isolated stack:**
  - 201 with `Location`;
  - 400: a field error, a malformed query, and `PUT` on `/total-cost`;
  - 404;
  - 405 with `Allow` on `POST /total-cost`;
  - 413 (a body over 1 MiB) and 415;
  - 503: `docker compose pause postgres`, answered after the request timeout;
  - 500: `docker compose stop postgres`;
  - every error arrives as `application/problem+json`.

**Pitfalls (verified):**
- When a handler has no `@Router`, swag exits 0 and prints nothing about it; the
  operation is simply absent. The mutation above confirms it again.
- swag reads `validate:"required"` on a response struct, a field's comment as its
  `description`, and `@Produce json` on DELETE as `produces`.
- To check in the specification's diff: the whole `@description` arrives, even when
  it spans several lines.

**Done when:**
- [ ] The diff of the generated specification holds only the changes above.
- [ ] It passes the official Swagger 2.0 JSON Schema. This is a one-off check in the
      scratchpad; nothing is added to the repository.
- [ ] Every documented status is reproduced, and every reproducible status is documented.
- [ ] The smoke mutation is RED, and GREEN once restored.
- [ ] The user has run "Try it out" for all six operations.
- [ ] `make lint test` is green.

### Task 2: The final review
Commits: one `fix(<scope>): …` per approved finding, each with its regression test.

**Reviewer:** a fresh context (Decision 5).

**Input:**
- the whole repository;
- `docs/design.md` and CLAUDE.md;
- the Review focus of the phase 1–4 plans.

**Checklist:**
- behavior against §4: statuses, messages, the order of field errors;
- errors: wrapped with `%w`, none swallowed, no panic on a request path;
- logging: each error once, at the HTTP boundary; no secrets; `*Context` methods;
- layers and depguard;
- comments that cite the design, or that explain what instead of why;
- dead code;
- test gaps;
- the Dockerfile, compose and CI against §10;
- `go mod tidy -diff`;
- govulncheck (Decision 6).

**Done when:**
- [ ] The report is shown in chat, each finding with `file:line` and a failure scenario.
- [ ] Each finding is either fixed (RED → GREEN, `make lint test` green) or declined
      with a reason.

### Task 3: The README
Commit: `docs: add the README`

**Scope:**
- `README.md`;
- CLAUDE.md, in the same commit, because the README introduces a command:
  - Commands: the migrate CLI line (Decision 1);
  - Gotchas: the dirty version is forced to N−1 (`force -- -1` for the first migration).

**Sections:**
1. The name, one line on the service, the CI badge, and a link to the design.
2. Quick start:
   - Docker with Compose 2.24 or later; no Go is needed for this path;
   - `docker compose up --build`;
   - the API and Swagger UI addresses;
   - `APP_PORT`;
   - `docker compose down`, and that `-v` deletes the data.
3. Walkthrough, for one user, with `curl` only:
   - create Yandex Plus, 07-2025…12-2025 at 400;
   - create Netflix, open-ended from 11-2025 at 599;
   - get one, then list with filters and a page;
   - the total over 2025: `6 × 400 + 2 × 599 = 3598`;
   - update, then delete both;
   - a 400 problem+json with several field errors;
   - the `Content-Type` note (415).
4. The total-cost rule in plain words (§2.7), on the same example:
   - price × the months of overlap, both ends inclusive;
   - an open-ended subscription counts until `to`;
   - no proration.
5. The API in brief:
   - the six endpoints: method, path and purpose only;
   - Swagger UI is the contract;
   - conventions: `MM-YYYY`, whole rubles, canonical UUIDs, pagination,
     case-insensitive `service_name`.
6. Configuration:
   - `.env.example` first, then an optional `.env`; for `make run` the shell wins;
   - only `APP_PORT` and `LOG_FORMAT=text` are named; the rest is in `.env.example`.
7. Development:
   - Go per `go.mod`, Docker and make; `make help`;
   - `make db` before `make run`: `compose.yaml` does not publish the database port;
   - `make run` next to a running compose stack: run one at a time, or move one;
   - `make test`, `make test-unit`, `make lint`;
   - `make up`, then `make smoke`;
   - pre-commit.
8. Migrations:
   - they are applied at startup;
   - after a failure: fix the file, `force N−1`, then restart;
   - rollback: `docker compose stop app`, then `down` with the new code, then the old build;
   - when the first migration failed: `force -- -1`, never `force 0`;
   - the password in `-database` is URL-encoded.
9. Architecture: the layers and their dependencies in a few lines; links to design §2 and §3.
10. Assumptions and limits:
    - the non-goals of §1.3;
    - problem+json is documented as JSON in Swagger 2.0;
    - totals above 2^53 for JavaScript clients;
    - duplicates are allowed;
    - PUT is last-write-wins.
11. License (MIT).

**Rules:**
- Every URL is in single quotes; every POST and PUT sends the `Content-Type` header.
- The walkthrough's user ID is a fixed UUID of its own, never the Swagger example's.
- The outputs shown are response bodies (`curl -sS`) from a real run; DELETE shows its
  status with `-w '%{http_code}\n'`. `Location` is described in the text, not shown.
  IDs differ on every run, and the README says so.
- The walkthrough deletes what it creates, so a second run on the same stack prints
  the same outputs apart from IDs.
- `make run` and the stack's `app` both serve on 8080. The README says to run one at
  a time (`docker compose stop app`) or to move one (`HTTP_ADDR=:8081 make run`). It
  does not rely on the bind failing: on Linux it fails (`address already in use`,
  verified), but where the system allows a wildcard bind next to a loopback one,
  `localhost:8080` would silently keep reaching the container.

**Pitfalls (verified):**
- `env_file` entries with `path` and `required` need Docker Compose 2.24.0 or later
  (Docker docs, "Set environment variables").
- The service refuses a dirty version (`run migrate force by hand`).
- An old build on a newer schema refuses to start (`no migration found for version N`),
  so `down` runs from the new code first.
- The CLI rejects `force -1` (`flag provided but not defined`); it takes `force -- -1`.
- `force 0` records version 0, which has no file: the next `up` fails with
  `no migration found for version 0`.

**To verify in this task:**
- `make db` next to a running `docker compose up` stack of the same project publishes
  the database port and keeps the data.
- The compose files use no other feature newer than Compose 2.24 (the Compose file
  reference).

**Done when:**
- [ ] A clean clone (Decision 10) passes both paths top to bottom:
      `docker compose up --build`, and `make db` + `make run`.
- [ ] The walkthrough is run twice on the same stack, with the same outputs apart from IDs.
- [ ] The migration procedures are run once on the isolated stack.
- [ ] `grep` finds no exact version, and every relative link resolves.

### Task 4: CLAUDE.md, the design, and the finish
Commit: `docs: finish CLAUDE.md and the design`, only if anything changes.

**Checklist:**
- CLAUDE.md:
  - every command in Commands runs as written;
  - every path in Architecture exists;
  - the Gotchas are still true;
  - Overview links the README;
  - Workflow says that a change after phase 5 starts with its own plan on its own branch.
- The design:
  - §2–§11 checked against the code;
  - Status: `implemented`.
- `make lint test pre-commit-run`.
- A clean clone: `docker compose up --build`, `make smoke`, then `docker compose stop app`
  with exit 0.
- `gh pr list`: no open Dependabot pull request, or each one is named in the pull request.

**Done when:**
- [ ] Everything above holds.
- [ ] CI is green on the pull request.
- [ ] `gh pr view`: the head is the last commit, and the title is `chore: final review (phase 5)`.
