# Spec Delta

## Purpose

Allows each metaldhcp instance to restrict DHCP offer responses to an explicit MAC allowlist per subnet, and to delay offers so that instances with tighter filters can answer first on shared BMC subnets. Also enables dynamic extension of static config subnets via OOBSubnet CRs, with CR fields taking precedence.

## ADDED Requirements

### Requirement: MAC allowlist filtering per subnet
When `allowedMACs` is non-empty on an OOBSubnet CR or config subnet, the instance SHALL silently ignore DHCPDiscover and DHCPRequest messages whose `chaddr` does not appear in the list. When `allowedMACs` is empty the instance SHALL answer all requests for that subnet (catch-all).

#### Scenario: Request from allowed MAC
- **WHEN** a DHCPDiscover arrives with `chaddr` present in `allowedMACs`
- **THEN** the instance proceeds with pool selection and offer generation as normal

#### Scenario: Request from unlisted MAC
- **WHEN** a DHCPDiscover arrives with `chaddr` not in `allowedMACs`
- **THEN** the instance does not send a DHCP offer and does not write a DHCPLease CR

#### Scenario: Empty allowedMACs (catch-all)
- **WHEN** `allowedMACs` is empty or unset on the matching subnet
- **THEN** the instance answers requests from any MAC, preserving existing behavior

### Requirement: Response delay per subnet
When `responseDelay` is non-zero on a subnet, the instance SHALL wait that duration after completing offer construction before sending the DHCP offer. When zero or unset, the instance sends immediately.

#### Scenario: Non-zero delay
- **WHEN** `responseDelay` is set to a positive duration (e.g. `2s`) on the matched subnet
- **THEN** the offer is sent after the specified delay

#### Scenario: Zero / unset delay
- **WHEN** `responseDelay` is zero or absent
- **THEN** the offer is sent immediately, preserving existing behavior

### Requirement: Filtering applies to both config and CR subnets
The `allowedMACs` and `responseDelay` fields SHALL be supported on both inline Helm config subnets (`oob.subnets[]`) and `OOBSubnet` CRs. Behavior SHALL be identical regardless of which path defines the subnet.

#### Scenario: Config subnet with allowedMACs
- **WHEN** a config subnet defines `allowedMACs` and a request arrives from a non-listed MAC
- **THEN** the instance does not respond, same as for a CR subnet

#### Scenario: OOBSubnet CR with responseDelay
- **WHEN** an OOBSubnet CR defines `responseDelay: 2s` and a request arrives for that subnet
- **THEN** the offer is sent after 2 seconds

## MODIFIED Requirements

### Requirement: IP allocation from operator-defined pools
The system SHALL allocate IPv4 addresses from pools defined by labeled `OOBSubnet` CRs. At startup, for each subnet in the inline Helm config, the system SHALL apply the following bootstrap logic:

- If no CR exists for that CIDR → create one from config values, annotated `dhcp.metal.ironcore.dev/managed-by: helm`.
- If a CR exists with `managed-by: helm` → update it from current config values.
- If a CR exists without that annotation → leave it unchanged.

After startup, only CRs are used for pool selection.

#### Scenario: Config subnet, no existing CR
- **WHEN** a config subnet is defined and no OOBSubnet CR exists for its CIDR at startup
- **THEN** the system creates a `managed-by: helm` OOBSubnet CR from the config values

#### Scenario: Config subnet, existing helm-managed CR
- **WHEN** a config subnet is defined and a `managed-by: helm` CR already exists for its CIDR
- **THEN** the system updates the CR with the current config values

#### Scenario: Config subnet, externally-managed CR
- **WHEN** a config subnet is defined and a CR exists for its CIDR without the `managed-by: helm` annotation
- **THEN** the CR is left unchanged and used as-is for pool selection

#### Scenario: Runtime pool selection
- **WHEN** a DHCP request arrives
- **THEN** the system selects a pool from labeled OOBSubnet CRs only; inline config values are not consulted after startup
