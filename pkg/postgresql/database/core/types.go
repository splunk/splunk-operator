package core

import (
	"time"

	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
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

type reconcileDBPhases string
type conditionTypes string
type conditionReasons string
type clusterReadyStatus string

const (
	retryDelay                = time.Second * 15
	clusterNotFoundRetryDelay = time.Second * 30

	postgresPort string = "5432"

	readOnlyEndpoint  string = "ro"
	readWriteEndpoint string = "rw"

	deletionPolicyRetain string = "Retain"

	postgresDatabaseFinalizerName string = "postgresdatabases.enterprise.splunk.com/finalizer"
	annotationRetainedFrom        string = "enterprise.splunk.com/retained-from"

	fieldManagerPrefix string = "postgresdatabase-"

	secretRoleAdmin   string = "admin"
	secretRoleRW      string = "rw"
	secretKeyPassword string = "password"

	labelManagedBy  string = "app.kubernetes.io/managed-by"
	labelCNPGReload string = "cnpg.io/reload"

	// Password generation — no symbols for PostgreSQL connection string compatibility.
	passwordLength  = 32
	passwordDigits  = 8
	passwordSymbols = 0

	// DB reconcile phases
	readyDBPhase        reconcileDBPhases = "Ready"
	pendingDBPhase      reconcileDBPhases = "Pending"
	provisioningDBPhase reconcileDBPhases = "Provisioning"
	failedDBPhase       reconcileDBPhases = "Failed"

	// condition types
	clusterReady    conditionTypes = "ClusterReady"
	rolesReady      conditionTypes = "RolesReady"
	databasesReady  conditionTypes = "DatabasesReady"
	secretsReady    conditionTypes = "SecretsReady"
	configMapsReady conditionTypes = "ConfigMapsReady"
	privilegesReady conditionTypes = "PrivilegesReady"

	// condition reasons
	reasonClusterNotFound          conditionReasons = "ClusterNotFound"
	reasonClusterProvisioning      conditionReasons = "ClusterProvisioning"
	reasonClusterInfoFetchFailed   conditionReasons = "ClusterInfoFetchNotPossible"
	reasonClusterAvailable         conditionReasons = "ClusterAvailable"
	reasonDatabasesAvailable       conditionReasons = "DatabasesAvailable"
	reasonSecretsCreated           conditionReasons = "SecretsCreated"
	reasonSecretsCreationFailed    conditionReasons = "SecretsCreationFailed"
	reasonWaitingForCNPG           conditionReasons = "WaitingForCNPG"
	reasonUsersCreationFailed      conditionReasons = "UsersCreationFailed"
	reasonUsersAvailable           conditionReasons = "UsersAvailable"
	reasonRoleConflict             conditionReasons = "RoleConflict"
	reasonConfigMapsCreationFailed conditionReasons = "ConfigMapsCreationFailed"
	reasonConfigMapsCreated        conditionReasons = "ConfigMapsCreated"
	reasonPrivilegesGranted        conditionReasons = "PrivilegesGranted"
	reasonPrivilegesGrantFailed    conditionReasons = "PrivilegesGrantFailed"

	// ClusterReady sentinel values returned by ensureClusterReady.
	// Exported so the controller adapter can switch on them if needed.
	ClusterNotFound         clusterReadyStatus = "NotFound"
	ClusterNotReady         clusterReadyStatus = "NotReady"
	ClusterNoProvisionerRef clusterReadyStatus = "NoProvisionerRef"
	ClusterReady            clusterReadyStatus = "Ready"
)

// clusterEndpoints holds fully-resolved connection hostnames for a cluster.
// PoolerRWHost and PoolerROHost are empty when connection pooling is disabled.
type clusterEndpoints struct {
	RWHost       string
	ROHost       string
	PoolerRWHost string
	PoolerROHost string
}

// deletionPlan separates databases by their DeletionPolicy for the cleanup workflow.
type deletionPlan struct {
	retained []enterprisev4.DatabaseDefinition
	deleted  []enterprisev4.DatabaseDefinition
}
