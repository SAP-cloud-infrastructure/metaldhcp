# oob-dhcp-server Specification

## Purpose
Provides IPv4 DHCP service for OOB/BMC management networks, allocating addresses from
operator-defined pools and recording every lease as a `DHCPLease` CR so the downstream
metal-onboarding-operator can react to new BMC/OOB devices without polling.

## Requirements

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
every successful address assignment, recording `macAddress`, `ip`, `subnet`, `gateway`,
`leaseTime`, and `clientID`.

#### Scenario: New lease
- **WHEN** an IP is allocated to a MAC address for the first time
- **THEN** a `DHCPLease` CR named after the normalized MAC is created with the assigned IP, lease time, and client ID

#### Scenario: Re-lease (same MAC)
- **WHEN** a MAC address with an existing `DHCPLease` sends a new request
- **THEN** the server assigns the same IP and patches the existing `DHCPLease` (no duplicate CRs are created)

### Requirement: Allocation state rebuilt from DHCPLease CRs on restart
The system SHALL seed its in-memory allocator from existing `DHCPLease` objects in the
configured namespace at startup, so previously allocated IPs are not re-issued after a
restart. When two `DHCPLease` CRs record the same IP, the system SHALL retain the live
lease, delete the expired one, and log the removal, so a single IP is never seeded to two
MACs.

#### Scenario: Restart recovery
- **WHEN** the server starts and `DHCPLease` CRs exist in the namespace
- **THEN** the IPs recorded in those leases are treated as in-use and will not be assigned to a different MAC

#### Scenario: Duplicate IP in ledger on restart
- **WHEN** the server starts and two `DHCPLease` CRs record the same IP, one of them expired beyond the grace period
- **THEN** the system seeds only the live lease, deletes the expired CR, logs the removal, and that IP maps to a single MAC

### Requirement: IPv6 not served
The system SHALL NOT allocate addresses or write leases for DHCPv6 requests; IPv6 OOB
allocation is out of scope.

#### Scenario: DHCPv6 request received
- **WHEN** a DHCPv6 packet arrives
- **THEN** the server passes it through without allocating an address or writing a DHCPLease CR

### Requirement: MAC-list and client-ID-list admission filter
An `OOBSubnet` pool MAY declare `macList`, `clientIdList`, or both, together with a `mode` (`allow` or `deny`). A client request SHALL be admitted to a pool only when it passes the filter; requests that fail the filter SHALL be silently dropped.

#### Scenario: allow mode — MAC matches macList
- **WHEN** a pool has `mode: allow` and the request's MAC address is in `macList`
- **THEN** the request is admitted to the pool

#### Scenario: allow mode — client ID matches clientIdList
- **WHEN** a pool has `mode: allow` and the request's client ID (option 61) is in `clientIdList`
- **THEN** the request is admitted to the pool

#### Scenario: allow mode — MAC matches in mixed list
- **WHEN** a pool has `mode: allow` with both `macList` and `clientIdList` set, and the request's MAC is in `macList` but its client ID is not in `clientIdList`
- **THEN** the request is admitted

#### Scenario: allow mode — client ID matches in mixed list
- **WHEN** a pool has `mode: allow` with both `macList` and `clientIdList` set, and the request's client ID is in `clientIdList` but its MAC is not in `macList`
- **THEN** the request is admitted

#### Scenario: allow mode — neither matches
- **WHEN** a pool has `mode: allow` and the request's MAC is not in `macList` and its client ID is not in `clientIdList`
- **THEN** the request is dropped and not allocated an IP from this pool

#### Scenario: deny mode — MAC matches macList
- **WHEN** a pool has `mode: deny` and the request's MAC address is in `macList`
- **THEN** the request is dropped

#### Scenario: deny mode — client ID matches clientIdList
- **WHEN** a pool has `mode: deny` and the request's client ID is in `clientIdList`
- **THEN** the request is dropped

#### Scenario: deny mode — MAC matches in mixed list
- **WHEN** a pool has `mode: deny` with both `macList` and `clientIdList` set, and the request's MAC is in `macList` but its client ID is not in `clientIdList`
- **THEN** the request is dropped

#### Scenario: deny mode — client ID matches in mixed list
- **WHEN** a pool has `mode: deny` with both `macList` and `clientIdList` set, and the request's client ID is in `clientIdList` but its MAC is not in `macList`
- **THEN** the request is dropped

#### Scenario: deny mode — neither matches
- **WHEN** a pool has `mode: deny` and the request's MAC is not in `macList` and its client ID is not in `clientIdList`
- **THEN** the request is admitted

#### Scenario: static leases bypass the filter
- **WHEN** a pool has `mode: allow` and the client's MAC or client ID matches a static lease entry in that pool
- **THEN** the client is admitted and receives its reserved IP regardless of `macList` and `clientIdList`

### Requirement: Expired dynamic leases are reclaimed for reuse
The system SHALL treat a dynamic lease whose `LeaseExpiresAt` has passed by more than the
configured grace period as reclaimable and SHALL reassign its IP to another client when
allocating. Reclamation occurs on demand during allocation; the system SHALL NOT run a
background reclamation sweep.

#### Scenario: Expired IP reused by a new client
- **WHEN** a new MAC requests an address and a candidate IP is held only by a dynamic lease expired beyond the grace period
- **THEN** the system assigns that IP to the new MAC

#### Scenario: Live lease is not reclaimed
- **WHEN** a dynamic lease's `LeaseExpiresAt` is in the future or still within the grace period
- **THEN** its IP is not offered to another MAC

### Requirement: Reclamation grace period is configurable
The system SHALL expose the reclamation grace period as configuration with a non-zero
default, so a client that briefly stops renewing is not dropped the instant its lease
expires.

#### Scenario: Client offline within grace keeps its address
- **WHEN** a client stops renewing but resumes before `LeaseExpiresAt` plus the grace period has elapsed
- **THEN** its lease is not reclaimed and it continues to hold the same IP

### Requirement: Reclaiming a lease deletes its record
When the system reclaims a dynamic lease it SHALL remove both the in-memory allocation
entry and the lease's `DHCPLease` CR, so a subsequent restart does not re-seed the
reclaimed assignment.

#### Scenario: Reclaimed lease does not survive restart
- **WHEN** a dynamic lease is reclaimed and its IP reassigned to a new MAC
- **THEN** the old lease's `DHCPLease` CR is deleted and a later restart seed does not restore the old MAC-to-IP mapping

### Requirement: At most one DHCPLease CR per IP
The system SHALL maintain at most one `DHCPLease` CR per IP address at any time; two
non-expired leases recording the same IP SHALL never coexist.

#### Scenario: Reassignment replaces the record
- **WHEN** an expired lease's IP is reassigned to a new MAC
- **THEN** only the new MAC's `DHCPLease` CR records that IP

### Requirement: Static leases are excluded from expiry and reclamation
The system SHALL never expire or reclaim a static (reserved) lease and SHALL never offer a
statically bound address to another client.

#### Scenario: Static binding is never reclaimed
- **WHEN** allocation needs an address and a candidate IP is statically bound
- **THEN** that IP is not offered to another MAC regardless of any elapsed time
