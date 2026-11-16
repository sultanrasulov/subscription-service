# Phase 2: Domain Model — Implementation Plan

> **Execution:** one task at a time, in order. Each task is reviewed and committed before the next one starts (CLAUDE.md, Workflow). Checkboxes (`- [ ]`) mark the "Done when" results.

- **Status:** approved. It becomes historical when phase 2 merges. From then on the code and `docs/design.md` are the source of truth.
- **Spec:** `docs/design.md` §2, the domain row of §11, the phase 2 row of §12; decisions #12–#14, #44, #56 and #75.
- **Branch:** `feature/domain-model`.
- **First commit:** `docs: add domain model plan`. A decision goes into the design before any code depends on it, so this commit also carries the service name characters rule:
  - design §2.2: the rule and why it exists;
  - design §2.6: invalid characters in the service name as an example error;
  - design §4.3: the rule and its message for `service_name`;
  - design §5.1: the database repeats the rule;
  - design §11: control characters and invalid UTF-8 in the domain tests;
  - decision #75, with #13 marked as amended.

## What this plan is

Like the phase 1 plan, it records intent: what each task must achieve, which rules apply, what it hands to the next task, when it is done, and the pitfalls already found and verified. It contains no function bodies or test code.

One difference: **Provides** lists the exact exported signatures. The domain's exported API is what phase 3 builds on, so its names are a decision of this plan, not of the implementer.

## Goal

The whole `internal/subscription/domain` package, written test-first (design §12): value objects, the aggregate, queries, ports and errors. The package depends only on the standard library and `github.com/google/uuid`.

Out of scope: the application service, the adapters and the wiring (phase 3). CI, the Dockerfile, the compose files and the dependencies do not change; `uuid` is already in `go.mod`.

## Global rules

- **TDD:** every behavior in "Done when" is covered by a test that was seen to fail before the code existed.
- **Tests:**
  - black-box (`package domain_test`), so they prove that a constructor is the only way to get a valid value;
  - table-driven, with `t.Parallel()`, like the existing tests;
  - errors are checked only with `errors.Is`, never by their text;
  - one file per type, with its `_test.go` next to it.
- **Value objects:**
  - unexported fields of value kinds only;
  - created only through their validating constructor;
  - methods on value receivers;
  - the zero value is invalid for all of them except `Price` (zero rubles is a valid price).
- **Exported API:** exactly what Provides lists. For example, there is no `Compare`, and `IsZero` exists only where a consumer needs it.
- **Domain boundaries:**
  - it never reads the clock;
  - it knows nothing about the `MM-YYYY` format (#5);
  - it does not log;
  - `context` appears only in the port signatures.
- **Errors:**
  - two categories, `ErrInvalid = errors.New("invalid data")` and `ErrNotFound = errors.New("not found")`;
  - each specific error is a sentinel built with `fmt.Errorf("%w: …", ErrInvalid)`, and constructors return it as is;
  - every sentinel lives in `errors.go`, so the whole catalog the HTTP adapter maps is in one place;
  - `ErrNotFound` is not `ErrInvalid`: if the categories merged, the HTTP adapter would answer 400 instead of 404;
  - identity is the pointer, so two sentinels with the same text are still different errors (verified), and the category is found with `errors.Is`;
  - callers add context, such as a row ID, in phase 3;
  - messages are lower case, with no final period;
  - the domain does not produce client-facing text; the HTTP adapter maps each error to a field and a message (design §2.6).
- **Comments:**
  - every exported identifier has a doc comment that starts with its name and ends with a period (revive `exported` and godot, verified);
  - comments stand on their own and never cite the design.
- **Commits:** one per task, `feat(domain): …`.

## Review focus

These failure modes are implied by the spec but not named in its tests. Each is covered by the task in brackets.

1. A NUL, another control character or invalid UTF-8 in a service name gives 400, not a 500 from the database (Task 3).
2. A failed `Update` leaves the aggregate unchanged (Task 5).
3. A zero value object is rejected wherever it is required: the parts of both periods, the aggregate, the total-cost query (Tasks 4–6).
4. Unicode whitespace is collapsed, while a zero-width space is not whitespace (Task 3).
5. Extreme `int64` prices are rejected without overflow (Task 2).

## Hands over to phase 3

Not done here; the phase 3 plan decides each one.

1. A stored row that fails domain validation is a server error (500), not a client error. The Postgres adapter must not let `ErrInvalid` escape, or the HTTP adapter would turn a corrupted row into a 400.
2. The HTTP adapter maps each domain error to a field and a message, built from the exported limit constants. Swag annotations cannot reference constants, so their numbers stay literals.
3. `uuid.Parse` also accepts `{…}`, `urn:uuid:…` and 32 hex digits without hyphens. How strictly `id` and `user_id` are parsed is open.
4. The list response returns `items: []`, never `null` (design §4.2).
5. Invalid UTF-8 can arrive only through a query string: `encoding/json` replaces it with U+FFFD in a body.
6. A new migration adds a CHECK constraint that repeats the characters rule (`service_name !~ '[[:cntrl:]]'`); the first migration is not edited. Verified: Postgres's `[[:cntrl:]]` matches ESC, DEL, NEL, U+009B, U+009F and tab, but not Cyrillic, NBSP or U+200B, under both `pg_c_utf8` and the `C` collation, exactly as `unicode.IsControl` does. NUL and invalid UTF-8 need no constraint: Postgres rejects them itself.

## Amendment: typed identifiers (decision #76)

Made after Task 5, in its own commit `refactor(domain): add typed identifiers as shared value objects`, before Task 6. Tasks 1–5 below record what they delivered at the time; this section and Task 6 describe the code from here on.

- **Why:** Task 5 gave the aggregate a bare `uuid.UUID` ID while the user ID was already a value object, so the model was inconsistent, and a second identifier would have repeated `UserID` line by line.
- **What:**
  - `internal/shared/valueobject` with `ID[K any]`, `NewID[K](uuid.UUID) (ID[K], error)` rejecting the nil UUID with `ErrNilID`, `UUID()` and `IsZero()`. The package admits only value objects that every bounded context may use; it has no serialization and no helpers.
  - `UserID` becomes `valueobject.ID[userIDKind]`; its exported API is unchanged. New `SubscriptionID = valueobject.ID[subscriptionIDKind]` with `NewSubscriptionID`, which returns `ErrNilSubscriptionID`.
  - `NewSubscription(id SubscriptionID, …)` and `ID() SubscriptionID`.
  - depguard: a strict `shared` rule (standard library, uuid, other `shared` packages); the domain may import `internal/shared/`; platform, the application and both adapters may not, so outside the domain identifiers are created only through the domain's constructors.
- **Rule change:** every typed identifier has `IsZero`, because it comes with the shared type; the "only where a consumer needs it" rule still holds for everything else.
- **Known pitfalls (verified):**
  - Go infers `K` from an assignment such as `var f func(uuid.UUID) (domain.UserID, error) = valueobject.NewID`, so code outside the domain can call `NewID` for an unexported kind. Its error would then be `ErrNilID`, outside the domain's categories, and reach the client as a 500. depguard closes that path for the application and both adapters; the nil check inside `NewID` stays as a second line of defense for future bounded contexts.
  - The domain's allow entry needs the trailing slash (`…/internal/shared/`). Without it, a sibling such as `internal/sharedevil` would match the prefix.
  - Declaring a new identifier with the marker of an existing one compiles. `TestIdentifiers_HaveDistinctKinds` catches it.

---

### Task 1: Errors, `MonthYear` and the structure test
Commit: `feat(domain): add MonthYear value object`

**Goal:** the error categories, the month value object, and a test that keeps every value object free of pointers.

**Scope:** `internal/subscription/domain` (`doc.go`, `errors.go`, `errors_test.go`, `month_year.go`, `structure_test.go`), `.golangci.yml`.

**Rules:**
- Year and month are stored as two fields. The month is checked before the year.
- The `domain` depguard rule gets an allow entry for the package itself, matched exactly with a trailing `$`, with a "why" comment: an external test package imports the package it tests. The deny entries stay as they are.
- A table test in `errors_test.go` requires every specific error to match `errors.Is(err, ErrInvalid)`, and `ErrNotFound` not to match it. Every later task adds its errors to the table.
- The structure test walks each type in its table recursively, through structs and arrays, and requires:
  - no exported fields;
  - no field of kind Pointer, Slice, Map, Chan, Func, Interface or UnsafePointer.

  Every later task adds its types to the table.

**Provides:**
- `ErrInvalid`, `ErrNotFound`, `ErrInvalidMonth`, `ErrYearOutOfRange`;
- `const MinYear = 2000`, `const MaxYear = 2100`;
- `func NewMonthYear(year int, month time.Month) (MonthYear, error)`;
- `func (m MonthYear) Year() int`, `func (m MonthYear) Month() time.Month`, `func (m MonthYear) Before(other MonthYear) bool`.

**Done when:** tests cover
- [ ] months −1, 0, 1, 12, 13 and years 1999, 2000, 2100, 2101;
- [ ] an invalid month and an invalid year together give `ErrInvalidMonth`;
- [ ] the error table test passes: every specific error matches `ErrInvalid`, and `ErrNotFound` does not;
- [ ] `12-2024` is before `01-2025`; equal values are not before each other and compare equal with `==`;
- [ ] the structure test passes for `MonthYear`;
- [ ] `make lint` is green with the tests in place;
- [ ] a manual check, not committed: a temporary `import "encoding/json"` in the domain code and in its test fails `make lint` with a depguard message.

**Known pitfalls (verified):**
- Storing a month as one index (`year*12+month`) makes the zero value "year 0, month 1", which then fails with `ErrYearOutOfRange` instead of `ErrInvalidMonth`.
- The `domain` rule is a strict allow-list and also applies to test files. Without an entry for the package itself, `package domain_test` fails lint.
- The trailing `$` is required. Without it the entry is a prefix match and also allows `domain/sub`; with it, the subpackage is still rejected.

### Task 2: `Price` and `UserID`
Commit: `feat(domain): add Price and UserID value objects`

**Goal:** the price in whole rubles and the user ID (design §2.2, decisions #12 and #44).

**Scope:** `internal/subscription/domain`.

**Rules:**
- `NewPrice` takes an `int64`, so the adapter can pass any integer it parsed, and `9999999999` gets the range error instead of a type error.
- `IsZero` exists only on `UserID` and `ServiceName`: the filter builder in phase 3 uses it to tell whether a filter is set.

**Provides:**
- `const MaxPrice = math.MaxInt32`;
- `func NewPrice(rubles int64) (Price, error)`, `func (p Price) Rubles() int64`, `ErrPriceOutOfRange`;
- `func NewUserID(id uuid.UUID) (UserID, error)`, `func (u UserID) UUID() uuid.UUID`, `func (u UserID) IsZero() bool`, `ErrNilUserID`.

**Done when:** tests cover
- [ ] prices `math.MinInt64`, −1, 0, max, max+1 and `math.MaxInt64`;
- [ ] the zero `Price` equals `NewPrice(0)`;
- [ ] `uuid.Nil` is rejected; a v4, a v7 and `uuid.Max` are accepted;
- [ ] `IsZero` is true for the zero `UserID` and false for a constructed one;
- [ ] both types are in the structure test;
- [ ] its errors are in the error table test.

### Task 3: `ServiceName`
Commit: `feat(domain): add ServiceName value object`

**Goal:** the normalized, validated service name (design §2.2, decisions #13 and #75).

**Scope:** `internal/subscription/domain`.

**Rules:**
- Normalization: `strings.Fields`, then a join with single spaces.
- Then, in this order:
  1. empty;
  2. invalid characters: the string is not valid UTF-8, or a rune is a control character (`unicode.IsControl`);
  3. longer than 100 runes.
- Case is preserved.

**Provides:**
- `const MaxServiceNameLength = 100`;
- `func NewServiceName(raw string) (ServiceName, error)`, `func (n ServiceName) String() string`, `func (n ServiceName) IsZero() bool`;
- `ErrEmptyServiceName`, `ErrServiceNameInvalidCharacters`, `ErrServiceNameTooLong`.

**Done when:** tests cover
- [ ] trimming and collapsing of tab, newline, NBSP U+00A0, U+3000 and NEL U+0085; `"Yandex\tPlus"` becomes `"Yandex Plus"`;
- [ ] `""` and a whitespace-only string give `ErrEmptyServiceName`;
- [ ] 100 runes are valid and 101 are not, for ASCII and for Cyrillic (100 Cyrillic runes are 200 bytes);
- [ ] case is preserved;
- [ ] NUL, ESC `\x1b`, DEL `\x7f`, the C1 character U+009B and the byte `0xFF` give `ErrServiceNameInvalidCharacters`;
- [ ] U+200B is kept: it is a format character, neither whitespace nor control;
- [ ] the type is in the structure test;
- [ ] its errors are in the error table test.

**Known pitfalls (verified):**
- Tab and NEL are control characters themselves. A check placed before normalization would reject the valid `"Yandex\tPlus"`.
- No Unicode normalization (NFC): it needs `golang.org/x/text`, which the design does not list. A decomposed "й" (и + U+0306) therefore does not match a precomposed one.

### Task 4: `SubscriptionPeriod` and `CalculationPeriod`
Commit: `feat(domain): add subscription and calculation periods`

**Goal:** the two period types, held by value (design §2.2, decision #56).

**Scope:** `internal/subscription/domain`.

**Rules:**
- A zero `MonthYear` in any argument gives `ErrInvalidMonth`, as the same raw input (month 0) would.
- Checks run in this order: start or from, then end or to, then their order.
- Everything is held by value.

**Provides:**
- `func NewSubscriptionPeriod(start, end MonthYear) (SubscriptionPeriod, error)`;
- `func NewOpenEndedSubscriptionPeriod(start MonthYear) (SubscriptionPeriod, error)`;
- `func (p SubscriptionPeriod) Start() MonthYear`, `func (p SubscriptionPeriod) End() (MonthYear, bool)`, `ErrEndBeforeStart`;
- `func NewCalculationPeriod(from, to MonthYear) (CalculationPeriod, error)`, `func (p CalculationPeriod) From() MonthYear`, `func (p CalculationPeriod) To() MonthYear`, `ErrInvertedCalculationPeriod`.

**Done when:** tests cover
- [ ] an end before the start fails; an end equal to the start (one month) is valid;
- [ ] `End()` returns false for an open-ended period and `(end, true)` for a closed one;
- [ ] a zero start, end, from or to gives `ErrInvalidMonth`;
- [ ] periods compare equal by value with `==`; a closed and an open-ended period with the same start are not equal;
- [ ] `from` equal to `to` is valid; `to` before `from` fails;
- [ ] both types are in the structure test;
- [ ] its errors are in the error table test.

### Task 5: `Subscription` aggregate
Commit: `feat(domain): add Subscription aggregate`

**Goal:** the one aggregate, with its invariants and full-replacement update (design §2.3, decisions #9, #11 and #16).

**Scope:** `internal/subscription/domain`.

**Rules:**
- The aggregate is passed by value. Every field is a value, so a copy is fully independent, there is no nil, and an in-memory fake in phase 3 cannot alias stored state.
- All its methods use pointer receivers, because `Update` changes state and receiver kinds are not mixed.
- The same constructor loads rows from the database.
- Zero fields are checked in field order, each with its own value object's error:
  - nil ID → `ErrNilSubscriptionID`;
  - zero `UserID` → `ErrNilUserID`;
  - zero name → `ErrEmptyServiceName`;
  - zero period → `ErrInvalidMonth`.
- A zero price is valid.
- `Update` checks everything before it assigns anything.

**Provides:**
- `func NewSubscription(id uuid.UUID, userID UserID, name ServiceName, price Price, period SubscriptionPeriod) (Subscription, error)`;
- `func (s *Subscription) ID() uuid.UUID`, `func (s *Subscription) UserID() UserID`, `func (s *Subscription) ServiceName() ServiceName`, `func (s *Subscription) Price() Price`, `func (s *Subscription) Period() SubscriptionPeriod`;
- `func (s *Subscription) Update(name ServiceName, price Price, period SubscriptionPeriod) error`;
- `ErrNilSubscriptionID`.

**Done when:** tests cover
- [ ] every field survives a round trip;
- [ ] each invalid zero field on its own gives its error; a zero price is accepted;
- [ ] `Update` replaces the name, the price and the period, and keeps the ID and the user ID; a closed period can be replaced by an open-ended one;
- [ ] an invalid `Update` returns an error, and the aggregate before and after compares equal with `==`;
- [ ] after `s2 := s1`, `s2.Update(…)` does not change `s1`;
- [ ] the type is in the structure test;
- [ ] its errors are in the error table test.

### Task 6: Queries and ports
Commit: `feat(domain): add queries and repository ports`

**Goal:** the inputs of list and total cost, and the two ports the Postgres adapter implements (design §2.4, §2.5).

**Scope:** `internal/subscription/domain`.

**Rules:**
- A zero field in `Filter` means "no filter", which is unambiguous because the zero `UserID` and `ServiceName` are invalid values. The zero `Filter` matches every record.
- The domain does not validate the page; the HTTP adapter does (design §2.4).
- The total-cost query requires its period, so it has a constructor and unexported fields: whatever holds a required value object rejects its zero value.
- The port contract lives in their doc comments:
  - `Get`, `Update` and `Delete` return an error that wraps `ErrNotFound` when the subscription does not exist;
  - `List` orders by ID, and `total` counts every match regardless of limit and offset;
  - `TotalCost` returns whole rubles, 0 when nothing matches.
- The structure test does not cover queries: `Filter` and `ListQuery` have exported fields on purpose, and `TotalCostQuery` contains a `Filter`.

**Provides:**
- `type Filter struct { UserID UserID; ServiceName ServiceName }`;
- `type ListQuery struct { Filter Filter; Limit, Offset int }`;
- `func NewTotalCostQuery(filter Filter, period CalculationPeriod) (TotalCostQuery, error)`, `func (q TotalCostQuery) Filter() Filter`, `func (q TotalCostQuery) Period() CalculationPeriod`;
- the ports:
  - `Repository`:
    - `Create(ctx context.Context, s Subscription) error`
    - `Get(ctx context.Context, id SubscriptionID) (Subscription, error)`
    - `Update(ctx context.Context, s Subscription) error`
    - `Delete(ctx context.Context, id SubscriptionID) error`
    - `List(ctx context.Context, q ListQuery) (items []Subscription, total int, err error)`
  - `TotalCostReader`:
    - `TotalCost(ctx context.Context, q TotalCostQuery) (int64, error)`

**Done when:**
- [ ] a zero period in `NewTotalCostQuery` gives `ErrInvalidMonth`; a valid query survives a round trip;
- [ ] for the whole phase, `make lint test` is green;
- [ ] `go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./internal/subscription/domain` prints only the package itself, `internal/shared/valueobject` and `github.com/google/uuid`;
- [ ] CI is green on the pull request.
