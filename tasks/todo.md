# Task Checklist: dzjjy Phase 0-2

- [x] Task 1: Runtime lock and terminal-state regression fixes
  - Acceptance: invalid app type unlocks; failed/max-restart states persist with cleared PID and explicit reason.
  - Verify: focused runtime/state tests.
  - Files: `internal/server/runtime/*`, `internal/server/state/*`
  - 2026-10-09 补齐：max-restart / 重启失败 / 崩溃现在发出 `failed` 终态事件，清除 PID 并携带
    `max_restart` / `restart_failure` / `crash` 终止原因（此前状态机会卡在 running+旧 PID）。
    回归测试：`runtime_terminal_test.go`、`restore_terminal_test.go`。

- [x] Task 2: Archive safety limits
  - Acceptance: per-file, total-expanded-size, file-count, and truncation limits fail explicitly.
  - Verify: archive unit tests and full suite.
  - Files: `internal/server/archive/*`
  - 2026-10-09 清理：删除 extractGzip 中恒假的 MaxFiles 死代码（`addFile` 已统一执行限额）。

- [x] Task 3: Lifecycle serialization, process identity/tree cleanup, shutdown
  - Acceptance: same-app operations serialize; uncertain PID is not killed; process tree cleanup and server shutdown are explicit.
  - Verify: runtime/handler/main tests.
  - Files: `internal/server/runtime/*`, `cmd/server/*`, related tests
  - 2026-10-09 补齐：进程身份校验（Linux /proc 起始时间、Windows 父 PID；不一致拒绝接管，
    未验证身份 stop 不 kill）与跨平台进程树回收（Linux 进程组 SIGKILL、Windows taskkill /T）。
    另修复 Restart 丢失 command 参数数组、Start 错误路径 ctx 泄漏。

- [x] Task 4: Strict YAML model and validation
  - Acceptance: App/StaticSite, required fields, command arrays, sandbox/template/TTL validation, unknown-field rejection.
  - Verify: config package tests.
  - Files: new config package and tests

- [x] Task 5: Apply/reconcile API and client
  - Acceptance: validate/apply behavior, idempotent generation, legacy compatibility, failure preservation.
  - Verify: handler/client integration tests.
  - Files: `internal/server/handler/*`, `internal/client/deploy/*`, `cmd/client/*`, state/runtime integration
  - 2026-10-09 补齐：客户端 apply 透出 generation/status/port/expires_at；运行中改配置的 409
    冲突路径与停止后重部署 generation+1 补充测试（`apply_generation_test.go`）；
    apply 501 文案修正；fast-exit 进程 apply 后状态覆盖为终态的竞态修复。

- [x] Task 6: StaticSite backend
  - Acceptance: safe archive root serving, independent fixed/auto ports, status, conflict failure, cleanup.
  - Verify: HTTP integration tests.
  - Files: new static backend, handler/state integration
  - 2026-10-09 补齐：手动 stop 现在会清理站点目录（此前仅 TTL/remove 清理）。

- [x] Task 7: Docker template/backend contract
  - Acceptance: controlled Python/Node/Go templates; explicit unavailable/platform errors; labels and restricted artifact handling; no fallback.
  - Verify: backend contract tests; real Docker daemon E2E only when daemon exists.
  - Files: new docker/backend packages and integration points
  - 2026-10-10 更新：真实 daemon E2E 已落地 CI——`docker_e2e` 构建标签的 e2e_test.go 覆盖
    全模板生命周期、stop 超时 kill、无标签容器拒绝、重启恢复，由 CI 的
    `Docker E2E (real daemon)` job（ubuntu runner）强制执行；本地无 daemon 时显式 skip。

- [x] Task 8: TTL and generation-aware persistence/recovery
  - Acceptance: absolute expires_at, restart/apply generation semantics, expired terminal state, distinct reasons, scheduler recovery.
  - Verify: deterministic TTL tests and state recovery tests.
  - Files: runtime/state/reconcile/scheduler
  - 2026-10-09 补齐：显式 restart 创建新 generation 并重算 TTL；服务重启后为 process backend 重建
    TTL 调度（停机期间到期直接清理标记 expired）；`expired` 终态不再被 restore 改写为 stopped、
    不会被 auto-restart 复活；TTL 清理对已自行退出的进程容错。

- [x] Task 9: Documentation and final review
  - Acceptance: docs describe safety boundaries and new interfaces; review findings fixed.
  - Verify: docs inspection, `go test ./...`, `go vet ./...`, cross-build checks, `git diff --check`.
  - Files: `docs/*`, `README*`, changed implementation/tests
  - 2026-10-09 补齐：ARCHITECTURE.md 模块树与身份/进程树/终态语义更新；TESTING.md 增补
    Docker E2E 状态声明（2026-10-10 起已在 CI 强制执行，见 Task 7）与 Phase 0-2 回归场景；
    README 中英文命令清单补 apply/validate。
    验证：`go test ./...`、`go vet ./...`、`GOOS=linux GOARCH=amd64 go build ./...`、
    `git diff --check` 全部通过；`go test -race` 因本机 CGO_ENABLED=0 无法运行（如实说明）。
