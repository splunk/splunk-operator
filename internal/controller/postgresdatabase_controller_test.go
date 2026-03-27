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

func expectReadyStatus(current *enterprisev4.PostgresDatabase, generation int64, expectedDatabase enterprisev4.DatabaseInfo) {
	expectStatusPhase(current, "Ready")
	Expect(current.Status.ObservedGeneration).NotTo(BeNil())
	Expect(*current.Status.ObservedGeneration).To(Equal(generation))
	Expect(current.Status.Databases).To(HaveLen(1))
	Expect(current.Status.Databases[0].Name).To(Equal(expectedDatabase.Name))
	Expect(current.Status.Databases[0].Ready).To(Equal(expectedDatabase.Ready))
	Expect(current.Status.Databases[0].AdminUserSecretRef).NotTo(BeNil())
	Expect(current.Status.Databases[0].RWUserSecretRef).NotTo(BeNil())
	Expect(current.Status.Databases[0].ConfigMapRef).NotTo(BeNil())

	for _, expected := range []struct {
		conditionType string
		reason        string
	}{
		{conditionType: "ClusterReady", reason: "ClusterAvailable"},
		{conditionType: "SecretsReady", reason: "SecretsCreated"},
		{conditionType: "ConfigMapsReady", reason: "ConfigMapsCreated"},
		{conditionType: "RolesReady", reason: "UsersAvailable"},
		{conditionType: "DatabasesReady", reason: "DatabasesAvailable"},
	} {
		expectStatusCondition(current, expected.conditionType, metav1.ConditionTrue, expected.reason)
	}

	Expect(meta.FindStatusCondition(current.Status.Conditions, "PrivilegesReady")).To(BeNil())
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

	When("the referenced PostgresCluster is missing", func() {
		Context("on the first reconcile", func() {
			It("adds the finalizer", func() {
				resourceName := "missing-cluster"
				requestName := types.NamespacedName{Name: resourceName, Namespace: namespace}

				createPostgresDatabaseResource(ctx, namespace, resourceName, "absent-cluster", []enterprisev4.DatabaseDefinition{{Name: "appdb"}})

				result, err := reconcilePostgresDatabase(ctx, requestName)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(ctrl.Result{}))

				current := &enterprisev4.PostgresDatabase{}
				Expect(k8sClient.Get(ctx, requestName, current)).To(Succeed())
				Expect(current.Finalizers).To(ContainElement(postgresDatabaseFinalizer))
			})
		})

		Context("after the finalizer is already present", func() {
			It("reports ClusterNotFound and requeues", func() {
				resourceName := "missing-cluster-with-finalizer"
				requestName := types.NamespacedName{Name: resourceName, Namespace: namespace}

				createPostgresDatabaseResource(ctx, namespace, resourceName, "absent-cluster", []enterprisev4.DatabaseDefinition{{Name: "appdb"}}, postgresDatabaseFinalizer)

				result, err := reconcilePostgresDatabase(ctx, requestName)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.RequeueAfter).To(Equal(30 * time.Second))

				current := &enterprisev4.PostgresDatabase{}
				Expect(k8sClient.Get(ctx, requestName, current)).To(Succeed())
				expectStatusPhase(current, "Pending")
				expectStatusCondition(current, "ClusterReady", metav1.ConditionFalse, "ClusterNotFound")
				clusterReady := meta.FindStatusCondition(current.Status.Conditions, "ClusterReady")
				Expect(clusterReady.ObservedGeneration).To(Equal(current.Generation))
			})
		})
	})

	When("the referenced PostgresCluster is ready", func() {
		Context("and live grants are not invoked", func() {
			It("reconciles secrets, configmaps, roles, and CNPG databases", func() {
				resourceName := "ready-cluster"
				clusterName := "tenant-cluster"
				cnpgClusterName := "tenant-cnpg"
				dbName := "appdb"
				requestName := types.NamespacedName{Name: resourceName, Namespace: namespace}

				createPostgresDatabaseResource(ctx, namespace, resourceName, clusterName, []enterprisev4.DatabaseDefinition{{Name: dbName}})
				postgresCluster := createPostgresClusterResource(ctx, namespace, clusterName)
				markPostgresClusterReady(ctx, postgresCluster, cnpgClusterName, namespace, false)
				cnpgCluster := createCNPGClusterResource(ctx, namespace, cnpgClusterName)
				markCNPGClusterReady(ctx, cnpgCluster, []string{"appdb_admin", "appdb_rw"}, "tenant-rw", "tenant-ro")

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
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "ready-cluster-appdb-admin", Namespace: namespace}, adminSecret)).To(Succeed())
				Expect(adminSecret.Data).To(HaveKey("password"))
				Expect(metav1.IsControlledBy(adminSecret, current)).To(BeTrue())

				rwSecret := &corev1.Secret{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "ready-cluster-appdb-rw", Namespace: namespace}, rwSecret)).To(Succeed())
				Expect(rwSecret.Data).To(HaveKey("password"))
				Expect(metav1.IsControlledBy(rwSecret, current)).To(BeTrue())

				configMap := &corev1.ConfigMap{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "ready-cluster-appdb-config", Namespace: namespace}, configMap)).To(Succeed())
				Expect(configMap.Data).To(HaveKeyWithValue("rw-host", "tenant-rw."+namespace+".svc.cluster.local"))
				Expect(configMap.Data).To(HaveKeyWithValue("ro-host", "tenant-ro."+namespace+".svc.cluster.local"))
				Expect(configMap.Data).To(HaveKeyWithValue("admin-user", "appdb_admin"))
				Expect(configMap.Data).To(HaveKeyWithValue("rw-user", "appdb_rw"))
				Expect(metav1.IsControlledBy(configMap, current)).To(BeTrue())

				updatedCluster := &enterprisev4.PostgresCluster{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, updatedCluster)).To(Succeed())
				Expect(updatedCluster.Spec.ManagedRoles).To(HaveLen(2))
				Expect([]string{
					updatedCluster.Spec.ManagedRoles[0].Name,
					updatedCluster.Spec.ManagedRoles[1].Name,
				}).To(ConsistOf("appdb_admin", "appdb_rw"))

				result, err = reconcilePostgresDatabase(ctx, requestName)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.RequeueAfter).To(Equal(15 * time.Second))

				cnpgDatabase := &cnpgv1.Database{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "ready-cluster-appdb", Namespace: namespace}, cnpgDatabase)).To(Succeed())
				Expect(cnpgDatabase.Spec.Name).To(Equal(dbName))
				Expect(cnpgDatabase.Spec.Owner).To(Equal("appdb_admin"))
				Expect(cnpgDatabase.Spec.ClusterRef.Name).To(Equal(cnpgClusterName))
				Expect(metav1.IsControlledBy(cnpgDatabase, current)).To(BeTrue())

				applied := true
				cnpgDatabase.Status.Applied = &applied
				Expect(k8sClient.Status().Update(ctx, cnpgDatabase)).To(Succeed())

				result, err = reconcilePostgresDatabase(ctx, requestName)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(ctrl.Result{}))

				Expect(k8sClient.Get(ctx, requestName, current)).To(Succeed())
				expectReadyStatus(current, current.Generation, enterprisev4.DatabaseInfo{Name: dbName, Ready: true})
			})
		})

		Context("and connection pooling is enabled", func() {
			It("adds pooler endpoints to the generated ConfigMap", func() {
				resourceName := "pooler-cluster"
				clusterName := "pooler-postgres"
				cnpgClusterName := "pooler-cnpg"
				dbName := "appdb"
				requestName := types.NamespacedName{Name: resourceName, Namespace: namespace}

				createPostgresDatabaseResource(ctx, namespace, resourceName, clusterName, []enterprisev4.DatabaseDefinition{{Name: dbName}})
				postgresCluster := createPostgresClusterResource(ctx, namespace, clusterName)
				markPostgresClusterReady(ctx, postgresCluster, cnpgClusterName, namespace, true)
				cnpgCluster := createCNPGClusterResource(ctx, namespace, cnpgClusterName)
				markCNPGClusterReady(ctx, cnpgCluster, []string{"appdb_admin", "appdb_rw"}, "tenant-rw", "tenant-ro")

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
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "pooler-cluster-appdb-config", Namespace: namespace}, configMap)).To(Succeed())
				Expect(configMap.Data).To(HaveKeyWithValue("pooler-rw-host", cnpgClusterName+"-pooler-rw."+namespace+".svc.cluster.local"))
				Expect(configMap.Data).To(HaveKeyWithValue("pooler-ro-host", cnpgClusterName+"-pooler-ro."+namespace+".svc.cluster.local"))
			})
		})
	})

	When("role ownership conflicts exist", func() {
		It("marks the resource failed and stops provisioning dependent resources", func() {
			resourceName := "conflict-cluster"
			clusterName := "conflict-postgres"
			requestName := types.NamespacedName{Name: resourceName, Namespace: namespace}

			createPostgresDatabaseResource(ctx, namespace, resourceName, clusterName, []enterprisev4.DatabaseDefinition{{Name: "appdb"}}, postgresDatabaseFinalizer)
			postgresCluster := createPostgresClusterResource(ctx, namespace, clusterName)
			markPostgresClusterReady(ctx, postgresCluster, "unused-cnpg", namespace, false)

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
			expectStatusPhase(current, "Failed")
			expectStatusCondition(current, "RolesReady", metav1.ConditionFalse, "RoleConflict")

			rolesReady := meta.FindStatusCondition(current.Status.Conditions, "RolesReady")
			Expect(rolesReady.Message).To(ContainSubstring("appdb_admin"))
			Expect(rolesReady.Message).To(ContainSubstring("postgresdatabase-legacy"))

			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "conflict-cluster-appdb-config", Namespace: namespace}, configMap)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			cnpgDatabase := &cnpgv1.Database{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "conflict-cluster-appdb", Namespace: namespace}, cnpgDatabase)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
	})

	When("the PostgresDatabase is being deleted", func() {
		Context("with retained and deleted databases", func() {
			It("orphans retained resources, removes deleted resources, and patches managed roles", func() {
				resourceName := "delete-cluster"
				clusterName := "delete-postgres"
				requestName := types.NamespacedName{Name: resourceName, Namespace: namespace}

				postgresDB := createPostgresDatabaseResource(ctx, namespace, resourceName, clusterName, []enterprisev4.DatabaseDefinition{
					{Name: "keepdb", DeletionPolicy: "Retain"},
					{Name: "dropdb"},
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
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "delete-cluster-keepdb-config", Namespace: namespace}, retainedConfigMap)).To(Succeed())
				Expect(retainedConfigMap.Annotations).To(HaveKeyWithValue("enterprise.splunk.com/retained-from", resourceName))
				Expect(retainedConfigMap.OwnerReferences).To(BeEmpty())

				retainedAdminSecret := &corev1.Secret{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "delete-cluster-keepdb-admin", Namespace: namespace}, retainedAdminSecret)).To(Succeed())
				Expect(retainedAdminSecret.Annotations).To(HaveKeyWithValue("enterprise.splunk.com/retained-from", resourceName))
				Expect(retainedAdminSecret.OwnerReferences).To(BeEmpty())

				retainedRWSecret := &corev1.Secret{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "delete-cluster-keepdb-rw", Namespace: namespace}, retainedRWSecret)).To(Succeed())
				Expect(retainedRWSecret.Annotations).To(HaveKeyWithValue("enterprise.splunk.com/retained-from", resourceName))
				Expect(retainedRWSecret.OwnerReferences).To(BeEmpty())

				retainedCNPGDatabase := &cnpgv1.Database{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "delete-cluster-keepdb", Namespace: namespace}, retainedCNPGDatabase)).To(Succeed())
				Expect(retainedCNPGDatabase.Annotations).To(HaveKeyWithValue("enterprise.splunk.com/retained-from", resourceName))
				Expect(retainedCNPGDatabase.OwnerReferences).To(BeEmpty())

				for _, name := range []string{
					"delete-cluster-dropdb-config",
					"delete-cluster-dropdb-admin",
					"delete-cluster-dropdb-rw",
				} {
					err = k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &corev1.Secret{})
					if name == "delete-cluster-dropdb-config" {
						err = k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &corev1.ConfigMap{})
					}
					Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected %s to be deleted", name)
				}

				err = k8sClient.Get(ctx, types.NamespacedName{Name: "delete-cluster-dropdb", Namespace: namespace}, &cnpgv1.Database{})
				Expect(apierrors.IsNotFound(err)).To(BeTrue())

				updatedCluster := &enterprisev4.PostgresCluster{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, updatedCluster)).To(Succeed())
				Expect(managedRoleNames(updatedCluster.Spec.ManagedRoles)).To(ConsistOf("keepdb_admin", "keepdb_rw"))

				current := &enterprisev4.PostgresDatabase{}
				err = k8sClient.Get(ctx, requestName, current)
				Expect(apierrors.IsNotFound(err) || !containsFinalizer(current.Finalizers, postgresDatabaseFinalizer)).To(BeTrue())
			})
		})
	})
})

func containsFinalizer(finalizers []string, target string) bool {
	return slices.Contains(finalizers, target)
}
