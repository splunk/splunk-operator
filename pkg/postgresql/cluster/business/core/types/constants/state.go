package pgcConstants

type State uint64

const (
	EmptyState  State = 0
	PoolerReady State = 1 << iota
	PoolerPending
	PoolerProvisioning
	PoolerConfiguring
	PoolerFailed

	ProvisionerReady
	ProvisionerPending
	ProvisionerProvisioning
	ProvisionerConfiguring
	ProvisionerFailed

	ConfigMapReady
	ConfigMapPending
	ConfigMapProvisioning
	ConfigMapConfiguring
	ConfigMapFailed

	SecretReady
	SecretPending
	SecretProvisioning
	SecretConfiguring
	SecretFailed

	ClusterReady
	ClusterPending
	ClusterProvisioning
	ClusterConfiguring
	ClusterFailed
)

const (
	ComponentsReady = PoolerReady | ProvisionerReady | SecretReady | ConfigMapReady
	OwnershipReady
)
