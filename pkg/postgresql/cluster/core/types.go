package core

import (
	"time"

	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
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
	Metrics  ports.Recorder
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
	InheritedAnnotations    map[string]string
}

// MergedConfig is the resolved configuration after overlaying PostgresCluster on PostgresClusterClass defaults.
type MergedConfig struct {
	Spec *enterprisev4.PostgresClusterSpec
	CNPG *enterprisev4.CNPGConfig
}

type reconcileClusterPhases string
type conditionTypes string
type conditionReasons string
type statusMessage = string
type objectKind string

const (
	retryDelay = time.Second * 15

	readOnlyEndpoint  string = "ro"
	readWriteEndpoint string = "rw"

	defaultDatabaseName         string = "postgres"
	superUsername               string = "postgres"
	defaultPort                 string = "5432"
	configKeyClusterRWEndpoint  string = "CLUSTER_RW_ENDPOINT"
	configKeyClusterROEndpoint  string = "CLUSTER_RO_ENDPOINT"
	configKeyClusterREndpoint   string = "CLUSTER_R_ENDPOINT"
	configKeyDefaultClusterPort string = "DEFAULT_CLUSTER_PORT"
	configKeySuperUserName      string = "SUPER_USER_NAME"
	configKeySuperUserSecretRef string = "SUPER_USER_SECRET_REF"
	configKeyPoolerRWEndpoint   string = "CLUSTER_POOLER_RW_ENDPOINT"
	configKeyPoolerROEndpoint   string = "CLUSTER_POOLER_RO_ENDPOINT"

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
	clusterReady      conditionTypes = "ClusterReady"
	poolerReady       conditionTypes = "PoolerReady"
	managedRolesReady conditionTypes = "ManagedRolesReady"
	secretsReady      conditionTypes = "SecretsReady"
	configMapsReady   conditionTypes = "ConfigMapsReady"

	// condition reasons — cluster/provisioner
	reasonClusterClassNotFound conditionReasons = "ClusterClassNotFound"
	reasonInvalidConfiguration conditionReasons = "InvalidConfiguration"
	reasonClusterBuildFailed   conditionReasons = "ClusterBuildFailed"
	reasonClusterGetFailed     conditionReasons = "ClusterGetFailed"
	reasonClusterPatchFailed   conditionReasons = "ClusterPatchFailed"

	// condition reasons — managedRolesReady
	reasonManagedRolesReady   conditionReasons = "ManagedRolesReconciled"
	reasonManagedRolesPending conditionReasons = "ManagedRolesPending"
	reasonManagedRolesFailed  conditionReasons = "ManagedRolesReconciliationFailed"

	// condition reasons — configMapsReady
	reasonConfigMapReady  conditionReasons = "ConfigMapReconciled"
	reasonConfigMapFailed conditionReasons = "ConfigMapReconciliationFailed"

	// condition reasons — secretsReady
	reasonUserSecretPending     conditionReasons = "UserSecretPending"
	reasonUserSecretFailed      conditionReasons = "UserSecretReconciliationFailed"
	reasonSuperUserSecretReady  conditionReasons = "SuperUserSecretReady"
	reasonSuperUserSecretFailed conditionReasons = "SuperUserSecretFailed"

	// condition reasons — lifecycle/finalizer
	reasonClusterDeleteFailed conditionReasons = "ClusterDeleteFailed"

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

	// status messages — provisioner health check
	msgProvisionerHealthy        statusMessage = "Provisioner cluster is healthy"
	msgCNPGPendingCreation       statusMessage = "CNPG cluster is pending creation"
	msgFmtCNPGProvisioning       statusMessage = "CNPG cluster provisioning: %s"
	msgCNPGSwitchover            statusMessage = "Cluster changing primary node"
	msgCNPGFailingOver           statusMessage = "Pod missing, need to change primary"
	msgFmtCNPGRestarting         statusMessage = "CNPG cluster restarting: %s"
	msgFmtCNPGUpgrading          statusMessage = "CNPG cluster upgrading: %s"
	msgCNPGApplyingConfiguration statusMessage = "Configuration change is being applied"
	msgCNPGPromoting             statusMessage = "Replica is being promoted to primary"
	msgCNPGWaitingForUser        statusMessage = "Action from the user is required"
	msgCNPGUnrecoverable         statusMessage = "Cluster failed, needs manual intervention"
	msgCNPGCannotCreateObjects   statusMessage = "Cluster resources cannot be created"
	msgFmtCNPGPluginError        statusMessage = "CNPG plugin error: %s"
	msgFmtCNPGImageError         statusMessage = "CNPG image error: %s"
	msgFmtCNPGClusterPhase       statusMessage = "CNPG cluster phase: %s"

	// status messages — aggregate and component readiness checks
	msgPoolerDisabled                 statusMessage = "Connection pooler disabled"
	msgPoolerConfigMissing            statusMessage = "Connection pooler enabled but configuration is missing"
	msgPoolersProvisioning            statusMessage = "Connection poolers are being provisioned"
	msgWaitRWPoolerObject             statusMessage = "Waiting for RW pooler object"
	msgWaitROPoolerObject             statusMessage = "Waiting for RO pooler object"
	msgPoolersNotReady                statusMessage = "Connection poolers are not ready yet"
	msgPoolersReady                   statusMessage = "Connection poolers are ready"
	msgConfigMapRefNotPublished       statusMessage = "ConfigMap reference not published yet"
	msgConfigMapNotFoundYet           statusMessage = "ConfigMap not found yet"
	msgFmtConfigMapMissingRequiredKey statusMessage = "ConfigMap missing required key %q"
	msgAccessConfigMapReady           statusMessage = "Access ConfigMap is ready"
	msgSecretRefNotPublished          statusMessage = "Superuser secret reference not published yet"
	msgSecretNotFoundYet              statusMessage = "Superuser secret not found yet"
	msgFmtSecretMissingKey            statusMessage = "Superuser secret missing key %q"
	msgSuperuserSecretReady           statusMessage = "Superuser secret is ready"
)
