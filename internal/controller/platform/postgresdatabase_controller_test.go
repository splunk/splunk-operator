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

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	dbcore "github.com/splunk/splunk-operator/pkg/postgresql/database/core"
	pgprometheus "github.com/splunk/splunk-operator/pkg/postgresql/shared/adapter/prometheus"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Event reasons emitted by the database controller on the external-secret path.
// Mirrors string constants in pkg/postgresql/database/core/events.go so the
// envtest can stay decoupled from the core package import.
const (
	dbEventRoleSecretsFailed     = "RoleSecretsFailed"
	dbEventRolesSecretsDrift     = "RolesSecretsDriftDetected"
	dbEventPostgresDatabaseReady = "PostgresDatabaseReady"
)

const postgresDatabaseFinalizer = "postgresdatabases.platform.splunk.com/finalizer"

// condition types
const (
	condClusterReady    = "ClusterReady"
	condSecretsReady    = "SecretsReady"
	condConfigMapsReady = "ConfigMapsReady"
	condRolesReady      = "RolesReady"
	condDatabasesReady  = "DatabasesReady"
	condPrivilegesReady = "PrivilegesReady"
	condCustomMetrics   = "CustomMetricsReady"
)

// condition reasons
const (
	reasonClusterNotFound       = "ClusterNotFound"
	reasonClusterAvailable      = "ClusterAvailable"
	reasonClusterProvisioning   = "ClusterProvisioning"
	reasonExternalSecretMissing = "ExternalSecretMissing"
	reasonManagedSecretMissing  = "ManagedSecretMissing"
	reasonManagedSecretConflict = "ManagedSecretOwnershipConflict"
	reasonSecretsCreated        = "SecretsCreated"
	reasonConfigMapsCreated     = "ConfigMapsCreated"
	reasonRolesAvailable        = "RolesAvailable"
	reasonDatabasesAvailable    = "DatabasesAvailable"
	reasonRoleConflict          = "RoleConflict"
	reasonWaitingForCNPG        = "WaitingForCNPG"
	reasonPrivilegesGranted     = "PrivilegesGranted"
)

// phases
const (
	phasePending      = "Pending"
	phaseProvisioning = "Provisioning"
	phaseReady        = "Ready"
	phaseFailed       = "Failed"
)

// annotations
const retainedFromAnnotation = "platform.splunk.com/retained-from"

// database names used across tests
const (
	dbAppdb  = "appdb"
	dbKeepdb = "payments"
	dbDropdb = "analytics"
)

func reconcilePostgresDatabase(ctx context.Context, nn types.NamespacedName) (ctrl.Result, error) {
	reconciler := &PostgresDatabaseReconciler{
		Client:         k8sClient,
		Scheme:         k8sClient.Scheme(),
		Recorder:       record.NewFakeRecorder(100),
		Metrics:        &pgprometheus.NoopRecorder{},
		FleetCollector: pgprometheus.NewFleetCollector(),
	}
	return reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
}

func reconcilePostgresDatabaseWithRecorder(ctx context.Context, nn types.NamespacedName, recorder record.EventRecorder) (ctrl.Result, error) {
	reconciler := &PostgresDatabaseReconciler{
		Client:         k8sClient,
		Scheme:         k8sClient.Scheme(),
		Recorder:       recorder,
		Metrics:        &pgprometheus.NoopRecorder{},
		FleetCollector: pgprometheus.NewFleetCollector(),
	}
	return reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
}

// collectEvents drains every queued event from a FakeRecorder into the
// provided slice. Non-blocking — returns once the channel is empty.
func collectEvents(events *[]string, recorder *record.FakeRecorder) {
	for {
		select {
		case e := <-recorder.Events:
			*events = append(*events, e)
		default:
			return
		}
	}
}

// containsEvent matches by both Kubernetes event type (Normal / Warning) and
// reason name. record.FakeRecorder formats events as
// "<type> <reason> <message>", so substring matching is sufficient.
func containsEvent(events []string, eventType, reason string) bool {
	for _, e := range events {
		if strings.Contains(e, eventType) && strings.Contains(e, reason) {
			return true
		}
	}
	return false
}

func publishedRoleNames(postgresDB *platformv1alpha1.PostgresDatabase) []string {
	var names []string
	for _, db := range postgresDB.Status.Databases {
		for _, role := range db.Roles {
			names = append(names, role.Name)
		}
	}
	return names
}

func adminRoleNameForTest(dbName string) string { return dbName + "_admin" }
func rwRoleNameForTest(dbName string) string    { return dbName + "_rw" }

func adminSecretNameForTest(resourceName, dbName string) string {
	return fmt.Sprintf("%s-%s-admin", resourceName, dbName)
}
func rwSecretNameForTest(resourceName, dbName string) string {
	return fmt.Sprintf("%s-%s-rw", resourceName, dbName)
}
func configMapNameForTest(resourceName, dbName string) string {
	return fmt.Sprintf("%s-%s-config", resourceName, dbName)
}
func cnpgDatabaseNameForTest(resourceName, dbName string) string {
	return fmt.Sprintf("%s-%s", resourceName, dbName)
}

func ownedByPostgresDatabase(postgresDB *platformv1alpha1.PostgresDatabase) []metav1.OwnerReference {
	controller := true
	blockOwnerDeletion := true
	return []metav1.OwnerReference{{
		APIVersion:         platformv1alpha1.GroupVersion.String(),
		Kind:               "PostgresDatabase",
		Name:               postgresDB.Name,
		UID:                postgresDB.UID,
		Controller:         &controller,
		BlockOwnerDeletion: &blockOwnerDeletion,
	}}
}

func createPostgresDatabaseResource(ctx context.Context, namespace, resourceName, clusterName string, databases []platformv1alpha1.DatabaseDefinition, finalizers ...string) *platformv1alpha1.PostgresDatabase {
	postgresDB := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{
			Name:       resourceName,
			Namespace:  namespace,
			Finalizers: finalizers,
		},
		Spec: platformv1alpha1.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: clusterName},
			Databases:  databases,
		},
	}
	Expect(k8sClient.Create(ctx, postgresDB)).To(Succeed())
	return postgresDB
}

func createPostgresClusterResource(ctx context.Context, namespace, clusterName string) *platformv1alpha1.PostgresCluster {
	postgresCluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: platformv1alpha1.PostgresClusterSpec{
			Class: "dev",
		},
	}
	Expect(k8sClient.Create(ctx, postgresCluster)).To(Succeed())
	return postgresCluster
}

func markPostgresClusterReady(ctx context.Context, postgresCluster *platformv1alpha1.PostgresCluster, cnpgClusterName, namespace string, poolerEnabled bool) {
	markPostgresClusterReadyWithPooler(ctx, postgresCluster, cnpgClusterName, namespace, poolerEnabled, poolerEnabled, poolerEnabled)
}

func markPostgresClusterReadyWithPooler(ctx context.Context, postgresCluster *platformv1alpha1.PostgresCluster, cnpgClusterName, namespace string, poolerEnabled, rwEnabled, roEnabled bool) {
	clusterPhase := "Ready"
	postgresCluster.Status.Phase = &clusterPhase
	postgresCluster.Status.ProvisionerRef = &corev1.ObjectReference{
		APIVersion: cnpgv1.SchemeGroupVersion.String(),
		Kind:       "Cluster",
		Name:       cnpgClusterName,
		Namespace:  namespace,
	}
	if poolerEnabled {
		postgresCluster.Status.ConnectionPoolerStatus = &platformv1alpha1.ConnectionPoolerStatus{
			Enabled:          true,
			ReadWriteEnabled: rwEnabled,
			ReadOnlyEnabled:  roEnabled,
		}
	}
	Expect(k8sClient.Status().Update(ctx, postgresCluster)).To(Succeed())
}

func createCNPGClusterResource(ctx context.Context, namespace, cnpgClusterName string) *cnpgv1.Cluster {
	return createCNPGClusterResourceWithInstances(ctx, namespace, cnpgClusterName, 2)
}

func createCNPGClusterResourceWithInstances(ctx context.Context, namespace, cnpgClusterName string, instances int) *cnpgv1.Cluster {
	cnpgCluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cnpgClusterName,
			Namespace: namespace,
		},
		Spec: cnpgv1.ClusterSpec{
			Instances: instances,
			StorageConfiguration: cnpgv1.StorageConfiguration{
				Size: "1Gi",
			},
		},
	}
	Expect(k8sClient.Create(ctx, cnpgCluster)).To(Succeed())
	return cnpgCluster
}

func markCNPGClusterReady(ctx context.Context, cnpgCluster *cnpgv1.Cluster, reconciledRoles []string, writeService, readService string) {
	cnpgCluster.Status.ManagedRolesStatus = cnpgv1.ManagedRoles{
		ByStatus: map[cnpgv1.RoleStatus][]string{
			cnpgv1.RoleStatusReconciled: reconciledRoles,
		},
	}
	cnpgCluster.Status.WriteService = writeService
	cnpgCluster.Status.ReadService = readService
	cnpgCluster.Status.ReadyInstances = cnpgCluster.Spec.Instances
	Expect(k8sClient.Status().Update(ctx, cnpgCluster)).To(Succeed())
}

type readyClusterScenario struct {
	namespace       string
	resourceName    string
	clusterName     string
	cnpgClusterName string
	dbName          string
	requestName     types.NamespacedName
}

func newReadyClusterScenario(namespace, resourceName, clusterName, cnpgClusterName, dbName string) readyClusterScenario {
	return readyClusterScenario{
		namespace:       namespace,
		resourceName:    resourceName,
		clusterName:     clusterName,
		cnpgClusterName: cnpgClusterName,
		dbName:          dbName,
		requestName:     types.NamespacedName{Name: resourceName, Namespace: namespace},
	}
}

func seedReadyClusterScenario(ctx context.Context, scenario readyClusterScenario, poolerEnabled bool) {
	seedReadyClusterScenarioWithDatabase(ctx, scenario, poolerEnabled, platformv1alpha1.DatabaseDefinition{Name: scenario.dbName}, 2)
}

func seedReadyClusterScenarioWithInstances(ctx context.Context, scenario readyClusterScenario, poolerEnabled bool, instances int) {
	seedReadyClusterScenarioWithDatabase(ctx, scenario, poolerEnabled, platformv1alpha1.DatabaseDefinition{Name: scenario.dbName}, instances)
}

func seedReadyClusterScenarioWithDatabase(ctx context.Context, scenario readyClusterScenario, poolerEnabled bool, database platformv1alpha1.DatabaseDefinition, instances int) {
	createPostgresDatabaseResource(ctx, scenario.namespace, scenario.resourceName, scenario.clusterName, []platformv1alpha1.DatabaseDefinition{database})
	postgresCluster := createPostgresClusterResource(ctx, scenario.namespace, scenario.clusterName)
	// Mirror the cluster controller's roPoolerWanted gate so the seeded fixture matches what
	// real reconciliation would publish: RO is suppressed below 2 declared instances.
	roEnabled := poolerEnabled && instances >= 2
	markPostgresClusterReadyWithPooler(ctx, postgresCluster, scenario.cnpgClusterName, scenario.namespace, poolerEnabled, poolerEnabled, roEnabled)
	cnpgCluster := createCNPGClusterResourceWithInstances(ctx, scenario.namespace, scenario.cnpgClusterName, instances)
	roles := dbcore.EffectiveRoleNames(database)
	markCNPGClusterReady(ctx, cnpgCluster, []string{roles.Admin, roles.RW}, "tenant-rw", "tenant-ro")
}

func expectReconcileResult(result ctrl.Result, err error, requeueAfter time.Duration) {
	Expect(err).NotTo(HaveOccurred())
	Expect(result.RequeueAfter).To(Equal(requeueAfter))
}

func expectEmptyReconcileResult(result ctrl.Result, err error) {
	Expect(err).NotTo(HaveOccurred())
	Expect(result).To(Equal(ctrl.Result{}))
}

func fetchPostgresDatabase(ctx context.Context, requestName types.NamespacedName) *platformv1alpha1.PostgresDatabase {
	current := &platformv1alpha1.PostgresDatabase{}
	Expect(k8sClient.Get(ctx, requestName, current)).To(Succeed())
	return current
}

func expectFinalizerAdded(ctx context.Context, requestName types.NamespacedName) *platformv1alpha1.PostgresDatabase {
	current := fetchPostgresDatabase(ctx, requestName)
	Expect(current.Finalizers).To(ContainElement(postgresDatabaseFinalizer))
	return current
}

func seedExistingDatabaseStatus(ctx context.Context, current *platformv1alpha1.PostgresDatabase, dbName string) {
	for _, database := range current.Spec.Databases {
		if database.Name != dbName || database.PasswordConfig != nil {
			continue
		}
		roles := dbcore.EffectiveRoleNames(database)
		for secretName, username := range map[string]string{
			adminSecretNameForTest(current.Name, dbName): roles.Admin,
			rwSecretNameForTest(current.Name, dbName):    roles.RW,
		} {
			secret := &corev1.Secret{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: current.Namespace}, secret)
			if apierrors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:            secretName,
						Namespace:       current.Namespace,
						OwnerReferences: ownedByPostgresDatabase(current),
					},
					Data: map[string][]byte{
						"username": []byte(username),
						"password": []byte("test-password"),
					},
				})).To(Succeed())
				continue
			}
			Expect(err).NotTo(HaveOccurred())
		}
		break
	}
	current.Status.Databases = []platformv1alpha1.DatabaseInfo{{Name: dbName}}
	Expect(k8sClient.Status().Update(ctx, current)).To(Succeed())
}

func expectProvisionedArtifacts(ctx context.Context, scenario readyClusterScenario, owner *platformv1alpha1.PostgresDatabase) {
	roles := roleNamesForTest(owner, scenario.dbName)
	adminSecret := &corev1.Secret{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: adminSecretNameForTest(scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, adminSecret)).To(Succeed())
	Expect(adminSecret.Data).To(HaveKey("password"))
	Expect(metav1.IsControlledBy(adminSecret, owner)).To(BeTrue())

	rwSecret := &corev1.Secret{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rwSecretNameForTest(scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, rwSecret)).To(Succeed())
	Expect(rwSecret.Data).To(HaveKey("password"))
	Expect(metav1.IsControlledBy(rwSecret, owner)).To(BeTrue())

	configMap := &corev1.ConfigMap{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configMapNameForTest(scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, configMap)).To(Succeed())
	Expect(configMap.Data).To(HaveKeyWithValue(dbcore.ConfigMapKeyDatabaseName, scenario.dbName))
	Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyDefaultClusterPort, pgconninfo.DefaultPort))
	Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyClusterRWEndpoint, "tenant-rw."+scenario.namespace+".svc.cluster.local"))
	Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyClusterROEndpoint, "tenant-ro."+scenario.namespace+".svc.cluster.local"))
	Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyClusterREndpoint, scenario.cnpgClusterName+"-r."+scenario.namespace+".svc.cluster.local"))
	Expect(configMap.Data).To(HaveKeyWithValue(dbcore.ConfigMapKeyAdminUser, roles.Admin))
	Expect(configMap.Data).To(HaveKeyWithValue(dbcore.ConfigMapKeyRWUser, roles.RW))
	Expect(metav1.IsControlledBy(configMap, owner)).To(BeTrue())
}

func expectManagedRolesPatched(ctx context.Context, scenario readyClusterScenario) {
	current := fetchPostgresDatabase(ctx, scenario.requestName)
	roles := roleNamesForTest(current, scenario.dbName)
	Expect(publishedRoleNames(current)).To(ConsistOf(roles.Admin, roles.RW))
	simulateClusterRoleOwnership(ctx, scenario.clusterName, scenario.namespace, current,
		roles.Admin, roles.RW)
}

func roleNamesForTest(postgresDB *platformv1alpha1.PostgresDatabase, dbName string) ports.DatabaseRoleNames {
	for _, db := range postgresDB.Spec.Databases {
		if db.Name == dbName {
			return dbcore.EffectiveRoleNames(db)
		}
	}
	return ports.DatabaseRoleNames{Admin: adminRoleNameForTest(dbName), RW: rwRoleNameForTest(dbName)}
}

func simulateClusterRoleOwnership(ctx context.Context, clusterName, namespace string, owner *platformv1alpha1.PostgresDatabase, roleNames ...string) {
	cluster := &platformv1alpha1.PostgresCluster{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, cluster)).To(Succeed())
	owners := make(map[string]platformv1alpha1.RoleOwnerReference, len(roleNames))
	for _, roleName := range roleNames {
		owners[roleName] = platformv1alpha1.RoleOwnerReference{Name: owner.Name, UID: string(owner.UID)}
	}
	cluster.Status.ManagedRolesStatus = &platformv1alpha1.ManagedRolesStatus{Reconciled: roleNames, RoleOwners: owners}
	Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())
}

func expectCNPGDatabaseCreated(ctx context.Context, scenario readyClusterScenario, owner *platformv1alpha1.PostgresDatabase) *cnpgv1.Database {
	cnpgDatabase := &cnpgv1.Database{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cnpgDatabaseNameForTest(scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, cnpgDatabase)).To(Succeed())
	Expect(cnpgDatabase.Spec.Name).To(Equal(scenario.dbName))
	Expect(cnpgDatabase.Spec.Owner).To(Equal(roleNamesForTest(owner, scenario.dbName).Admin))
	Expect(cnpgDatabase.Spec.ClusterRef.Name).To(Equal(scenario.cnpgClusterName))
	Expect(metav1.IsControlledBy(cnpgDatabase, owner)).To(BeTrue())
	return cnpgDatabase
}

func markCNPGDatabaseApplied(ctx context.Context, cnpgDatabase *cnpgv1.Database) {
	applied := true
	cnpgDatabase.Status.Applied = &applied
	Expect(k8sClient.Status().Update(ctx, cnpgDatabase)).To(Succeed())
}

func expectPoolerConfigMap(ctx context.Context, scenario readyClusterScenario) {
	configMap := &corev1.ConfigMap{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configMapNameForTest(scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, configMap)).To(Succeed())
	Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyPoolerRWEndpoint, scenario.cnpgClusterName+"-pooler-rw."+scenario.namespace+".svc.cluster.local"))
	Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyPoolerROEndpoint, scenario.cnpgClusterName+"-pooler-ro."+scenario.namespace+".svc.cluster.local"))
}

func seedMissingClusterScenario(ctx context.Context, namespace, resourceName string, finalizers ...string) types.NamespacedName {
	createPostgresDatabaseResource(ctx, namespace, resourceName, "absent-cluster", []platformv1alpha1.DatabaseDefinition{{Name: dbAppdb}}, finalizers...)
	return types.NamespacedName{Name: resourceName, Namespace: namespace}
}

func seedConflictScenario(ctx context.Context, namespace, resourceName, clusterName string) types.NamespacedName {
	createPostgresDatabaseResource(ctx, namespace, resourceName, clusterName, []platformv1alpha1.DatabaseDefinition{{Name: dbAppdb}}, postgresDatabaseFinalizer)
	postgresCluster := createPostgresClusterResource(ctx, namespace, clusterName)
	cnpgClusterName := clusterName + "-cnpg"
	markPostgresClusterReady(ctx, postgresCluster, cnpgClusterName, namespace, false)
	cnpgCluster := createCNPGClusterResource(ctx, namespace, cnpgClusterName)
	markCNPGClusterReady(ctx, cnpgCluster, nil, "tenant-rw", "tenant-ro")
	return types.NamespacedName{Name: resourceName, Namespace: namespace}
}

func seedOwnedDatabaseArtifacts(ctx context.Context, namespace, resourceName, clusterName string, postgresDB *platformv1alpha1.PostgresDatabase, dbNames ...string) {
	ownerReferences := ownedByPostgresDatabase(postgresDB)
	for _, dbName := range dbNames {
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:            adminSecretNameForTest(resourceName, dbName),
				Namespace:       namespace,
				OwnerReferences: ownerReferences,
			},
			Data: map[string][]byte{
				"username": []byte(adminRoleNameForTest(dbName)),
				"password": []byte("test-password"),
			},
		})).To(Succeed())

		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:            rwSecretNameForTest(resourceName, dbName),
				Namespace:       namespace,
				OwnerReferences: ownerReferences,
			},
			Data: map[string][]byte{
				"username": []byte(rwRoleNameForTest(dbName)),
				"password": []byte("test-password"),
			},
		})).To(Succeed())

		Expect(k8sClient.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            configMapNameForTest(resourceName, dbName),
				Namespace:       namespace,
				OwnerReferences: ownerReferences,
			},
		})).To(Succeed())

		Expect(k8sClient.Create(ctx, &cnpgv1.Database{
			ObjectMeta: metav1.ObjectMeta{
				Name:            cnpgDatabaseNameForTest(resourceName, dbName),
				Namespace:       namespace,
				OwnerReferences: ownerReferences,
			},
			Spec: cnpgv1.DatabaseSpec{
				ClusterRef: corev1.LocalObjectReference{Name: clusterName},
				Name:       dbName,
				Owner:      adminRoleNameForTest(dbName),
			},
		})).To(Succeed())
	}
}

func expectPublishedRoleExists(postgresDB *platformv1alpha1.PostgresDatabase, roleName string, exists bool) {
	rolesByName := make(map[string]platformv1alpha1.DatabaseRoleInfo)
	for _, db := range postgresDB.Status.Databases {
		for _, role := range db.Roles {
			rolesByName[role.Name] = role
		}
	}
	Expect(rolesByName).To(HaveKey(roleName))
	Expect(rolesByName[roleName].Exists).To(Equal(exists), "role %s: expected Exists=%v", roleName, exists)
}

func expectRetainedArtifact(ctx context.Context, name, namespace, resourceName string, obj client.Object) {
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, obj)).To(Succeed())
	Expect(obj.GetAnnotations()).To(HaveKeyWithValue(retainedFromAnnotation, resourceName))
	Expect(obj.GetOwnerReferences()).To(BeEmpty())
}

func expectDeletedArtifact(ctx context.Context, name, namespace string, obj client.Object) {
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, obj)
	Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected %s to be deleted", name)
}

func expectStatusPhase(current *platformv1alpha1.PostgresDatabase, expectedPhase string) {
	Expect(current.Status.Phase).NotTo(BeNil())
	Expect(*current.Status.Phase).To(Equal(expectedPhase))
}

func expectStatusCondition(current *platformv1alpha1.PostgresDatabase, conditionType string, expectedStatus metav1.ConditionStatus, expectedReason string) {
	condition := meta.FindStatusCondition(current.Status.Conditions, conditionType)
	Expect(condition).NotTo(BeNil(), "missing status condition %s", conditionType)
	Expect(condition.Status).To(Equal(expectedStatus), "unexpected status for %s", conditionType)
	Expect(condition.Reason).To(Equal(expectedReason), "unexpected reason for %s", conditionType)
}

func expectReadyStatus(current *platformv1alpha1.PostgresDatabase, generation int64, expectedDatabase platformv1alpha1.DatabaseInfo) {
	expectStatusPhase(current, phaseReady)
	Expect(current.Status.Databases).To(HaveLen(1))
	Expect(current.Status.Databases[0].Name).To(Equal(expectedDatabase.Name))
	Expect(current.Status.Databases[0].Ready).To(Equal(expectedDatabase.Ready))
	Expect(current.Status.Databases[0].AdminUserSecretRef).NotTo(BeNil())
	Expect(current.Status.Databases[0].RWUserSecretRef).NotTo(BeNil())
	Expect(current.Status.Databases[0].ConfigMapRef).NotTo(BeNil())
	Expect(current.Status.ObservedGeneration).NotTo(BeNil())
	Expect(*current.Status.ObservedGeneration).To(Equal(generation))
}

func reconcilePostgresDatabaseToReady(ctx context.Context, scenario readyClusterScenario, poolerEnabled bool) *platformv1alpha1.PostgresDatabase {
	seedReadyClusterScenario(ctx, scenario, poolerEnabled)

	result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
	expectEmptyReconcileResult(result, err)

	current := expectFinalizerAdded(ctx, scenario.requestName)
	seedExistingDatabaseStatus(ctx, current, scenario.dbName)

	result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
	expectReconcileResult(result, err, 15*time.Second)
	expectProvisionedArtifacts(ctx, scenario, current)
	expectManagedRolesPatched(ctx, scenario)

	result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
	expectReconcileResult(result, err, 15*time.Second)
	cnpgDatabase := expectCNPGDatabaseCreated(ctx, scenario, current)
	markCNPGDatabaseApplied(ctx, cnpgDatabase)

	result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
	expectEmptyReconcileResult(result, err)

	current = fetchPostgresDatabase(ctx, scenario.requestName)
	expectReadyStatus(current, current.Generation, platformv1alpha1.DatabaseInfo{Name: scenario.dbName, Ready: true})
	return current
}

var _ = Describe("PostgresDatabase Controller", Label("postgres"), func() {
	var (
		ctx       context.Context
		namespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = fmt.Sprintf("postgresdatabase-%d", time.Now().UnixNano())
		Expect(k8sClient.Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		})).To(Succeed())
	})

	AfterEach(func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
	})

	When("the referenced PostgresCluster is missing", func() {
		Context("on the first reconcile", func() {
			It("adds the finalizer", func() {
				requestName := seedMissingClusterScenario(ctx, namespace, "missing-cluster")

				result, err := reconcilePostgresDatabase(ctx, requestName)
				expectEmptyReconcileResult(result, err)

				current := fetchPostgresDatabase(ctx, requestName)
				Expect(current.Finalizers).To(ContainElement(postgresDatabaseFinalizer))
			})
		})

		Context("after the finalizer is already present", func() {
			It("reports ClusterNotFound and requeues", func() {
				requestName := seedMissingClusterScenario(ctx, namespace, "missing-cluster-with-finalizer", postgresDatabaseFinalizer)

				result, err := reconcilePostgresDatabase(ctx, requestName)
				expectReconcileResult(result, err, 30*time.Second)

				current := fetchPostgresDatabase(ctx, requestName)
				expectStatusPhase(current, phasePending)
				expectStatusCondition(current, condClusterReady, metav1.ConditionFalse, reasonClusterNotFound)
				clusterReady := meta.FindStatusCondition(current.Status.Conditions, condClusterReady)
				Expect(clusterReady.ObservedGeneration).To(Equal(current.Generation))
			})
		})
	})

	When("the referenced PostgresCluster is ready", func() {
		Context("and live grants are not invoked", func() {
			It("reconciles secrets, configmaps, roles, and CNPG databases", func() {
				scenario := newReadyClusterScenario(namespace, "ready-cluster", "tenant-cluster", "tenant-cnpg", dbAppdb)
				seedReadyClusterScenario(ctx, scenario, false)

				result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
				expectEmptyReconcileResult(result, err)

				current := expectFinalizerAdded(ctx, scenario.requestName)
				seedExistingDatabaseStatus(ctx, current, scenario.dbName)

				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectReconcileResult(result, err, 15*time.Second)
				expectProvisionedArtifacts(ctx, scenario, current)
				expectManagedRolesPatched(ctx, scenario)

				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectReconcileResult(result, err, 15*time.Second)
				cnpgDatabase := expectCNPGDatabaseCreated(ctx, scenario, current)
				markCNPGDatabaseApplied(ctx, cnpgDatabase)

				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectEmptyReconcileResult(result, err)

				current = fetchPostgresDatabase(ctx, scenario.requestName)
				expectReadyStatus(current, current.Generation, platformv1alpha1.DatabaseInfo{Name: scenario.dbName, Ready: true})
				expectStatusCondition(current, condClusterReady, metav1.ConditionTrue, reasonClusterAvailable)
				expectStatusCondition(current, condSecretsReady, metav1.ConditionTrue, reasonSecretsCreated)
				expectStatusCondition(current, condConfigMapsReady, metav1.ConditionTrue, reasonConfigMapsCreated)
				expectStatusCondition(current, condRolesReady, metav1.ConditionTrue, reasonRolesAvailable)
				expectStatusCondition(current, condDatabasesReady, metav1.ConditionTrue, reasonDatabasesAvailable)
				expectStatusCondition(current, condPrivilegesReady, metav1.ConditionTrue, reasonPrivilegesGranted)
			})

			It("propagates overridden role names through reconciliation", func() {
				scenario := newReadyClusterScenario(namespace, "custom-role-names", "custom-role-cluster", "custom-role-cnpg", dbAppdb)
				database := platformv1alpha1.DatabaseDefinition{
					Name:          scenario.dbName,
					AdminRoleName: "tenant_owner",
					RWRoleName:    "tenant_rw",
				}
				seedReadyClusterScenarioWithDatabase(ctx, scenario, false, database, 2)

				result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
				expectEmptyReconcileResult(result, err)
				current := expectFinalizerAdded(ctx, scenario.requestName)
				seedExistingDatabaseStatus(ctx, current, scenario.dbName)

				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectReconcileResult(result, err, 15*time.Second)
				current = fetchPostgresDatabase(ctx, scenario.requestName)
				expectProvisionedArtifacts(ctx, scenario, current)
				expectManagedRolesPatched(ctx, scenario)
				Expect(publishedRoleNames(current)).To(ConsistOf("tenant_owner", "tenant_rw"))

				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectReconcileResult(result, err, 15*time.Second)
				cnpgDatabase := expectCNPGDatabaseCreated(ctx, scenario, current)
				markCNPGDatabaseApplied(ctx, cnpgDatabase)

				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectEmptyReconcileResult(result, err)
				current = fetchPostgresDatabase(ctx, scenario.requestName)
				Expect(current.Status.Databases[0].Roles).To(ConsistOf(
					platformv1alpha1.DatabaseRoleInfo{Name: "tenant_owner", SecretRef: &corev1.LocalObjectReference{Name: adminSecretNameForTest(scenario.resourceName, scenario.dbName)}, Exists: true},
					platformv1alpha1.DatabaseRoleInfo{Name: "tenant_rw", SecretRef: &corev1.LocalObjectReference{Name: rwSecretNameForTest(scenario.resourceName, scenario.dbName)}, Exists: true},
				))
			})

			It("gates readiness on the current custom-metrics acknowledgement", func() {
				const (
					sourceName = "database-handshake-metrics"
					sourceKey  = "queries.yaml"
				)
				scenario := newReadyClusterScenario(namespace, "metrics-handshake", "metrics-cluster", "metrics-cnpg", dbAppdb)
				Expect(k8sClient.Create(ctx, &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: sourceName, Namespace: namespace},
					Data: map[string]string{sourceKey: `database_handshake_metric:
  type: gauge
  help: "Database handshake metric"
  query: "SELECT 1 AS value"
  value: value
`},
				})).To(Succeed())
				createPostgresDatabaseResource(ctx, scenario.namespace, scenario.resourceName, scenario.clusterName, []platformv1alpha1.DatabaseDefinition{{
					Name: scenario.dbName,
					Monitoring: &platformv1alpha1.DatabaseMonitoring{
						CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{{
							LocalObjectReference: corev1.LocalObjectReference{Name: sourceName},
							Key:                  sourceKey,
						}},
					},
				}})
				postgresCluster := createPostgresClusterResource(ctx, scenario.namespace, scenario.clusterName)
				markPostgresClusterReady(ctx, postgresCluster, scenario.cnpgClusterName, scenario.namespace, false)
				cnpgCluster := createCNPGClusterResource(ctx, scenario.namespace, scenario.cnpgClusterName)
				markCNPGClusterReady(ctx, cnpgCluster,
					[]string{adminRoleNameForTest(scenario.dbName), rwRoleNameForTest(scenario.dbName)},
					"metrics-rw", "metrics-ro")

				result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
				expectEmptyReconcileResult(result, err)
				current := expectFinalizerAdded(ctx, scenario.requestName)

				By("publishing the contribution before provisioning")
				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectReconcileResult(result, err, 15*time.Second)
				current = fetchPostgresDatabase(ctx, scenario.requestName)
				Expect(current.Status.CustomMetricsPublication).NotTo(BeNil())
				Expect(current.Status.CustomMetricsPublication.ObservedGeneration).To(Equal(current.Generation))
				Expect(current.Status.CustomMetricsPublication.Contributions).To(HaveLen(1))
				Expect(current.Status.CustomMetricsPublication.Contributions[0].Exists).To(BeTrue())
				revision := current.Status.CustomMetricsPublication.Contributions[0].Revision
				Expect(revision).NotTo(BeEmpty())

				Eventually(func(g Gomega) {
					result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(result.RequeueAfter).To(Equal(15 * time.Second))
					g.Expect(publishedRoleNames(fetchPostgresDatabase(ctx, scenario.requestName))).To(
						ConsistOf(adminRoleNameForTest(scenario.dbName), rwRoleNameForTest(scenario.dbName)),
					)
				}, "5s", "100ms").Should(Succeed())
				current = fetchPostgresDatabase(ctx, scenario.requestName)
				simulateClusterRoleOwnership(ctx, scenario.clusterName, scenario.namespace, current,
					adminRoleNameForTest(scenario.dbName), rwRoleNameForTest(scenario.dbName))

				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectReconcileResult(result, err, 15*time.Second)
				cnpgDatabase := expectCNPGDatabaseCreated(ctx, scenario, current)
				current = fetchPostgresDatabase(ctx, scenario.requestName)
				Expect(current.Status.Databases).To(HaveLen(1))
				Expect(current.Status.CustomMetricsPublication.Contributions[0].Revision).To(Equal(revision))
				current.Status.Databases[0].Ready = true
				current.Status.Databases[0].DatabaseRef = &corev1.LocalObjectReference{Name: cnpgDatabaseNameForTest(scenario.resourceName, scenario.dbName)}
				Expect(k8sClient.Status().Update(ctx, current)).To(Succeed())
				markCNPGDatabaseApplied(ctx, cnpgDatabase)

				By("waiting for acknowledgement before final readiness")
				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectReconcileResult(result, err, 15*time.Second)
				current = fetchPostgresDatabase(ctx, scenario.requestName)
				expectStatusPhase(current, phaseProvisioning)
				expectStatusCondition(current, condCustomMetrics, metav1.ConditionUnknown, "CustomMetricsPending")

				setAcknowledgement := func(desiredRevision, appliedRevision string, status metav1.ConditionStatus, reason string) {
					GinkgoHelper()
					cluster := &platformv1alpha1.PostgresCluster{}
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: scenario.clusterName, Namespace: namespace}, cluster)).To(Succeed())
					cluster.Status.CustomMetricsStatus = &platformv1alpha1.CustomMetricsStatus{
						DatabaseContributions: []platformv1alpha1.DatabaseCustomMetricsStatus{{
							PostgresDatabaseName: current.Name,
							PostgresDatabaseUID:  string(current.UID),
							DatabaseName:         scenario.dbName,
							DesiredRevision:      desiredRevision,
							AppliedRevision:      appliedRevision,
							Status:               status,
							Reason:               reason,
							Message:              reason,
						}},
					}
					Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())
				}

				By("rejecting a stale acknowledgement")
				setAcknowledgement("stale-revision", "stale-revision", metav1.ConditionTrue, "CustomMetricsReady")
				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectReconcileResult(result, err, 15*time.Second)
				current = fetchPostgresDatabase(ctx, scenario.requestName)
				expectStatusPhase(current, phaseProvisioning)
				expectStatusCondition(current, condCustomMetrics, metav1.ConditionUnknown, "CustomMetricsPending")

				By("remaining provisioning while the provider is configuring the current revision")
				setAcknowledgement(revision, "", metav1.ConditionUnknown, "CustomMetricsConfiguring")
				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectReconcileResult(result, err, 15*time.Second)
				current = fetchPostgresDatabase(ctx, scenario.requestName)
				expectStatusPhase(current, phaseProvisioning)
				expectStatusCondition(current, condCustomMetrics, metav1.ConditionUnknown, "CustomMetricsConfiguring")

				By("exposing a matching negative acknowledgement even when the prior safe revision is still applied")
				setAcknowledgement(revision, revision, metav1.ConditionFalse, "InvalidQueryDefinition")
				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectReconcileResult(result, err, 15*time.Second)
				current = fetchPostgresDatabase(ctx, scenario.requestName)
				expectStatusPhase(current, phaseFailed)
				expectStatusCondition(current, condCustomMetrics, metav1.ConditionFalse, "InvalidQueryDefinition")

				By("becoming ready only after the exact revision is applied")
				setAcknowledgement(revision, revision, metav1.ConditionTrue, "CustomMetricsReady")
				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectEmptyReconcileResult(result, err)
				current = fetchPostgresDatabase(ctx, scenario.requestName)
				expectStatusPhase(current, phaseReady)
				expectStatusCondition(current, condCustomMetrics, metav1.ConditionTrue, "CustomMetricsReady")

				By("publishing and waiting for an explicit disablement tombstone")
				Expect(k8sClient.Get(ctx, scenario.requestName, current)).To(Succeed())
				current.Spec.Databases[0].Monitoring = nil
				Expect(k8sClient.Update(ctx, current)).To(Succeed())
				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectReconcileResult(result, err, 15*time.Second)
				current = fetchPostgresDatabase(ctx, scenario.requestName)
				Expect(current.Status.CustomMetricsPublication).NotTo(BeNil())
				Expect(current.Status.CustomMetricsPublication.Contributions).To(HaveLen(1))
				Expect(current.Status.CustomMetricsPublication.Contributions[0].Exists).To(BeFalse())
				disabledRevision := current.Status.CustomMetricsPublication.Contributions[0].Revision
				Expect(disabledRevision).NotTo(Equal(revision))

				expectStatusPhase(current, phaseProvisioning)
				expectStatusCondition(current, condCustomMetrics, metav1.ConditionUnknown, "CustomMetricsPending")

				setAcknowledgement(disabledRevision, disabledRevision, metav1.ConditionTrue, "CustomMetricsDisabled")
				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectEmptyReconcileResult(result, err)
				current = fetchPostgresDatabase(ctx, scenario.requestName)
				expectStatusPhase(current, phaseReady)
				expectStatusCondition(current, condCustomMetrics, metav1.ConditionTrue, "CustomMetricsDisabled")
			})
		})

		Context("and external superuser secret is reconciled", func() {
			It("reconciles external secrets, configmaps, roles, and CNPG databases", func() {
				scenario := newReadyClusterScenario(namespace, "ready-cluster", "tenant-cluster", "tenant-cnpg", dbAppdb)

				createPostgresDatabaseResource(ctx, scenario.namespace,
					scenario.resourceName, scenario.clusterName,
					[]platformv1alpha1.DatabaseDefinition{{
						Name: scenario.dbName,
						PasswordConfig: &platformv1alpha1.PasswordConfig{
							ExternalAdminSecretRef: corev1.LocalObjectReference{
								Name: "external-admin-secret"},
							ExternalRWSecretRef: corev1.LocalObjectReference{
								Name: "external-rw-secret"}}}})

				Expect(k8sClient.Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "external-admin-secret",
						Namespace: scenario.namespace,
						// The secret owner sets cnpg.io/reload; the operator only validates it.
						Labels: map[string]string{"cnpg.io/reload": "true"},
					},
					Data: map[string][]byte{
						"username": []byte(adminRoleNameForTest(scenario.dbName)),
						"password": []byte("username"),
					},
				})).To(Succeed())
				Expect(k8sClient.Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "external-rw-secret",
						Namespace: scenario.namespace,
						// The secret owner sets cnpg.io/reload; the operator only validates it.
						Labels: map[string]string{"cnpg.io/reload": "true"},
					},
					Data: map[string][]byte{
						"username": []byte(rwRoleNameForTest(scenario.dbName)),
						"password": []byte("username"),
					},
				})).To(Succeed())
				postgresCluster := createPostgresClusterResource(ctx, scenario.namespace, scenario.clusterName)
				markPostgresClusterReady(ctx, postgresCluster, scenario.cnpgClusterName, scenario.namespace, false)
				cnpgCluster := createCNPGClusterResource(ctx, scenario.namespace, scenario.cnpgClusterName)
				markCNPGClusterReady(ctx, cnpgCluster, []string{adminRoleNameForTest(scenario.dbName), rwRoleNameForTest(scenario.dbName)}, "tenant-rw", "tenant-ro")
				result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
				expectEmptyReconcileResult(result, err)

				current := expectFinalizerAdded(ctx, scenario.requestName)
				seedExistingDatabaseStatus(ctx, current, scenario.dbName)

				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectReconcileResult(result, err, 15*time.Second)

				configMap := &corev1.ConfigMap{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configMapNameForTest(scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, configMap)).To(Succeed())
				Expect(configMap.Data).To(HaveKeyWithValue(dbcore.ConfigMapKeyDatabaseName, scenario.dbName))
				Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyDefaultClusterPort, pgconninfo.DefaultPort))
				Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyClusterRWEndpoint, "tenant-rw."+scenario.namespace+".svc.cluster.local"))
				Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyClusterROEndpoint, "tenant-ro."+scenario.namespace+".svc.cluster.local"))
				Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyClusterREndpoint, scenario.cnpgClusterName+"-r."+scenario.namespace+".svc.cluster.local"))
				Expect(configMap.Data).To(HaveKeyWithValue(dbcore.ConfigMapKeyAdminUser, adminRoleNameForTest(scenario.dbName)))
				Expect(configMap.Data).To(HaveKeyWithValue(dbcore.ConfigMapKeyRWUser, rwRoleNameForTest(scenario.dbName)))
				Expect(metav1.IsControlledBy(configMap, current)).To(BeTrue())

				expectManagedRolesPatched(ctx, scenario)

				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectReconcileResult(result, err, 15*time.Second)
				cnpgDatabase := expectCNPGDatabaseCreated(ctx, scenario, current)
				markCNPGDatabaseApplied(ctx, cnpgDatabase)

				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectEmptyReconcileResult(result, err)

				current = fetchPostgresDatabase(ctx, scenario.requestName)
				expectReadyStatus(current, current.Generation, platformv1alpha1.DatabaseInfo{Name: scenario.dbName, Ready: true})
				expectStatusCondition(current, condClusterReady, metav1.ConditionTrue, reasonClusterAvailable)
				expectStatusCondition(current, condSecretsReady, metav1.ConditionTrue, reasonSecretsCreated)

				for _, dbinfo := range current.Status.Databases {
					Expect(dbinfo.AdminUserSecretRef).NotTo(BeNil())
					Expect(dbinfo.RWUserSecretRef).NotTo(BeNil())
					Expect(dbinfo.AdminUserSecretRef.Name).To(Equal("external-admin-secret"))
					Expect(dbinfo.RWUserSecretRef.Name).To(Equal("external-rw-secret"))
				}
				expectStatusCondition(current, condConfigMapsReady, metav1.ConditionTrue, reasonConfigMapsCreated)
				expectStatusCondition(current, condRolesReady, metav1.ConditionTrue, reasonRolesAvailable)
				expectStatusCondition(current, condDatabasesReady, metav1.ConditionTrue, reasonDatabasesAvailable)
				expectStatusCondition(current, condPrivilegesReady, metav1.ConditionTrue, reasonPrivilegesGranted)
			})

			It("catches missing external secrets and emits an event", func() {
				scenario := newReadyClusterScenario(namespace, "ready-cluster", "tenant-cluster", "tenant-cnpg", dbAppdb)

				createPostgresDatabaseResource(ctx, scenario.namespace,
					scenario.resourceName, scenario.clusterName,
					[]platformv1alpha1.DatabaseDefinition{{
						Name: scenario.dbName,
						PasswordConfig: &platformv1alpha1.PasswordConfig{
							ExternalAdminSecretRef: corev1.LocalObjectReference{
								Name: "external-admin-secret12"},
							ExternalRWSecretRef: corev1.LocalObjectReference{
								Name: "external-rw-secret12"}}}})

				postgresCluster := createPostgresClusterResource(ctx, scenario.namespace, scenario.clusterName)
				markPostgresClusterReady(ctx, postgresCluster, scenario.cnpgClusterName, scenario.namespace, false)
				cnpgCluster := createCNPGClusterResource(ctx, scenario.namespace, scenario.cnpgClusterName)
				markCNPGClusterReady(ctx, cnpgCluster, []string{adminRoleNameForTest(scenario.dbName), rwRoleNameForTest(scenario.dbName)}, "tenant-rw", "tenant-ro")

				// Inject a shared recorder so events survive across reconciles.
				recorder := record.NewFakeRecorder(100)
				result, err := reconcilePostgresDatabaseWithRecorder(ctx, scenario.requestName, recorder)
				expectEmptyReconcileResult(result, err)

				current := expectFinalizerAdded(ctx, scenario.requestName)
				seedExistingDatabaseStatus(ctx, current, scenario.dbName)

				result, err = reconcilePostgresDatabaseWithRecorder(ctx, scenario.requestName, recorder)
				Expect(err).NotTo(BeNil())

				current = fetchPostgresDatabase(ctx, scenario.requestName)
				expectStatusCondition(current, condSecretsReady, metav1.ConditionFalse, reasonExternalSecretMissing)
				Expect(current.Status.CustomMetricsPublication).NotTo(BeNil())
				Expect(current.Status.CustomMetricsPublication.ObservedGeneration).To(Equal(current.Generation))
				Expect(current.Status.CustomMetricsPublication.Contributions).To(HaveLen(1))
				Expect(current.Status.CustomMetricsPublication.Contributions[0].DatabaseName).To(Equal(scenario.dbName))
				Expect(current.Status.CustomMetricsPublication.Contributions[0].Exists).To(BeFalse(),
					"non-participation must be published before the unrelated Secret gate fails")

				received := make([]string, 0, 16)
				collectEvents(&received, recorder)
				Expect(containsEvent(received, corev1.EventTypeWarning, dbEventRoleSecretsFailed)).To(
					BeTrue(),
					"Warning %s must be emitted when external secrets are missing; events seen: %v",
					dbEventRoleSecretsFailed, received)
				// The Ready event must not have leaked while we were still
				// failing — guards against an updateStatus-then-emitNormal
				// regression where the ready event fires before the
				// SecretsReady condition is actually True.
				Expect(containsEvent(received, corev1.EventTypeNormal, dbEventPostgresDatabaseReady)).To(
					BeFalse(),
					"PostgresDatabaseReady must not fire while SecretsReady is False; events seen: %v", received)
			})

			It("recovers SecretsReady when missing external secrets are created", func() {
				scenario := newReadyClusterScenario(namespace, "ready-cluster", "tenant-cluster", "tenant-cnpg", dbAppdb)

				const (
					adminSecretName = "recovery-admin-secret"
					rwSecretName    = "recovery-rw-secret"
				)
				createPostgresDatabaseResource(ctx, scenario.namespace,
					scenario.resourceName, scenario.clusterName,
					[]platformv1alpha1.DatabaseDefinition{{
						Name: scenario.dbName,
						PasswordConfig: &platformv1alpha1.PasswordConfig{
							ExternalAdminSecretRef: corev1.LocalObjectReference{Name: adminSecretName},
							ExternalRWSecretRef:    corev1.LocalObjectReference{Name: rwSecretName},
						}}})

				postgresCluster := createPostgresClusterResource(ctx, scenario.namespace, scenario.clusterName)
				markPostgresClusterReady(ctx, postgresCluster, scenario.cnpgClusterName, scenario.namespace, false)
				cnpgCluster := createCNPGClusterResource(ctx, scenario.namespace, scenario.cnpgClusterName)
				markCNPGClusterReady(ctx, cnpgCluster, []string{adminRoleNameForTest(scenario.dbName), rwRoleNameForTest(scenario.dbName)}, "tenant-rw", "tenant-ro")

				recorder := record.NewFakeRecorder(100)

				// Pass 1: finalizer.
				result, err := reconcilePostgresDatabaseWithRecorder(ctx, scenario.requestName, recorder)
				expectEmptyReconcileResult(result, err)
				current := expectFinalizerAdded(ctx, scenario.requestName)
				seedExistingDatabaseStatus(ctx, current, scenario.dbName)

				// Pass 2: secrets missing — condition flips False, Warning emitted.
				_, err = reconcilePostgresDatabaseWithRecorder(ctx, scenario.requestName, recorder)
				Expect(err).To(HaveOccurred())
				current = fetchPostgresDatabase(ctx, scenario.requestName)
				expectStatusCondition(current, condSecretsReady, metav1.ConditionFalse, reasonExternalSecretMissing)

				received := make([]string, 0, 16)
				collectEvents(&received, recorder)
				Expect(containsEvent(received, corev1.EventTypeWarning, dbEventRoleSecretsFailed)).To(BeTrue(),
					"baseline Warning %s missing; events seen: %v", dbEventRoleSecretsFailed, received)

				// Now create both external Secrets in the cluster — mirrors an
				// ExternalSecret CR materializing after admission.
				Expect(k8sClient.Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      adminSecretName,
						Namespace: scenario.namespace,
						Labels:    map[string]string{"cnpg.io/reload": "true"},
					},
					Data: map[string][]byte{
						"username": []byte(adminRoleNameForTest(scenario.dbName)),
						"password": []byte("admin-pw"),
					},
				})).To(Succeed())
				Expect(k8sClient.Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rwSecretName,
						Namespace: scenario.namespace,
						Labels:    map[string]string{"cnpg.io/reload": "true"},
					},
					Data: map[string][]byte{
						"username": []byte(rwRoleNameForTest(scenario.dbName)),
						"password": []byte("rw-pw"),
					},
				})).To(Succeed())

				result, err = reconcilePostgresDatabaseWithRecorder(ctx, scenario.requestName, recorder)
				expectReconcileResult(result, err, 15*time.Second)

				current = fetchPostgresDatabase(ctx, scenario.requestName)
				expectStatusCondition(current, condSecretsReady, metav1.ConditionTrue, reasonSecretsCreated)

				for _, name := range []string{adminSecretName, rwSecretName} {
					got := &corev1.Secret{}
					Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: scenario.namespace}, got)).To(Succeed())
					Expect(got.Labels).To(HaveKeyWithValue("cnpg.io/reload", "true"),
						"external Secret %s must carry cnpg.io/reload=true after recovery", name)
				}

				// Drain the recorder once more — no fresh Warning must have
				// fired during the recovery pass.
				received = received[:0]
				collectEvents(&received, recorder)
				Expect(containsEvent(received, corev1.EventTypeWarning, dbEventRoleSecretsFailed)).To(
					BeFalse(),
					"%s must not be re-emitted once external secrets are present; events seen: %v",
					dbEventRoleSecretsFailed, received)
			})

			It("flips SecretsReady back to False when an external secret is deleted after ready", func() {
				scenario := newReadyClusterScenario(namespace, "ready-cluster", "tenant-cluster", "tenant-cnpg", dbAppdb)

				const (
					adminSecretName = "drift-admin-secret"
					rwSecretName    = "drift-rw-secret"
				)
				createPostgresDatabaseResource(ctx, scenario.namespace,
					scenario.resourceName, scenario.clusterName,
					[]platformv1alpha1.DatabaseDefinition{{
						Name: scenario.dbName,
						PasswordConfig: &platformv1alpha1.PasswordConfig{
							ExternalAdminSecretRef: corev1.LocalObjectReference{Name: adminSecretName},
							ExternalRWSecretRef:    corev1.LocalObjectReference{Name: rwSecretName},
						}}})

				Expect(k8sClient.Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      adminSecretName,
						Namespace: scenario.namespace,
						Labels:    map[string]string{"cnpg.io/reload": "true"},
					},
					Data: map[string][]byte{
						"username": []byte(adminRoleNameForTest(scenario.dbName)),
						"password": []byte("admin-pw"),
					},
				})).To(Succeed())
				Expect(k8sClient.Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      rwSecretName,
						Namespace: scenario.namespace,
						Labels:    map[string]string{"cnpg.io/reload": "true"},
					},
					Data: map[string][]byte{
						"username": []byte(rwRoleNameForTest(scenario.dbName)),
						"password": []byte("rw-pw"),
					},
				})).To(Succeed())

				postgresCluster := createPostgresClusterResource(ctx, scenario.namespace, scenario.clusterName)
				markPostgresClusterReady(ctx, postgresCluster, scenario.cnpgClusterName, scenario.namespace, false)
				cnpgCluster := createCNPGClusterResource(ctx, scenario.namespace, scenario.cnpgClusterName)
				markCNPGClusterReady(ctx, cnpgCluster, []string{adminRoleNameForTest(scenario.dbName), rwRoleNameForTest(scenario.dbName)}, "tenant-rw", "tenant-ro")

				recorder := record.NewFakeRecorder(100)

				// Drive the standard happy-path bring-up so the resource
				// reaches SecretsReady=True before we induce drift.
				result, err := reconcilePostgresDatabaseWithRecorder(ctx, scenario.requestName, recorder)
				expectEmptyReconcileResult(result, err)
				current := expectFinalizerAdded(ctx, scenario.requestName)
				seedExistingDatabaseStatus(ctx, current, scenario.dbName)

				result, err = reconcilePostgresDatabaseWithRecorder(ctx, scenario.requestName, recorder)
				expectReconcileResult(result, err, 15*time.Second)
				current = fetchPostgresDatabase(ctx, scenario.requestName)
				expectStatusCondition(current, condSecretsReady, metav1.ConditionTrue, reasonSecretsCreated)

				// Baseline: no Warning event has fired yet on the
				// SecretsReady path. (We tolerate Warnings from later phases
				// — the assertion is reason-scoped.)
				received := make([]string, 0, 16)
				collectEvents(&received, recorder)
				Expect(containsEvent(received, corev1.EventTypeWarning, dbEventRoleSecretsFailed)).To(BeFalse(),
					"%s must not be emitted while SecretsReady is True; events seen: %v",
					dbEventRoleSecretsFailed, received)

				// Induce drift — delete the admin Secret out from under us.
				Expect(k8sClient.Delete(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: adminSecretName, Namespace: scenario.namespace},
				})).To(Succeed())

				_, err = reconcilePostgresDatabaseWithRecorder(ctx, scenario.requestName, recorder)
				Expect(err).To(HaveOccurred())

				current = fetchPostgresDatabase(ctx, scenario.requestName)
				expectStatusCondition(current, condSecretsReady, metav1.ConditionFalse, reasonExternalSecretMissing)

				received = received[:0]
				collectEvents(&received, recorder)
				Expect(containsEvent(received, corev1.EventTypeWarning, dbEventRoleSecretsFailed)).To(BeTrue(),
					"deleting an external Secret must re-emit %s; events seen: %v",
					dbEventRoleSecretsFailed, received)
			})

			It("reconciles external secrets, cms etc. idempotently", func() {
				scenario := newReadyClusterScenario(namespace, "ready-cluster", "tenant-cluster", "tenant-cnpg", dbAppdb)

				createPostgresDatabaseResource(ctx, scenario.namespace,
					scenario.resourceName, scenario.clusterName,
					[]platformv1alpha1.DatabaseDefinition{{
						Name: scenario.dbName,
						PasswordConfig: &platformv1alpha1.PasswordConfig{
							ExternalAdminSecretRef: corev1.LocalObjectReference{
								Name: "external-admin-secret"},
							ExternalRWSecretRef: corev1.LocalObjectReference{
								Name: "external-rw-secret"}}}})

				Expect(k8sClient.Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "external-admin-secret",
						Namespace: scenario.namespace,
						// The secret owner sets cnpg.io/reload; the operator only validates it.
						Labels: map[string]string{"cnpg.io/reload": "true"},
					},
					Data: map[string][]byte{
						"username": []byte(adminRoleNameForTest(scenario.dbName)),
						"password": []byte("username"),
					},
				})).To(Succeed())
				Expect(k8sClient.Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "external-rw-secret",
						Namespace: scenario.namespace,
						// The secret owner sets cnpg.io/reload; the operator only validates it.
						Labels: map[string]string{"cnpg.io/reload": "true"},
					},
					Data: map[string][]byte{
						"username": []byte(rwRoleNameForTest(scenario.dbName)),
						"password": []byte("username"),
					},
				})).To(Succeed())
				postgresCluster := createPostgresClusterResource(ctx, scenario.namespace, scenario.clusterName)
				markPostgresClusterReady(ctx, postgresCluster, scenario.cnpgClusterName, scenario.namespace, false)
				cnpgCluster := createCNPGClusterResource(ctx, scenario.namespace, scenario.cnpgClusterName)
				markCNPGClusterReady(ctx, cnpgCluster, []string{adminRoleNameForTest(scenario.dbName), rwRoleNameForTest(scenario.dbName)}, "tenant-rw", "tenant-ro")
				result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
				expectEmptyReconcileResult(result, err)

				current := expectFinalizerAdded(ctx, scenario.requestName)
				seedExistingDatabaseStatus(ctx, current, scenario.dbName)

				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectReconcileResult(result, err, 15*time.Second)

				configMap := &corev1.ConfigMap{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configMapNameForTest(scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, configMap)).To(Succeed())
				Expect(configMap.Data).To(HaveKeyWithValue(dbcore.ConfigMapKeyDatabaseName, scenario.dbName))
				Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyDefaultClusterPort, pgconninfo.DefaultPort))
				Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyClusterRWEndpoint, "tenant-rw."+scenario.namespace+".svc.cluster.local"))
				Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyClusterROEndpoint, "tenant-ro."+scenario.namespace+".svc.cluster.local"))
				Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyClusterREndpoint, scenario.cnpgClusterName+"-r."+scenario.namespace+".svc.cluster.local"))
				Expect(configMap.Data).To(HaveKeyWithValue(dbcore.ConfigMapKeyAdminUser, adminRoleNameForTest(scenario.dbName)))
				Expect(configMap.Data).To(HaveKeyWithValue(dbcore.ConfigMapKeyRWUser, rwRoleNameForTest(scenario.dbName)))
				Expect(metav1.IsControlledBy(configMap, current)).To(BeTrue())

				expectManagedRolesPatched(ctx, scenario)

				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectReconcileResult(result, err, 15*time.Second)
				cnpgDatabase := expectCNPGDatabaseCreated(ctx, scenario, current)
				markCNPGDatabaseApplied(ctx, cnpgDatabase)

				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectEmptyReconcileResult(result, err)

				current = fetchPostgresDatabase(ctx, scenario.requestName)
				expectReadyStatus(current, current.Generation, platformv1alpha1.DatabaseInfo{Name: scenario.dbName, Ready: true})
				expectStatusCondition(current, condClusterReady, metav1.ConditionTrue, reasonClusterAvailable)
				expectStatusCondition(current, condSecretsReady, metav1.ConditionTrue, reasonSecretsCreated)
				expectStatusCondition(current, condConfigMapsReady, metav1.ConditionTrue, reasonConfigMapsCreated)
				expectStatusCondition(current, condRolesReady, metav1.ConditionTrue, reasonRolesAvailable)
				expectStatusCondition(current, condDatabasesReady, metav1.ConditionTrue, reasonDatabasesAvailable)
				expectStatusCondition(current, condPrivilegesReady, metav1.ConditionTrue, reasonPrivilegesGranted)
			})
		})

		Context("and connection pooling is enabled", func() {
			It("adds pooler endpoints to the generated ConfigMap", func() {
				scenario := newReadyClusterScenario(namespace, "pooler-cluster", "pooler-postgres", "pooler-cnpg", dbAppdb)
				seedReadyClusterScenario(ctx, scenario, true)

				result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
				expectEmptyReconcileResult(result, err)

				current := fetchPostgresDatabase(ctx, scenario.requestName)
				seedExistingDatabaseStatus(ctx, current, scenario.dbName)

				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectReconcileResult(result, err, 15*time.Second)
				expectPoolerConfigMap(ctx, scenario)
			})

			It("publishes empty pooler-ro-host when the cluster runs with a single instance", func() {
				scenario := newReadyClusterScenario(namespace, "pooler-single", "pooler-single-postgres", "pooler-single-cnpg", dbAppdb)
				seedReadyClusterScenarioWithInstances(ctx, scenario, true, 1)

				result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
				expectEmptyReconcileResult(result, err)

				current := fetchPostgresDatabase(ctx, scenario.requestName)
				seedExistingDatabaseStatus(ctx, current, scenario.dbName)

				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectReconcileResult(result, err, 15*time.Second)

				configMap := &corev1.ConfigMap{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configMapNameForTest(scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, configMap)).To(Succeed())
				Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyPoolerRWEndpoint, scenario.cnpgClusterName+"-pooler-rw."+scenario.namespace+".svc.cluster.local"))
				Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyPoolerROEndpoint, ""))
				Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyClusterROEndpoint, ""))
			})

			It("repopulates ro-host and pooler-ro-host when the cluster scales 1->2", func() {
				scenario := newReadyClusterScenario(namespace, "pooler-scaleup", "pooler-scaleup-postgres", "pooler-scaleup-cnpg", dbAppdb)
				seedReadyClusterScenarioWithInstances(ctx, scenario, true, 1)

				result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
				expectEmptyReconcileResult(result, err)

				current := fetchPostgresDatabase(ctx, scenario.requestName)
				seedExistingDatabaseStatus(ctx, current, scenario.dbName)

				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectReconcileResult(result, err, 15*time.Second)

				cmKey := types.NamespacedName{Name: configMapNameForTest(scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}
				configMap := &corev1.ConfigMap{}
				Expect(k8sClient.Get(ctx, cmKey, configMap)).To(Succeed())
				Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyClusterROEndpoint, ""))
				Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyPoolerROEndpoint, ""))

				postgresCluster := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: scenario.clusterName, Namespace: scenario.namespace}, postgresCluster)).To(Succeed())
				postgresCluster.Status.ConnectionPoolerStatus.ReadOnlyEnabled = true
				Expect(k8sClient.Status().Update(ctx, postgresCluster)).To(Succeed())

				cnpgCluster := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: scenario.cnpgClusterName, Namespace: scenario.namespace}, cnpgCluster)).To(Succeed())
				cnpgCluster.Status.ReadyInstances = 2
				Expect(k8sClient.Status().Update(ctx, cnpgCluster)).To(Succeed())

				result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
				expectReconcileResult(result, err, 15*time.Second)

				Expect(k8sClient.Get(ctx, cmKey, configMap)).To(Succeed())
				Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyClusterROEndpoint, "tenant-ro."+scenario.namespace+".svc.cluster.local"))
				Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyPoolerROEndpoint, scenario.cnpgClusterName+"-pooler-ro."+scenario.namespace+".svc.cluster.local"))
			})
		})
	})

	When("the referenced PostgresCluster exists but is not ready", func() {
		It("waits for cluster to be provisioned and sets ClusterReady=False with reason ClusterProvisioning", func() {
			scenario := newReadyClusterScenario(namespace, "not-ready-cluster", "not-ready-postgres", "not-ready-cnpg", dbAppdb)
			createPostgresDatabaseResource(ctx, scenario.namespace, scenario.resourceName, scenario.clusterName, []platformv1alpha1.DatabaseDefinition{{Name: scenario.dbName}})
			createPostgresClusterResource(ctx, scenario.namespace, scenario.clusterName)
			// Do NOT call markPostgresClusterReady to leave it in provisioning state

			result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
			expectEmptyReconcileResult(result, err)

			current := expectFinalizerAdded(ctx, scenario.requestName)

			result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
			expectReconcileResult(result, err, 15*time.Second)

			current = fetchPostgresDatabase(ctx, scenario.requestName)
			expectStatusPhase(current, phasePending)
			expectStatusCondition(current, condClusterReady, metav1.ConditionFalse, reasonClusterProvisioning)
		})
	})

	When("owned resource drift occurs after the PostgresDatabase is ready", func() {
		It("repairs configmap content drift", func() {
			scenario := newReadyClusterScenario(namespace, "configmap-drift", "tenant-cluster", "tenant-cnpg", "appdb")
			owner := reconcilePostgresDatabaseToReady(ctx, scenario, false)

			configMap := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-%s-config", scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, configMap)).To(Succeed())
			configMap.Data[pgconninfo.KeyClusterRWEndpoint] = "unexpected.example"
			Expect(k8sClient.Update(ctx, configMap)).To(Succeed())

			result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
			expectEmptyReconcileResult(result, err)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configMap.Name, Namespace: configMap.Namespace}, configMap)).To(Succeed())
			Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyClusterRWEndpoint, "tenant-rw."+scenario.namespace+".svc.cluster.local"))

			current := fetchPostgresDatabase(ctx, scenario.requestName)
			expectReadyStatus(current, current.Generation, platformv1alpha1.DatabaseInfo{Name: scenario.dbName, Ready: true})
			Expect(metav1.IsControlledBy(configMap, owner)).To(BeTrue())
		})

		It("recreates a deleted configmap", func() {
			scenario := newReadyClusterScenario(namespace, "configmap-delete", "tenant-cluster", "tenant-cnpg", "appdb")
			reconcilePostgresDatabaseToReady(ctx, scenario, false)

			configMapName := fmt.Sprintf("%s-%s-config", scenario.resourceName, scenario.dbName)
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: scenario.namespace},
			})).To(Succeed())

			result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
			expectEmptyReconcileResult(result, err)

			configMap := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: scenario.namespace}, configMap)).To(Succeed())
			Expect(configMap.Data).To(HaveKeyWithValue(pgconninfo.KeyClusterRWEndpoint, "tenant-rw."+scenario.namespace+".svc.cluster.local"))
		})

		It("reports a deleted managed user secret distinctly without repeated warnings", func() {
			scenario := newReadyClusterScenario(namespace, "secret-delete", "tenant-cluster", "tenant-cnpg", "appdb")
			reconcilePostgresDatabaseToReady(ctx, scenario, false)
			recorder := record.NewFakeRecorder(100)

			secretName := fmt.Sprintf("%s-%s-admin", scenario.resourceName, scenario.dbName)
			Expect(k8sClient.Delete(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: scenario.namespace},
			})).To(Succeed())

			result, err := reconcilePostgresDatabaseWithRecorder(ctx, scenario.requestName, recorder)
			expectReconcileResult(result, err, 15*time.Second)

			current := fetchPostgresDatabase(ctx, scenario.requestName)
			expectStatusPhase(current, "Provisioning")
			expectStatusCondition(current, "SecretsReady", metav1.ConditionFalse, reasonManagedSecretMissing)

			received := make([]string, 0, 16)
			collectEvents(&received, recorder)
			Expect(containsEvent(received, corev1.EventTypeWarning, dbEventRolesSecretsDrift)).To(BeTrue(),
				"missing managed Secret should emit one drift warning; events seen: %v", received)

			result, err = reconcilePostgresDatabaseWithRecorder(ctx, scenario.requestName, recorder)
			expectReconcileResult(result, err, 15*time.Second)
			received = received[:0]
			collectEvents(&received, recorder)
			Expect(containsEvent(received, corev1.EventTypeWarning, dbEventRolesSecretsDrift)).To(BeFalse(),
				"same missing managed Secret reason must not emit duplicate warnings; events seen: %v", received)

			missing := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: scenario.namespace}, missing)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("reports a foreign-owned managed user secret distinctly without repeated warnings", func() {
			scenario := newReadyClusterScenario(namespace, "secret-foreign-owner", "tenant-cluster", "tenant-cnpg", "appdb")
			reconcilePostgresDatabaseToReady(ctx, scenario, false)
			recorder := record.NewFakeRecorder(100)

			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-%s-admin", scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, secret)).To(Succeed())
			secret.OwnerReferences = []metav1.OwnerReference{{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "foreign-secret-controller",
				UID:        types.UID("foreign-secret-controller"),
				Controller: ptr.To(true),
			}}
			Expect(k8sClient.Update(ctx, secret)).To(Succeed())

			result, err := reconcilePostgresDatabaseWithRecorder(ctx, scenario.requestName, recorder)
			expectReconcileResult(result, err, 15*time.Second)

			current := fetchPostgresDatabase(ctx, scenario.requestName)
			expectStatusPhase(current, "Provisioning")
			expectStatusCondition(current, "SecretsReady", metav1.ConditionFalse, reasonManagedSecretConflict)

			received := make([]string, 0, 16)
			collectEvents(&received, recorder)
			Expect(containsEvent(received, corev1.EventTypeWarning, dbEventRolesSecretsDrift)).To(BeTrue(),
				"foreign-owned managed Secret should emit one drift warning; events seen: %v", received)

			result, err = reconcilePostgresDatabaseWithRecorder(ctx, scenario.requestName, recorder)
			expectReconcileResult(result, err, 15*time.Second)
			received = received[:0]
			collectEvents(&received, recorder)
			Expect(containsEvent(received, corev1.EventTypeWarning, dbEventRolesSecretsDrift)).To(BeFalse(),
				"same foreign-owner reason must not emit duplicate warnings; events seen: %v", received)
		})

		It("re-attaches ownership when a managed user secret loses its owner reference", func() {
			scenario := newReadyClusterScenario(namespace, "secret-adopt", "tenant-cluster", "tenant-cnpg", "appdb")
			owner := reconcilePostgresDatabaseToReady(ctx, scenario, false)

			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-%s-admin", scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, secret)).To(Succeed())
			secret.OwnerReferences = nil
			Expect(k8sClient.Update(ctx, secret)).To(Succeed())

			result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
			expectEmptyReconcileResult(result, err)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}, secret)).To(Succeed())
			Expect(metav1.IsControlledBy(secret, owner)).To(BeTrue())

			current := fetchPostgresDatabase(ctx, scenario.requestName)
			expectReadyStatus(current, current.Generation, platformv1alpha1.DatabaseInfo{Name: scenario.dbName, Ready: true})
		})

		It("creates secrets and configmaps for a newly added database while preserving existing ones", func() {
			scenario := newReadyClusterScenario(namespace, "new-database", "tenant-cluster", "tenant-cnpg", "appdb")
			current := reconcilePostgresDatabaseToReady(ctx, scenario, false)

			current.Spec.Databases = append(current.Spec.Databases, platformv1alpha1.DatabaseDefinition{Name: "analytics"})
			Expect(k8sClient.Update(ctx, current)).To(Succeed())

			result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
			expectReconcileResult(result, err, 15*time.Second)

			for _, secretName := range []string{
				fmt.Sprintf("%s-analytics-admin", scenario.resourceName),
				fmt.Sprintf("%s-analytics-rw", scenario.resourceName),
			} {
				secret := &corev1.Secret{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: scenario.namespace}, secret)).To(Succeed())
			}

			configMap := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-analytics-config", scenario.resourceName), Namespace: scenario.namespace}, configMap)).To(Succeed())
			Expect(configMap.Data).To(HaveKeyWithValue(dbcore.ConfigMapKeyDatabaseName, "analytics"))

			existingSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-%s-admin", scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, existingSecret)).To(Succeed())
		})

		It("recreates a deleted CNPG Database", func() {
			scenario := newReadyClusterScenario(namespace, "cnpg-database-delete", "tenant-cluster", "tenant-cnpg", "appdb")
			owner := reconcilePostgresDatabaseToReady(ctx, scenario, false)

			cnpgDatabaseName := fmt.Sprintf("%s-%s", scenario.resourceName, scenario.dbName)
			Expect(k8sClient.Delete(ctx, &cnpgv1.Database{
				ObjectMeta: metav1.ObjectMeta{Name: cnpgDatabaseName, Namespace: scenario.namespace},
			})).To(Succeed())

			result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
			expectReconcileResult(result, err, 15*time.Second)

			cnpgDatabase := &cnpgv1.Database{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cnpgDatabaseName, Namespace: scenario.namespace}, cnpgDatabase)).To(Succeed())
			Expect(cnpgDatabase.Spec.Name).To(Equal(scenario.dbName))
			Expect(metav1.IsControlledBy(cnpgDatabase, owner)).To(BeTrue())

			markCNPGDatabaseApplied(ctx, cnpgDatabase)
			result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
			expectEmptyReconcileResult(result, err)
		})
	})

	When("the CNPG Database exists but has not been applied yet", func() {
		It("waits for CNPG to apply the database and sets DatabasesReady=False with reason WaitingForCNPG", func() {
			scenario := newReadyClusterScenario(namespace, "cnpg-wait", "tenant-cluster", "tenant-cnpg", dbAppdb)
			seedReadyClusterScenario(ctx, scenario, false)

			result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
			expectEmptyReconcileResult(result, err)

			current := expectFinalizerAdded(ctx, scenario.requestName)
			seedExistingDatabaseStatus(ctx, current, scenario.dbName)

			result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
			expectReconcileResult(result, err, 15*time.Second)
			expectProvisionedArtifacts(ctx, scenario, current)
			expectManagedRolesPatched(ctx, scenario)

			result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
			expectReconcileResult(result, err, 15*time.Second)
			expectCNPGDatabaseCreated(ctx, scenario, current)
			// Do NOT call markCNPGDatabaseApplied to leave it waiting

			result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
			expectReconcileResult(result, err, 15*time.Second)

			current = fetchPostgresDatabase(ctx, scenario.requestName)
			expectStatusCondition(current, condDatabasesReady, metav1.ConditionFalse, reasonWaitingForCNPG)
		})
	})

	When("managed roles have been patched but CNPG has not reconciled them yet", func() {
		It("waits for CNPG to reconcile roles and sets RolesReady=False with reason WaitingForCNPG", func() {
			scenario := newReadyClusterScenario(namespace, "roles-wait", "tenant-cluster", "tenant-cnpg", dbAppdb)
			createPostgresDatabaseResource(ctx, scenario.namespace, scenario.resourceName, scenario.clusterName, []platformv1alpha1.DatabaseDefinition{{Name: scenario.dbName}})
			postgresCluster := createPostgresClusterResource(ctx, scenario.namespace, scenario.clusterName)
			markPostgresClusterReady(ctx, postgresCluster, scenario.cnpgClusterName, scenario.namespace, false)
			cnpgCluster := createCNPGClusterResource(ctx, scenario.namespace, scenario.cnpgClusterName)
			// Mark with service endpoints but no reconciled roles — ConfigMaps need hosts but roles should stay pending
			markCNPGClusterReady(ctx, cnpgCluster, []string{}, "tenant-rw", "tenant-ro")

			result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
			expectEmptyReconcileResult(result, err)

			current := expectFinalizerAdded(ctx, scenario.requestName)
			seedExistingDatabaseStatus(ctx, current, scenario.dbName)

			result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
			expectReconcileResult(result, err, 15*time.Second)
			expectProvisionedArtifacts(ctx, scenario, current)
			expectManagedRolesPatched(ctx, scenario)

			current = fetchPostgresDatabase(ctx, scenario.requestName)
			expectStatusCondition(current, condRolesReady, metav1.ConditionFalse, reasonWaitingForCNPG)
		})
	})

	When("postgresdatabase secondary-resource predicates run", func() {
		It("triggers on cnpg database generation change and ignores status-only updates", func() {
			pred := predicate.GenerationChangedPredicate{}

			Expect(pred.Update(event.UpdateEvent{
				ObjectOld: &cnpgv1.Database{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
				ObjectNew: &cnpgv1.Database{ObjectMeta: metav1.ObjectMeta{Generation: 2}},
			})).To(BeTrue())
			Expect(pred.Update(event.UpdateEvent{
				ObjectOld: &cnpgv1.Database{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
				ObjectNew: &cnpgv1.Database{ObjectMeta: metav1.ObjectMeta{Generation: 1}},
			})).To(BeFalse())
		})

		It("suppresses secret create and update events but triggers on delete", func() {
			pred := databaseSecretPredicator()

			Expect(pred.Create(event.CreateEvent{})).To(BeFalse())
			Expect(pred.Update(event.UpdateEvent{
				ObjectOld: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secret", Namespace: "test", ResourceVersion: "1"}},
				ObjectNew: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secret", Namespace: "test", ResourceVersion: "2"}},
			})).To(BeFalse())
			Expect(pred.Delete(event.DeleteEvent{})).To(BeTrue())
		})

		It("treats configmap create, update, and delete events as drift triggers", func() {
			pred := predicate.ResourceVersionChangedPredicate{}

			Expect(pred.Create(event.CreateEvent{})).To(BeTrue())
			Expect(pred.Update(event.UpdateEvent{
				ObjectOld: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: "test", ResourceVersion: "1"}},
				ObjectNew: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: "test", ResourceVersion: "2"}},
			})).To(BeTrue())
			Expect(pred.Delete(event.DeleteEvent{})).To(BeTrue())
		})

		It("passes PostgresCluster create events", func() {
			pred := postgresClusterForDatabasePredicator()
			Expect(pred.Create(event.CreateEvent{})).To(BeTrue())
		})

		It("blocks PostgresCluster delete and generic events", func() {
			pred := postgresClusterForDatabasePredicator()
			Expect(pred.Delete(event.DeleteEvent{})).To(BeFalse())
			Expect(pred.Generic(event.GenericEvent{})).To(BeFalse())
		})

		It("blocks PostgresCluster updates that don't change ConnectionPoolerStatus", func() {
			pred := postgresClusterForDatabasePredicator()
			oldCluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"},
				Status: platformv1alpha1.PostgresClusterStatus{
					ConnectionPoolerStatus: &platformv1alpha1.ConnectionPoolerStatus{Enabled: true, ReadWriteEnabled: true, ReadOnlyEnabled: true},
				},
			}
			newCluster := oldCluster.DeepCopy()
			Expect(pred.Update(event.UpdateEvent{ObjectOld: oldCluster, ObjectNew: newCluster})).To(BeFalse())
		})

		It("passes PostgresCluster updates that introduce ConnectionPoolerStatus", func() {
			pred := postgresClusterForDatabasePredicator()
			oldCluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"},
			}
			newCluster := oldCluster.DeepCopy()
			newCluster.Status.ConnectionPoolerStatus = &platformv1alpha1.ConnectionPoolerStatus{Enabled: true, ReadWriteEnabled: true}
			Expect(pred.Update(event.UpdateEvent{ObjectOld: oldCluster, ObjectNew: newCluster})).To(BeTrue())
		})

		It("passes PostgresCluster updates that toggle ReadOnlyEnabled", func() {
			pred := postgresClusterForDatabasePredicator()
			oldCluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"},
				Status: platformv1alpha1.PostgresClusterStatus{
					ConnectionPoolerStatus: &platformv1alpha1.ConnectionPoolerStatus{Enabled: true, ReadWriteEnabled: true, ReadOnlyEnabled: false},
				},
			}
			newCluster := oldCluster.DeepCopy()
			newCluster.Status.ConnectionPoolerStatus.ReadOnlyEnabled = true
			Expect(pred.Update(event.UpdateEvent{ObjectOld: oldCluster, ObjectNew: newCluster})).To(BeTrue())
		})

		It("passes PostgresCluster updates that toggle ReadWriteEnabled", func() {
			pred := postgresClusterForDatabasePredicator()
			oldCluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"},
				Status: platformv1alpha1.PostgresClusterStatus{
					ConnectionPoolerStatus: &platformv1alpha1.ConnectionPoolerStatus{Enabled: true, ReadWriteEnabled: true, ReadOnlyEnabled: true},
				},
			}
			newCluster := oldCluster.DeepCopy()
			newCluster.Status.ConnectionPoolerStatus.ReadWriteEnabled = false
			Expect(pred.Update(event.UpdateEvent{ObjectOld: oldCluster, ObjectNew: newCluster})).To(BeTrue())
		})

		It("passes PostgresCluster custom-metrics acknowledgement updates", func() {
			pred := postgresClusterForDatabasePredicator()
			oldCluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"},
			}
			newCluster := oldCluster.DeepCopy()
			newCluster.Status.CustomMetricsStatus = &platformv1alpha1.CustomMetricsStatus{
				DatabaseContributions: []platformv1alpha1.DatabaseCustomMetricsStatus{{
					PostgresDatabaseName: "app",
					PostgresDatabaseUID:  "uid",
					DatabaseName:         "orders",
					DesiredRevision:      "revision",
					AppliedRevision:      "revision",
					Status:               metav1.ConditionTrue,
					Reason:               "CustomMetricsReady",
				}},
			}
			Expect(pred.Update(event.UpdateEvent{ObjectOld: oldCluster, ObjectNew: newCluster})).To(BeTrue())
		})

		It("blocks PostgresCluster updates that change unrelated status fields", func() {
			pred := postgresClusterForDatabasePredicator()
			phaseReady := "Ready"
			phaseProvisioning := "Provisioning"
			oldCluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"},
				Status: platformv1alpha1.PostgresClusterStatus{
					Phase:                  &phaseReady,
					ConnectionPoolerStatus: &platformv1alpha1.ConnectionPoolerStatus{Enabled: true, ReadWriteEnabled: true},
				},
			}
			newCluster := oldCluster.DeepCopy()
			newCluster.Status.Phase = &phaseProvisioning
			Expect(pred.Update(event.UpdateEvent{ObjectOld: oldCluster, ObjectNew: newCluster})).To(BeFalse())
		})

		It("passes PostgresCluster updates when ReadyInstances drops below the RO threshold", func() {
			pred := postgresClusterForDatabasePredicator()
			oldCluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"},
				Status: platformv1alpha1.PostgresClusterStatus{
					ConnectionPoolerStatus: &platformv1alpha1.ConnectionPoolerStatus{Enabled: true, ReadWriteEnabled: true, ReadOnlyEnabled: true},
					ReadyInstances:         ptr.To(int32(2)),
				},
			}
			newCluster := oldCluster.DeepCopy()
			newCluster.Status.ReadyInstances = ptr.To(int32(1))
			Expect(pred.Update(event.UpdateEvent{ObjectOld: oldCluster, ObjectNew: newCluster})).To(BeTrue())
		})

		It("passes PostgresCluster updates when ReadyInstances crosses to the RO threshold", func() {
			pred := postgresClusterForDatabasePredicator()
			oldCluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"},
				Status: platformv1alpha1.PostgresClusterStatus{
					ConnectionPoolerStatus: &platformv1alpha1.ConnectionPoolerStatus{Enabled: true, ReadWriteEnabled: true, ReadOnlyEnabled: true},
					ReadyInstances:         ptr.To(int32(1)),
				},
			}
			newCluster := oldCluster.DeepCopy()
			newCluster.Status.ReadyInstances = ptr.To(int32(2))
			Expect(pred.Update(event.UpdateEvent{ObjectOld: oldCluster, ObjectNew: newCluster})).To(BeTrue())
		})

		It("blocks PostgresCluster updates when ReadyInstances changes within the same RO-availability state", func() {
			pred := postgresClusterForDatabasePredicator()
			oldCluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"},
				Status: platformv1alpha1.PostgresClusterStatus{
					ConnectionPoolerStatus: &platformv1alpha1.ConnectionPoolerStatus{Enabled: true, ReadWriteEnabled: true, ReadOnlyEnabled: true},
					ReadyInstances:         ptr.To(int32(2)),
				},
			}
			newCluster := oldCluster.DeepCopy()
			newCluster.Status.ReadyInstances = ptr.To(int32(3))
			Expect(pred.Update(event.UpdateEvent{ObjectOld: oldCluster, ObjectNew: newCluster})).To(BeFalse())
		})

		It("does not trigger on annotation changes", func() {
			pred := postgresDatabasePredicator()
			oldDB := &platformv1alpha1.PostgresDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
					Annotations: map[string]string{
						"some-annotation": "old",
					},
				},
			}
			newDB := &platformv1alpha1.PostgresDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
					Annotations: map[string]string{
						"some-annotation": "new",
					},
				},
			}

			Expect(pred.Update(event.UpdateEvent{ObjectOld: oldDB, ObjectNew: newDB})).To(BeFalse())
		})
	})

	When("role ownership inversion is active", func() {
		It("drives the status handshake from credential publication through the role gate", func() {
			scenario := newReadyClusterScenario(namespace, "handshake-db", "handshake-pg", "handshake-cnpg", dbAppdb)
			createPostgresDatabaseResource(ctx, scenario.namespace, scenario.resourceName, scenario.clusterName, []platformv1alpha1.DatabaseDefinition{{Name: scenario.dbName}})
			postgresCluster := createPostgresClusterResource(ctx, scenario.namespace, scenario.clusterName)
			markPostgresClusterReady(ctx, postgresCluster, scenario.cnpgClusterName, scenario.namespace, false)
			cnpgCluster := createCNPGClusterResource(ctx, scenario.namespace, scenario.cnpgClusterName)
			markCNPGClusterReady(ctx, cnpgCluster, nil, "tenant-rw", "tenant-ro")

			result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
			expectEmptyReconcileResult(result, err)
			current := expectFinalizerAdded(ctx, scenario.requestName)
			seedExistingDatabaseStatus(ctx, current, scenario.dbName)

			result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
			expectReconcileResult(result, err, 15*time.Second)
			current = fetchPostgresDatabase(ctx, scenario.requestName)
			Expect(publishedRoleNames(current)).To(ConsistOf(adminRoleNameForTest(scenario.dbName), rwRoleNameForTest(scenario.dbName)))
			expectStatusCondition(current, condRolesReady, metav1.ConditionFalse, reasonWaitingForCNPG)

			simulateClusterRoleOwnership(ctx, scenario.clusterName, scenario.namespace, current,
				adminRoleNameForTest(scenario.dbName), rwRoleNameForTest(scenario.dbName))

			result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
			expectReconcileResult(result, err, 15*time.Second)
			cnpgDatabase := expectCNPGDatabaseCreated(ctx, scenario, current)
			markCNPGDatabaseApplied(ctx, cnpgDatabase)

			result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
			expectEmptyReconcileResult(result, err)
			current = fetchPostgresDatabase(ctx, scenario.requestName)
			expectStatusCondition(current, condRolesReady, metav1.ConditionTrue, reasonRolesAvailable)
		})

		It("surfaces cluster-published role conflicts on each offending database", func() {
			clusterName := "conflict-pg"
			cnpgClusterName := "conflict-cnpg"
			postgresCluster := createPostgresClusterResource(ctx, namespace, clusterName)
			markPostgresClusterReady(ctx, postgresCluster, cnpgClusterName, namespace, false)
			cnpgCluster := createCNPGClusterResource(ctx, namespace, cnpgClusterName)
			markCNPGClusterReady(ctx, cnpgCluster, nil, "tenant-rw", "tenant-ro")

			first := createPostgresDatabaseResource(ctx, namespace, "conflict-a", clusterName, []platformv1alpha1.DatabaseDefinition{{Name: dbAppdb}}, postgresDatabaseFinalizer)
			second := createPostgresDatabaseResource(ctx, namespace, "conflict-b", clusterName, []platformv1alpha1.DatabaseDefinition{{Name: dbAppdb}}, postgresDatabaseFinalizer)

			for _, nn := range []types.NamespacedName{{Name: first.Name, Namespace: namespace}, {Name: second.Name, Namespace: namespace}} {
				result, err := reconcilePostgresDatabase(ctx, nn)
				expectReconcileResult(result, err, 15*time.Second)
			}

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, postgresCluster)).To(Succeed())
			postgresCluster.Status.ManagedRolesStatus = &platformv1alpha1.ManagedRolesStatus{Conflicts: []platformv1alpha1.RoleConflict{
				{Role: adminRoleNameForTest(dbAppdb), AttemptedBy: platformv1alpha1.RoleOwnerReference{Name: first.Name, UID: string(first.UID)}},
				{Role: rwRoleNameForTest(dbAppdb), AttemptedBy: platformv1alpha1.RoleOwnerReference{Name: first.Name, UID: string(first.UID)}},
				{Role: adminRoleNameForTest(dbAppdb), AttemptedBy: platformv1alpha1.RoleOwnerReference{Name: second.Name, UID: string(second.UID)}},
				{Role: rwRoleNameForTest(dbAppdb), AttemptedBy: platformv1alpha1.RoleOwnerReference{Name: second.Name, UID: string(second.UID)}},
			}}
			Expect(k8sClient.Status().Update(ctx, postgresCluster)).To(Succeed())

			for _, db := range []*platformv1alpha1.PostgresDatabase{first, second} {
				result, err := reconcilePostgresDatabase(ctx, types.NamespacedName{Name: db.Name, Namespace: namespace})
				expectReconcileResult(result, err, 15*time.Second)
				current := fetchPostgresDatabase(ctx, types.NamespacedName{Name: db.Name, Namespace: namespace})
				condition := meta.FindStatusCondition(current.Status.Conditions, condRolesReady)
				Expect(condition).NotTo(BeNil())
				Expect(condition.Status).To(Equal(metav1.ConditionFalse))
				Expect(condition.Reason).To(Equal(reasonRoleConflict))
			}
		})
	})

	When("role ownership conflicts exist", func() {
		It("marks the resource failed and stops provisioning dependent resources", func() {
			resourceName := "conflict-cluster"
			clusterName := "conflict-postgres"
			requestName := seedConflictScenario(ctx, namespace, resourceName, clusterName)

			current := fetchPostgresDatabase(ctx, requestName)
			cluster := &platformv1alpha1.PostgresCluster{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, cluster)).To(Succeed())
			incumbent := platformv1alpha1.RoleOwnerReference{Name: "other-db", UID: "other-uid"}
			self := platformv1alpha1.RoleOwnerReference{Name: current.Name, UID: string(current.UID)}
			cluster.Status.ManagedRolesStatus = &platformv1alpha1.ManagedRolesStatus{
				Conflicts: []platformv1alpha1.RoleConflict{
					{Role: adminRoleNameForTest(dbAppdb), ClaimedBy: &incumbent, AttemptedBy: self},
					{Role: rwRoleNameForTest(dbAppdb), ClaimedBy: &incumbent, AttemptedBy: self},
				},
			}
			Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

			result, err := reconcilePostgresDatabase(ctx, requestName)
			expectReconcileResult(result, err, 15*time.Second)

			current = fetchPostgresDatabase(ctx, requestName)
			expectStatusPhase(current, phaseFailed)
			expectStatusCondition(current, condRolesReady, metav1.ConditionFalse, reasonRoleConflict)

			cnpgDatabase := &cnpgv1.Database{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: cnpgDatabaseNameForTest("conflict-cluster", dbAppdb), Namespace: namespace}, cnpgDatabase)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
	})

	When("a database is removed from spec.databases while the CR stays alive", func() {
		It("stops publishing the removed database's roles and keeps the retained roles published", func() {
			resourceName := "live-db-removal"
			clusterName := "live-db-removal-postgres"
			cnpgClusterName := "live-db-removal-cnpg"
			requestName := types.NamespacedName{Name: resourceName, Namespace: namespace}

			postgresDB := createPostgresDatabaseResource(ctx, namespace, resourceName, clusterName, []platformv1alpha1.DatabaseDefinition{
				{Name: dbKeepdb},
				{Name: dbDropdb},
			}, postgresDatabaseFinalizer)
			Expect(k8sClient.Get(ctx, requestName, postgresDB)).To(Succeed())

			postgresCluster := createPostgresClusterResource(ctx, namespace, clusterName)
			markPostgresClusterReady(ctx, postgresCluster, cnpgClusterName, namespace, false)
			cnpgCluster := createCNPGClusterResource(ctx, namespace, cnpgClusterName)
			markCNPGClusterReady(ctx, cnpgCluster, []string{
				adminRoleNameForTest(dbKeepdb), rwRoleNameForTest(dbKeepdb),
				adminRoleNameForTest(dbDropdb), rwRoleNameForTest(dbDropdb),
			}, "tenant-rw", "tenant-ro")

			seedOwnedDatabaseArtifacts(ctx, namespace, resourceName, cnpgClusterName, postgresDB, dbKeepdb, dbDropdb)

			simulateClusterRoleOwnership(ctx, clusterName, namespace, postgresDB,
				adminRoleNameForTest(dbKeepdb), rwRoleNameForTest(dbKeepdb),
				adminRoleNameForTest(dbDropdb), rwRoleNameForTest(dbDropdb))

			postgresDB.Spec.Databases = []platformv1alpha1.DatabaseDefinition{{Name: dbKeepdb}}
			Expect(k8sClient.Update(ctx, postgresDB)).To(Succeed())

			result, err := reconcilePostgresDatabase(ctx, requestName)
			expectReconcileResult(result, err, 15*time.Second)

			updatedDB := fetchPostgresDatabase(ctx, requestName)
			Expect(publishedRoleNames(updatedDB)).To(ConsistOf(adminRoleNameForTest(dbKeepdb), rwRoleNameForTest(dbKeepdb)))
			expectPublishedRoleExists(updatedDB, adminRoleNameForTest(dbKeepdb), true)
			expectPublishedRoleExists(updatedDB, rwRoleNameForTest(dbKeepdb), true)
		})
	})

	When("the PostgresDatabase is being deleted", func() {
		Context("with retained and deleted databases", func() {
			It("orphans retained resources, removes deleted resources, and patches managed roles", func() {
				resourceName := "delete-cluster"
				clusterName := "delete-postgres"
				requestName := types.NamespacedName{Name: resourceName, Namespace: namespace}

				postgresDB := createPostgresDatabaseResource(ctx, namespace, resourceName, clusterName, []platformv1alpha1.DatabaseDefinition{
					{Name: dbKeepdb, DeletionPolicy: "Retain"},
					{Name: dbDropdb},
				}, postgresDatabaseFinalizer)
				Expect(k8sClient.Get(ctx, requestName, postgresDB)).To(Succeed())

				createPostgresClusterResource(ctx, namespace, clusterName)

				seedOwnedDatabaseArtifacts(ctx, namespace, resourceName, clusterName, postgresDB, dbKeepdb, dbDropdb)

				simulateClusterRoleOwnership(ctx, clusterName, namespace, postgresDB,
					adminRoleNameForTest(dbKeepdb), rwRoleNameForTest(dbKeepdb),
					adminRoleNameForTest(dbDropdb), rwRoleNameForTest(dbDropdb))

				Expect(k8sClient.Delete(ctx, postgresDB)).To(Succeed())

				result, err := reconcilePostgresDatabase(ctx, requestName)
				expectReconcileResult(result, err, 15*time.Second)

				expectRetainedArtifact(ctx, configMapNameForTest(resourceName, dbKeepdb), namespace, resourceName, &corev1.ConfigMap{})
				expectRetainedArtifact(ctx, adminSecretNameForTest(resourceName, dbKeepdb), namespace, resourceName, &corev1.Secret{})
				expectRetainedArtifact(ctx, rwSecretNameForTest(resourceName, dbKeepdb), namespace, resourceName, &corev1.Secret{})
				expectRetainedArtifact(ctx, cnpgDatabaseNameForTest(resourceName, dbKeepdb), namespace, resourceName, &cnpgv1.Database{})

				expectDeletedArtifact(ctx, configMapNameForTest(resourceName, dbDropdb), namespace, &corev1.ConfigMap{})
				expectDeletedArtifact(ctx, adminSecretNameForTest(resourceName, dbDropdb), namespace, &corev1.Secret{})
				expectDeletedArtifact(ctx, rwSecretNameForTest(resourceName, dbDropdb), namespace, &corev1.Secret{})
				expectDeletedArtifact(ctx, cnpgDatabaseNameForTest(resourceName, dbDropdb), namespace, &cnpgv1.Database{})

				blocked := fetchPostgresDatabase(ctx, requestName)
				expectPublishedRoleExists(blocked, adminRoleNameForTest(dbDropdb), false)
				expectPublishedRoleExists(blocked, rwRoleNameForTest(dbDropdb), false)
				expectStatusPhase(blocked, "Deleting")
				Expect(blocked.Finalizers).To(ContainElement(postgresDatabaseFinalizer))

				simulateClusterRoleOwnership(ctx, clusterName, namespace, postgresDB)

				result, err = reconcilePostgresDatabase(ctx, requestName)
				expectEmptyReconcileResult(result, err)

				err = k8sClient.Get(ctx, requestName, &platformv1alpha1.PostgresDatabase{})
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			})
		})
	})

	When("extensions are declared on a database", func() {
		It("propagates them as ensure:present to the CNPG Database spec", func() {
			scenario := newReadyClusterScenario(namespace, "ext-create", "tenant-cluster", "tenant-cnpg", dbAppdb)
			createPostgresDatabaseResource(ctx, scenario.namespace, scenario.resourceName, scenario.clusterName, []platformv1alpha1.DatabaseDefinition{
				{Name: scenario.dbName, Extensions: []string{"pg_trgm", "unaccent"}},
			}, postgresDatabaseFinalizer)
			postgresCluster := createPostgresClusterResource(ctx, scenario.namespace, scenario.clusterName)
			markPostgresClusterReady(ctx, postgresCluster, scenario.cnpgClusterName, scenario.namespace, false)
			cnpgCluster := createCNPGClusterResource(ctx, scenario.namespace, scenario.cnpgClusterName)
			markCNPGClusterReady(ctx, cnpgCluster, []string{adminRoleNameForTest(scenario.dbName), rwRoleNameForTest(scenario.dbName)}, "tenant-rw", "tenant-ro")

			current := fetchPostgresDatabase(ctx, scenario.requestName)
			seedExistingDatabaseStatus(ctx, current, scenario.dbName)

			result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
			expectReconcileResult(result, err, 15*time.Second)

			simulateClusterRoleOwnership(ctx, scenario.clusterName, scenario.namespace, current,
				adminRoleNameForTest(scenario.dbName), rwRoleNameForTest(scenario.dbName))

			result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
			expectReconcileResult(result, err, 15*time.Second)

			cnpgDatabase := &cnpgv1.Database{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cnpgDatabaseNameForTest(scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, cnpgDatabase)).To(Succeed())
			Expect(cnpgDatabase.Spec.Extensions).To(ConsistOf(
				cnpgv1.ExtensionSpec{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "pg_trgm", Ensure: cnpgv1.EnsurePresent}},
				cnpgv1.ExtensionSpec{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "unaccent", Ensure: cnpgv1.EnsurePresent}},
			))
		})

		It("marks a removed extension as ensure:absent on the next reconcile", func() {
			scenario := newReadyClusterScenario(namespace, "ext-remove", "tenant-cluster", "tenant-cnpg", dbAppdb)
			createPostgresDatabaseResource(ctx, scenario.namespace, scenario.resourceName, scenario.clusterName, []platformv1alpha1.DatabaseDefinition{
				{Name: scenario.dbName, Extensions: []string{"pg_trgm", "unaccent"}},
			}, postgresDatabaseFinalizer)
			postgresCluster := createPostgresClusterResource(ctx, scenario.namespace, scenario.clusterName)
			markPostgresClusterReady(ctx, postgresCluster, scenario.cnpgClusterName, scenario.namespace, false)
			cnpgCluster := createCNPGClusterResource(ctx, scenario.namespace, scenario.cnpgClusterName)
			markCNPGClusterReady(ctx, cnpgCluster, []string{adminRoleNameForTest(scenario.dbName), rwRoleNameForTest(scenario.dbName)}, "tenant-rw", "tenant-ro")

			current := fetchPostgresDatabase(ctx, scenario.requestName)
			seedExistingDatabaseStatus(ctx, current, scenario.dbName)

			reconcilePostgresDatabase(ctx, scenario.requestName)
			simulateClusterRoleOwnership(ctx, scenario.clusterName, scenario.namespace, current,
				adminRoleNameForTest(scenario.dbName), rwRoleNameForTest(scenario.dbName))
			reconcilePostgresDatabase(ctx, scenario.requestName)

			cnpgDatabase := &cnpgv1.Database{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cnpgDatabaseNameForTest(scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, cnpgDatabase)).To(Succeed())
			markCNPGDatabaseApplied(ctx, cnpgDatabase)

			current = fetchPostgresDatabase(ctx, scenario.requestName)
			current.Spec.Databases[0].Extensions = []string{"pg_trgm"}
			Expect(k8sClient.Update(ctx, current)).To(Succeed())

			reconcilePostgresDatabase(ctx, scenario.requestName)

			cnpgDatabase = &cnpgv1.Database{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cnpgDatabaseNameForTest(scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, cnpgDatabase)).To(Succeed())
			Expect(cnpgDatabase.Spec.Extensions).To(ConsistOf(
				cnpgv1.ExtensionSpec{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "pg_trgm", Ensure: cnpgv1.EnsurePresent}},
				cnpgv1.ExtensionSpec{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "unaccent", Ensure: cnpgv1.EnsureAbsent}},
			))

			reconcilePostgresDatabase(ctx, scenario.requestName)
			cnpgDatabase = &cnpgv1.Database{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cnpgDatabaseNameForTest(scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, cnpgDatabase)).To(Succeed())
			Expect(cnpgDatabase.Spec.Extensions).To(ConsistOf(
				cnpgv1.ExtensionSpec{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "pg_trgm", Ensure: cnpgv1.EnsurePresent}},
				cnpgv1.ExtensionSpec{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "unaccent", Ensure: cnpgv1.EnsureAbsent}},
			))
		})
	})

	When("a retained CNPG Database exists without owner reference", func() {
		It("re-adopts the resource and removes retained annotation", func() {
			scenario := newReadyClusterScenario(namespace, "adopt-cnpg", "tenant-cluster", "tenant-cnpg", dbAppdb)
			createPostgresDatabaseResource(ctx, scenario.namespace, scenario.resourceName, scenario.clusterName, []platformv1alpha1.DatabaseDefinition{{Name: scenario.dbName}}, postgresDatabaseFinalizer)
			postgresCluster := createPostgresClusterResource(ctx, scenario.namespace, scenario.clusterName)
			markPostgresClusterReady(ctx, postgresCluster, scenario.cnpgClusterName, scenario.namespace, false)
			cnpgCluster := createCNPGClusterResource(ctx, scenario.namespace, scenario.cnpgClusterName)
			markCNPGClusterReady(ctx, cnpgCluster, []string{adminRoleNameForTest(scenario.dbName), rwRoleNameForTest(scenario.dbName)}, "tenant-rw", "tenant-ro")

			// Create a CNPG Database with retained annotation but no owner reference
			retainedCNPGDb := &cnpgv1.Database{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cnpgDatabaseNameForTest(scenario.resourceName, scenario.dbName),
					Namespace: scenario.namespace,
					Annotations: map[string]string{
						retainedFromAnnotation: scenario.resourceName,
					},
				},
				Spec: cnpgv1.DatabaseSpec{
					ClusterRef: corev1.LocalObjectReference{Name: scenario.cnpgClusterName},
					Name:       scenario.dbName,
					Owner:      adminRoleNameForTest(scenario.dbName),
				},
			}
			Expect(k8sClient.Create(ctx, retainedCNPGDb)).To(Succeed())

			// Finalizer already present — first reconcile goes straight to provisioning
			current := fetchPostgresDatabase(ctx, scenario.requestName)
			seedExistingDatabaseStatus(ctx, current, scenario.dbName)

			result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
			expectReconcileResult(result, err, 15*time.Second)
			expectProvisionedArtifacts(ctx, scenario, current)
			expectManagedRolesPatched(ctx, scenario)

			// Second reconcile: roles ready, re-adopts the retained CNPG Database, waits for applied
			result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
			expectReconcileResult(result, err, 15*time.Second)

			adoptedDb := &cnpgv1.Database{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cnpgDatabaseNameForTest(scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, adoptedDb)).To(Succeed())
			Expect(metav1.IsControlledBy(adoptedDb, current)).To(BeTrue())
			_, hasRetainedAnnotation := adoptedDb.Annotations[retainedFromAnnotation]
			Expect(hasRetainedAnnotation).To(BeFalse())

			markCNPGDatabaseApplied(ctx, adoptedDb)
			result, err = reconcilePostgresDatabase(ctx, scenario.requestName)
			expectEmptyReconcileResult(result, err)
		})
	})

	When("the parent PostgresCluster is deleted and recreated", func() {
		It("can recover and reconcile back to Ready state after cluster recreation", func() {
			// Setup: Create PostgresDatabase and bring it to Ready
			scenario := newReadyClusterScenario(namespace, "cluster-recreation", "tenant-cluster", "tenant-cnpg", dbAppdb)
			current := reconcilePostgresDatabaseToReady(ctx, scenario, false)
			Expect(current.Status.Phase).NotTo(BeNil())
			Expect(*current.Status.Phase).To(Equal(phaseReady))

			// Delete the PostgresCluster (simulating external deletion)
			postgresCluster := &platformv1alpha1.PostgresCluster{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: scenario.clusterName, Namespace: scenario.namespace}, postgresCluster)).To(Succeed())
			Expect(k8sClient.Delete(ctx, postgresCluster)).To(Succeed())

			// Delete the CNPG Cluster (as it would be garbage collected by the cluster controller)
			cnpgCluster := &cnpgv1.Cluster{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: scenario.cnpgClusterName, Namespace: scenario.namespace}, cnpgCluster)).To(Succeed())
			Expect(k8sClient.Delete(ctx, cnpgCluster)).To(Succeed())

			// Recreate the PostgresCluster
			newPostgresCluster := createPostgresClusterResource(ctx, scenario.namespace, scenario.clusterName)
			markPostgresClusterReady(ctx, newPostgresCluster, scenario.cnpgClusterName, scenario.namespace, false)
			newCNPGCluster := createCNPGClusterResource(ctx, scenario.namespace, scenario.cnpgClusterName)
			markCNPGClusterReady(ctx, newCNPGCluster, []string{adminRoleNameForTest(scenario.dbName), rwRoleNameForTest(scenario.dbName)}, "tenant-rw", "tenant-ro")
			simulateClusterRoleOwnership(ctx, scenario.clusterName, scenario.namespace, current,
				adminRoleNameForTest(scenario.dbName), rwRoleNameForTest(scenario.dbName))

			// Mark the CNPG Database as applied (simulating CNPG reconciliation)
			cnpgDatabase := &cnpgv1.Database{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cnpgDatabaseNameForTest(scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, cnpgDatabase)).To(Succeed())
			markCNPGDatabaseApplied(ctx, cnpgDatabase)

			// Manually reconcile to verify the controller can recover
			Eventually(func() string {
				result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
				if err != nil {
					return ""
				}
				// Multiple reconciles may be needed as cluster status is re-evaluated
				if result.RequeueAfter > 0 {
					return ""
				}

				current := fetchPostgresDatabase(ctx, scenario.requestName)
				if current.Status.Phase == nil {
					return ""
				}
				return *current.Status.Phase
			}, 30*time.Second, 1*time.Second).Should(Equal(phaseReady), "PostgresDatabase should return to Ready state after cluster recreation")

			// Verify all conditions are healthy
			current = fetchPostgresDatabase(ctx, scenario.requestName)
			expectStatusCondition(current, condClusterReady, metav1.ConditionTrue, reasonClusterAvailable)
			expectStatusCondition(current, condSecretsReady, metav1.ConditionTrue, reasonSecretsCreated)
			expectStatusCondition(current, condConfigMapsReady, metav1.ConditionTrue, reasonConfigMapsCreated)
			expectStatusCondition(current, condRolesReady, metav1.ConditionTrue, reasonRolesAvailable)
			expectStatusCondition(current, condDatabasesReady, metav1.ConditionTrue, reasonDatabasesAvailable)
		})
	})
	When("enqueuePostgresDatabasesForCluster is called", func() {
		It("returns a request for each PostgresDatabase that references the cluster", func() {
			reconciler := &PostgresDatabaseReconciler{
				Client:         k8sClient,
				Scheme:         k8sClient.Scheme(),
				Recorder:       record.NewFakeRecorder(100),
				Metrics:        &pgprometheus.NoopRecorder{},
				FleetCollector: pgprometheus.NewFleetCollector(),
			}
			cluster := createPostgresClusterResource(ctx, namespace, "enqueue-cluster")
			createPostgresDatabaseResource(ctx, namespace, "db-matches", "enqueue-cluster", []platformv1alpha1.DatabaseDefinition{{Name: dbAppdb}})
			createPostgresDatabaseResource(ctx, namespace, "db-no-match", "other-cluster", []platformv1alpha1.DatabaseDefinition{{Name: dbAppdb}})
			reqs := reconciler.enqueuePostgresDatabasesForCluster(ctx, cluster)

			Expect(reqs).To(HaveLen(1))
			Expect(reqs[0].NamespacedName).To(Equal(types.NamespacedName{Name: "db-matches", Namespace: namespace}))
		})
		It("returns an empty list if no PostgresDatabases reference the cluster", func() {
			reconciler := &PostgresDatabaseReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(100),
				Metrics:  &pgprometheus.NoopRecorder{},
			}
			cluster := createPostgresClusterResource(ctx, namespace, "enqueue-cluster-empty")
			reqs := reconciler.enqueuePostgresDatabasesForCluster(ctx, cluster)

			Expect(reqs).To(BeEmpty())
		})
	})
	When("a PostgresDatabase resource is created, the kubebuilder validation works and", func() {
		It("rejects database names containing underscores or hyphens", func() {
			for i, databaseName := range []string{"my_db", "_mydb", "my__db", "my-db", "-mydb", "my--db"} {
				postgresDB := &platformv1alpha1.PostgresDatabase{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("invalid-database-name-%d", i),
						Namespace: namespace,
					},
					Spec: platformv1alpha1.PostgresDatabaseSpec{
						ClusterRef: corev1.LocalObjectReference{Name: "tenant-cluster"},
						Databases:  []platformv1alpha1.DatabaseDefinition{{Name: databaseName}},
					},
				}

				err := k8sClient.Create(ctx, postgresDB)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("spec.databases[0].name"))
				Expect(err.Error()).To(ContainSubstring("should match '^[a-z][a-z0-9]*$'"))
			}
		})

		It("should catch empty secrets", func() {
			scenario := newReadyClusterScenario(namespace, "password-config-wrong", "tenant-cluster", "tenant-cnpg", dbAppdb)
			postgresDB := &platformv1alpha1.PostgresDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:       scenario.resourceName,
					Namespace:  namespace,
					Finalizers: []string{dbAppdb},
				},
				Spec: platformv1alpha1.PostgresDatabaseSpec{
					ClusterRef: corev1.LocalObjectReference{Name: scenario.clusterName},
					Databases: []platformv1alpha1.DatabaseDefinition{{Name: scenario.dbName,
						PasswordConfig: &platformv1alpha1.PasswordConfig{
							ExternalAdminSecretRef: corev1.LocalObjectReference{Name: ""},
							ExternalRWSecretRef:    corev1.LocalObjectReference{Name: ""},
						}}}},
			}
			Expect(k8sClient.Create(ctx, postgresDB)).NotTo(Succeed())
		})
		It("should catch indifferent secrets", func() {
			scenario := newReadyClusterScenario(namespace, "password-config-wrong", "tenant-cluster", "tenant-cnpg", dbAppdb)
			postgresDB := &platformv1alpha1.PostgresDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:       scenario.resourceName,
					Namespace:  namespace,
					Finalizers: []string{dbAppdb},
				},
				Spec: platformv1alpha1.PostgresDatabaseSpec{
					ClusterRef: corev1.LocalObjectReference{Name: scenario.clusterName},
					Databases: []platformv1alpha1.DatabaseDefinition{{Name: scenario.dbName,
						PasswordConfig: &platformv1alpha1.PasswordConfig{
							ExternalAdminSecretRef: corev1.LocalObjectReference{Name: "indiff"},
							ExternalRWSecretRef:    corev1.LocalObjectReference{Name: "indiff"},
						}}}},
			}
			Expect(k8sClient.Create(ctx, postgresDB)).NotTo(Succeed())
		})
	})
})
