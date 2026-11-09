# Tasks

## 1. Config and data model

- [x] 1.1 Add `TFTPFile` struct (`VendorClass string`, `Filename string`) and `TFTPFiles []TFTPFile` field to `OOBConfig` in `internal/api/config.go`. Verify: `go build ./...` succeeds; struct fields are yaml-tagged correctly.
- [x] 1.2 Store `tftpFiles` on `K8sClient` (loaded from `OOBConfig.TFTPFiles` in the plugin setup path alongside `serverIP`). Verify: the field is accessible in the handler.

## 2. File-selection logic

- [x] 2.1 Implement `selectTFTPFile(files []api.TFTPFile, classID string) string` in `plugins/oob/plugin.go` (or a small helper file): iterate in order, return the first entry whose non-empty `VendorClass` is a prefix of `classID`; after the loop, return the first entry with an empty `VendorClass` (fallback). Return `""` if nothing matches. Verify: unit tests cover exact prefix match, partial prefix match, fallback, no-match-no-fallback, and empty list.
- [x] 2.2 In the phase-1 path of the oob handler (after the iPXE phase-2 block, before the final `return resp, false`): when `len(k.tftpFiles) > 0`, call `selectTFTPFile`, set options 66 (`dhcpv4.OptTFTPServerName(k.serverIP)`) and 67 (`dhcpv4.OptBootFileName(filename)`) when a filename is returned, log a warning when `filename == ""`, then `return resp, true`. When `tftpFiles` is empty, fall through to `return resp, false` as before. Verify: unit tests cover (a) match found → options 66/67 set, chain stopped; (b) no match, no fallback → options 66/67 absent, chain stopped; (c) tftpFiles empty → options 66/67 absent, chain continues (`false`).

## 3. Helm chart

- [x] 3.1 Add `oob.tftpFiles` to `chart/metaldhcp/values.yaml` as an empty list with a commented example showing two arch entries and a fallback. Verify: `helm template metaldhcp ./chart/metaldhcp` succeeds with no tftpFiles set.
- [x] 3.2 Add `tftpFiles` rendering to `chart/metaldhcp/templates/configmap.yaml` (under `oob.yaml`): iterate `oob.tftpFiles`, emit `filename` always and `vendorClass` conditionally. Verify: `helm template metaldhcp ./chart/metaldhcp -f dev/values.yaml` renders tftpFiles correctly when entries are present in dev values; absent when the list is empty.
- [x] 3.3 Replace `tftp.ipxeURL` with `tftp.ipxeFiles: [{url, filename}]` in `chart/metaldhcp/values.yaml`. Replace the single-`wget` `fetch-ipxe` init container in `deployment.yaml` with a shell loop over `tftp.ipxeFiles`. Keep `tftp.nbpFilename` for the nbp-plugin path. Verify: `helm template` with `tftp.ipxeFiles` set renders one `wget` line per entry; with `tftp.ipxeFiles` empty the init container is omitted.

## 4. Integration smoke test

- [x] 4.1 Add a dev/values.yaml `tftpFiles` entry for `PXEClient:Arch:00007` → `snponly.efi` and a fallback `snponly.efi` entry, and add a test case to `dev/test-dhcp.py`: send a DISCOVER with option 60 = `PXEClient:Arch:00007:UNDI:003000` and verify option 67 = `snponly.efi` in the OFFER. Verify: test passes under `make tilt-up`.
