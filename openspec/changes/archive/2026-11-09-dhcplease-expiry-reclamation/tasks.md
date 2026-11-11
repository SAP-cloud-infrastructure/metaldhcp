# Tasks

## 1. Allocator: expiry-aware entries and reclamation

- [x] 1.1 Replace `byMAC map[string]net.IP` with an entry type carrying `ip net.IP` and `expiresAt time.Time` (zero for static), and add a reverse `byIP map[string]string` (IP → MAC key); update `assign`, `Reserve`, and `Restore` to maintain all three plus `inUse`.
- [x] 1.2 Add a reclamation grace field to `Allocator`, set at construction.
- [x] 1.3 Make `Restore` expiry-aware: accept the lease's `LeaseExpiresAt`; skip `reserved` MACs as today; if the IP is already held by a *different* MAC, keep the live entry, drop the other, and log (never double-map).
- [x] 1.4 Extend `firstFree` to reclaim: return a never-used address first; only when none remain, evict an in-use IP whose holder is dynamic (not in `reserved`) with `expiresAt + grace < now`, clearing its `byMAC`/`byIP`/`inUse` entries.
- [x] 1.5 Change `Allocate` to also return the evicted MAC key (if any) so the caller can delete its CR; keep static and future/within-grace entries non-reclaimable.
- [x] 1.6 Unit tests: reclaim past grace; no reclaim within grace or with future expiry; static/reserved never reclaimed; prefer-free before reclaim; `Allocate` reports the evicted MAC; `Restore` duplicate-IP resolution keeps the live entry.

## 2. Config: reclamation grace

- [x] 2.1 Add a reclamation grace duration to the oob plugin config (`internal/api/config.go`) with a non-zero default (~one `leaseTime`); parse/validate like other durations.
- [x] 2.2 Wire the configured grace into `allocator.New(...)` where the allocator is constructed.
- [x] 2.3 Expose the grace in the Helm chart (`chart/metaldhcp` values + configmap) if it is operator-tunable; default preserved when unset.

## 3. Plugin: CR lifecycle

- [x] 3.1 In the oob plugin allocation path, after `Allocate` returns an evicted MAC key, delete that MAC's `DHCPLease` CR outside the allocator lock; tolerate already-deleted.
- [x] 3.2 Make `seedFromLeases` enforce one-CR-per-IP: build an IP → lease index while seeding, and on a duplicate keep the live lease, delete the expired duplicate's CR (log the removal), and pass `LeaseExpiresAt` into `Restore`.

## 4. Tests

- [x] 4.1 Plugin/integration tests: a reclaimed lease's `DHCPLease` CR is deleted when its IP is reassigned; an expired IP is reused by a new MAC end-to-end.
- [x] 4.2 Seed test: a ledger with two CRs on one IP (one expired beyond grace) seeds only the live lease and deletes the expired CR.

## 5. Validation

- [x] 5.1 CI gates pass: `make test`, the codegen check (`make generate manifests fmt` leaves no diff), and `helm lint` (defaults + dev values). (`make lint`/golangci-lint is not a CI gate; the reclamation code is golangci-clean, repo-wide `make lint` has pre-existing unrelated failures.)
- [x] 5.2 `openspec validate dhcplease-expiry-reclamation --strict` passes.

## Workflow follow-up

- Archive the change after implementation and review (`openspec archive dhcplease-expiry-reclamation`), which folds the delta into `openspec/specs/oob-dhcp-server/spec.md`.
