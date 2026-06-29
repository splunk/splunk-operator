/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package core

import (
	"time"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// managedRole is the cluster controller's in-memory representation of a PostgreSQL
// role it computes from PostgresDatabase status and feeds into CNPG managed.roles.
// It is internal controller state, not a served CRD field.
type managedRole struct {
	// Name of the role/user to create.
	Name string

	// PasswordSecretRef references a Secret and the key within it containing the password for this role.
	PasswordSecretRef *corev1.SecretKeySelector

	// Exists controls whether the role should be present (true) or absent (false) in PostgreSQL.
	Exists bool
}

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
	ImageName            string
	Instances            int
	PrimaryUpdateMethod  string
	PgHBA                []string
	DefaultDatabase      string
	Owner                string
	StorageSize          string
	Resources            corev1.ResourceRequirements
	InheritedAnnotations map[string]string
	ServerAltDNSNames    []string
	Backup               *normalizedBackupSpec
	Plugins              []normalizedPluginSpec
	BootstrapType        bootstrapKind
}

// bootstrapKind enumerates the CNPG bootstrap strategies the operator selects between.
// It is an internal drift-detection value (not a CRD field), compared only for equality.
type bootstrapKind string

const (
	bootstrapInitDB   bootstrapKind = "initdb"
	bootstrapRecovery bootstrapKind = "recovery"
)

type normalizedBackupSpec struct {
	Target                 string
	VolumeSnapshotClass    string
	WalClassName           string
	SnapshotOwnerReference string
	Online                 *bool
	Labels                 map[string]string
	Annotations            map[string]string
}

type normalizedPluginSpec struct {
	Name          string
	Enabled       bool
	IsWALArchiver bool
	Parameters    map[string]string
}

// normalizedCNPGPoolerSpec is a subset of cnpgv1.PoolerSpec fields used for drift detection.
// Only fields we set in buildCNPGPooler are included — CNPG/Kubernetes-injected defaults are
// excluded to avoid false-positive drift on every reconcile.
type normalizedCNPGPoolerSpec struct {
	ClusterName         string
	Type                string
	Instances           int32
	PoolMode            string
	Parameters          map[string]string
	TemplateAnnotations map[string]string
	TemplateContainers  []string
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

	// minInstancesForSwitchover is the minimum effective instance count a class
	// using primaryUpdateMethod=switchover requires: switchover needs a replica
	// to fail over to.
	minInstancesForSwitchover = 2

	defaultServerCACertKey string = "ca.crt"
	defaultDatabaseName    string = "postgres"
	superUsername          string = "postgres"
	labelCNPGReload        string = "cnpg.io/reload"

	secretKeyPassword      string = "password"
	secretKeyUsername      string = "username"
	requiredSecretUsername string = "postgres"
	defaultSecretSuffix    string = "-secret"
	defaultPoolerSuffix    string = "-pooler-"
	defaultConfigMapSuffix string = "-configmap"
	poolerSANSuffix        string = ".svc.cluster.local"

	clusterDeletionPolicyDelete string = "Delete"
	clusterDeletionPolicyRetain string = "Retain"

	// PostgresClusterFinalizerName is exported so the primary adapter (controller) can
	// reference it in event predicates without duplicating the string.
	PostgresClusterFinalizerName string = "postgresclusters.enterprise.splunk.com/finalizer"

	// postgresqlParametersFieldManager owns only CNPG spec.postgresql.parameters keys applied from PostgresCluster.spec.postgresqlConfig.
	postgresqlParametersFieldManager string = "splunk-postgrescluster-postgresql-parameters"

	// legacyPostgreSQLParametersUpdateManager is the controller-runtime default manager name used by
	// pre-SSA MergeFrom patches. It is used only to adopt stale managedFields ownership for parameter keys.
	legacyPostgreSQLParametersUpdateManager string = "manager"

	// cluster phases
	readyClusterPhase        reconcileClusterPhases = "Ready"
	pendingClusterPhase      reconcileClusterPhases = "Pending"
	provisioningClusterPhase reconcileClusterPhases = "Provisioning"
	configuringClusterPhase  reconcileClusterPhases = "Configuring"
	failedClusterPhase       reconcileClusterPhases = "Failed"

	// condition types
	clusterReady      conditionTypes = "ClusterReady"
	poolerReady       conditionTypes = "PoolerReady"
	backupReady       conditionTypes = "BackupReady"
	objectStoreReady  conditionTypes = "ObjectStoreReady"
	managedRolesReady conditionTypes = "ManagedRolesReady"
	secretsReady      conditionTypes = "SecretsReady"
	configMapsReady   conditionTypes = "ConfigMapsReady"

	// condition reasons — cross-component
	reasonUpstreamNotReady conditionReasons = "UpstreamNotReady"

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
	reasonSuperUserSecretReady          conditionReasons = "SuperUserSecretReady"
	reasonSuperUserSecretFailed         conditionReasons = "SuperUserSecretFailed"
	reasonExternalSecretInvalid         conditionReasons = "ExternalSecretInvalid"
	reasonExternalSecretMissingData     conditionReasons = "ExternalSecretMissingData"
	reasonExternalSecretMissingKeys     conditionReasons = "ExternalSecretMissingKeys"
	reasonExternalSecretInvalidUsername conditionReasons = "ExternalSecretInvalidUsername"
	reasonExternalSecretMissing         conditionReasons = "ExternalSecretMissing"
	reasonExternalSecretMissingLabel    conditionReasons = "ExternalSecretMissingReloadLabel"

	// condition reasons — lifecycle/finalizer
	reasonClusterDeleteFailed conditionReasons = "ClusterDeleteFailed"

	// condition reasons — poolerReady
	reasonPoolerReconciliationFailed conditionReasons = "PoolerReconciliationFailed"
	reasonPoolerConfigMissing        conditionReasons = "PoolerConfigMissing"
	reasonPoolerCreating             conditionReasons = "PoolerCreating"
	reasonPoolerDisabled             conditionReasons = "PoolerDisabled"
	reasonPoolerSANsPending          conditionReasons = "PoolerSANsPending"
	reasonPoolerTLSLeafPending       conditionReasons = "PoolerTLSLeafPending"
	reasonPoolerTLSLeafInvalidCert   conditionReasons = "PoolerTLSLeafInvalidCert"
	reasonAllInstancesReady          conditionReasons = "AllInstancesReady"

	// condition reasons — backupReady
	reasonBackupDisabled              conditionReasons = "BackupDisabled"
	reasonBackupConfigured            conditionReasons = "BackupConfigured"
	reasonBackupVolumeSnapshotMissing conditionReasons = "BackupVolumeSnapshotMissing"
	reasonBackupProviderMissing       conditionReasons = "BackupProviderMissing"
	reasonScheduledBackupCreated      conditionReasons = "ScheduledBackupCreated"
	reasonScheduledBackupFailed       conditionReasons = "ScheduledBackupFailed"

	// condition reasons — objectStoreReady
	reasonObjectStoreDisabled        conditionReasons = "ObjectStoreDisabled"
	reasonObjectStoreConfigured      conditionReasons = "ObjectStoreConfigured"
	reasonObjectStoreReconcileFailed conditionReasons = "ObjectStoreReconcileFailed"

	// condition reasons — CNPG cluster phase mapping
	reasonCNPGClusterHealthy     conditionReasons = "CNPGClusterHealthy"
	reasonCNPGProvisioning       conditionReasons = "CNPGClusterProvisioning"
	reasonCNPGRecovery           conditionReasons = "CNPGClusterRecovery"
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

	// status messages — cross-component
	msgUpstreamNotReady statusMessage = "Waiting for upstream components"

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
	msgFmtCNPGScaling            statusMessage = "Scaling cluster: %d/%d instances ready"
	msgFmtCNPGStorageResizing    statusMessage = "Resizing storage: %d/%d PVCs pending"

	// status messages — backup
	msgBackupDisabled              statusMessage = "Backup is not enabled"
	msgBackupVolumeSnapshotMissing statusMessage = "Backup enabled but cnpg.backup.volumeSnapshot is not configured in the class"
	msgScheduledBackupReady        statusMessage = "Scheduled backup is configured and active"
	msgFmtScheduledBackupFailed    statusMessage = "Failed to reconcile scheduled backup: %v"

	// status messages — aggregate and component readiness checks
	msgPoolerDisabled                 statusMessage = "Connection pooler disabled"
	msgPoolerConfigMissing            statusMessage = "Connection pooler enabled but configuration is missing"
	msgPoolersProvisioning            statusMessage = "Connection poolers are being provisioned"
	msgWaitRWPoolerObject             statusMessage = "Waiting for RW pooler object"
	msgWaitROPoolerObject             statusMessage = "Waiting for RO pooler object"
	msgPoolersNotReady                statusMessage = "Connection poolers are not ready yet"
	msgPoolerSANsPending              statusMessage = "Waiting for pooler SAN reconcile"
	msgPoolerTLSLeafPending           statusMessage = "Waiting for pooler server TLS leaf to match spec"
	msgFmtPoolerTLSLeafInvalidCert    statusMessage = "Server TLS secret %s/%s cannot be parsed; see operator logs"
	msgPoolersReady                   statusMessage = "Connection poolers are ready"
	msgConfigMapRefNotPublished       statusMessage = "ConfigMap reference not published yet"
	msgConfigMapNotFoundYet           statusMessage = "ConfigMap not found yet"
	msgConfigMapCAMetadataPending     statusMessage = "Waiting for CA metadata in access ConfigMap"
	msgFmtConfigMapMissingRequiredKey statusMessage = "ConfigMap missing required key %q"
	msgAccessConfigMapReady           statusMessage = "Access ConfigMap is ready"
	msgExternalSecretInvalid          statusMessage = "External superuser secret is invalid"
	msgExternalSecretMissing          statusMessage = "External superuser secret is missing"
	msgExternalSecretInvalidUsername  statusMessage = "External superuser secret username is invalid"
	msgExternalSecretGenericFailure   statusMessage = "Failed to fetch external superuser secret"
	msgExternalSecretMissingLabel     statusMessage = "External superuser secret must carry the cnpg.io/reload=\"true\" label so CNPG reloads on rotation"
	msgFmtSecretMissingKey            statusMessage = "Superuser secret missing key %q"
	msgSuperuserSecretReady           statusMessage = "Superuser secret is ready"
)
