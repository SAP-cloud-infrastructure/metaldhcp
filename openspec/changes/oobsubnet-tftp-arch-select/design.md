# Design

## Context

The oob plugin processes every DHCP request and already intercepts phase 2 (iPXE loaded)
to stop the plugin chain and set the chain URL. For phase 1 (PXE ROM, option 60 =
`PXEClient:Arch:...`), it currently returns `false` (continue chain) so the downstream
nbp plugin sets options 66 and 67 from a single static URL configured in `config.yaml`.

Option 60 (vendor class identifier) is available on every phase-1 request; the value
encodes the CPU architecture (e.g. `PXEClient:Arch:00007` = x86-64 UEFI, `:00014` = ARM64
UEFI). No code today reads it for filename selection.

## Goals / Non-Goals

**Goals:**
- Select option 67 (bootfile name) and option 66 (TFTP server) per request based on
  option 60 prefix matching, configured via a global `tftpFiles` list in oob.yaml.
- Zero behaviour change when `tftpFiles` is empty (nbp runs as before).
- Stop the chain whenever `tftpFiles` is non-empty so nbp cannot override the result.

**Non-Goals:**
- Per-subnet tftpFiles (global config only; all subnets share the same binary set).
- TFTP binary delivery (operators must ensure binaries are present in the tftpd container).
- OOBSubnet CRD changes (tftpFiles is not exposed as a Kubernetes field).

## Decisions

### 1. Implement in the oob plugin, not a new plugin

The oob plugin is the only component that sees both the pool context and the raw request
options. Adding a new coredhcp plugin would need a way to pass the phase-2 decision and
the server IP to it; reusing the oob plugin avoids that coupling.

Alternatives considered: a standalone `tftp-arch` plugin after nbp in the chain.
Rejected: it would run after nbp, requiring oob to signal "skip nbp" via response state,
which the coredhcp chain model does not support cleanly.

### 2. tftpFiles as a global OOBConfig field (not per-subnet)

The binary set is a deployment property, not a subnet property. A given metaldhcp pod
always serves the same architecture mix regardless of which pool a client lands in.
Keeping it global avoids CRD and reconcile changes.

### 3. Stop the chain whenever tftpFiles is non-empty

When tftpFiles is configured, the oob plugin takes full ownership of options 66/67: it
sets them if a match (or fallback) is found, and sets nothing if neither is found.
Either way it stops the chain so nbp cannot inject a global default.

Operator intent is explicit: if you configure tftpFiles, you own the entire bootfile
selection. Unknown clients with no match and no fallback receive no bootfile option —
this is intentional and surfaced by the absence of a fallback entry.

### 4. Prefix match on option 60

Clients include extra fields after the arch code
(`PXEClient:Arch:00007:UNDI:003000`). A prefix match against the configured vendorClass
string is sufficient and future-proof. Exact match would break on new firmware variants.

### 5. option 66 value = OOBConfig.ServerIP

The TFTP server address is already known to the oob plugin as `k.serverIP`. Using it
for option 66 keeps configuration DRY; the operator does not need to repeat the server
IP in the tftpFiles list.

## Data model

```go
// internal/api/config.go
type TFTPFile struct {
    VendorClass string `yaml:"vendorClass,omitempty"` // prefix to match; empty = fallback
    Filename    string `yaml:"filename"`
}

// added to OOBConfig:
TFTPFiles []TFTPFile `yaml:"tftpFiles,omitempty"`
```

## Plugin change (pseudo-code)

```
// at the end of the phase-1 path (after iPXE phase-2 block, before return false):
if len(k.tftpFiles) > 0 {
    classID := string(req.Options.Get(dhcpv4.OptionClassIdentifier))
    filename := selectTFTPFile(k.tftpFiles, classID)
    if filename != "" {
        resp.Options.Update(dhcpv4.OptTFTPServerName(k.serverIP))
        resp.Options.Update(dhcpv4.OptBootFileName(filename))
    }
    return resp, true  // stop chain; oob owns 66/67 when tftpFiles is set
}
return resp, false  // tftpFiles empty: nbp runs as before
```

`selectTFTPFile` iterates tftpFiles in order, returns the first match whose VendorClass
is a prefix of classID, then falls back to the first entry with an empty VendorClass.

## Helm chart: tftp.ipxeFiles

The current `tftp.ipxeURL` + `tftp.nbpFilename` scalars can only fetch one binary at
startup. Replace them with `tftp.ipxeFiles: [{url, filename}]`:

```yaml
# values.yaml
tftp:
  ipxeFiles:
    - url: https://example.com/ipxe-x64.efi
      filename: ipxe-x64.efi
    - url: https://example.com/ipxe-arm64.efi
      filename: ipxe-arm64.efi
```

The `fetch-ipxe` init container changes from a single `wget` invocation to a shell loop:

```yaml
command: ["sh", "-c"]
args:
  - |
    {{- range .Values.tftp.ipxeFiles }}
    wget -O /ipxe/{{ .filename }} {{ .url | quote }}
    {{- end }}
```

`tftp.ipxeURL` and `tftp.nbpFilename` are removed. The `nbp` plugin in `config.yaml` still
uses `tftp.nbpFilename` for the advertised TFTP path — when `tftpFiles` is set, the nbp
plugin is bypassed anyway; when it is not set, operators must name one of their
`ipxeFiles` entries to match `nbpFilename`, or set `nbpFilename` to an explicit value.
Simplest: keep `tftp.nbpFilename` as a separate scalar for the nbp plugin URL while
deprecating `tftp.ipxeURL`. Only `tftp.ipxeURL` is removed (it becomes `tftp.ipxeFiles`);
`tftp.nbpFilename` stays for the nbp-only path.

**Migration:** existing users of `tftp.ipxeURL` must move to `tftp.ipxeFiles`:

```yaml
# before
tftp:
  ipxeURL: https://example.com/snponly.efi
  nbpFilename: snponly.efi

# after
tftp:
  nbpFilename: snponly.efi  # still used by nbp plugin when tftpFiles is empty
  ipxeFiles:
    - url: https://example.com/snponly.efi
      filename: snponly.efi
```


`oob.tftpFiles` list in `values.yaml`; `configmap.yaml` renders it into `oob.yaml`:

```yaml
{{- with .Values.oob.tftpFiles }}
tftpFiles:
  {{- range . }}
  - filename: {{ .filename | quote }}
    {{- if .vendorClass }}
    vendorClass: {{ .vendorClass | quote }}
    {{- end }}
  {{- end }}
{{- end }}
```

## Risks / Trade-offs

- **tftpFiles non-empty + no fallback → unknown clients get no bootfile.** Operator must
  add a fallback entry or accept that unlisted architectures are not served. This is
  intentional but easy to misconfigure. Mitigation: log a warning when no match is found
  and tftpFiles is non-empty.

- **nbp plugin becomes inactive when tftpFiles is set.** Operators who previously relied
  on nbp for a single global bootfile must migrate to a tftpFiles list with one fallback
  entry when they enable per-arch selection.

## Migration Plan

Existing deployments: no change — `tftpFiles` defaults to empty, nbp continues to run.

New per-arch deployments:
1. Add `oob.tftpFiles` in Helm values.
2. Ensure tftpd container has all listed binaries (bake in or add init containers).
3. Remove or keep `nbp:` in `config.yaml` — it is harmless but inactive once tftpFiles
   is non-empty.
