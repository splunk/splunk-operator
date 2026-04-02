package core

import (
	"time"

	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ReconcileContext bundles infrastructure dependencies injected by the controller
// shell (primary adapter). The service layer declares what it needs via this struct
// rather than reaching into context — keeping ports explicit and testable.
type ReconcileContext struct {
	Client   client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// normalizedCNPGClusterSpec is a subset of cnpgv1.ClusterSpec fields used for drift detection.
// Only fields we set in buildCNPGClusterSpec are included — CNPG-injected defaults are excluded
// to avoid false-positive drift on every reconcile.
type normalizedCNPGClusterSpec struct {
	ImageName               string
	Instances               int
	CustomDefinedParameters map[string]string
	PgHBA                   []string
	DefaultDatabase         string
	Owner                   string
	StorageSize             string
	Resources               corev1.ResourceRequirements
}

// MergedConfig is the resolved configuration after overlaying PostgresCluster on PostgresClusterClass defaults.
type MergedConfig struct {
	Spec *enterprisev4.PostgresClusterSpec
	CNPG *enterprisev4.CNPGConfig
}

type reconcileClusterPhases string
type conditionTypes string
type conditionReasons string
type objectKind string

const (
	retryDelay = time.Second * 15

	readOnlyEndpoint  string = "ro"
	readWriteEndpoint string = "rw"

	defaultDatabaseName string = "postgres"
	superUsername       string = "postgres"
	defaultPort         string = "5432"

	secretKeyPassword      string = "password"
	defaultSecretSuffix    string = "-secret"
	defaultPoolerSuffix    string = "-pooler-"
	defaultConfigMapSuffix string = "-configmap"

	clusterDeletionPolicyDelete string = "Delete"
	clusterDeletionPolicyRetain string = "Retain"

	// PostgresClusterFinalizerName is exported so the primary adapter (controller) can
	// reference it in event predicates without duplicating the string.
	PostgresClusterFinalizerName string = "postgresclusters.enterprise.splunk.com/finalizer"

	// cluster phases
	readyClusterPhase        reconcileClusterPhases = "Ready"
	pendingClusterPhase      reconcileClusterPhases = "Pending"
	provisioningClusterPhase reconcileClusterPhases = "Provisioning"
	configuringClusterPhase  reconcileClusterPhases = "Configuring"
	failedClusterPhase       reconcileClusterPhases = "Failed"

	// condition types
	clusterReady conditionTypes = "ClusterReady"
	poolerReady  conditionTypes = "PoolerReady"

	// condition reasons — clusterReady
	reasonClusterClassNotFound  conditionReasons = "ClusterClassNotFound"
	reasonManagedRolesFailed    conditionReasons = "ManagedRolesReconciliationFailed"
	reasonClusterBuildFailed    conditionReasons = "ClusterBuildFailed"
	reasonClusterBuildSucceeded conditionReasons = "ClusterBuildSucceeded"
	reasonClusterGetFailed      conditionReasons = "ClusterGetFailed"
	reasonClusterPatchFailed    conditionReasons = "ClusterPatchFailed"
	reasonInvalidConfiguration  conditionReasons = "InvalidConfiguration"
	reasonConfigMapFailed       conditionReasons = "ConfigMapReconciliationFailed"
	reasonUserSecretFailed      conditionReasons = "UserSecretReconciliationFailed"
	reasonSuperUserSecretFailed conditionReasons = "SuperUserSecretFailed"
	reasonClusterDeleteFailed   conditionReasons = "ClusterDeleteFailed"

	// condition reasons — poolerReady
	reasonPoolerReconciliationFailed conditionReasons = "PoolerReconciliationFailed"
	reasonPoolerConfigMissing        conditionReasons = "PoolerConfigMissing"
	reasonPoolerCreating             conditionReasons = "PoolerCreating"
	reasonAllInstancesReady          conditionReasons = "AllInstancesReady"

	// condition reasons — CNPG cluster phase mapping
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
)
