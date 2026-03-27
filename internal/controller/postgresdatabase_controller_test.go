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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const postgresDatabaseFinalizer = "postgresdatabases.enterprise.splunk.com/finalizer"

func reconcilePostgresDatabase(ctx context.Context, nn types.NamespacedName) (ctrl.Result, error) {
	reconciler := &PostgresDatabaseReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
	}
	return reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
}

func controlledBy(owner metav1.Object, obj metav1.Object) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID != owner.GetUID() {
			continue
		}
		if ref.Controller != nil && *ref.Controller {
			return true
		}
	}
	return false
}

func namespacedName(namespace, name string) types.NamespacedName {
	return types.NamespacedName{Name: name, Namespace: namespace}
}

func managedRoleNames(roles []enterprisev4.ManagedRole) []string {
	names := make([]string, 0, len(roles))
	for _, role := range roles {
		names = append(names, role.Name)
	}
	return names
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

func startPostgresDatabaseManager(ctx context.Context) {
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 k8sClient.Scheme(),
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
	})
	Expect(err).NotTo(HaveOccurred())
	Expect((&PostgresDatabaseReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr)).To(Succeed())

	managerCtx, cancel := context.WithCancel(ctx)
	errCh := make(chan error, 1)
	go func() {
		errCh <- mgr.Start(managerCtx)
	}()

	DeferCleanup(func() {
		cancel()
		Eventually(errCh, time.Second).Should(Receive(BeNil()))
	})
}

var _ = Describe("PostgresDatabase Controller", func() {
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

	It("adds a finalizer before processing a missing cluster", func() {
		resourceName := "missing-cluster"
		requestName := namespacedName(namespace, resourceName)

		postgresDB := &enterprisev4.PostgresDatabase{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: enterprisev4.PostgresDatabaseSpec{
				ClusterRef: corev1.LocalObjectReference{Name: "absent-cluster"},
				Databases: []enterprisev4.DatabaseDefinition{
					{Name: "appdb"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, postgresDB)).To(Succeed())

		result, err := reconcilePostgresDatabase(ctx, requestName)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ctrl.Result{}))

		current := &enterprisev4.PostgresDatabase{}
		Expect(k8sClient.Get(ctx, requestName, current)).To(Succeed())
		Expect(current.Finalizers).To(ContainElement(postgresDatabaseFinalizer))

		result, err = reconcilePostgresDatabase(ctx, requestName)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(30 * time.Second))

		Expect(k8sClient.Get(ctx, requestName, current)).To(Succeed())
		Expect(current.Status.Phase).NotTo(BeNil())
		Expect(*current.Status.Phase).To(Equal("Pending"))

		clusterReady := meta.FindStatusCondition(current.Status.Conditions, "ClusterReady")
		Expect(clusterReady).NotTo(BeNil())
		Expect(clusterReady.Status).To(Equal(metav1.ConditionFalse))
		Expect(clusterReady.Reason).To(Equal("ClusterNotFound"))
		Expect(clusterReady.ObservedGeneration).To(Equal(current.Generation))
	})

	It("reconciles Kubernetes resources for a ready cluster without invoking live grants", func() {
		resourceName := "ready-cluster"
		clusterName := "tenant-cluster"
		cnpgClusterName := "tenant-cnpg"
		dbName := "appdb"
		requestName := namespacedName(namespace, resourceName)

		postgresDB := &enterprisev4.PostgresDatabase{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: enterprisev4.PostgresDatabaseSpec{
				ClusterRef: corev1.LocalObjectReference{Name: clusterName},
				Databases: []enterprisev4.DatabaseDefinition{
					{Name: dbName},
				},
			},
		}
		Expect(k8sClient.Create(ctx, postgresDB)).To(Succeed())

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

		clusterPhase := "Ready"
		postgresCluster.Status.Phase = &clusterPhase
		postgresCluster.Status.ProvisionerRef = &corev1.ObjectReference{
			APIVersion: cnpgv1.SchemeGroupVersion.String(),
			Kind:       "Cluster",
			Name:       cnpgClusterName,
			Namespace:  namespace,
		}
		Expect(k8sClient.Status().Update(ctx, postgresCluster)).To(Succeed())

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

		cnpgCluster.Status.ManagedRolesStatus = cnpgv1.ManagedRoles{
			ByStatus: map[cnpgv1.RoleStatus][]string{
				cnpgv1.RoleStatusReconciled: {"appdb_admin", "appdb_rw"},
			},
		}
		cnpgCluster.Status.WriteService = "tenant-rw"
		cnpgCluster.Status.ReadService = "tenant-ro"
		Expect(k8sClient.Status().Update(ctx, cnpgCluster)).To(Succeed())

		result, err := reconcilePostgresDatabase(ctx, requestName)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ctrl.Result{}))

		current := &enterprisev4.PostgresDatabase{}
		Expect(k8sClient.Get(ctx, requestName, current)).To(Succeed())

		current.Status.Databases = []enterprisev4.DatabaseInfo{{Name: dbName}}
		Expect(k8sClient.Status().Update(ctx, current)).To(Succeed())

		result, err = reconcilePostgresDatabase(ctx, requestName)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(15 * time.Second))

		adminSecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, namespacedName(namespace, "ready-cluster-appdb-admin"), adminSecret)).To(Succeed())
		Expect(adminSecret.Data).To(HaveKey("password"))
		Expect(controlledBy(current, adminSecret)).To(BeTrue())

		rwSecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, namespacedName(namespace, "ready-cluster-appdb-rw"), rwSecret)).To(Succeed())
		Expect(rwSecret.Data).To(HaveKey("password"))
		Expect(controlledBy(current, rwSecret)).To(BeTrue())

		configMap := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, namespacedName(namespace, "ready-cluster-appdb-config"), configMap)).To(Succeed())
		Expect(configMap.Data).To(HaveKeyWithValue("rw-host", "tenant-rw."+namespace+".svc.cluster.local"))
		Expect(configMap.Data).To(HaveKeyWithValue("ro-host", "tenant-ro."+namespace+".svc.cluster.local"))
		Expect(configMap.Data).To(HaveKeyWithValue("admin-user", "appdb_admin"))
		Expect(configMap.Data).To(HaveKeyWithValue("rw-user", "appdb_rw"))
		Expect(controlledBy(current, configMap)).To(BeTrue())

		updatedCluster := &enterprisev4.PostgresCluster{}
		Expect(k8sClient.Get(ctx, namespacedName(namespace, clusterName), updatedCluster)).To(Succeed())
		Expect(updatedCluster.Spec.ManagedRoles).To(HaveLen(2))
		Expect([]string{
			updatedCluster.Spec.ManagedRoles[0].Name,
			updatedCluster.Spec.ManagedRoles[1].Name,
		}).To(ConsistOf("appdb_admin", "appdb_rw"))

		result, err = reconcilePostgresDatabase(ctx, requestName)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(15 * time.Second))

		cnpgDatabase := &cnpgv1.Database{}
		Expect(k8sClient.Get(ctx, namespacedName(namespace, "ready-cluster-appdb"), cnpgDatabase)).To(Succeed())
		Expect(cnpgDatabase.Spec.Name).To(Equal(dbName))
		Expect(cnpgDatabase.Spec.Owner).To(Equal("appdb_admin"))
		Expect(cnpgDatabase.Spec.ClusterRef.Name).To(Equal(cnpgClusterName))
		Expect(controlledBy(current, cnpgDatabase)).To(BeTrue())

		applied := true
		cnpgDatabase.Status.Applied = &applied
		Expect(k8sClient.Status().Update(ctx, cnpgDatabase)).To(Succeed())

		result, err = reconcilePostgresDatabase(ctx, requestName)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ctrl.Result{}))

		Expect(k8sClient.Get(ctx, requestName, current)).To(Succeed())
		Expect(current.Status.Phase).NotTo(BeNil())
		Expect(*current.Status.Phase).To(Equal("Ready"))
		Expect(current.Status.ObservedGeneration).NotTo(BeNil())
		Expect(*current.Status.ObservedGeneration).To(Equal(current.Generation))
		Expect(current.Status.Databases).To(HaveLen(1))
		Expect(current.Status.Databases[0].Name).To(Equal(dbName))
		Expect(current.Status.Databases[0].Ready).To(BeTrue())
		Expect(current.Status.Databases[0].AdminUserSecretRef).NotTo(BeNil())
		Expect(current.Status.Databases[0].RWUserSecretRef).NotTo(BeNil())
		Expect(current.Status.Databases[0].ConfigMapRef).NotTo(BeNil())

		for _, conditionType := range []string{"ClusterReady", "SecretsReady", "ConfigMapsReady", "RolesReady", "DatabasesReady"} {
			condition := meta.FindStatusCondition(current.Status.Conditions, conditionType)
			Expect(condition).NotTo(BeNil(), "missing status condition %s", conditionType)
			Expect(condition.Status).To(Equal(metav1.ConditionTrue), "unexpected status for %s", conditionType)
		}

		Expect(meta.FindStatusCondition(current.Status.Conditions, "PrivilegesReady")).To(BeNil())
	})

	It("completes the ready-cluster workflow under a running manager and progresses on requeue", func() {
		shortPollingInterval := 10 * time.Millisecond
		steadyPollingInterval := 25 * time.Millisecond
		shortTimeout := 5 * time.Second
		readyDatabaseTimeout := 25 * time.Second
		workflowCompletionTimeout := 10 * time.Second

		resourceName := "manager-ready-cluster"
		clusterName := "manager-tenant-cluster"
		cnpgClusterName := "manager-tenant-cnpg"
		dbName := "appdb"
		requestName := namespacedName(namespace, resourceName)

		postgresDB := &enterprisev4.PostgresDatabase{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: enterprisev4.PostgresDatabaseSpec{
				ClusterRef: corev1.LocalObjectReference{Name: clusterName},
				Databases: []enterprisev4.DatabaseDefinition{
					{Name: dbName},
				},
			},
		}
		Expect(k8sClient.Create(ctx, postgresDB)).To(Succeed())
		Expect(k8sClient.Get(ctx, requestName, postgresDB)).To(Succeed())
		postgresDB.Status.Databases = []enterprisev4.DatabaseInfo{{Name: dbName}}
		Expect(k8sClient.Status().Update(ctx, postgresDB)).To(Succeed())

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

		clusterPhase := "Ready"
		postgresCluster.Status.Phase = &clusterPhase
		postgresCluster.Status.ProvisionerRef = &corev1.ObjectReference{
			APIVersion: cnpgv1.SchemeGroupVersion.String(),
			Kind:       "Cluster",
			Name:       cnpgClusterName,
			Namespace:  namespace,
		}
		Expect(k8sClient.Status().Update(ctx, postgresCluster)).To(Succeed())

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
		cnpgCluster.Status.ManagedRolesStatus = cnpgv1.ManagedRoles{
			ByStatus: map[cnpgv1.RoleStatus][]string{
				cnpgv1.RoleStatusReconciled: {"appdb_admin", "appdb_rw"},
			},
		}
		cnpgCluster.Status.WriteService = "tenant-rw"
		cnpgCluster.Status.ReadService = "tenant-ro"
		Expect(k8sClient.Status().Update(ctx, cnpgCluster)).To(Succeed())

		startPostgresDatabaseManager(ctx)

		Eventually(func(g Gomega) {
			current := &enterprisev4.PostgresDatabase{}
			g.Expect(k8sClient.Get(ctx, requestName, current)).To(Succeed())
			g.Expect(current.Finalizers).To(ContainElement(postgresDatabaseFinalizer))
		}).WithTimeout(shortTimeout).WithPolling(shortPollingInterval).Should(Succeed())

		Eventually(func(g Gomega) {
			adminSecret := &corev1.Secret{}
			g.Expect(k8sClient.Get(ctx, namespacedName(namespace, "manager-ready-cluster-appdb-admin"), adminSecret)).To(Succeed())

			rwSecret := &corev1.Secret{}
			g.Expect(k8sClient.Get(ctx, namespacedName(namespace, "manager-ready-cluster-appdb-rw"), rwSecret)).To(Succeed())

			configMap := &corev1.ConfigMap{}
			g.Expect(k8sClient.Get(ctx, namespacedName(namespace, "manager-ready-cluster-appdb-config"), configMap)).To(Succeed())
			g.Expect(configMap.Data).To(HaveKeyWithValue("rw-host", "tenant-rw."+namespace+".svc.cluster.local"))
			g.Expect(configMap.Data).To(HaveKeyWithValue("ro-host", "tenant-ro."+namespace+".svc.cluster.local"))
		}).WithTimeout(shortTimeout).WithPolling(shortPollingInterval).Should(Succeed())

		Eventually(func(g Gomega) {
			updatedCluster := &enterprisev4.PostgresCluster{}
			g.Expect(k8sClient.Get(ctx, namespacedName(namespace, clusterName), updatedCluster)).To(Succeed())
			g.Expect(managedRoleNames(updatedCluster.Spec.ManagedRoles)).To(ConsistOf("appdb_admin", "appdb_rw"))
		}).WithTimeout(shortTimeout).WithPolling(shortPollingInterval).Should(Succeed())

		Eventually(func(g Gomega) {
			cnpgDatabase := &cnpgv1.Database{}
			g.Expect(k8sClient.Get(ctx, namespacedName(namespace, "manager-ready-cluster-appdb"), cnpgDatabase)).To(Succeed())
			g.Expect(cnpgDatabase.Spec.Name).To(Equal(dbName))
			g.Expect(cnpgDatabase.Spec.Owner).To(Equal("appdb_admin"))
			g.Expect(cnpgDatabase.Spec.ClusterRef.Name).To(Equal(cnpgClusterName))

			applied := true
			if cnpgDatabase.Status.Applied == nil || !*cnpgDatabase.Status.Applied {
				cnpgDatabase.Status.Applied = &applied
				g.Expect(k8sClient.Status().Update(ctx, cnpgDatabase)).To(Succeed())
			}
		}).WithTimeout(readyDatabaseTimeout).WithPolling(steadyPollingInterval).Should(Succeed())

		Eventually(func(g Gomega) {
			current := &enterprisev4.PostgresDatabase{}
			g.Expect(k8sClient.Get(ctx, requestName, current)).To(Succeed())
			g.Expect(current.Status.Phase).NotTo(BeNil())
			g.Expect(*current.Status.Phase).To(Equal("Ready"))
			g.Expect(current.Status.ObservedGeneration).NotTo(BeNil())
			g.Expect(*current.Status.ObservedGeneration).To(Equal(current.Generation))
			g.Expect(current.Status.Databases).To(HaveLen(1))
			g.Expect(current.Status.Databases[0].Name).To(Equal(dbName))
			g.Expect(current.Status.Databases[0].Ready).To(BeTrue())

			for _, conditionType := range []string{"ClusterReady", "SecretsReady", "ConfigMapsReady", "RolesReady", "DatabasesReady"} {
				condition := meta.FindStatusCondition(current.Status.Conditions, conditionType)
				g.Expect(condition).NotTo(BeNil(), "missing status condition %s", conditionType)
				g.Expect(condition.Status).To(Equal(metav1.ConditionTrue), "unexpected status for %s", conditionType)
			}
		}).WithTimeout(workflowCompletionTimeout).WithPolling(steadyPollingInterval).Should(Succeed())
	})

	It("marks role conflicts in status and stops before provisioning dependent resources", func() {
		resourceName := "conflict-cluster"
		clusterName := "conflict-postgres"
		requestName := namespacedName(namespace, resourceName)

		postgresDB := &enterprisev4.PostgresDatabase{
			ObjectMeta: metav1.ObjectMeta{
				Name:       resourceName,
				Namespace:  namespace,
				Finalizers: []string{postgresDatabaseFinalizer},
			},
			Spec: enterprisev4.PostgresDatabaseSpec{
				ClusterRef: corev1.LocalObjectReference{Name: clusterName},
				Databases: []enterprisev4.DatabaseDefinition{
					{Name: "appdb"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, postgresDB)).To(Succeed())

		clusterPhase := "Ready"
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
		postgresCluster.Status.Phase = &clusterPhase
		postgresCluster.Status.ProvisionerRef = &corev1.ObjectReference{
			APIVersion: cnpgv1.SchemeGroupVersion.String(),
			Kind:       "Cluster",
			Name:       "unused-cnpg",
			Namespace:  namespace,
		}
		Expect(k8sClient.Status().Update(ctx, postgresCluster)).To(Succeed())

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
						{"name": "appdb_admin", "exists": true},
						{"name": "appdb_rw", "exists": true},
					},
				},
			},
		}
		Expect(k8sClient.Patch(ctx, conflictPatch, client.Apply, client.FieldOwner("postgresdatabase-legacy"))).To(Succeed())

		result, err := reconcilePostgresDatabase(ctx, requestName)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ctrl.Result{}))

		current := &enterprisev4.PostgresDatabase{}
		Expect(k8sClient.Get(ctx, requestName, current)).To(Succeed())
		Expect(current.Status.Phase).NotTo(BeNil())
		Expect(*current.Status.Phase).To(Equal("Failed"))

		rolesReady := meta.FindStatusCondition(current.Status.Conditions, "RolesReady")
		Expect(rolesReady).NotTo(BeNil())
		Expect(rolesReady.Status).To(Equal(metav1.ConditionFalse))
		Expect(rolesReady.Reason).To(Equal("RoleConflict"))
		Expect(rolesReady.Message).To(ContainSubstring("appdb_admin"))
		Expect(rolesReady.Message).To(ContainSubstring("postgresdatabase-legacy"))

		configMap := &corev1.ConfigMap{}
		err = k8sClient.Get(ctx, namespacedName(namespace, "conflict-cluster-appdb-config"), configMap)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())

		cnpgDatabase := &cnpgv1.Database{}
		err = k8sClient.Get(ctx, namespacedName(namespace, "conflict-cluster-appdb"), cnpgDatabase)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("orphan-retains selected resources, deletes others, and patches managed roles on deletion", func() {
		resourceName := "delete-cluster"
		clusterName := "delete-postgres"
		requestName := namespacedName(namespace, resourceName)

		postgresDB := &enterprisev4.PostgresDatabase{
			ObjectMeta: metav1.ObjectMeta{
				Name:       resourceName,
				Namespace:  namespace,
				Finalizers: []string{postgresDatabaseFinalizer},
			},
			Spec: enterprisev4.PostgresDatabaseSpec{
				ClusterRef: corev1.LocalObjectReference{Name: clusterName},
				Databases: []enterprisev4.DatabaseDefinition{
					{Name: "keepdb", DeletionPolicy: "Retain"},
					{Name: "dropdb"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, postgresDB)).To(Succeed())
		Expect(k8sClient.Get(ctx, requestName, postgresDB)).To(Succeed())

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
						{"name": "keepdb_admin", "exists": true, "passwordSecretRef": map[string]any{"name": "delete-cluster-keepdb-admin", "key": "password"}},
						{"name": "keepdb_rw", "exists": true, "passwordSecretRef": map[string]any{"name": "delete-cluster-keepdb-rw", "key": "password"}},
						{"name": "dropdb_admin", "exists": true, "passwordSecretRef": map[string]any{"name": "delete-cluster-dropdb-admin", "key": "password"}},
						{"name": "dropdb_rw", "exists": true, "passwordSecretRef": map[string]any{"name": "delete-cluster-dropdb-rw", "key": "password"}},
					},
				},
			},
		}
		Expect(k8sClient.Patch(ctx, initialRolesPatch, client.Apply, client.FieldOwner("postgresdatabase-delete-cluster"))).To(Succeed())

		for _, dbName := range []string{"keepdb", "dropdb"} {
			adminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            fmt.Sprintf("%s-%s-admin", resourceName, dbName),
					Namespace:       namespace,
					OwnerReferences: ownedByPostgresDatabase(postgresDB),
				},
			}
			Expect(k8sClient.Create(ctx, adminSecret)).To(Succeed())

			rwSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            fmt.Sprintf("%s-%s-rw", resourceName, dbName),
					Namespace:       namespace,
					OwnerReferences: ownedByPostgresDatabase(postgresDB),
				},
			}
			Expect(k8sClient.Create(ctx, rwSecret)).To(Succeed())

			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:            fmt.Sprintf("%s-%s-config", resourceName, dbName),
					Namespace:       namespace,
					OwnerReferences: ownedByPostgresDatabase(postgresDB),
				},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			cnpgDatabase := &cnpgv1.Database{
				ObjectMeta: metav1.ObjectMeta{
					Name:            fmt.Sprintf("%s-%s", resourceName, dbName),
					Namespace:       namespace,
					OwnerReferences: ownedByPostgresDatabase(postgresDB),
				},
				Spec: cnpgv1.DatabaseSpec{
					ClusterRef: corev1.LocalObjectReference{Name: clusterName},
					Name:       dbName,
					Owner:      fmt.Sprintf("%s_admin", dbName),
				},
			}
			Expect(k8sClient.Create(ctx, cnpgDatabase)).To(Succeed())
		}

		Expect(k8sClient.Delete(ctx, postgresDB)).To(Succeed())

		result, err := reconcilePostgresDatabase(ctx, requestName)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ctrl.Result{}))

		retainedConfigMap := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, namespacedName(namespace, "delete-cluster-keepdb-config"), retainedConfigMap)).To(Succeed())
		Expect(retainedConfigMap.Annotations).To(HaveKeyWithValue("enterprise.splunk.com/retained-from", resourceName))
		Expect(retainedConfigMap.OwnerReferences).To(BeEmpty())

		retainedAdminSecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, namespacedName(namespace, "delete-cluster-keepdb-admin"), retainedAdminSecret)).To(Succeed())
		Expect(retainedAdminSecret.Annotations).To(HaveKeyWithValue("enterprise.splunk.com/retained-from", resourceName))
		Expect(retainedAdminSecret.OwnerReferences).To(BeEmpty())

		retainedRWSecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, namespacedName(namespace, "delete-cluster-keepdb-rw"), retainedRWSecret)).To(Succeed())
		Expect(retainedRWSecret.Annotations).To(HaveKeyWithValue("enterprise.splunk.com/retained-from", resourceName))
		Expect(retainedRWSecret.OwnerReferences).To(BeEmpty())

		retainedCNPGDatabase := &cnpgv1.Database{}
		Expect(k8sClient.Get(ctx, namespacedName(namespace, "delete-cluster-keepdb"), retainedCNPGDatabase)).To(Succeed())
		Expect(retainedCNPGDatabase.Annotations).To(HaveKeyWithValue("enterprise.splunk.com/retained-from", resourceName))
		Expect(retainedCNPGDatabase.OwnerReferences).To(BeEmpty())

		for _, name := range []string{
			"delete-cluster-dropdb-config",
			"delete-cluster-dropdb-admin",
			"delete-cluster-dropdb-rw",
		} {
			err = k8sClient.Get(ctx, namespacedName(namespace, name), &corev1.Secret{})
			if name == "delete-cluster-dropdb-config" {
				err = k8sClient.Get(ctx, namespacedName(namespace, name), &corev1.ConfigMap{})
			}
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected %s to be deleted", name)
		}

		err = k8sClient.Get(ctx, namespacedName(namespace, "delete-cluster-dropdb"), &cnpgv1.Database{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())

		updatedCluster := &enterprisev4.PostgresCluster{}
		Expect(k8sClient.Get(ctx, namespacedName(namespace, clusterName), updatedCluster)).To(Succeed())
		Expect(managedRoleNames(updatedCluster.Spec.ManagedRoles)).To(ConsistOf("keepdb_admin", "keepdb_rw"))

		current := &enterprisev4.PostgresDatabase{}
		err = k8sClient.Get(ctx, requestName, current)
		Expect(apierrors.IsNotFound(err) || !containsFinalizer(current.Finalizers, postgresDatabaseFinalizer)).To(BeTrue())
	})

	It("adds pooler endpoints to the generated ConfigMap when connection pooling is enabled", func() {
		resourceName := "pooler-cluster"
		clusterName := "pooler-postgres"
		cnpgClusterName := "pooler-cnpg"
		dbName := "appdb"
		requestName := namespacedName(namespace, resourceName)

		postgresDB := &enterprisev4.PostgresDatabase{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: namespace,
			},
			Spec: enterprisev4.PostgresDatabaseSpec{
				ClusterRef: corev1.LocalObjectReference{Name: clusterName},
				Databases: []enterprisev4.DatabaseDefinition{
					{Name: dbName},
				},
			},
		}
		Expect(k8sClient.Create(ctx, postgresDB)).To(Succeed())

		clusterPhase := "Ready"
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

		postgresCluster.Status.Phase = &clusterPhase
		postgresCluster.Status.ProvisionerRef = &corev1.ObjectReference{
			APIVersion: cnpgv1.SchemeGroupVersion.String(),
			Kind:       "Cluster",
			Name:       cnpgClusterName,
			Namespace:  namespace,
		}
		postgresCluster.Status.ConnectionPoolerStatus = &enterprisev4.ConnectionPoolerStatus{Enabled: true}
		Expect(k8sClient.Status().Update(ctx, postgresCluster)).To(Succeed())

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
		cnpgCluster.Status.ManagedRolesStatus = cnpgv1.ManagedRoles{
			ByStatus: map[cnpgv1.RoleStatus][]string{
				cnpgv1.RoleStatusReconciled: {"appdb_admin", "appdb_rw"},
			},
		}
		cnpgCluster.Status.WriteService = "tenant-rw"
		cnpgCluster.Status.ReadService = "tenant-ro"
		Expect(k8sClient.Status().Update(ctx, cnpgCluster)).To(Succeed())

		result, err := reconcilePostgresDatabase(ctx, requestName)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ctrl.Result{}))

		current := &enterprisev4.PostgresDatabase{}
		Expect(k8sClient.Get(ctx, requestName, current)).To(Succeed())
		current.Status.Databases = []enterprisev4.DatabaseInfo{{Name: dbName}}
		Expect(k8sClient.Status().Update(ctx, current)).To(Succeed())

		result, err = reconcilePostgresDatabase(ctx, requestName)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(15 * time.Second))

		configMap := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, namespacedName(namespace, "pooler-cluster-appdb-config"), configMap)).To(Succeed())
		Expect(configMap.Data).To(HaveKeyWithValue("pooler-rw-host", cnpgClusterName+"-pooler-rw."+namespace+".svc.cluster.local"))
		Expect(configMap.Data).To(HaveKeyWithValue("pooler-ro-host", cnpgClusterName+"-pooler-ro."+namespace+".svc.cluster.local"))
	})
})

func containsFinalizer(finalizers []string, target string) bool {
	return slices.Contains(finalizers, target)
}
