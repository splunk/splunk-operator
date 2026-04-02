package pgcConstants

type Reason string

const (
	// condition reasons — clusterReady
	reasonClusterClassNotFound  Reason = "ClusterClassNotFound"
	reasonManagedRolesFailed    Reason = "ManagedRolesReconciliationFailed"
	reasonClusterBuildFailed    Reason = "ClusterBuildFailed"
	reasonClusterBuildSucceeded Reason = "ClusterBuildSucceeded"
	reasonClusterGetFailed      Reason = "ClusterGetFailed"
	reasonClusterPatchFailed    Reason = "ClusterPatchFailed"
	reasonInvalidConfiguration  Reason = "InvalidConfiguration"
	reasonConfigMapFailed       Reason = "ConfigMapReconciliationFailed"
	reasonUserSecretFailed      Reason = "UserSecretReconciliationFailed"
	reasonSuperUserSecretFailed Reason = "SuperUserSecretFailed"
	reasonClusterDeleteFailed   Reason = "ClusterDeleteFailed"

	// condition reasons — poolerReady
	reasonPoolerReconciliationFailed Reason = "PoolerReconciliationFailed"
	reasonPoolerConfigMissing        Reason = "PoolerConfigMissing"
	reasonPoolerCreating             Reason = "PoolerCreating"
	reasonAllInstancesReady          Reason = "AllInstancesReady"

	// condition reasons — Provisioner cluster phase mapping
	reasonProvisionerClusterNotHealthy  Reason = "ClusterNotHealthy"
	reasonProvisionerClusterHealthy     Reason = "ClusterHealthy"
	reasonProvisionerProvisioning       Reason = "ClusterProvisioning"
	reasonProvisionerSwitchover         Reason = "Switchover"
	reasonProvisionerFailingOver        Reason = "FailingOver"
	reasonProvisionerRestarting         Reason = "Restarting"
	reasonProvisionerUpgrading          Reason = "Upgrading"
	reasonProvisionerApplyingConfig     Reason = "ApplyingConfiguration"
	reasonProvisionerPromoting          Reason = "Promoting"
	reasonProvisionerWaitingForUser     Reason = "WaitingForUser"
	reasonProvisionerUnrecoverable      Reason = "Unrecoverable"
	reasonProvisionerProvisioningFailed Reason = "ProvisioningFailed"
	reasonProvisionerPluginError        Reason = "PluginError"
	reasonProvisionerImageError         Reason = "ImageError"
)
