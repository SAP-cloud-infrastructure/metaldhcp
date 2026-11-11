# Spec Delta

## ADDED Requirements

### Requirement: Reject contradictory filter configuration
The `OOBSubnet` validating webhook SHALL reject a resource that sets a non-empty `macList` together with `mode: allowAll`, since the list would be silently ignored.

#### Scenario: macList with allowAll is rejected
- **WHEN** an `OOBSubnet` is created or updated with `mode: allowAll` and a non-empty `macList`
- **THEN** admission is denied with an error explaining that `macList` requires `allow` or `deny`

## MODIFIED Requirements

### Requirement: MAC allowlist filtering per subnet
Each subnet (an `OOBSubnet` CR or an inline config subnet) SHALL declare a filter `mode` — one of `allowAll`, `allow`, or `deny`, defaulting to `allowAll` — together with a `macList`. The mode determines which DHCPDiscover/DHCPRequest messages the instance answers, comparing the request `chaddr` against `macList` using normalized, case- and separator-insensitive MAC values.

#### Scenario: allowAll serves every MAC
- **WHEN** `mode` is `allowAll`
- **THEN** the instance answers requests from any MAC and ignores `macList`

#### Scenario: allow serves only listed MACs
- **WHEN** `mode` is `allow` and a request's normalized `chaddr` is in `macList`
- **THEN** the instance answers it, and a request whose MAC is not listed is silently ignored with no DHCPLease written

#### Scenario: Empty allow serves nobody
- **WHEN** `mode` is `allow` and `macList` is empty
- **THEN** the instance answers no requests for that subnet

#### Scenario: deny serves all except listed
- **WHEN** `mode` is `deny` and a request's normalized `chaddr` is in `macList`
- **THEN** the instance silently ignores it, while requests from all other MACs are answered

#### Scenario: Matching is format-insensitive
- **WHEN** `macList` contains a MAC in colon, dash, or separator-free form in any case
- **THEN** it matches the request's `chaddr` regardless of the format either side is written in

### Requirement: Filtering applies to both config and CR subnets
The `mode` and `macList` fields SHALL be supported on both inline Helm config subnets (`oob.subnets[]`) and `OOBSubnet` CRs. Behavior SHALL be identical regardless of which path defines the subnet.

#### Scenario: Config subnet with allow
- **WHEN** a config subnet sets `mode: allow` and a request arrives from a MAC not in `macList`
- **THEN** the instance does not respond, same as for a CR subnet

#### Scenario: OOBSubnet CR with deny
- **WHEN** an `OOBSubnet` CR sets `mode: deny` with a MAC in `macList` and that MAC sends a request
- **THEN** the instance does not respond
