# API Reference

## Packages
- [dhcp.metal.ironcore.dev/v1alpha1](#dhcpmetalironcoredevv1alpha1)


## dhcp.metal.ironcore.dev/v1alpha1


### Resource Types
- [DHCPLease](#dhcplease)
- [DHCPLeaseList](#dhcpleaselist)
- [OOBSubnet](#oobsubnet)
- [OOBSubnetList](#oobsubnetlist)



#### DHCPLease



DHCPLease records an IPv4 lease issued by metaldhcp. It is the durable allocation
ledger (the oob plugin rebuilds its in-memory allocator from these on startup) and
the event that triggers the onboarding flow in the onboarding operator. metaldhcp
owns these objects; the object name is the normalized MAC (e.g. "3cecefaabbcc") so
re-leases patch the same object.



_Appears in:_
- [DHCPLeaseList](#dhcpleaselist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `dhcp.metal.ironcore.dev/v1alpha1` | | |
| `kind` _string_ | `DHCPLease` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  | Optional: \{\} <br /> |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  | Optional: \{\} <br /> |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.30/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DHCPLeaseSpec](#dhcpleasespec)_ |  |  |  |


#### DHCPLeaseList









| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `dhcp.metal.ironcore.dev/v1alpha1` | | |
| `kind` _string_ | `DHCPLeaseList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  | Optional: \{\} <br /> |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  | Optional: \{\} <br /> |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.30/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[DHCPLease](#dhcplease) array_ |  |  |  |


#### DHCPLeaseSpec







_Appears in:_
- [DHCPLease](#dhcplease)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `macAddress` _string_ |  |  | Required: \{\} <br /> |
| `ip` _string_ |  |  | Required: \{\} <br /> |
| `gateway` _string_ | Gateway is the pool gateway for the assigned address. |  |  |
| `leaseTime` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.30/#duration-v1-meta)_ | LeaseTime is the lease duration offered for this address. Updated on re-lease. |  |  |
| `clientID` _string_ | ClientID is the DHCPv4 client identifier (option 61) from the request, if present. |  |  |
| `hostname` _string_ | Hostname is the client hostname, either from option 12 in the request or from a<br />static lease config entry. |  |  |


#### OOBSubnet



OOBSubnet defines an IPv4 pool for out-of-band / BMC management ports. It is
synced into the cluster by an external entity (the onboarding operator / NetBox
sync) and is read-only to metaldhcp. The oob plugin selects the subnet whose
CIDR matches the DHCP relay/link information and allocates a free address from it.



_Appears in:_
- [OOBSubnetList](#oobsubnetlist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `dhcp.metal.ironcore.dev/v1alpha1` | | |
| `kind` _string_ | `OOBSubnet` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  | Optional: \{\} <br /> |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  | Optional: \{\} <br /> |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.30/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[OOBSubnetSpec](#oobsubnetspec)_ |  |  |  |


#### OOBSubnetList









| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `dhcp.metal.ironcore.dev/v1alpha1` | | |
| `kind` _string_ | `OOBSubnetList` | | |
| `kind` _string_ | Kind is a string value representing the REST resource this object represents.<br />Servers may infer this from the endpoint the client submits requests to.<br />Cannot be updated.<br />In CamelCase.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds |  | Optional: \{\} <br /> |
| `apiVersion` _string_ | APIVersion defines the versioned schema of this representation of an object.<br />Servers should convert recognized schemas to the latest internal value, and<br />may reject unrecognized values.<br />More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources |  | Optional: \{\} <br /> |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.30/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[OOBSubnet](#oobsubnet) array_ |  |  |  |


#### OOBSubnetSpec







_Appears in:_
- [OOBSubnet](#oobsubnet)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `cidr` _string_ | CIDR is the IPv4 network from which addresses are allocated, e.g. "10.0.1.0/24". |  | Required: \{\} <br /> |
| `gateway` _string_ | Gateway, when set, is excluded from allocation and offered as the router. |  |  |
| `leaseTime` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.30/#duration-v1-meta)_ | LeaseTime is the default lease duration for addresses from this subnet. When<br />unset the server-wide lease_time plugin value applies. |  |  |
| `rangeStart` _string_ | RangeStart and RangeEnd optionally narrow allocation to a window within CIDR<br />(inclusive). When unset the whole usable host range is used. |  |  |
| `rangeEnd` _string_ |  |  |  |
| `bootURL` _string_ | BootURL, when set, is sent as the DHCP boot file URL (BootFileName field) in<br />the offer/ack. Used for per-pool UEFI HTTP boot or iPXE chain-loading. |  |  |


