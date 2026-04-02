package pgcConstants

type State uint8

const (
	EmptyState State = iota
	PoolerReady
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
