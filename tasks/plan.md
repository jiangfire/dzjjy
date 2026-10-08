# Implementation Plan: dzjjy Phase 0-2

## Overview
Implement the approved scope in `docs/REVIEW-AND-DESIGN.md`: reliability fixes, strict single-app YAML with idempotent apply/reconcile, StaticSite serving, controlled Docker templates, and absolute TTL lifecycle. Preserve legacy multipart APIs and string arguments; exclude the document's Phase 3 capabilities.

## Architecture Decisions
- Keep the existing Go server, state store, runtime manager, and HTTP API boundaries. Add typed configuration and backend interfaces rather than introducing Kubernetes-style resources or a controller framework.
- Use strict `yaml.v3` decoding for `dzjjy/v1` `App` and `StaticSite`; normalize before comparing desired state.
- Serialize app mutations through the existing multi-manager boundary and a per-app lock. Reconcile is explicit and idempotent; no file watch or prune.
- Treat process, static-site listener, and Docker container as separate lifecycle backends. Docker requests fail explicitly when Docker/template/platform support is unavailable.
- Persist desired configuration, generation, backend identity, observed status, timestamps, and termination reason through the existing state store.
- Keep safety boundaries explicit: archive limits, safe static paths, controlled Docker template metadata, no arbitrary Docker arguments or host paths, and no implicit Docker-to-process fallback.

## Task List

### Phase 0: Reliability foundation
- [ ] Add regression tests and fixes for runtime lock release and terminal state persistence.
- [ ] Add bounded archive extraction for file/count/total limits with truncation detection.
- [ ] Add per-app lifecycle serialization and process identity checks; implement process-tree stop per platform where supported.
- [ ] Add server shutdown orchestration and flush behavior.

### Checkpoint: Foundation
- [ ] Focused runtime/archive/handler/state tests pass.
- [ ] Existing `go test ./...` and `go vet ./...` remain clean.

### Phase 1: YAML, reconcile, and StaticSite
- [ ] Define strict YAML model and validation for `App` and `StaticSite`, including command arrays and TTL.
- [ ] Add config apply/validate API/client paths while preserving legacy deploy behavior.
- [ ] Add desired/generation/observed state representation and idempotent process reconcile.
- [ ] Add safe StaticSite file server with independent port allocation, status exposure, and cleanup.

### Checkpoint: Declarative process flow
- [ ] YAML parsing, apply failure preservation, idempotent apply, static serving/path safety, and route tests pass.

### Phase 2: Docker and TTL
- [ ] Define platform-owned Python/Node/Go templates and a Docker backend contract with explicit unavailable/platform errors.
- [ ] Implement controlled artifact/container lifecycle, labels, timeout stop, kill/remove, and recovery hooks.
- [ ] Implement absolute TTL scheduling and generation-aware expiry for all supported backends.
- [ ] Add persistence/recovery and termination-reason tests.

### Checkpoint: Lifecycle complete
- [ ] Full unit/integration tests pass; Docker daemon E2E is run only if a real daemon is available.

### Documentation and review
- [ ] Update architecture and usage documentation for YAML, StaticSite, process safety, Docker boundaries, and TTL.
- [ ] Review changed code across correctness, readability, architecture, security, and performance; fix in-scope findings.
- [ ] Run `go test ./...`, `go vet ./...`, cross-compile checks where possible, and `git diff --check`.

## Risks and Mitigations
| Risk | Impact | Mitigation |
| --- | --- | --- |
| Existing state/API shapes are relied on by tests and users | High | Read and extend existing types; preserve legacy fields and routes; add compatibility tests. |
| Docker client behavior varies across Windows/Linux daemons | High | Keep backend interface explicit, validate template platform support, add contract tests, report real-daemon E2E separately. |
| Static listeners and process state outlive requests | High | Per-app serialization, explicit cleanup, persisted observed state, shutdown hook, and generation checks. |
| Archive expansion can exhaust disk/memory | High | Enforce upload, per-file, total-size, and file-count limits before accepting extraction. |
| PID reuse can kill unrelated processes | High | Persist identity metadata and refuse management when identity cannot be confirmed. |

## Verification Commands
- `go test ./...`
- `go vet ./...`
- `go test -race ./...` when cgo is enabled
- `GOOS=linux GOARCH=amd64 go test ./...` where the host toolchain permits
- `GOOS=windows GOARCH=amd64 go test ./...`
- `git diff --check`

## Boundaries
- No idle suspend, temporary URLs, routing, network rules, port mappings, Secret, volume, resource quota, gVisor/runsc, Kubernetes import, database, etcd, or arbitrary Dockerfile/image/argument input.
- No git commit is created; existing unrelated user changes remain untouched.
