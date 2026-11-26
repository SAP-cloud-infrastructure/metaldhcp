# Spec Delta

## ADDED Requirements

### Requirement: Periodic resync of Subnet CRs
When `publishStaticLeases` is enabled and `resyncInterval` is non-zero, the server SHALL
periodically re-read all Subnet CRs and publish Lease CRs for any static lease entries not
yet recorded. This covers both newly created Subnet CRs and updated static lease lists in
existing ones.

#### Scenario: New Subnet CR picked up within one resync interval
- **WHEN** a new Subnet CR is created externally after startup AND `publishStaticLeases` is `true` AND `resyncInterval` is non-zero
- **THEN** within one resync interval Lease CRs exist for all its static lease entries — no DHCP traffic is required to trigger publication

#### Scenario: Updated static lease list picked up within one resync interval
- **WHEN** an existing Subnet CR is updated with new static lease entries AND `publishStaticLeases` is `true` AND `resyncInterval` is non-zero
- **THEN** within one resync interval Lease CRs exist for the newly added entries

#### Scenario: Resync disabled
- **WHEN** `resyncInterval` is set to `"0"`
- **THEN** no periodic resync goroutine runs; new subnets and new static lease entries are only published when inbound DHCP traffic triggers a re-read
