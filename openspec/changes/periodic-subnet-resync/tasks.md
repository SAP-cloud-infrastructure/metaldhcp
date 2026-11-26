# Tasks

## 1. Config

- [ ] 1.1 Add `ResyncInterval string` field (`yaml:"resyncInterval,omitempty"`) to `OOBConfig` in `internal/api/config.go` and verify `go build ./...` succeeds

## 2. Resync goroutine

- [ ] 2.1 Add `resyncInterval time.Duration` to `K8sClient` struct in `plugins/oob/k8s.go`
- [ ] 2.2 Parse `cfg.ResyncInterval` in `NewK8sClient` (mirror `reclaimGrace` logic: warn + skip on bad input) and assign to `k.resyncInterval`; verify `go build ./...` succeeds
- [ ] 2.3 After the existing bootstrap calls in `NewK8sClient`, if `k.resyncInterval > 0` start a goroutine that ticks on that interval, calls `k.oobSubnetPools(ctx)`, and returns on `ctx.Done()`; verify `go build ./...` succeeds

## 3. Tests and verification

- [ ] 3.1 Add a unit test `TestResyncInterval` in `plugins/oob/` that constructs a `K8sClient` with `resyncInterval > 0`, waits for two ticks, and asserts that `oobSubnetPools` was called the expected number of times; verify `go test ./plugins/oob/...` passes
- [ ] 3.2 Run `make lint` and fix any reported issues

## Workflow follow-up

- Archive the change after PR is merged.
