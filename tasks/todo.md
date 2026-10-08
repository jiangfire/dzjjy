# Task Checklist: dzjjy Phase 0-2

- [ ] Task 1: Runtime lock and terminal-state regression fixes
  - Acceptance: invalid app type unlocks; failed/max-restart states persist with cleared PID and explicit reason.
  - Verify: focused runtime/state tests.
  - Files: `internal/server/runtime/*`, `internal/server/state/*`

- [ ] Task 2: Archive safety limits
  - Acceptance: per-file, total-expanded-size, file-count, and truncation limits fail explicitly.
  - Verify: archive unit tests and full suite.
  - Files: `internal/server/archive/*`

- [ ] Task 3: Lifecycle serialization, process identity/tree cleanup, shutdown
  - Acceptance: same-app operations serialize; uncertain PID is not killed; process tree cleanup and server shutdown are explicit.
  - Verify: runtime/handler/main tests.
  - Files: `internal/server/runtime/*`, `cmd/server/*`, related tests

- [ ] Task 4: Strict YAML model and validation
  - Acceptance: App/StaticSite, required fields, command arrays, sandbox/template/TTL validation, unknown-field rejection.
  - Verify: config package tests.
  - Files: new config package and tests

- [ ] Task 5: Apply/reconcile API and client
  - Acceptance: validate/apply behavior, idempotent generation, legacy compatibility, failure preservation.
  - Verify: handler/client integration tests.
  - Files: `internal/server/handler/*`, `internal/client/deploy/*`, `cmd/client/*`, state/runtime integration

- [ ] Task 6: StaticSite backend
  - Acceptance: safe archive root serving, independent fixed/auto ports, status, conflict failure, cleanup.
  - Verify: HTTP integration tests.
  - Files: new static backend, handler/state integration

- [ ] Task 7: Docker template/backend contract
  - Acceptance: controlled Python/Node/Go templates; explicit unavailable/platform errors; labels and restricted artifact handling; no fallback.
  - Verify: backend contract tests; real Docker E2E only when daemon exists.
  - Files: new docker/backend packages and integration points

- [ ] Task 8: TTL and generation-aware persistence/recovery
  - Acceptance: absolute expires_at, restart/apply generation semantics, expired terminal state, distinct reasons, scheduler recovery.
  - Verify: deterministic TTL tests and state recovery tests.
  - Files: runtime/state/reconcile/scheduler

- [ ] Task 9: Documentation and final review
  - Acceptance: docs describe safety boundaries and new interfaces; review findings fixed.
  - Verify: docs inspection, `go test ./...`, `go vet ./...`, cross-build checks, `git diff --check`.
  - Files: `docs/*`, `README*`, changed implementation/tests
