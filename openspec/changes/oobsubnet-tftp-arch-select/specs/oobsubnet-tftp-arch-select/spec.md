# Spec Delta

## Purpose

Enables the DHCP server to serve architecture-appropriate TFTP boot files by matching
the client's vendor class identifier (DHCP option 60) against a configured mapping,
eliminating the need for per-arch relay-side workarounds in mixed-architecture fleets.

## ADDED Requirements

### Requirement: Per-architecture TFTP file selection via vendor class
The system SHALL select option 67 (bootfile name) and option 66 (TFTP server name) based
on the client's option 60 (vendor class identifier) when a `tftpFiles` mapping is
configured. Matching SHALL use prefix comparison against the option 60 value. If no
entry matches, the system SHALL use the fallback entry (an entry with no vendorClass). If
no fallback exists, options 66 and 67 SHALL NOT be set by this mechanism.

#### Scenario: Matching vendorClass selects the corresponding filename
- **WHEN** a DHCP DISCOVER arrives with option 60 = `PXEClient:Arch:00007:UNDI:003000`
- **AND** tftpFiles contains `{ vendorClass: "PXEClient:Arch:00007", filename: "ipxe-x64.efi" }`
- **THEN** the OFFER includes option 67 = `ipxe-x64.efi` and option 66 = the server IP

#### Scenario: No-match falls back to the default entry
- **WHEN** a DHCP DISCOVER arrives with option 60 = `PXEClient:Arch:00014:UNDI:003000`
- **AND** tftpFiles has no entry with vendorClass `PXEClient:Arch:00014`
- **AND** tftpFiles contains a fallback entry `{ filename: "snponly.efi" }` (no vendorClass)
- **THEN** the OFFER includes option 67 = `snponly.efi`

#### Scenario: No tftpFiles config — nbp plugin handles bootfile as before
- **WHEN** `tftpFiles` is not configured (empty list)
- **THEN** the oob plugin does NOT set options 66/67; downstream nbp plugin behaviour is unchanged

#### Scenario: Phase-2 iPXE requests are not affected
- **WHEN** a DHCP REQUEST carries the iPXE user-class marker identifying phase 2
- **THEN** tftpFiles selection does NOT apply; the server responds with the bootURL as usual

### Requirement: tftpFiles ordering — first match wins
The system SHALL use the first `tftpFiles` entry whose vendorClass prefix matches
option 60. Entries are evaluated in declaration order; the fallback entry (no
vendorClass) SHOULD appear last.

#### Scenario: First matching entry is used when multiple could match
- **WHEN** tftpFiles lists `PXEClient:Arch:00007` before `PXEClient:Arch:0000`
- **AND** the client sends option 60 = `PXEClient:Arch:00007:UNDI:003000`
- **THEN** the entry for `PXEClient:Arch:00007` is selected
