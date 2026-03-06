package controller

import (
	corev1 "k8s.io/api/core/v1"
	"time"
)

// This struct is used to compare the merged configuration from PostgresClusterClass and PostgresClusterSpec
// in a normalized way, and not to use CNPG-default values which are causing false positive diff state while reconciliation loop.
// It contains only the fields that are relevant for our reconciliation and that we want to compare when deciding whether to update the CNPG Cluster spec or not.
type normalizedCNPGClusterSpec struct {
	ImageName string
	Instances int
	// Parameters we set, instead of complete spec from CNPG
	CustomDefinedParameters map[string]string
	PgHBA                   []string
	DefaultDatabase         string
	Owner                   string
	StorageSize             string
	Resources               corev1.ResourceRequirements
}

type reconcilePhases string
type reconcileClusterPhases string
type conditionTypes string
type conditionReasons string
type clusterReadyStatus string

const (
	// retryDelay is the default requeue interval when waiting on external state (CNPG, cluster).
	retryDelay = time.Second * 15
	// clusterNotFoundRetryDelay is longer than retryDelay — a missing cluster is unlikely
	// to appear in 15 s and hammering the API is wasteful.
	clusterNotFoundRetryDelay = time.Second * 30
	// cluster endpoint suffixes
	readOnlyEndpoint  string = "ro"
	readWriteEndpoint string = "rw"
	// default database name
	defaultDatabaseName string = "postgres"
	defaultUsername     string = "postgres"

	// phases
	ready        reconcilePhases = "Ready"
	pending      reconcilePhases = "Pending"
	provisioning reconcilePhases = "Provisioning"
	failed       reconcilePhases = "Failed"

	// cluster phases
	readyClusterPhase        reconcileClusterPhases = "Ready"
	pendingClusterPhase      reconcileClusterPhases = "Pending"
	provisioningClusterPhase reconcileClusterPhases = "Provisioning"
	configuringClusterPhase  reconcileClusterPhases = "Configuring"
	failedClusterPhase       reconcileClusterPhases = "Failed"

	// Condition types
	clusterReady   conditionTypes = "ClusterReady"
	poolerReady    conditionTypes = "PoolerReady"
	usersReady     conditionTypes = "UsersReady"
	databasesReady conditionTypes = "DatabasesReady"
	secretsReady   conditionTypes = "SecretsReady"
	// TODO - to use in the future implementation
	// privilegesReady conditionTypes = "PrivilegesReady"

	// Condition reasons
	reasonClusterNotFound        conditionReasons = "ClusterNotFound"
	reasonClusterProvisioning    conditionReasons = "ClusterProvisioning"
	reasonClusterInfoFetchFailed conditionReasons = "ClusterInfoFetchNotPossible"
	reasonClusterAvailable       conditionReasons = "ClusterAvailable"
	reasonDatabasesAvailable     conditionReasons = "DatabasesAvailable"
	reasonSecretsCreated         conditionReasons = "SecretsCreated"
	reasonSecretsCreationFailed  conditionReasons = "SecretsCreationFailed"
	reasonWaitingForCNPG         conditionReasons = "WaitingForCNPG"
	reasonUsersCreationFailed    conditionReasons = "UsersCreationFailed"
	reasonUsersAvailable         conditionReasons = "UsersAvailable"

	// Additional condition reasons for clusterReady conditionType
	reasonClusterClassNotFound  conditionReasons = "ClusterClassNotFound"
	reasonManagedRolesFailed    conditionReasons = "ManagedRolesReconciliationFailed"
	reasonClusterBuildFailed    conditionReasons = "ClusterBuildFailed"
	reasonClusterBuildSucceeded conditionReasons = "ClusterBuildSucceeded"
	reasonClusterGetFailed      conditionReasons = "ClusterGetFailed"
	reasonClusterPatchFailed    conditionReasons = "ClusterPatchFailed"
	reasonInvalidConfiguration  conditionReasons = "InvalidConfiguration"

	// Additional condition reasons for poolerReady conditionType
	reasonPoolerReconciliationFailed conditionReasons = "PoolerReconciliationFailed"
	reasonPoolerConfigMissing        conditionReasons = "PoolerConfigMissing"
	reasonPoolerCreating             conditionReasons = "PoolerCreating"
	reasonAllInstancesReady          conditionReasons = "AllInstancesReady"

	// Additional condition reasons for mapping CNPG cluster statuses
	reasonCNPGClusterNotHealthy  conditionReasons = "CNPGClusterNotHealthy"
	reasonCNPGClusterHealthy     conditionReasons = "CNPGClusterHealthy"
	reasonCNPGProvisioning       conditionReasons = "CNPGClusterProvisioning"
	reasonCNPGSwitchover         conditionReasons = "CNPGSwitchover"
	reasonCNPGFailingOver        conditionReasons = "CNPGFailingOver"
	reasonCNPGRestarting         conditionReasons = "CNPGRestarting"
	reasonCNPGUpgrading          conditionReasons = "CNPGUpgrading"
	reasonCNPGApplyingConfig     conditionReasons = "CNPGApplyingConfiguration"
	reasonCNPGPromoting          conditionReasons = "CNPGPromoting"
	reasonCNPGWaitingForUser     conditionReasons = "CNPGWaitingForUser"
	reasonCNPGUnrecoverable      conditionReasons = "CNPGUnrecoverable"
	reasonCNPGProvisioningFailed conditionReasons = "CNPGProvisioningFailed"
	reasonCNPGPluginError        conditionReasons = "CNPGPluginError"
	reasonCNPGImageError         conditionReasons = "CNPGImageError"

	// Cluster status
	ClusterNotFound         clusterReadyStatus = "NotFound"
	ClusterNotReady         clusterReadyStatus = "NotReady"
	ClusterNoProvisionerRef clusterReadyStatus = "NoProvisionerRef"
	ClusterReady            clusterReadyStatus = "Ready"
)
