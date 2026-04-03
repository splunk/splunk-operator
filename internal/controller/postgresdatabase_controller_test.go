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
	"slices"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	pgprometheus "github.com/splunk/splunk-operator/pkg/postgresql/shared/adapter/prometheus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const postgresDatabaseFinalizer = "postgresdatabases.enterprise.splunk.com/finalizer"

// condition types
const (
	condClusterReady    = "ClusterReady"
	condSecretsReady    = "SecretsReady"
	condConfigMapsReady = "ConfigMapsReady"
	condRolesReady      = "RolesReady"
	condDatabasesReady  = "DatabasesReady"
	condPrivilegesReady = "PrivilegesReady"
)

// condition reasons
const (
	reasonClusterNotFound    = "ClusterNotFound"
	reasonClusterAvailable   = "ClusterAvailable"
	reasonSecretsCreated     = "SecretsCreated"
	reasonConfigMapsCreated  = "ConfigMapsCreated"
	reasonUsersAvailable     = "UsersAvailable"
	reasonDatabasesAvailable = "DatabasesAvailable"
	reasonRoleConflict       = "RoleConflict"
)

// phases
const (
	phasePending = "Pending"
	phaseReady   = "Ready"
	phaseFailed  = "Failed"
)

// annotations
const retainedFromAnnotation = "enterprise.splunk.com/retained-from"

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

func managedRoleNames(roles []enterprisev4.ManagedRole) []string {
	names := make([]string, 0, len(roles))
	for _, role := range roles {
		names = append(names, role.Name)
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

func ownedByPostgresDatabase(postgresDB *enterprisev4.PostgresDatabase) []metav1.OwnerReference {
	controller := true
	blockOwnerDeletion := true
	return []metav1.OwnerReference{{
		APIVersion:         enterprisev4.GroupVersion.String(),
		Kind:               "PostgresDatabase",
		Name:               postgresDB.Name,
		UID:                postgresDB.UID,
		Controller:         &controller,
		BlockOwnerDeletion: &blockOwnerDeletion,
	}}
}

func createPostgresDatabaseResource(ctx context.Context, namespace, resourceName, clusterName string, databases []enterprisev4.DatabaseDefinition, finalizers ...string) *enterprisev4.PostgresDatabase {
	postgresDB := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{
			Name:       resourceName,
			Namespace:  namespace,
			Finalizers: finalizers,
		},
		Spec: enterprisev4.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: clusterName},
			Databases:  databases,
		},
	}
	Expect(k8sClient.Create(ctx, postgresDB)).To(Succeed())
	return postgresDB
}

func createPostgresClusterResource(ctx context.Context, namespace, clusterName string) *enterprisev4.PostgresCluster {
	postgresCluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: enterprisev4.PostgresClusterSpec{
			Class: "dev",
		},
	}
	Expect(k8sClient.Create(ctx, postgresCluster)).To(Succeed())
	return postgresCluster
}

func markPostgresClusterReady(ctx context.Context, postgresCluster *enterprisev4.PostgresCluster, cnpgClusterName, namespace string, poolerEnabled bool) {
	clusterPhase := "Ready"
	postgresCluster.Status.Phase = &clusterPhase
	postgresCluster.Status.ProvisionerRef = &corev1.ObjectReference{
		APIVersion: cnpgv1.SchemeGroupVersion.String(),
		Kind:       "Cluster",
		Name:       cnpgClusterName,
		Namespace:  namespace,
	}
	if poolerEnabled {
		postgresCluster.Status.ConnectionPoolerStatus = &enterprisev4.ConnectionPoolerStatus{Enabled: true}
	}
	Expect(k8sClient.Status().Update(ctx, postgresCluster)).To(Succeed())
}

func createCNPGClusterResource(ctx context.Context, namespace, cnpgClusterName string) *cnpgv1.Cluster {
	cnpgCluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cnpgClusterName,
			Namespace: namespace,
		},
		Spec: cnpgv1.ClusterSpec{
			Instances: 1,
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
	createPostgresDatabaseResource(ctx, scenario.namespace, scenario.resourceName, scenario.clusterName, []enterprisev4.DatabaseDefinition{{Name: scenario.dbName}})
	postgresCluster := createPostgresClusterResource(ctx, scenario.namespace, scenario.clusterName)
	markPostgresClusterReady(ctx, postgresCluster, scenario.cnpgClusterName, scenario.namespace, poolerEnabled)
	cnpgCluster := createCNPGClusterResource(ctx, scenario.namespace, scenario.cnpgClusterName)
	markCNPGClusterReady(ctx, cnpgCluster, []string{adminRoleNameForTest(scenario.dbName), rwRoleNameForTest(scenario.dbName)}, "tenant-rw", "tenant-ro")
}

func expectReconcileResult(result ctrl.Result, err error, requeueAfter time.Duration) {
	Expect(err).NotTo(HaveOccurred())
	Expect(result.RequeueAfter).To(Equal(requeueAfter))
}

func expectEmptyReconcileResult(result ctrl.Result, err error) {
	Expect(err).NotTo(HaveOccurred())
	Expect(result).To(Equal(ctrl.Result{}))
}

func fetchPostgresDatabase(ctx context.Context, requestName types.NamespacedName) *enterprisev4.PostgresDatabase {
	current := &enterprisev4.PostgresDatabase{}
	Expect(k8sClient.Get(ctx, requestName, current)).To(Succeed())
	return current
}

func expectFinalizerAdded(ctx context.Context, requestName types.NamespacedName) *enterprisev4.PostgresDatabase {
	current := fetchPostgresDatabase(ctx, requestName)
	Expect(current.Finalizers).To(ContainElement(postgresDatabaseFinalizer))
	return current
}

func seedExistingDatabaseStatus(ctx context.Context, current *enterprisev4.PostgresDatabase, dbName string) {
	current.Status.Databases = []enterprisev4.DatabaseInfo{{Name: dbName}}
	Expect(k8sClient.Status().Update(ctx, current)).To(Succeed())
}

func expectProvisionedArtifacts(ctx context.Context, scenario readyClusterScenario, owner *enterprisev4.PostgresDatabase) {
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
	Expect(configMap.Data).To(HaveKeyWithValue("rw-host", "tenant-rw."+scenario.namespace+".svc.cluster.local"))
	Expect(configMap.Data).To(HaveKeyWithValue("ro-host", "tenant-ro."+scenario.namespace+".svc.cluster.local"))
	Expect(configMap.Data).To(HaveKeyWithValue("admin-user", adminRoleNameForTest(scenario.dbName)))
	Expect(configMap.Data).To(HaveKeyWithValue("rw-user", rwRoleNameForTest(scenario.dbName)))
	Expect(metav1.IsControlledBy(configMap, owner)).To(BeTrue())
}

func expectManagedRolesPatched(ctx context.Context, scenario readyClusterScenario) {
	updatedCluster := &enterprisev4.PostgresCluster{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: scenario.clusterName, Namespace: scenario.namespace}, updatedCluster)).To(Succeed())
	Expect(managedRoleNames(updatedCluster.Spec.ManagedRoles)).To(ConsistOf(adminRoleNameForTest(scenario.dbName), rwRoleNameForTest(scenario.dbName)))
}

func expectCNPGDatabaseCreated(ctx context.Context, scenario readyClusterScenario, owner *enterprisev4.PostgresDatabase) *cnpgv1.Database {
	cnpgDatabase := &cnpgv1.Database{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cnpgDatabaseNameForTest(scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, cnpgDatabase)).To(Succeed())
	Expect(cnpgDatabase.Spec.Name).To(Equal(scenario.dbName))
	Expect(cnpgDatabase.Spec.Owner).To(Equal(adminRoleNameForTest(scenario.dbName)))
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
	Expect(configMap.Data).To(HaveKeyWithValue("pooler-rw-host", scenario.cnpgClusterName+"-pooler-rw."+scenario.namespace+".svc.cluster.local"))
	Expect(configMap.Data).To(HaveKeyWithValue("pooler-ro-host", scenario.cnpgClusterName+"-pooler-ro."+scenario.namespace+".svc.cluster.local"))
}

func seedMissingClusterScenario(ctx context.Context, namespace, resourceName string, finalizers ...string) types.NamespacedName {
	createPostgresDatabaseResource(ctx, namespace, resourceName, "absent-cluster", []enterprisev4.DatabaseDefinition{{Name: dbAppdb}}, finalizers...)
	return types.NamespacedName{Name: resourceName, Namespace: namespace}
}

func seedConflictScenario(ctx context.Context, namespace, resourceName, clusterName string) types.NamespacedName {
	createPostgresDatabaseResource(ctx, namespace, resourceName, clusterName, []enterprisev4.DatabaseDefinition{{Name: dbAppdb}}, postgresDatabaseFinalizer)
	postgresCluster := createPostgresClusterResource(ctx, namespace, clusterName)
	markPostgresClusterReady(ctx, postgresCluster, "unused-cnpg", namespace, false)
	return types.NamespacedName{Name: resourceName, Namespace: namespace}
}

func seedOwnedDatabaseArtifacts(ctx context.Context, namespace, resourceName, clusterName string, postgresDB *enterprisev4.PostgresDatabase, dbNames ...string) {
	ownerReferences := ownedByPostgresDatabase(postgresDB)
	for _, dbName := range dbNames {
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:            adminSecretNameForTest(resourceName, dbName),
				Namespace:       namespace,
				OwnerReferences: ownerReferences,
			},
		})).To(Succeed())

		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:            rwSecretNameForTest(resourceName, dbName),
				Namespace:       namespace,
				OwnerReferences: ownerReferences,
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

func expectManagedRoleExists(cluster *enterprisev4.PostgresCluster, roleName string, exists bool) {
	rolesByName := make(map[string]enterprisev4.ManagedRole, len(cluster.Spec.ManagedRoles))
	for _, r := range cluster.Spec.ManagedRoles {
		rolesByName[r.Name] = r
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

func expectStatusPhase(current *enterprisev4.PostgresDatabase, expectedPhase string) {
	Expect(current.Status.Phase).NotTo(BeNil())
	Expect(*current.Status.Phase).To(Equal(expectedPhase))
}

func expectStatusCondition(current *enterprisev4.PostgresDatabase, conditionType string, expectedStatus metav1.ConditionStatus, expectedReason string) {
	condition := meta.FindStatusCondition(current.Status.Conditions, conditionType)
	Expect(condition).NotTo(BeNil(), "missing status condition %s", conditionType)
	Expect(condition.Status).To(Equal(expectedStatus), "unexpected status for %s", conditionType)
	Expect(condition.Reason).To(Equal(expectedReason), "unexpected reason for %s", conditionType)
}

func expectReadyStatus(current *enterprisev4.PostgresDatabase, expectedDatabase enterprisev4.DatabaseInfo) {
	expectStatusPhase(current, "Ready")
	Expect(current.Status.Databases).To(HaveLen(1))
	Expect(current.Status.Databases[0].Name).To(Equal(expectedDatabase.Name))
	Expect(current.Status.Databases[0].Ready).To(Equal(expectedDatabase.Ready))
	Expect(current.Status.Databases[0].AdminUserSecretRef).NotTo(BeNil())
	Expect(current.Status.Databases[0].RWUserSecretRef).NotTo(BeNil())
	Expect(current.Status.Databases[0].ConfigMapRef).NotTo(BeNil())
}

func reconcilePostgresDatabaseToReady(ctx context.Context, scenario readyClusterScenario, poolerEnabled bool) *enterprisev4.PostgresDatabase {
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
	expectReadyStatus(current, enterprisev4.DatabaseInfo{Name: scenario.dbName, Ready: true})
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
				expectStatusPhase(current, "Pending")
				expectStatusCondition(current, "ClusterReady", metav1.ConditionFalse, "ClusterNotFound")
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
				expectReadyStatus(current, enterprisev4.DatabaseInfo{Name: scenario.dbName, Ready: true})
				expectStatusCondition(current, "ClusterReady", metav1.ConditionTrue, "ClusterAvailable")
				expectStatusCondition(current, "SecretsReady", metav1.ConditionTrue, "SecretsCreated")
				expectStatusCondition(current, "ConfigMapsReady", metav1.ConditionTrue, "ConfigMapsCreated")
				expectStatusCondition(current, "RolesReady", metav1.ConditionTrue, "UsersAvailable")
				expectStatusCondition(current, "DatabasesReady", metav1.ConditionTrue, "DatabasesAvailable")
				Expect(meta.FindStatusCondition(current.Status.Conditions, "PrivilegesReady")).To(BeNil())
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
		})
	})

	When("owned resource drift occurs after the PostgresDatabase is ready", func() {
		It("repairs configmap content drift", func() {
			scenario := newReadyClusterScenario(namespace, "configmap-drift", "tenant-cluster", "tenant-cnpg", "appdb")
			owner := reconcilePostgresDatabaseToReady(ctx, scenario, false)

			configMap := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-%s-config", scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, configMap)).To(Succeed())
			configMap.Data["rw-host"] = "unexpected.example"
			Expect(k8sClient.Update(ctx, configMap)).To(Succeed())

			result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
			expectEmptyReconcileResult(result, err)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configMap.Name, Namespace: configMap.Namespace}, configMap)).To(Succeed())
			Expect(configMap.Data).To(HaveKeyWithValue("rw-host", "tenant-rw."+scenario.namespace+".svc.cluster.local"))

			current := fetchPostgresDatabase(ctx, scenario.requestName)
			expectReadyStatus(current, enterprisev4.DatabaseInfo{Name: scenario.dbName, Ready: true})
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
			Expect(configMap.Data).To(HaveKeyWithValue("rw-host", "tenant-rw."+scenario.namespace+".svc.cluster.local"))
		})

		It("does not recreate a deleted managed user secret", func() {
			scenario := newReadyClusterScenario(namespace, "secret-delete", "tenant-cluster", "tenant-cnpg", "appdb")
			reconcilePostgresDatabaseToReady(ctx, scenario, false)

			secretName := fmt.Sprintf("%s-%s-admin", scenario.resourceName, scenario.dbName)
			Expect(k8sClient.Delete(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: scenario.namespace},
			})).To(Succeed())

			result, err := reconcilePostgresDatabase(ctx, scenario.requestName)
			expectReconcileResult(result, err, 15*time.Second)

			current := fetchPostgresDatabase(ctx, scenario.requestName)
			expectStatusPhase(current, "Provisioning")
			expectStatusCondition(current, "SecretsReady", metav1.ConditionFalse, "SecretsDriftDetected")

			missing := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: scenario.namespace}, missing)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
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
			expectReadyStatus(current, enterprisev4.DatabaseInfo{Name: scenario.dbName, Ready: true})
		})

		It("creates secrets and configmaps for a newly added database while preserving existing ones", func() {
			scenario := newReadyClusterScenario(namespace, "new-database", "tenant-cluster", "tenant-cnpg", "appdb")
			current := reconcilePostgresDatabaseToReady(ctx, scenario, false)

			current.Spec.Databases = append(current.Spec.Databases, enterprisev4.DatabaseDefinition{Name: "analytics"})
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
			Expect(configMap.Data).To(HaveKeyWithValue("dbname", "analytics"))

			existingSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-%s-admin", scenario.resourceName, scenario.dbName), Namespace: scenario.namespace}, existingSecret)).To(Succeed())
		})
	})

	When("postgresdatabase secondary-resource predicates run", func() {
		It("treats cnpg database applied-state, create, and delete changes as drift triggers", func() {
			pred := postgresDatabaseCNPGDatabasePredicator()

			oldApplied := true
			newApplied := false
			Expect(pred.Create(event.CreateEvent{})).To(BeTrue())
			Expect(pred.Update(event.UpdateEvent{
				ObjectOld: &cnpgv1.Database{Status: cnpgv1.DatabaseStatus{Applied: &oldApplied}},
				ObjectNew: &cnpgv1.Database{Status: cnpgv1.DatabaseStatus{Applied: &newApplied}},
			})).To(BeTrue())
			Expect(pred.Delete(event.DeleteEvent{})).To(BeTrue())
		})

		It("ignores cnpg database updates that do not change readiness or ownership", func() {
			pred := postgresDatabaseCNPGDatabasePredicator()

			applied := true
			Expect(pred.Update(event.UpdateEvent{
				ObjectOld: &cnpgv1.Database{
					ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "test"},
					Status:     cnpgv1.DatabaseStatus{Applied: &applied},
				},
				ObjectNew: &cnpgv1.Database{
					ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "test"},
					Status:     cnpgv1.DatabaseStatus{Applied: &applied},
				},
			})).To(BeFalse())
		})

		It("treats secret create, update, and delete events as drift triggers", func() {
			pred := predicate.ResourceVersionChangedPredicate{}

			Expect(pred.Create(event.CreateEvent{})).To(BeTrue())
			Expect(pred.Update(event.UpdateEvent{
				ObjectOld: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secret", Namespace: "test", ResourceVersion: "1"}},
				ObjectNew: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secret", Namespace: "test", ResourceVersion: "2"}},
			})).To(BeTrue())
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
	})

	When("role ownership conflicts exist", func() {
		It("marks the resource failed and stops provisioning dependent resources", func() {
			resourceName := "conflict-cluster"
			clusterName := "conflict-postgres"
			requestName := seedConflictScenario(ctx, namespace, resourceName, clusterName)

			conflictPatch := &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": enterprisev4.GroupVersion.String(),
					"kind":       "PostgresCluster",
					"metadata": map[string]any{
						"name":      clusterName,
						"namespace": namespace,
					},
					"spec": map[string]any{
						"managedRoles": []map[string]any{
							{"name": adminRoleNameForTest(dbAppdb), "exists": true},
							{"name": rwRoleNameForTest(dbAppdb), "exists": true},
						},
					},
				},
			}
			Expect(k8sClient.Patch(ctx, conflictPatch, client.Apply, client.FieldOwner("postgresdatabase-legacy"))).To(Succeed())

			result, err := reconcilePostgresDatabase(ctx, requestName)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("role conflict detected"))
			Expect(result).To(Equal(ctrl.Result{}))

			current := fetchPostgresDatabase(ctx, requestName)
			expectStatusPhase(current, phaseFailed)
			expectStatusCondition(current, condRolesReady, metav1.ConditionFalse, reasonRoleConflict)

			rolesReady := meta.FindStatusCondition(current.Status.Conditions, condRolesReady)
			Expect(rolesReady.Message).To(ContainSubstring(adminRoleNameForTest(dbAppdb)))
			Expect(rolesReady.Message).To(ContainSubstring("postgresdatabase-legacy"))

			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: configMapNameForTest("conflict-cluster", dbAppdb), Namespace: namespace}, configMap)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			cnpgDatabase := &cnpgv1.Database{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: cnpgDatabaseNameForTest("conflict-cluster", dbAppdb), Namespace: namespace}, cnpgDatabase)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
	})

	When("a database is removed from spec.databases while the CR stays alive", func() {
		It("marks the removed database roles as absent in postgres cluster and keeps the retained roles present", func() {
			resourceName := "live-db-removal"
			clusterName := "live-db-removal-postgres"
			cnpgClusterName := "live-db-removal-cnpg"
			requestName := types.NamespacedName{Name: resourceName, Namespace: namespace}

			postgresDB := createPostgresDatabaseResource(ctx, namespace, resourceName, clusterName, []enterprisev4.DatabaseDefinition{
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

			initialRolesPatch := &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": enterprisev4.GroupVersion.String(),
					"kind":       "PostgresCluster",
					"metadata":   map[string]any{"name": clusterName, "namespace": namespace},
					"spec": map[string]any{
						"managedRoles": []map[string]any{
							{"name": adminRoleNameForTest(dbKeepdb), "exists": true, "passwordSecretRef": map[string]any{"name": resourceName + "-" + dbKeepdb + "-admin", "key": "password"}},
							{"name": rwRoleNameForTest(dbKeepdb), "exists": true, "passwordSecretRef": map[string]any{"name": resourceName + "-" + dbKeepdb + "-rw", "key": "password"}},
							{"name": adminRoleNameForTest(dbDropdb), "exists": true, "passwordSecretRef": map[string]any{"name": resourceName + "-" + dbDropdb + "-admin", "key": "password"}},
							{"name": rwRoleNameForTest(dbDropdb), "exists": true, "passwordSecretRef": map[string]any{"name": resourceName + "-" + dbDropdb + "-rw", "key": "password"}},
						},
					},
				},
			}
			Expect(k8sClient.Patch(ctx, initialRolesPatch, client.Apply, client.FieldOwner("postgresdatabase-"+resourceName))).To(Succeed())

			seedOwnedDatabaseArtifacts(ctx, namespace, resourceName, clusterName, postgresDB, dbKeepdb, dbDropdb)

			postgresDB.Spec.Databases = []enterprisev4.DatabaseDefinition{{Name: dbKeepdb}}
			Expect(k8sClient.Update(ctx, postgresDB)).To(Succeed())

			result, err := reconcilePostgresDatabase(ctx, requestName)
			expectReconcileResult(result, err, 15*time.Second)

			updatedCluster := &enterprisev4.PostgresCluster{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, updatedCluster)).To(Succeed())

			expectManagedRoleExists(updatedCluster, adminRoleNameForTest(dbKeepdb), true)
			expectManagedRoleExists(updatedCluster, rwRoleNameForTest(dbKeepdb), true)
			expectManagedRoleExists(updatedCluster, adminRoleNameForTest(dbDropdb), false)
			expectManagedRoleExists(updatedCluster, rwRoleNameForTest(dbDropdb), false)
		})
	})

	When("the PostgresDatabase is being deleted", func() {
		Context("with retained and deleted databases", func() {
			It("orphans retained resources, removes deleted resources, and patches managed roles", func() {
				resourceName := "delete-cluster"
				clusterName := "delete-postgres"
				requestName := types.NamespacedName{Name: resourceName, Namespace: namespace}

				postgresDB := createPostgresDatabaseResource(ctx, namespace, resourceName, clusterName, []enterprisev4.DatabaseDefinition{
					{Name: dbKeepdb, DeletionPolicy: "Retain"},
					{Name: dbDropdb},
				}, postgresDatabaseFinalizer)
				Expect(k8sClient.Get(ctx, requestName, postgresDB)).To(Succeed())

				createPostgresClusterResource(ctx, namespace, clusterName)

				initialRolesPatch := &unstructured.Unstructured{
					Object: map[string]any{
						"apiVersion": enterprisev4.GroupVersion.String(),
						"kind":       "PostgresCluster",
						"metadata": map[string]any{
							"name":      clusterName,
							"namespace": namespace,
						},
						"spec": map[string]any{
							"managedRoles": []map[string]any{
								{"name": adminRoleNameForTest(dbKeepdb), "exists": true, "passwordSecretRef": map[string]any{"name": resourceName + "-" + dbKeepdb + "-admin", "key": "password"}},
								{"name": rwRoleNameForTest(dbKeepdb), "exists": true, "passwordSecretRef": map[string]any{"name": resourceName + "-" + dbKeepdb + "-rw", "key": "password"}},
								{"name": adminRoleNameForTest(dbDropdb), "exists": true, "passwordSecretRef": map[string]any{"name": resourceName + "-" + dbDropdb + "-admin", "key": "password"}},
								{"name": rwRoleNameForTest(dbDropdb), "exists": true, "passwordSecretRef": map[string]any{"name": resourceName + "-" + dbDropdb + "-rw", "key": "password"}},
							},
						},
					},
				}
				Expect(k8sClient.Patch(ctx, initialRolesPatch, client.Apply, client.FieldOwner("postgresdatabase-"+resourceName))).To(Succeed())

				seedOwnedDatabaseArtifacts(ctx, namespace, resourceName, clusterName, postgresDB, dbKeepdb, dbDropdb)

				Expect(k8sClient.Delete(ctx, postgresDB)).To(Succeed())

				result, err := reconcilePostgresDatabase(ctx, requestName)
				expectEmptyReconcileResult(result, err)

				expectRetainedArtifact(ctx, configMapNameForTest(resourceName, dbKeepdb), namespace, resourceName, &corev1.ConfigMap{})
				expectRetainedArtifact(ctx, adminSecretNameForTest(resourceName, dbKeepdb), namespace, resourceName, &corev1.Secret{})
				expectRetainedArtifact(ctx, rwSecretNameForTest(resourceName, dbKeepdb), namespace, resourceName, &corev1.Secret{})
				expectRetainedArtifact(ctx, cnpgDatabaseNameForTest(resourceName, dbKeepdb), namespace, resourceName, &cnpgv1.Database{})

				expectDeletedArtifact(ctx, configMapNameForTest(resourceName, dbDropdb), namespace, &corev1.ConfigMap{})
				expectDeletedArtifact(ctx, adminSecretNameForTest(resourceName, dbDropdb), namespace, &corev1.Secret{})
				expectDeletedArtifact(ctx, rwSecretNameForTest(resourceName, dbDropdb), namespace, &corev1.Secret{})
				expectDeletedArtifact(ctx, cnpgDatabaseNameForTest(resourceName, dbDropdb), namespace, &cnpgv1.Database{})

				updatedCluster := &enterprisev4.PostgresCluster{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, updatedCluster)).To(Succeed())

				expectManagedRoleExists(updatedCluster, adminRoleNameForTest(dbKeepdb), true)
				expectManagedRoleExists(updatedCluster, rwRoleNameForTest(dbKeepdb), true)
				expectManagedRoleExists(updatedCluster, adminRoleNameForTest(dbDropdb), false)
				expectManagedRoleExists(updatedCluster, rwRoleNameForTest(dbDropdb), false)

				current := &enterprisev4.PostgresDatabase{}
				err = k8sClient.Get(ctx, requestName, current)
				Expect(apierrors.IsNotFound(err) || !slices.Contains(current.Finalizers, postgresDatabaseFinalizer)).To(BeTrue())
			})
		})
	})
})
