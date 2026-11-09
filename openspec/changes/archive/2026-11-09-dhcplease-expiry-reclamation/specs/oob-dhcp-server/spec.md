# Spec Delta

## MODIFIED Requirements

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

## ADDED Requirements

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
