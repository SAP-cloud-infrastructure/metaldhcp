# Spec Delta

## ADDED Requirements

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
