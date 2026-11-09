# Spec Delta

## MODIFIED Requirements

### Requirement: IPv4 DHCP serving on OOB networks
The system SHALL respond to DHCPv4 DISCOVER and REQUEST messages received directly or
via DHCP relay agents on OOB/BMC network segments. When a `tftpFiles` mapping is
configured, the system SHALL set options 66 and 67 based on the client's option 60
(vendor class identifier). The `nbp` plugin configuration does not affect option 67
in this case.

#### Scenario: Direct request (no relay)
- **WHEN** a DHCPv4 DISCOVER or REQUEST arrives with `giaddr` unset
- **THEN** the server selects the pool whose CIDR contains the candidate IP derived from `clientIP` → `requestedIP` → `serverIP`, allocates an address, and returns an OFFER or ACK

#### Scenario: Relayed request (giaddr set)
- **WHEN** a DHCPv4 DISCOVER or REQUEST arrives with a non-zero `giaddr`
- **THEN** the server selects the pool whose CIDR contains `giaddr` (the relay agent's subnet), allocates an address within that pool, and returns an OFFER or ACK

#### Scenario: No matching pool
- **WHEN** no pool's CIDR matches the candidate IP or `giaddr`
- **THEN** the server drops the request and logs the reason

#### Scenario: tftpFiles configured — oob sets options 66/67, nbp does not override
- **WHEN** `tftpFiles` is non-empty and the request is phase 1 (not iPXE phase 2)
- **THEN** the server sets options 66 and 67 from the tftpFiles match; the `nbp` plugin does not override option 67
