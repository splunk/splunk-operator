package pgcConstants

type State uint64

const (
	EmptyState         State = 0
	PoolerReady        State = 1 << iota
	PoolerPending      State = 1 << iota
	PoolerProvisioning State = 1 << iota
	PoolerConfiguring  State = 1 << iota
	PoolerFailed       State = 1 << iota

	ProvisionerReady        State = 1 << iota
	ProvisionerPending      State = 1 << iota
	ProvisionerProvisioning State = 1 << iota
	ProvisionerConfiguring  State = 1 << iota
	ProvisionerFailed       State = 1 << iota

	ConfigMapReady        State = 1 << iota
	ConfigMapPending      State = 1 << iota
	ConfigMapProvisioning State = 1 << iota
	ConfigMapConfiguring  State = 1 << iota
	ConfigMapFailed       State = 1 << iota

	SecretReady        State = 1 << iota
	SecretPending      State = 1 << iota
	SecretProvisioning State = 1 << iota
	SecretConfiguring  State = 1 << iota
	SecretFailed       State = 1 << iota

	ClusterReady        State = 1 << iota
	ClusterPending      State = 1 << iota
	ClusterProvisioning State = 1 << iota
	ClusterConfiguring  State = 1 << iota
	ClusterFailed       State = 1 << iota
)

const (
	ComponentsReady = PoolerReady | ProvisionerReady | SecretReady | ConfigMapReady
	OwnershipReady
)
