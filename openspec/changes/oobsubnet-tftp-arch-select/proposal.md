# Proposal

## Why

PXE clients announce their CPU architecture in DHCP option 60 (vendor class identifier,
e.g. `PXEClient:Arch:00007` for x86-64 UEFI, `PXEClient:Arch:00014` for ARM64 UEFI).
Currently the oob plugin offers a single TFTP filename to all clients, so a mixed-arch
fleet cannot boot from one metaldhcp deployment without extra relay-side logic.

## What Changes

- Add a `tftpFiles` list to the global oob config (and its Helm chart representation)
  mapping vendorClass prefixes to TFTP filenames, with an optional fallback entry.
- Extend the oob plugin's phase-1 handler to read option 60, match against `tftpFiles`,
  and set options 66 (TFTP server) and 67 (bootfile name) directly instead of deferring
  to the downstream nbp plugin. When no match is found and a fallback entry exists, the
  fallback filename is used. When `tftpFiles` is empty, the nbp plugin continues to
  handle options 66/67 as today (no behaviour change for existing deployments).
- The nbp plugin is skipped (chain stopped) whenever oob selects a filename from
  `tftpFiles`, preventing a downstream nbp from overwriting option 67.
- The TFTP server address (option 66) is set to the configured `serverIP`, which is
  already known to the oob plugin.

- Extend `tftp.ipxeFiles` in the Helm chart (replacing the current `tftp.ipxeURL` +
  `tftp.nbpFilename` scalars) to allow fetching multiple binaries at startup. The
  `fetch-ipxe` init container iterates the list and downloads each file into the shared
  `/ipxe` volume that tftpd serves. Operators who need per-arch binaries set both
  `oob.tftpFiles` (DHCP selection) and `tftp.ipxeFiles` (binary delivery).

## Capabilities

### New Capabilities

- `oobsubnet-tftp-arch-select`: Per-architecture TFTP file selection — the DHCP server
  selects option 67 (bootfile name) based on the client's option 60 (vendor class
  identifier) using a configured mapping.

### Modified Capabilities

- `oob-dhcp-server`: Adds a new DHCP response behaviour (arch-driven option 67
  selection) to the existing IPv4 serving requirement.

## Impact

- `internal/api/config.go`: new `TFTPFile` struct and `TFTPFiles []TFTPFile` field on `OOBConfig`
- `plugins/oob/plugin.go`: read option 60, match tftpFiles, set options 66/67, stop chain
- `plugins/oob/k8s.go`: propagate `TFTPFiles` from OOBSubnet CRs (if exposed there) or keep global only
- `chart/metaldhcp/templates/configmap.yaml`: render `tftpFiles` into oob.yaml
- `chart/metaldhcp/values.yaml`: add `oob.tftpFiles` list; replace `tftp.ipxeURL` +
  `tftp.nbpFilename` with `tftp.ipxeFiles: [{url, filename}]`
- `chart/metaldhcp/templates/deployment.yaml`: replace the single-file `fetch-ipxe` init
  container with a loop-based one that downloads all `tftp.ipxeFiles` entries
