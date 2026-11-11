# oobsubnet-static-leases Specification

## Purpose
Defines per-subnet static DHCP reservations (MAC → fixed IP) for both the inline Helm config (`subnets[].staticLeases`) and the `OOBSubnet` CRD (`spec.staticLeases`), replacing the former top-level `staticLeases` section.

## Requirements

### Requirement: Per-subnet static leases in Helm config
Each entry in `OOBConfig.subnets` SHALL support a `staticLeases` list of client→IP reservations. Each entry identifies a client by exactly one of `mac` or `clientId`; the other SHALL be empty. A static lease pins that client to a specific IP within the subnet for every DHCP request from that client.

#### Scenario: Static IP returned for matching MAC
- **WHEN** a DISCOVER arrives from a MAC that matches a `staticLeases` entry (by `mac`) in the subnet whose CIDR contains the relay giaddr
- **THEN** the server offers the configured fixed IP, and the gateway and bootURL from that subnet

#### Scenario: Static IP returned for matching client ID
- **WHEN** a DISCOVER arrives with a client ID that matches a `staticLeases` entry (by `clientId`) in the subnet whose CIDR contains the relay giaddr
- **THEN** the server offers the configured fixed IP, and the gateway and bootURL from that subnet

#### Scenario: Static IP reserved against dynamic allocation
- **WHEN** a static lease IP is configured in a subnet
- **THEN** dynamic allocation SHALL NOT assign that IP to any other client

### Requirement: Per-subnet static leases in OOBSubnet CRD
`OOBSubnetSpec` SHALL support a `staticLeases` list of client→IP reservations with the same semantics as the Helm config path. Each entry identifies a client by exactly one of `mac` or `clientId`; the other SHALL be empty.

#### Scenario: Static IP returned from CRD entry by MAC
- **WHEN** a DISCOVER arrives from a MAC that matches a `staticLeases` entry (by `mac`) in an `OOBSubnet` CR whose CIDR contains the relay giaddr
- **THEN** the server offers the configured fixed IP, and the gateway and bootURL from that CR

#### Scenario: Static IP returned from CRD entry by client ID
- **WHEN** a DISCOVER arrives with a client ID that matches a `staticLeases` entry (by `clientId`) in an `OOBSubnet` CR whose CIDR contains the relay giaddr
- **THEN** the server offers the configured fixed IP, and the gateway and bootURL from that CR

#### Scenario: CRD static lease takes effect without restart
- **WHEN** an `OOBSubnet` CR is created or updated with a new `staticLeases` entry after the server is running
- **THEN** subsequent DHCP requests from that client SHALL receive the reserved IP without a server restart

#### Scenario: CRD static IP reserved against dynamic allocation
- **WHEN** a static lease IP is present in an `OOBSubnet` CR
- **THEN** dynamic allocation SHALL NOT assign that IP to any other client once the CR has been observed

### Requirement: Admission validation of static lease IPs
The API server SHALL reject an `OOBSubnet` create or update where any `staticLeases` entry has an IP outside the subnet's `cidr`, with a human-readable error message identifying the offending MAC and IP.

#### Scenario: IP outside CIDR rejected
- **WHEN** an `OOBSubnet` is applied with a static lease whose IP is not within `spec.cidr`
- **THEN** the admission webhook returns a descriptive error, e.g. `static lease 00:11:22:33:44:55 → 10.99.0.5 is outside subnet CIDR 10.0.1.0/24`

#### Scenario: IP inside CIDR accepted
- **WHEN** an `OOBSubnet` is applied with all static lease IPs within `spec.cidr`
- **THEN** the resource is admitted without error

#### Scenario: Multiple violations reported together
- **WHEN** multiple static leases have IPs outside `spec.cidr`
- **THEN** the error message SHALL enumerate all offending entries

### Requirement: Admission validation of static lease client identifier
The API server SHALL reject an `OOBSubnet` create or update where any `staticLeases` entry has both `mac` and `clientId` set, or where neither is set, with a human-readable error message.

#### Scenario: Both mac and clientId set — rejected
- **WHEN** an `OOBSubnet` is applied with a static lease entry that has both `mac` and `clientId` populated
- **THEN** the admission webhook returns an error identifying the conflicting entry

#### Scenario: Neither mac nor clientId set — rejected
- **WHEN** an `OOBSubnet` is applied with a static lease entry that has neither `mac` nor `clientId`
- **THEN** the admission webhook returns an error identifying the incomplete entry

#### Scenario: Exactly one identifier set — accepted
- **WHEN** an `OOBSubnet` is applied with static lease entries each having exactly one of `mac` or `clientId`
- **THEN** the resource is admitted without error
