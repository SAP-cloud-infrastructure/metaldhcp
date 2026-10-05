# Spec Delta

## Purpose

Provides IPv4 DHCP service for OOB/BMC management networks, allocating addresses from
operator-defined pools and recording every lease as a `DHCPLease` CR so the downstream
metal-onboarding-operator can react to new BMC/OOB devices without polling.

## ADDED Requirements

### Requirement: IPv4 DHCP serving on OOB networks
The system SHALL respond to DHCPv4 DISCOVER and REQUEST messages received directly or
via DHCP relay agents on OOB/BMC network segments.

#### Scenario: Direct request (no relay)
- **WHEN** a DHCPv4 DISCOVER or REQUEST arrives with `giaddr` unset
- **THEN** the server selects the pool whose CIDR contains the candidate IP derived from `clientIP` → `requestedIP` → `serverIP`, allocates an address, and returns an OFFER or ACK

#### Scenario: Relayed request (giaddr set)
- **WHEN** a DHCPv4 DISCOVER or REQUEST arrives with a non-zero `giaddr`
- **THEN** the server selects the pool whose CIDR contains `giaddr` (the relay agent's subnet), allocates an address within that pool, and returns an OFFER or ACK

#### Scenario: No matching pool
- **WHEN** no pool's CIDR matches the candidate IP or `giaddr`
- **THEN** the server drops the request and logs the reason

### Requirement: IP allocation from operator-defined pools
The system SHALL allocate IPv4 addresses from pools defined in the oob plugin config
(`subnets` list) and/or `OOBSubnet` CRs selected by label. Config-defined pools SHALL
take precedence when present.

#### Scenario: Config pool allocation
- **WHEN** a request matches a config-defined pool
- **THEN** the server allocates from that pool; `OOBSubnet` CRs for the same CIDR are ignored

#### Scenario: OOBSubnet CR pool allocation
- **WHEN** no config pools are defined and a labeled `OOBSubnet` CR matches
- **THEN** the server allocates from that CR's pool

#### Scenario: Exact-IP honored
- **WHEN** the client requests a specific IP that is within the matching pool and currently free
- **THEN** the server assigns that exact IP

#### Scenario: Pool exhausted
- **WHEN** all allocatable IPs in the matching pool are in use
- **THEN** the server drops the request and logs exhaustion

### Requirement: DHCPLease CR written per lease
The system SHALL create or update a `DHCPLease` CR (group `dhcp.metal.ironcore.dev`) on
every successful address assignment, recording `macAddress`, `ip`, `leaseTime`, and
`clientID`.

#### Scenario: New lease
- **WHEN** an IP is allocated to a MAC address for the first time
- **THEN** a `DHCPLease` CR named after the normalized MAC is created with the assigned IP, lease time, and client ID

#### Scenario: Re-lease (same MAC)
- **WHEN** a MAC address with an existing `DHCPLease` sends a new request
- **THEN** the server assigns the same IP and patches the existing `DHCPLease` (no duplicate CRs are created)

### Requirement: Allocation state rebuilt from DHCPLease CRs on restart
The system SHALL seed its in-memory allocator from existing `DHCPLease` objects in the
configured namespace at startup, so previously allocated IPs are not re-issued after a
restart.

#### Scenario: Restart recovery
- **WHEN** the server starts and `DHCPLease` CRs exist in the namespace
- **THEN** the IPs recorded in those leases are treated as in-use and will not be assigned to a different MAC

### Requirement: IPv6 not served
The system SHALL NOT allocate addresses or write leases for DHCPv6 requests; IPv6 OOB
allocation is out of scope.

#### Scenario: DHCPv6 request received
- **WHEN** a DHCPv6 packet arrives
- **THEN** the server passes it through without allocating an address or writing a DHCPLease CR
