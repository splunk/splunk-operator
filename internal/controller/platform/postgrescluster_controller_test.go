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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/record"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pgprometheus "github.com/splunk/splunk-operator/pkg/postgresql/shared/adapter/prometheus"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	mtypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

/*
* Test cases:
* PC-01 creates managed resources and status refs
* PC-02 adds finalizer on reconcile
* PC-07 is idempotent across repeated reconciles
* PC-03 Delete policy removes children and finalizer
* PC-04 Retain policy preserves children and removes ownerRefs
* PC-05 fails when PostgresClusterClass is missing
* PC-06 restores drifted managed spec
* PC-08 triggers on generation/finalizer/deletion changes
* PC-09 ignores no-op updates
 */

func CollectEvents(events *[]string, recorder *record.FakeRecorder) {
	for {
		select {
		case e := <-recorder.Events:
			*events = append(*events, e)
		default:
			return
		}
	}
}

func ContainsEvent(events []string, eventType string, event string) bool {
	for _, e := range events {
		if strings.Contains(e, eventType) && strings.Contains(e, event) {
			return true
		}
	}
	return false
}

type provisioningDurationObservation struct {
	controller string
	seconds    float64
}

type captureMetricsRecorder struct {
	provisioningDurations []provisioningDurationObservation
}

func (r *captureMetricsRecorder) IncStatusTransition(string, string, string, string) {}
func (r *captureMetricsRecorder) ObserveProvisioningDuration(controller string, seconds float64) {
	r.provisioningDurations = append(r.provisioningDurations, provisioningDurationObservation{controller: controller, seconds: seconds})
}
func (r *captureMetricsRecorder) SetClusterPhases(map[string]float64)        {}
func (r *captureMetricsRecorder) SetPoolerEnabledClusters(float64)           {}
func (r *captureMetricsRecorder) SetDatabasePhases(map[string]float64)       {}
func (r *captureMetricsRecorder) SetManagedUsers(string, map[string]float64) {}

var _ ports.Recorder = (*captureMetricsRecorder)(nil)

// seedCNPGClusterServerCASecret creates a minimal CNPG-style server CA Secret (ca.crt) so the access
// ConfigMap can expose SERVER_CA_* keys once status.certificates.serverCASecret points at it.
func seedCNPGClusterServerCASecret(ctx context.Context, c client.Client, clusterName, ns string) string {
	caSecretName := clusterName + "-server-ca"
	Expect(c.Create(ctx, &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: caSecretName, Namespace: ns},
		Data: map[string][]byte{
			"ca.crt": []byte("-----BEGIN CERTIFICATE-----\nMIIBtest\n-----END CERTIFICATE-----\n"),
		},
	})).To(Succeed())
	return caSecretName
}

func selfSignedLeafCertPEM(dnsNames []string) []byte {
	GinkgoHelper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	Expect(err).NotTo(HaveOccurred())
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     dnsNames,
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	if len(dnsNames) > 0 {
		tmpl.Subject = pkix.Name{CommonName: dnsNames[0]}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// ensureCNPGServerTLSLeafSecret seeds the CNPG server TLS Secret so poolerModel can pass
// the materialized-leaf check (clusterModel.IsServerTLSLeafAlignedWithSpec, the read side
// of the ClusterRuntimeProbe port) in envtest, where no real CNPG cert controller runs.
func ensureCNPGServerTLSLeafSecret(ctx context.Context, c client.Client, clusterName, ns string) {
	GinkgoHelper()
	cnpg := &cnpgv1.Cluster{}
	Expect(c.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: ns}, cnpg)).To(Succeed())
	var dns []string
	if cnpg.Spec.Certificates != nil && len(cnpg.Spec.Certificates.ServerAltDNSNames) > 0 {
		dns = append([]string(nil), cnpg.Spec.Certificates.ServerAltDNSNames...)
	}
	if len(dns) == 0 {
		return
	}
	secName := clusterName + "-server"
	pemCert := selfSignedLeafCertPEM(dns)
	sec := &v1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secName, Namespace: ns}}
	_, err := controllerutil.CreateOrUpdate(ctx, c, sec, func() error {
		if sec.Data == nil {
			sec.Data = map[string][]byte{}
		}
		sec.Data[v1.TLSCertKey] = pemCert
		return nil
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(c.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: ns}, cnpg)).To(Succeed())
	cnpg.Status.Certificates.ServerTLSSecret = secName
	Expect(c.Status().Update(ctx, cnpg)).To(Succeed())
}

func markCNPGClusterHealthy(cnpg *cnpgv1.Cluster, clusterName, caSecretName string) {
	cnpg.Status.Phase = cnpgv1.PhaseHealthy
	cnpg.Status.WriteService = clusterName + "-rw"
	cnpg.Status.ReadService = clusterName + "-ro"
	// Two ready instances so the RO endpoint is published.
	const healthyReadyInstances = 2
	cnpg.Status.Instances = healthyReadyInstances
	cnpg.Status.ReadyInstances = healthyReadyInstances
	cnpg.Status.CurrentPrimary = "example"
	if caSecretName != "" {
		cnpg.Status.Certificates.CertificatesConfiguration.ServerCASecret = caSecretName
	}
}

func markCNPGClusterBackupReady(cnpg *cnpgv1.Cluster, clusterName, caSecretName string) {
	markCNPGClusterHealthy(cnpg, clusterName, caSecretName)
	cnpg.Status.TargetPrimary = cnpg.Status.CurrentPrimary
	cnpg.Status.InstancesStatus = map[cnpgv1.PodStatus][]string{
		cnpgv1.PodHealthy: {cnpg.Status.CurrentPrimary, clusterName + "-2"},
	}
}

func currentMajorUpgradePhase(ctx context.Context, key types.NamespacedName) string {
	GinkgoHelper()
	pc := &platformv1alpha1.PostgresCluster{}
	Expect(k8sClient.Get(ctx, key, pc)).To(Succeed())
	Expect(pc.Status.PostgresMajorUpgradeStatus).NotTo(BeEmpty())
	current := pc.Status.PostgresMajorUpgradeStatus[len(pc.Status.PostgresMajorUpgradeStatus)-1]
	Expect(current.Phase).NotTo(BeNil())
	return *current.Phase
}

func seedClusterScopedDatabaseRoles(ctx context.Context, namespace, name, clusterName string, roleNames ...string) {
	GinkgoHelper()
	db := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: platformv1alpha1.PostgresDatabaseSpec{
			ClusterRef: v1.LocalObjectReference{Name: clusterName},
			Databases:  []platformv1alpha1.DatabaseDefinition{{Name: "app"}},
		},
	}
	Expect(k8sClient.Create(ctx, db)).To(Succeed())
	roles := make([]platformv1alpha1.DatabaseRoleInfo, 0, len(roleNames))
	for _, r := range roleNames {
		roles = append(roles, platformv1alpha1.DatabaseRoleInfo{Name: r, SecretRef: &v1.LocalObjectReference{Name: name + "-" + r}, Exists: true})
	}
	db.Status.Databases = []platformv1alpha1.DatabaseInfo{{Name: "app", Roles: roles}}
	Expect(k8sClient.Status().Update(ctx, db)).To(Succeed())
}

func applyCNPGPostgreSQLParameters(ctx context.Context, c client.Client, name, namespace, fieldManager string, params map[string]string) {
	GinkgoHelper()

	patch := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": cnpgv1.SchemeGroupVersion.String(),
			"kind":       cnpgv1.ClusterKind,
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"postgresql": map[string]any{
					"parameters": params,
				},
			},
		},
	}

	Expect(c.Apply(ctx, client.ApplyConfigurationFromUnstructured(patch), client.FieldOwner(fieldManager))).To(Succeed())
}

var _ = Describe("PostgresCluster Controller", Label("postgres"), func() {

	const (
		postgresVersion    = "15.10"
		clusterMemberCount = int32(2)
		storageAmount      = "1Gi"
		poolerEnabled      = false
		deletePolicy       = "Delete"
		retainPolicy       = "Retain"
		namespace          = "default"
		classNamePrefix    = "postgresql-dev-"
		clusterNamePrefix  = "postgresql-cluster-dev-"
		provisioner        = "postgresql.cnpg.io"
	)

	var (
		ctx               context.Context
		clusterName       string
		className         string
		classNameMetrics  string
		classNamePooler   string
		classNameBackup   string
		pgCluster         *platformv1alpha1.PostgresCluster
		pgClusterClass    *platformv1alpha1.PostgresClusterClass
		pgClusterKey      types.NamespacedName
		pgClusterClassKey types.NamespacedName
		reconciler        *PostgresClusterReconciler
		req               reconcile.Request
		fakeRecorder      *record.FakeRecorder
	)

	reconcileNTimes := func(times int) {
		for range times {
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
		}
	}
	// After Actuate applies a CNPG spec patch, clusterModel.Converge may return pending
	// (RequeueAfter) while status is still PhaseHealthy — PostgresClusterService then returns
	// before the runtime phase (managed roles, ConfigMap, pooler). A second Reconcile runs
	// runtime once spec drift is cleared.
	reconcileAfterCNPGHealthyOrPatch := func() { reconcileNTimes(2) }

	markCNPGHealthy := func(cnpg *cnpgv1.Cluster, instances int32) {
		cnpg.Status.Phase = cnpgv1.PhaseHealthy
		cnpg.Status.Instances = int(instances)
		cnpg.Status.ReadyInstances = int(instances)
		cnpg.Status.WriteService = cnpg.Name + "-rw"
		cnpg.Status.ReadService = cnpg.Name + "-ro"
	}

	acknowledgeCNPGMetricsConfigMap := func(enabled bool) {
		GinkgoHelper()
		cnpg := &cnpgv1.Cluster{}
		Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
		cnpg.Status.ConfigMapResourceVersion.Metrics = map[string]string{}
		if enabled {
			generated := &v1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      clusterName + "-metrics",
				Namespace: namespace,
			}, generated)).To(Succeed())
			cnpg.Status.ConfigMapResourceVersion.Metrics[generated.Name] = generated.ResourceVersion
		}
		Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
	}

	BeforeEach(func() {
		nameSuffix := fmt.Sprintf("%d-%d-%d",
			GinkgoParallelProcess(),
			GinkgoRandomSeed(),
			CurrentSpecReport().LeafNodeLocation.LineNumber,
		)

		ctx = context.Background()
		clusterName = clusterNamePrefix + nameSuffix
		className = classNamePrefix + nameSuffix
		classNameMetrics = classNamePrefix + "metrics-" + nameSuffix
		classNamePooler = classNamePrefix + "pooler-" + nameSuffix
		classNameBackup = classNamePrefix + "backup-" + nameSuffix
		pgClusterKey = types.NamespacedName{Name: clusterName, Namespace: namespace}
		pgClusterClassKey = types.NamespacedName{Name: className, Namespace: namespace}

		pgClusterClass = &platformv1alpha1.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: className},
			Spec: platformv1alpha1.PostgresClusterClassSpec{
				Provisioner: provisioner,
				Config: &platformv1alpha1.PostgresClusterClassConfig{
					Instances:       ptr.To(clusterMemberCount),
					Storage:         ptr.To(resource.MustParse(storageAmount)),
					PostgresVersion: ptr.To(postgresVersion),
					ConnectionPooler: &platformv1alpha1.ConnectionPoolerEnableConfig{
						Enabled: ptr.To(poolerEnabled),
					},
				},
				CNPG: &platformv1alpha1.CNPGConfig{},
			},
		}

		pgClassPostgresMetrics := &platformv1alpha1.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: classNameMetrics},
			Spec: platformv1alpha1.PostgresClusterClassSpec{
				Provisioner: provisioner,
				Config: &platformv1alpha1.PostgresClusterClassConfig{
					Instances:       ptr.To(clusterMemberCount),
					Storage:         ptr.To(resource.MustParse(storageAmount)),
					PostgresVersion: ptr.To(postgresVersion),
					Monitoring: &platformv1alpha1.PostgresMonitoringClassConfig{
						PostgreSQLMetrics: &platformv1alpha1.MetricsClassConfig{Enabled: ptr.To(true)},
					},
				},
				CNPG: &platformv1alpha1.CNPGConfig{},
			},
		}

		pgClassPoolerMetrics := &platformv1alpha1.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: classNamePooler},
			Spec: platformv1alpha1.PostgresClusterClassSpec{
				Provisioner: provisioner,
				Config: &platformv1alpha1.PostgresClusterClassConfig{
					Instances:       ptr.To(clusterMemberCount),
					Storage:         ptr.To(resource.MustParse(storageAmount)),
					PostgresVersion: ptr.To(postgresVersion),
					ConnectionPooler: &platformv1alpha1.ConnectionPoolerEnableConfig{
						Enabled: ptr.To(true),
					},
					Monitoring: &platformv1alpha1.PostgresMonitoringClassConfig{
						ConnectionPoolerMetrics: &platformv1alpha1.MetricsClassConfig{Enabled: ptr.To(true)},
					},
				},
				CNPG: &platformv1alpha1.CNPGConfig{
					ConnectionPooler: &platformv1alpha1.ConnectionPoolerConfig{
						Instances: ptr.To(int32(2)),
						Mode:      ptr.To(platformv1alpha1.ConnectionPoolerModeTransaction),
					},
				},
			},
		}

		pgClassBackup := &platformv1alpha1.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: classNameBackup},
			Spec: platformv1alpha1.PostgresClusterClassSpec{
				Provisioner: provisioner,
				Config: &platformv1alpha1.PostgresClusterClassConfig{
					Instances:       ptr.To(clusterMemberCount),
					Storage:         ptr.To(resource.MustParse(storageAmount)),
					PostgresVersion: ptr.To(postgresVersion),
					Backup: &platformv1alpha1.BackupConfig{
						Enabled:  ptr.To(true),
						Schedule: ptr.To("0 2 * * *"),
					},
				},
				CNPG: &platformv1alpha1.CNPGConfig{
					Backup: &platformv1alpha1.CNPGBackupConfig{
						Target: ptr.To("prefer-standby"),
						VolumeSnapshot: &platformv1alpha1.CNPGVolumeSnapshotConfig{
							ClassName: ptr.To("csi-snapclass"),
							Online:    ptr.To(true),
						},
					},
				},
			},
		}

		Expect(k8sClient.Create(ctx, pgClusterClass)).To(Succeed())
		Expect(k8sClient.Create(ctx, pgClassPostgresMetrics)).To(Succeed())
		Expect(k8sClient.Create(ctx, pgClassPoolerMetrics)).To(Succeed())
		Expect(k8sClient.Create(ctx, pgClassBackup)).To(Succeed())

		pgCluster = &platformv1alpha1.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
			Spec: platformv1alpha1.PostgresClusterSpec{
				Class:                 className,
				ClusterDeletionPolicy: ptr.To(deletePolicy),
			},
		}
		fakeRecorder = record.NewFakeRecorder(100)
		reconciler = &PostgresClusterReconciler{
			Client:         k8sClient,
			Scheme:         k8sClient.Scheme(),
			Recorder:       fakeRecorder,
			Metrics:        &pgprometheus.NoopRecorder{},
			FleetCollector: pgprometheus.NewFleetCollector(),
		}
		req = reconcile.Request{NamespacedName: types.NamespacedName{Name: clusterName, Namespace: namespace}}
	})

	AfterEach(func() {
		By("Deleting PostgresCluster and letting reconcile run finalizer cleanup")

		// Best-effort delete (object might already be gone in some specs)
		err := k8sClient.Get(ctx, pgClusterKey, pgCluster)
		if err == nil {
			Expect(k8sClient.Delete(ctx, pgCluster)).To(Succeed())
		} else {
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}

		// Drive delete reconcile path until finalizer is removed and object disappears
		Eventually(func() bool {
			_, recErr := reconciler.Reconcile(ctx, req)
			if recErr != nil {
				// Some envtest runs may not have CNPG CRDs installed in the API server.
				// In that case, remove finalizer directly so fixture teardown remains deterministic.
				if meta.IsNoMatchError(recErr) {
					current := &platformv1alpha1.PostgresCluster{}
					getErr := k8sClient.Get(ctx, pgClusterKey, current)
					if apierrors.IsNotFound(getErr) {
						return true
					}
					if getErr != nil {
						return false
					}
					controllerutil.RemoveFinalizer(current, core.PostgresClusterFinalizerName)
					if err := k8sClient.Update(ctx, current); err != nil && !apierrors.IsNotFound(err) {
						return false
					}
					if err := k8sClient.Delete(ctx, current); err != nil && !apierrors.IsNotFound(err) {
						return false
					}
				} else {
					return false
				}
			}
			getErr := k8sClient.Get(ctx, pgClusterKey, &platformv1alpha1.PostgresCluster{})
			return apierrors.IsNotFound(getErr)
		}, "10s", "500ms").Should(BeTrue())

		By("Cleaning up PostgresClusterClass fixtures")
		for _, key := range []types.NamespacedName{
			pgClusterClassKey,
			{Name: classNameMetrics},
			{Name: classNamePooler},
			{Name: classNameBackup},
		} {
			existing := &platformv1alpha1.PostgresClusterClass{}
			err = k8sClient.Get(ctx, key, existing)
			if err == nil {
				Expect(k8sClient.Delete(ctx, existing)).To(Succeed())
			} else {
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}
		}
	})

	When("under typical usage and expecting healthy PostgresCluster state", func() {
		Context("when reconciling", func() {
			// PC-02
			It("adds finalizer on reconcile", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(1)

				pc := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				Expect(controllerutil.ContainsFinalizer(pc, core.PostgresClusterFinalizerName)).To(BeTrue())
			})

			// PC-01
			It("creates managed resources and status refs", func() {
				received := make([]string, 0, 16)

				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				seedClusterScopedDatabaseRoles(ctx, namespace, "managed-roles-db-pc01", clusterName, "app_user", "app_user_rw")
				// pass 1: add finalizer; pass 2: create CNPG cluster/secret/status.
				reconcileNTimes(2)

				pc := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				cond := meta.FindStatusCondition(pc.Status.Conditions, "ClusterReady")
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				Expect(cond.Reason).To(Equal("CNPGClusterProvisioning"))

				secretCond := meta.FindStatusCondition(pc.Status.Conditions, "SecretsReady")
				Expect(secretCond).NotTo(BeNil())
				Expect(secretCond.Status).To(Equal(metav1.ConditionTrue))
				Expect(secretCond.Reason).To(Equal("SuperUserSecretReady"))

				configMapCond := meta.FindStatusCondition(pc.Status.Conditions, "ConfigMapsReady")
				// ConfigMap converge runs in the runtime phase; at this point reconcile may
				// still be returning from provisioner pending and not have written it yet.
				Expect(configMapCond).To(BeNil())

				// ClusterReady must not fire while provisioning — secret convergence must
				// not promote the overall phase to Ready prematurely.
				CollectEvents(&received, fakeRecorder)
				Expect(ContainsEvent(received, v1.EventTypeNormal, core.EventClusterReady)).To(
					BeFalse(), "ClusterReady must not fire during provisioning, got: %v", received)

				// Simulate CNPG becoming healthy first, but without managed roles status published yet.
				caSecretName := seedCNPGClusterServerCASecret(ctx, k8sClient, clusterName, namespace)
				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGClusterHealthy(cnpg, clusterName, caSecretName)
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				reconcileAfterCNPGHealthyOrPatch()

				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				managedRolesCond := meta.FindStatusCondition(pc.Status.Conditions, "ManagedRolesReady")
				Expect(managedRolesCond).NotTo(BeNil())
				Expect(managedRolesCond.Status).To(Equal(metav1.ConditionFalse))
				Expect(managedRolesCond.Reason).To(Equal("ManagedRolesPending"))

				// Simulate external CNPG controller publishing managed roles status.
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				cnpg.Status.ManagedRolesStatus = cnpgv1.ManagedRoles{
					ByStatus: map[cnpgv1.RoleStatus][]string{
						cnpgv1.RoleStatusReconciled: {"app_user", "app_user_rw"},
					},
				}
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				reconcileAfterCNPGHealthyOrPatch()

				// Expect cnpg status progression propagation
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				cond = meta.FindStatusCondition(pc.Status.Conditions, "ClusterReady")
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				Expect(cond.Reason).To(Equal("CNPGClusterHealthy"))

				secretCond = meta.FindStatusCondition(pc.Status.Conditions, "SecretsReady")
				Expect(secretCond).NotTo(BeNil())
				Expect(secretCond.Status).To(Equal(metav1.ConditionTrue))
				Expect(secretCond.Reason).To(Equal("SuperUserSecretReady"))

				configMapCond = meta.FindStatusCondition(pc.Status.Conditions, "ConfigMapsReady")
				Expect(configMapCond).NotTo(BeNil())
				Expect(configMapCond.Status).To(Equal(metav1.ConditionTrue))
				Expect(configMapCond.Reason).To(Equal("ConfigMapReconciled"))

				managedRolesCond = meta.FindStatusCondition(pc.Status.Conditions, "ManagedRolesReady")
				Expect(managedRolesCond).NotTo(BeNil())
				Expect(managedRolesCond.Status).To(Equal(metav1.ConditionTrue))
				Expect(managedRolesCond.Reason).To(Equal("ManagedRolesReconciled"))

				// Pooler is disabled in this suite fixture, but converge publishes PoolerReady=True with disabled message.
				poolerCond := meta.FindStatusCondition(pc.Status.Conditions, "PoolerReady")
				Expect(poolerCond).NotTo(BeNil())
				Expect(poolerCond.Status).To(Equal(metav1.ConditionTrue))
				Expect(poolerCond.Reason).To(Equal("PoolerDisabled"))
				Expect(poolerCond.Message).To(Equal("Connection pooler disabled"))

				Expect(pc.Status.ManagedRolesStatus).NotTo(BeNil())
				Expect(pc.Status.ManagedRolesStatus.Reconciled).To(ContainElements("app_user", "app_user_rw"))

				Expect(pc.Status.Phase).NotTo(BeNil())
				Expect(*pc.Status.Phase).To(Equal("Ready"))
				Expect(pc.Status.ProvisionerRef).NotTo(BeNil())
				Expect(pc.Status.ProvisionerRef.Kind).To(Equal("Cluster"))
				Expect(pc.Status.ProvisionerRef.Name).To(Equal(clusterName))

				Expect(pc.Status.Resources).NotTo(BeNil())
				Expect(pc.Status.Resources.SuperUserSecretRef).NotTo(BeNil())
				Expect(pc.Status.Resources.ConfigMapRef).NotTo(BeNil())

				CollectEvents(&received, fakeRecorder)
				Expect(ContainsEvent(
					received,
					v1.EventTypeNormal, core.EventConfigMapReconciled,
				)).To(BeTrue(), "events seen: %v", received)
				Expect(ContainsEvent(
					received,
					v1.EventTypeNormal, core.EventClusterReady,
				)).To(BeTrue(), "events seen: %v", received)
			})

			It("drives the pg_upgrade major-version workflow and observes one readiness duration", func() {
				targetVersion := "16"
				initialImage := fmt.Sprintf("ghcr.io/cloudnative-pg/postgresql:%s", postgresVersion)
				targetImage := "registry.example.com/team/postgresql:16-bookworm@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

				// Use the backup-enabled class so the upgrade gate creates a CNPG
				// Backup that matches the target Cluster's VolumeSnapshot config.
				pgCluster.Spec.Class = classNameBackup
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				Expect(cnpg.Spec.ImageName).To(Equal(initialImage))
				caSecretName := seedCNPGClusterServerCASecret(ctx, k8sClient, clusterName, namespace)
				markCNPGClusterBackupReady(cnpg, clusterName, caSecretName)
				cnpg.Status.Image = initialImage
				cnpg.Status.PGDataImageInfo = &cnpgv1.ImageInfo{Image: initialImage, MajorVersion: 15}
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				reconcileAfterCNPGHealthyOrPatch()

				pc := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				Expect(pc.Status.Phase).NotTo(BeNil())
				Expect(*pc.Status.Phase).To(Equal("Ready"))

				metrics := &captureMetricsRecorder{}
				reconciler.Metrics = metrics
				completedPhase := string(mvutypes.Completed)
				strategy := mvutypes.MajorUpgradeFlowPgUpgrade
				sourceVersion := "14"
				pc.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{
					Phase:           &completedPhase,
					Strategy:        &strategy,
					SourcePgVersion: &sourceVersion,
					TargetPgVersion: ptr.To(postgresVersion),
				}}
				Expect(k8sClient.Status().Update(ctx, pc)).To(Succeed())

				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				pc.Spec.PostgresVersion = ptr.To(targetVersion)
				pc.Spec.PostgresImage = ptr.To(targetImage)
				pc.Spec.ImagePullSecrets = []v1.LocalObjectReference{{Name: "target-registry-creds"}}
				pc.Spec.PostgresMajorUpgradeConfig = &platformv1alpha1.PostgresMajorUpgradeConfig{
					Allow:    ptr.To(true),
					Strategy: &[]string{mvutypes.MajorUpgradeFlowPgUpgrade}[0],
				}
				Expect(k8sClient.Update(ctx, pc)).To(Succeed())

				// First reconcile: use case enters PreUpgradeBackup and the adapter creates the
				// CNPG Backup CR. CNPG is not running in envtest so we patch it to completed.
				_, err := reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(currentMajorUpgradePhase(ctx, pgClusterKey)).To(Equal(string(mvutypes.PreUpgradeBackup)))
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				Expect(pc.Status.LastTransitionTime).NotTo(BeNil())
				lastTransitionTime := *pc.Status.LastTransitionTime

				backupName := fmt.Sprintf("%s-pre-upgrade-%s-%s", clusterName, postgresVersion, targetVersion)
				backup := &cnpgv1.Backup{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: backupName, Namespace: namespace}, backup)).To(Succeed())
				Expect(backup.Spec.Method).To(Equal(cnpgv1.BackupMethodVolumeSnapshot))
				Expect(cnpg.Spec.Backup).NotTo(BeNil())
				Expect(cnpg.Spec.Backup.VolumeSnapshot).NotTo(BeNil())
				Expect(backup.Status.Phase).NotTo(Equal(cnpgv1.BackupPhaseCompleted))

				// A pending Backup must keep the workflow at its safety gate.
				_, err = reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(currentMajorUpgradePhase(ctx, pgClusterKey)).To(Equal(string(mvutypes.PreUpgradeBackup)))

				backup.Status.Phase = cnpgv1.BackupPhaseCompleted
				Expect(k8sClient.Status().Update(ctx, backup)).To(Succeed())

				// Second reconcile: backup Done → gate passes → use case advances to Preflight.
				_, err = reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				Expect(cnpg.Spec.ImageName).To(Equal(initialImage), "provisioner drift must be blocked before pg_upgrade starts")
				Expect(currentMajorUpgradePhase(ctx, pgClusterKey)).To(Equal(string(mvutypes.Preflight)))

				// CNPG applies the target image while the data directory still uses the
				// source image. A major-upgrade phase must keep the readiness cycle open.
				cnpg = &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				_, err = reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				Expect(cnpg.Spec.ImageName).To(Equal(targetImage))
				Expect(cnpg.Spec.ImagePullSecrets).To(Equal([]cnpgv1.LocalObjectReference{{Name: "target-registry-creds"}}))
				Expect(currentMajorUpgradePhase(ctx, pgClusterKey)).To(Equal(string(mvutypes.Upgrading)))

				cnpg.Status.Phase = cnpgv1.PhaseMajorUpgrade
				cnpg.Status.Image = targetImage
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				_, err = reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(currentMajorUpgradePhase(ctx, pgClusterKey)).To(Equal(string(mvutypes.Upgrading)))
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				Expect(pc.Status.LastTransitionTime).NotTo(BeNil())
				Expect(*pc.Status.LastTransitionTime).To(Equal(lastTransitionTime))
				Expect(metrics.provisioningDurations).To(BeEmpty())

				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGClusterBackupReady(cnpg, clusterName, caSecretName)
				cnpg.Status.Image = targetImage
				cnpg.Status.PGDataImageInfo = &cnpgv1.ImageInfo{Image: targetImage, MajorVersion: 16}
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())

				_, err = reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(currentMajorUpgradePhase(ctx, pgClusterKey)).To(Equal(string(mvutypes.Verifying)))

				_, err = reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(currentMajorUpgradePhase(ctx, pgClusterKey)).To(Equal(string(mvutypes.PostUpgradeBackup)))

				// The previous reconcile ran onVerifying and persisted PostUpgradeBackup.
				// The postUpgradeBackup intercept (and BackupNow) only runs on the next
				// reconcile — trigger it so the Backup CR is created before we patch it.
				_, err = reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(currentMajorUpgradePhase(ctx, pgClusterKey)).To(Equal(string(mvutypes.PostUpgradeBackup)))

				postBackupName := fmt.Sprintf("%s-post-upgrade-%s-%s", clusterName, postgresVersion, targetVersion)
				postBackup := &cnpgv1.Backup{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: postBackupName, Namespace: namespace}, postBackup)).To(Succeed())
				postBackup.Status.Phase = cnpgv1.BackupPhaseCompleted
				Expect(k8sClient.Status().Update(ctx, postBackup)).To(Succeed())

				_, err = reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(currentMajorUpgradePhase(ctx, pgClusterKey)).To(Equal(string(mvutypes.Completed)))

				Eventually(func(g Gomega) {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
					g.Expect(pc.Status.Phase).NotTo(BeNil())
					g.Expect(*pc.Status.Phase).To(Equal("Ready"))
					g.Expect(pc.Status.LastTransitionTime).To(BeNil())
				}).WithTimeout(45 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())

				Expect(metrics.provisioningDurations).To(HaveLen(1))
				Expect(metrics.provisioningDurations[0].controller).To(Equal(ports.ControllerCluster))
				Expect(metrics.provisioningDurations[0].seconds).To(BeNumerically(">", 0))

				reconcileNTimes(2)
				Expect(metrics.provisioningDurations).To(HaveLen(1))

				received := make([]string, 0, 16)
				CollectEvents(&received, fakeRecorder)
				Expect(ContainsEvent(received, v1.EventTypeNormal, mvutypes.EventMajorUpgradeScheduled)).To(
					BeTrue(), "MajorUpgradeScheduled event must fire when workflow enters Preflight; events seen: %v", received)
				Expect(ContainsEvent(received, v1.EventTypeNormal, mvutypes.EventMajorUpgradeStarted)).To(
					BeTrue(), "MajorUpgradeStarted event must fire when image patch succeeds; events seen: %v", received)
				Expect(ContainsEvent(received, v1.EventTypeNormal, mvutypes.EventMajorUpgradeCompleted)).To(
					BeTrue(), "MajorUpgradeCompleted event must fire when post-upgrade backup completes; events seen: %v", received)
			})

			It("reconciles external superuser secret and creates managed resources w/ status refs", func() {
				received := make([]string, 0, 16)

				pgCluster.Spec.PasswordConfig = &platformv1alpha1.SuperuserPasswordConfig{
					SuperuserExternalSecretRef: v1.LocalObjectReference{
						Name: "external-superuser-secret",
					},
				}

				Expect(k8sClient.Create(ctx, &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "external-superuser-secret",
						Namespace: namespace,
						// The owner of an external secret is responsible for the
						// cnpg.io/reload label; the operator validates, never stamps it.
						Labels: map[string]string{"cnpg.io/reload": "true"},
					},
					Data: map[string][]byte{
						"username": []byte("postgres"),
						"password": []byte("username"),
					},
				})).To(Succeed())

				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				seedClusterScopedDatabaseRoles(ctx, namespace, "managed-roles-db-ext", clusterName, "app_user", "app_user_rw")
				// pass 1: add finalizer; pass 2: create CNPG cluster/secret/status.
				reconcileNTimes(2)

				pc := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				cond := meta.FindStatusCondition(pc.Status.Conditions, "ClusterReady")
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				Expect(cond.Reason).To(Equal("CNPGClusterProvisioning"))

				secretCond := meta.FindStatusCondition(pc.Status.Conditions, "SecretsReady")
				Expect(secretCond).NotTo(BeNil())
				Expect(secretCond.Status).To(Equal(metav1.ConditionTrue))
				Expect(secretCond.Reason).To(Equal("SuperUserSecretReady"))

				configMapCond := meta.FindStatusCondition(pc.Status.Conditions, "ConfigMapsReady")
				// ConfigMap converge runs in the runtime phase; at this point reconcile may
				// still be returning from provisioner pending and not have written it yet.
				Expect(configMapCond).To(BeNil())

				// ClusterReady must not fire while provisioning — secret convergence must
				// not promote the overall phase to Ready prematurely.
				CollectEvents(&received, fakeRecorder)
				Expect(ContainsEvent(received, v1.EventTypeNormal, core.EventClusterReady)).To(
					BeFalse(), "ClusterReady must not fire during provisioning, got: %v", received)

				// Simulate CNPG becoming healthy first, but without managed roles status published yet.
				caSecretName := seedCNPGClusterServerCASecret(ctx, k8sClient, clusterName, namespace)
				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGClusterHealthy(cnpg, clusterName, caSecretName)
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				reconcileAfterCNPGHealthyOrPatch()

				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				managedRolesCond := meta.FindStatusCondition(pc.Status.Conditions, "ManagedRolesReady")
				Expect(managedRolesCond).NotTo(BeNil())
				Expect(managedRolesCond.Status).To(Equal(metav1.ConditionFalse))
				Expect(managedRolesCond.Reason).To(Equal("ManagedRolesPending"))

				// Simulate external CNPG controller publishing managed roles status.
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				cnpg.Status.ManagedRolesStatus = cnpgv1.ManagedRoles{
					ByStatus: map[cnpgv1.RoleStatus][]string{
						cnpgv1.RoleStatusReconciled: {"app_user", "app_user_rw"},
					},
				}
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				reconcileAfterCNPGHealthyOrPatch()

				// Expect cnpg status progression propagation
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				cond = meta.FindStatusCondition(pc.Status.Conditions, "ClusterReady")

				Expect(pc.Status.Resources.SuperUserSecretRef).NotTo(BeNil())
				Expect(pc.Status.Resources.SuperUserSecretRef.Name).To(Equal("external-superuser-secret"))

				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				Expect(cond.Reason).To(Equal("CNPGClusterHealthy"))

				secret := &v1.Secret{}
				Expect(k8sClient.Get(
					ctx, types.NamespacedName{
						Name: "external-superuser-secret", Namespace: namespace},
					secret)).To(Succeed())
				Expect(secret).NotTo(BeNil())
				Expect(secret.Labels).To(HaveKeyWithValue("cnpg.io/reload", "true"))

				secretCond = meta.FindStatusCondition(pc.Status.Conditions, "SecretsReady")
				Expect(secretCond).NotTo(BeNil())
				Expect(secretCond.Status).To(Equal(metav1.ConditionTrue))
				Expect(secretCond.Reason).To(Equal("SuperUserSecretReady"))

				configMapCond = meta.FindStatusCondition(pc.Status.Conditions, "ConfigMapsReady")
				Expect(configMapCond).NotTo(BeNil())
				Expect(configMapCond.Status).To(Equal(metav1.ConditionTrue))
				Expect(configMapCond.Reason).To(Equal("ConfigMapReconciled"))

				managedRolesCond = meta.FindStatusCondition(pc.Status.Conditions, "ManagedRolesReady")
				Expect(managedRolesCond).NotTo(BeNil())
				Expect(managedRolesCond.Status).To(Equal(metav1.ConditionTrue))
				Expect(managedRolesCond.Reason).To(Equal("ManagedRolesReconciled"))

				// Pooler is disabled in this suite fixture, but converge publishes PoolerReady=True with disabled message.
				poolerCond := meta.FindStatusCondition(pc.Status.Conditions, "PoolerReady")
				Expect(poolerCond).NotTo(BeNil())
				Expect(poolerCond.Status).To(Equal(metav1.ConditionTrue))
				Expect(poolerCond.Reason).To(Equal("PoolerDisabled"))
				Expect(poolerCond.Message).To(Equal("Connection pooler disabled"))

				Expect(pc.Status.ManagedRolesStatus).NotTo(BeNil())
				Expect(pc.Status.ManagedRolesStatus.Reconciled).To(ContainElements("app_user", "app_user_rw"))

				Expect(pc.Status.Phase).NotTo(BeNil())
				Expect(*pc.Status.Phase).To(Equal("Ready"))
				Expect(pc.Status.ProvisionerRef).NotTo(BeNil())
				Expect(pc.Status.ProvisionerRef.Kind).To(Equal("Cluster"))
				Expect(pc.Status.ProvisionerRef.Name).To(Equal(clusterName))

				Expect(pc.Status.Resources).NotTo(BeNil())
				Expect(pc.Status.Resources.SuperUserSecretRef).NotTo(BeNil())
				Expect(pc.Status.Resources.ConfigMapRef).NotTo(BeNil())

				CollectEvents(&received, fakeRecorder)
				Expect(ContainsEvent(
					received,
					v1.EventTypeNormal, core.EventConfigMapReconciled,
				)).To(BeTrue(), "events seen: %v", received)
				Expect(ContainsEvent(
					received,
					v1.EventTypeNormal, core.EventClusterReady,
				)).To(BeTrue(), "events seen: %v", received)
				// Secret-specific success signal — guards against a regression
				// where the cluster reaches ClusterReady without secretModel
				// ever publishing its own Ready event.
				Expect(ContainsEvent(
					received,
					v1.EventTypeNormal, core.EventSecretReady,
				)).To(BeTrue(), "events seen: %v", received)
			})

			It("sets secret missing condition when appropriate", func() {
				received := make([]string, 0, 16)

				pgCluster.Spec.PasswordConfig = &platformv1alpha1.SuperuserPasswordConfig{
					SuperuserExternalSecretRef: v1.LocalObjectReference{
						Name: "missing-superuser-secret-status-failed",
					},
				}

				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				// pass 1: add finalizer; pass 2: secretModel.Actuate hits the
				// missing-secret branch in externalSecretActuate and bubbles up a
				// secretReconcileError (reason ExternalSecretMissing) → Converge
				// writes SecretsReady=False/ExternalSecretMissing.
				reconciler.Reconcile(ctx, req)
				_, err := reconciler.Reconcile(ctx, req)
				Expect(err).To(HaveOccurred())

				pc := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())

				secretCond := meta.FindStatusCondition(pc.Status.Conditions, "SecretsReady")
				Expect(secretCond).NotTo(BeNil())
				Expect(secretCond.Status).To(Equal(metav1.ConditionFalse))
				Expect(secretCond.Reason).To(Equal("ExternalSecretMissing"))

				CollectEvents(&received, fakeRecorder)
				Expect(ContainsEvent(
					received,
					v1.EventTypeWarning, core.EventSecretReconcileFailed,
				)).To(BeTrue(),
					"Warning %s must be emitted when the external superuser secret is missing; events seen: %v",
					core.EventSecretReconcileFailed, received)
				Expect(ContainsEvent(
					received,
					v1.EventTypeNormal, core.EventClusterReady,
				)).To(BeFalse(),
					"ClusterReady must not fire while SecretsReady=ExternalSecretMissing; events seen: %v", received)
				Expect(ContainsEvent(
					received,
					v1.EventTypeNormal, core.EventSecretReady,
				)).To(BeFalse(),
					"SecretReady must not fire while the external secret is missing; events seen: %v", received)
			})

			// PC-07
			It("is idempotent across repeated reconciles", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)
				reconcileNTimes(3)

				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				Expect(cnpg.Spec.Instances).To(Equal(int(clusterMemberCount)))

				pc := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				cond := meta.FindStatusCondition(pc.Status.Conditions, "ClusterReady")
				Expect(cond).NotTo(BeNil())
				Expect(cond.ObservedGeneration).To(Equal(pc.Generation))
			})

			It("patches the CNPG image and reports configuring state during a minor PostgreSQL upgrade", func() {
				const upgradedPostgresVersion = "15.13"
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				Expect(cnpg.Spec.ImageName).To(Equal("ghcr.io/cloudnative-pg/postgresql:" + postgresVersion))

				// Seed the CNPG server CA Secret + status ref so the access ConfigMap can publish
				// SERVER_CA_* keys; otherwise configMap converge loops on ConfigMapCAMetadataPending
				// and the aggregate ClusterReady never settles True.
				caSecretName := seedCNPGClusterServerCASecret(ctx, k8sClient, clusterName, namespace)
				markCNPGClusterHealthy(cnpg, clusterName, caSecretName)
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				reconcileAfterCNPGHealthyOrPatch()

				current := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
				current.Spec.PostgresVersion = ptr.To(upgradedPostgresVersion)
				Expect(k8sClient.Update(ctx, current)).To(Succeed())

				reconcileNTimes(1)

				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				Expect(cnpg.Spec.ImageName).To(Equal("ghcr.io/cloudnative-pg/postgresql:" + upgradedPostgresVersion))

				Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
				cond := meta.FindStatusCondition(current.Status.Conditions, "ClusterReady")
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				Expect(cond.Reason).To(Equal("CNPGClusterProvisioning"))
				Expect(current.Status.Phase).NotTo(BeNil())
				Expect(*current.Status.Phase).To(Equal("Provisioning"))

				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				cnpg.Status.Phase = cnpgv1.PhaseUpgrade
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				reconcileNTimes(1)

				Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
				cond = meta.FindStatusCondition(current.Status.Conditions, "ClusterReady")
				Expect(cond).NotTo(BeNil())
				Expect(cond.Reason).To(Equal("CNPGUpgrading"))
				Expect(current.Status.Phase).NotTo(BeNil())
				Expect(*current.Status.Phase).To(Equal("Configuring"))

				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				cnpg.Status.Phase = cnpgv1.PhaseSwitchover
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				reconcileNTimes(1)

				Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
				cond = meta.FindStatusCondition(current.Status.Conditions, "ClusterReady")
				Expect(cond).NotTo(BeNil())
				Expect(cond.Reason).To(Equal("CNPGSwitchover"))
				Expect(current.Status.Phase).NotTo(BeNil())
				Expect(*current.Status.Phase).To(Equal("Configuring"))

				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGHealthy(cnpg, clusterMemberCount)
				// Re-assert the ServerCASecret on this status write; envtest does not run a
				// CNPG controller to keep it populated across our status mutations.
				cnpg.Status.Certificates.CertificatesConfiguration.ServerCASecret = caSecretName
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())

				// After upgrade choreography the controller may need several ticks (image rollout
				// gate, pooler SAN, status writers) before ClusterReady returns True.
				Eventually(func(g Gomega) {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
					cond = meta.FindStatusCondition(current.Status.Conditions, "ClusterReady")
					g.Expect(cond).NotTo(BeNil())
					g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(cond.Reason).To(Equal("CNPGClusterHealthy"))
					g.Expect(current.Status.Phase).NotTo(BeNil())
					g.Expect(*current.Status.Phase).To(Equal("Ready"))
				}).WithTimeout(45 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())
			})

		})
	})

	When("monitoring is configured", func() {
		const (
			scrapeAnnotationKey = "prometheus.io/scrape"
			pathAnnotationKey   = "prometheus.io/path"
			portAnnotationKey   = "prometheus.io/port"
			metricsPath         = "/metrics"
			postgresPort        = "9187"
			poolerPort          = "9127"
		)

		Context("with PostgreSQL metrics enabled in class", func() {
			BeforeEach(func() {
				pgCluster.Spec.Class = classNameMetrics
			})

			It("adds scrape annotations to the CNPG Cluster", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				caSecretName := seedCNPGClusterServerCASecret(ctx, k8sClient, clusterName, namespace)
				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())

				markCNPGClusterHealthy(cnpg, clusterName, caSecretName)
				cnpg.Status.ManagedRolesStatus = cnpgv1.ManagedRoles{
					ByStatus: map[cnpgv1.RoleStatus][]string{
						cnpgv1.RoleStatusReconciled: {"app_user", "app_user_rw"},
					},
				}
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())

				reconcileAfterCNPGHealthyOrPatch()

				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				Expect(cnpg.Spec.InheritedMetadata).NotTo(BeNil())
				Expect(cnpg.Spec.InheritedMetadata.Annotations).To(HaveKeyWithValue(scrapeAnnotationKey, "true"))
				Expect(cnpg.Spec.InheritedMetadata.Annotations).To(HaveKeyWithValue(pathAnnotationKey, metricsPath))
				Expect(cnpg.Spec.InheritedMetadata.Annotations).To(HaveKeyWithValue(portAnnotationKey, postgresPort))
			})

			It("removes scrape annotations when disabled by cluster override", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				Expect(cnpg.Spec.InheritedMetadata).NotTo(BeNil())
				Expect(cnpg.Spec.InheritedMetadata.Annotations).To(HaveKeyWithValue(portAnnotationKey, postgresPort))

				current := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
				current.Spec.Monitoring = &platformv1alpha1.PostgresClusterMonitoring{
					PostgreSQLMetrics: ptr.To(false),
				}
				Expect(k8sClient.Update(ctx, current)).To(Succeed())
				reconcileAfterCNPGHealthyOrPatch()

				Eventually(func(g Gomega) {
					updated := &cnpgv1.Cluster{}
					g.Expect(k8sClient.Get(ctx, pgClusterKey, updated)).To(Succeed())
					g.Expect(updated.Spec.InheritedMetadata).NotTo(BeNil())
					g.Expect(updated.Spec.InheritedMetadata.Annotations).NotTo(HaveKey(scrapeAnnotationKey))
					g.Expect(updated.Spec.InheritedMetadata.Annotations).NotTo(HaveKey(pathAnnotationKey))
					g.Expect(updated.Spec.InheritedMetadata.Annotations).NotTo(HaveKey(portAnnotationKey))
				}, "20s", "250ms").Should(Succeed())
			})
		})

		Context("with connection pooler metrics enabled in class", func() {
			BeforeEach(func() {
				pgCluster.Spec.Class = classNamePooler
			})

			It("adds scrape annotations to poolers only after the CNPG cluster becomes healthy", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				rwKey := types.NamespacedName{Name: clusterName + "-pooler-rw", Namespace: namespace}
				roKey := types.NamespacedName{Name: clusterName + "-pooler-ro", Namespace: namespace}

				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, rwKey, &cnpgv1.Pooler{}))).To(BeTrue())
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, roKey, &cnpgv1.Pooler{}))).To(BeTrue())

				pc := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				poolerCond := meta.FindStatusCondition(pc.Status.Conditions, "PoolerReady")
				// Pooler component is gated behind provisioner readiness, so before CNPG
				// becomes healthy the condition may not be written yet.
				Expect(poolerCond).To(BeNil())

				caSecretName := seedCNPGClusterServerCASecret(ctx, k8sClient, clusterName, namespace)
				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGClusterHealthy(cnpg, clusterName, caSecretName)
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())

				reconcileAfterCNPGHealthyOrPatch()
				ensureCNPGServerTLSLeafSecret(ctx, k8sClient, clusterName, namespace)

				Eventually(func(g Gomega) {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())

					rw := &cnpgv1.Pooler{}
					g.Expect(k8sClient.Get(ctx, rwKey, rw)).To(Succeed())
					g.Expect(rw.Spec.Template).NotTo(BeNil())
					g.Expect(rw.Spec.Template.ObjectMeta.Annotations).To(HaveKeyWithValue(scrapeAnnotationKey, "true"))
					g.Expect(rw.Spec.Template.ObjectMeta.Annotations).To(HaveKeyWithValue(pathAnnotationKey, metricsPath))
					g.Expect(rw.Spec.Template.ObjectMeta.Annotations).To(HaveKeyWithValue(portAnnotationKey, poolerPort))

					ro := &cnpgv1.Pooler{}
					g.Expect(k8sClient.Get(ctx, roKey, ro)).To(Succeed())
					g.Expect(ro.Spec.Template).NotTo(BeNil())
					g.Expect(ro.Spec.Template.ObjectMeta.Annotations).To(HaveKeyWithValue(scrapeAnnotationKey, "true"))
					g.Expect(ro.Spec.Template.ObjectMeta.Annotations).To(HaveKeyWithValue(pathAnnotationKey, metricsPath))
					g.Expect(ro.Spec.Template.ObjectMeta.Annotations).To(HaveKeyWithValue(portAnnotationKey, poolerPort))

					// Simulate CNPG pooler controller publishing status progression.
					if rw.Status.Instances < 2 {
						rw.Status.Instances = 2
						g.Expect(k8sClient.Status().Update(ctx, rw)).To(Succeed())
					}
					if ro.Status.Instances < 2 {
						ro.Status.Instances = 2
						g.Expect(k8sClient.Status().Update(ctx, ro)).To(Succeed())
					}
				}, "20s", "250ms").Should(Succeed())

				Eventually(func(g Gomega) {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())

					updated := &platformv1alpha1.PostgresCluster{}
					g.Expect(k8sClient.Get(ctx, pgClusterKey, updated)).To(Succeed())
					poolerReadyCond := meta.FindStatusCondition(updated.Status.Conditions, "PoolerReady")
					g.Expect(poolerReadyCond).NotTo(BeNil())
					g.Expect(poolerReadyCond.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(poolerReadyCond.Reason).To(Equal("AllInstancesReady"))
				}, "20s", "250ms").Should(Succeed())
			})

			It("does not create RO pooler when spec.instances=1 and publishes empty pooler RO endpoint", func() {
				pgCluster.Spec.Instances = ptr.To(int32(1))
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				rwKey := types.NamespacedName{Name: clusterName + "-pooler-rw", Namespace: namespace}
				roKey := types.NamespacedName{Name: clusterName + "-pooler-ro", Namespace: namespace}

				caSecretName := seedCNPGClusterServerCASecret(ctx, k8sClient, clusterName, namespace)
				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGHealthy(cnpg, 1)
				cnpg.Status.Certificates.CertificatesConfiguration.ServerCASecret = caSecretName
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())

				reconcileAfterCNPGHealthyOrPatch()
				ensureCNPGServerTLSLeafSecret(ctx, k8sClient, clusterName, namespace)

				Eventually(func(g Gomega) {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())

					rw := &cnpgv1.Pooler{}
					g.Expect(k8sClient.Get(ctx, rwKey, rw)).To(Succeed())
					g.Expect(apierrors.IsNotFound(k8sClient.Get(ctx, roKey, &cnpgv1.Pooler{}))).To(BeTrue())

					if rw.Status.Instances < 2 {
						rw.Status.Instances = 2
						g.Expect(k8sClient.Status().Update(ctx, rw)).To(Succeed())
					}
				}, "20s", "250ms").Should(Succeed())

				pc := &platformv1alpha1.PostgresCluster{}
				Eventually(func(g Gomega) {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
					g.Expect(pc.Status.Resources).NotTo(BeNil())
					g.Expect(pc.Status.Resources.ConfigMapRef).NotTo(BeNil())
				}, "20s", "250ms").Should(Succeed())

				cm := &v1.ConfigMap{}
				cmKey := types.NamespacedName{Name: pc.Status.Resources.ConfigMapRef.Name, Namespace: namespace}
				Expect(k8sClient.Get(ctx, cmKey, cm)).To(Succeed())
				Expect(cm.Data["CLUSTER_POOLER_RW_ENDPOINT"]).NotTo(BeEmpty())
				Expect(cm.Data).To(HaveKey("CLUSTER_POOLER_RO_ENDPOINT"))
				Expect(cm.Data["CLUSTER_POOLER_RO_ENDPOINT"]).To(BeEmpty())
			})

			It("deletes RO pooler when scaling 2->1", func() {
				pgCluster.Spec.Instances = ptr.To(int32(2))
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				rwKey := types.NamespacedName{Name: clusterName + "-pooler-rw", Namespace: namespace}
				roKey := types.NamespacedName{Name: clusterName + "-pooler-ro", Namespace: namespace}

				caSecretName := seedCNPGClusterServerCASecret(ctx, k8sClient, clusterName, namespace)
				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGHealthy(cnpg, 2)
				cnpg.Status.Certificates.CertificatesConfiguration.ServerCASecret = caSecretName
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())

				Eventually(func(g Gomega) {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())

					rw := &cnpgv1.Pooler{}
					ro := &cnpgv1.Pooler{}
					g.Expect(k8sClient.Get(ctx, rwKey, rw)).To(Succeed())
					g.Expect(k8sClient.Get(ctx, roKey, ro)).To(Succeed())

					if rw.Status.Instances < 2 {
						rw.Status.Instances = 2
						g.Expect(k8sClient.Status().Update(ctx, rw)).To(Succeed())
					}
					if ro.Status.Instances < 2 {
						ro.Status.Instances = 2
						g.Expect(k8sClient.Status().Update(ctx, ro)).To(Succeed())
					}
				}, "20s", "250ms").Should(Succeed())

				pc := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				pc.Spec.Instances = ptr.To(int32(1))
				Expect(k8sClient.Update(ctx, pc)).To(Succeed())

				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGHealthy(cnpg, 1)
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())

				Eventually(func(g Gomega) {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(k8sClient.Get(ctx, rwKey, &cnpgv1.Pooler{})).To(Succeed())
					g.Expect(apierrors.IsNotFound(k8sClient.Get(ctx, roKey, &cnpgv1.Pooler{}))).To(BeTrue())
				}, "20s", "250ms").Should(Succeed())
			})

			It("respects readOnly=false at instances=2", func() {
				pgCluster.Spec.Instances = ptr.To(int32(2))
				pgCluster.Spec.ConnectionPooler = &platformv1alpha1.ConnectionPoolerEnableConfig{
					Enabled:  ptr.To(true),
					ReadOnly: ptr.To(false),
				}
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				rwKey := types.NamespacedName{Name: clusterName + "-pooler-rw", Namespace: namespace}
				roKey := types.NamespacedName{Name: clusterName + "-pooler-ro", Namespace: namespace}

				caSecretName := seedCNPGClusterServerCASecret(ctx, k8sClient, clusterName, namespace)
				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGHealthy(cnpg, 2)
				cnpg.Status.Certificates.CertificatesConfiguration.ServerCASecret = caSecretName
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())

				reconcileAfterCNPGHealthyOrPatch()
				ensureCNPGServerTLSLeafSecret(ctx, k8sClient, clusterName, namespace)

				Eventually(func(g Gomega) {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())

					rw := &cnpgv1.Pooler{}
					g.Expect(k8sClient.Get(ctx, rwKey, rw)).To(Succeed())
					g.Expect(apierrors.IsNotFound(k8sClient.Get(ctx, roKey, &cnpgv1.Pooler{}))).To(BeTrue(), "RO pooler must be absent when readOnly=false")

					if rw.Status.Instances < 2 {
						rw.Status.Instances = 2
						g.Expect(k8sClient.Status().Update(ctx, rw)).To(Succeed())
					}
				}, "20s", "250ms").Should(Succeed())

				pc := &platformv1alpha1.PostgresCluster{}
				Eventually(func(g Gomega) {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
					g.Expect(pc.Status.Resources).NotTo(BeNil())
					g.Expect(pc.Status.Resources.ConfigMapRef).NotTo(BeNil())
				}, "20s", "250ms").Should(Succeed())

				cm := &v1.ConfigMap{}
				cmKey := types.NamespacedName{Name: pc.Status.Resources.ConfigMapRef.Name, Namespace: namespace}
				Expect(k8sClient.Get(ctx, cmKey, cm)).To(Succeed())
				Expect(cm.Data["CLUSTER_POOLER_RW_ENDPOINT"]).NotTo(BeEmpty())
				Expect(cm.Data).To(HaveKey("CLUSTER_POOLER_RO_ENDPOINT"))
				Expect(cm.Data["CLUSTER_POOLER_RO_ENDPOINT"]).To(BeEmpty())
			})

			It("creates RO pooler when scaling 1->2", func() {
				pgCluster.Spec.Instances = ptr.To(int32(1))
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				rwKey := types.NamespacedName{Name: clusterName + "-pooler-rw", Namespace: namespace}
				roKey := types.NamespacedName{Name: clusterName + "-pooler-ro", Namespace: namespace}

				caSecretName := seedCNPGClusterServerCASecret(ctx, k8sClient, clusterName, namespace)
				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGHealthy(cnpg, 1)
				cnpg.Status.Certificates.CertificatesConfiguration.ServerCASecret = caSecretName
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())

				reconcileAfterCNPGHealthyOrPatch()
				ensureCNPGServerTLSLeafSecret(ctx, k8sClient, clusterName, namespace)

				Eventually(func(g Gomega) {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())

					rw := &cnpgv1.Pooler{}
					g.Expect(k8sClient.Get(ctx, rwKey, rw)).To(Succeed())
					g.Expect(apierrors.IsNotFound(k8sClient.Get(ctx, roKey, &cnpgv1.Pooler{}))).To(BeTrue())

					if rw.Status.Instances < 2 {
						rw.Status.Instances = 2
						g.Expect(k8sClient.Status().Update(ctx, rw)).To(Succeed())
					}
				}, "20s", "250ms").Should(Succeed())

				pc := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				pc.Spec.Instances = ptr.To(int32(2))
				Expect(k8sClient.Update(ctx, pc)).To(Succeed())

				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGHealthy(cnpg, 2)
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())

				reconcileAfterCNPGHealthyOrPatch()
				ensureCNPGServerTLSLeafSecret(ctx, k8sClient, clusterName, namespace)

				Eventually(func(g Gomega) {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())

					rw := &cnpgv1.Pooler{}
					ro := &cnpgv1.Pooler{}
					g.Expect(k8sClient.Get(ctx, rwKey, rw)).To(Succeed())
					g.Expect(k8sClient.Get(ctx, roKey, ro)).To(Succeed())

					if ro.Status.Instances < 2 {
						ro.Status.Instances = 2
						g.Expect(k8sClient.Status().Update(ctx, ro)).To(Succeed())
					}
				}, "20s", "250ms").Should(Succeed())

				pcAfter := &platformv1alpha1.PostgresCluster{}
				Eventually(func(g Gomega) {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(k8sClient.Get(ctx, pgClusterKey, pcAfter)).To(Succeed())
					g.Expect(pcAfter.Status.Resources).NotTo(BeNil())
					g.Expect(pcAfter.Status.Resources.ConfigMapRef).NotTo(BeNil())
				}, "20s", "250ms").Should(Succeed())

				cm := &v1.ConfigMap{}
				cmKey := types.NamespacedName{Name: pcAfter.Status.Resources.ConfigMapRef.Name, Namespace: namespace}
				Expect(k8sClient.Get(ctx, cmKey, cm)).To(Succeed())
				Expect(cm.Data["CLUSTER_POOLER_RW_ENDPOINT"]).NotTo(BeEmpty())
				Expect(cm.Data["CLUSTER_POOLER_RO_ENDPOINT"]).NotTo(BeEmpty())
			})
		})
	})

	When("backup is configured", func() {
		Context("with backup enabled in class", func() {
			BeforeEach(func() {
				pgCluster.Spec.Class = classNameBackup
			})

			It("creates a ScheduledBackup and sets BackupReady condition after CNPG becomes healthy", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				// Before CNPG is healthy, provisioner blocks and backup component is not reached.
				pc := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				backupCond := meta.FindStatusCondition(pc.Status.Conditions, "BackupReady")
				Expect(backupCond).To(BeNil())

				// Simulate CNPG becoming healthy.
				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGClusterHealthy(cnpg, clusterName, "")
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				reconcileNTimes(1)

				// ScheduledBackup should be created.
				sbKey := types.NamespacedName{Name: clusterName + "-backup", Namespace: namespace}
				sb := &cnpgv1.ScheduledBackup{}
				Expect(k8sClient.Get(ctx, sbKey, sb)).To(Succeed())
				Expect(sb.Spec.Schedule).To(Equal("0 0 2 * * *"))
				Expect(sb.Spec.Method).To(Equal(cnpgv1.BackupMethodVolumeSnapshot))
				Expect(sb.Spec.Target).To(Equal(cnpgv1.BackupTargetStandby))
				Expect(sb.Spec.Cluster.Name).To(Equal(clusterName))

				// BackupReady condition should be True.
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				backupCond = meta.FindStatusCondition(pc.Status.Conditions, "BackupReady")
				Expect(backupCond).NotTo(BeNil())
				Expect(backupCond.Status).To(Equal(metav1.ConditionTrue))
				Expect(backupCond.Reason).To(Equal("BackupConfigured"))

				// BackupStatus should be populated.
				Expect(pc.Status.BackupStatus).NotTo(BeNil())
				Expect(pc.Status.BackupStatus.VolumeSnapshot).NotTo(BeNil())
				Expect(pc.Status.BackupStatus.VolumeSnapshot.Enabled).To(BeTrue())

				// Verify event.
				received := make([]string, 0, 16)
				CollectEvents(&received, fakeRecorder)
				Expect(ContainsEvent(received, v1.EventTypeNormal, core.EventBackupConfigured)).To(BeTrue(), "events: %v", received)
			})

			It("sets backup config on the CNPG Cluster spec", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				Expect(cnpg.Spec.Backup).NotTo(BeNil())
				Expect(cnpg.Spec.Backup.Target).To(Equal(cnpgv1.BackupTargetStandby))
				Expect(cnpg.Spec.Backup.VolumeSnapshot).NotTo(BeNil())
				Expect(cnpg.Spec.Backup.VolumeSnapshot.ClassName).To(Equal("csi-snapclass"))
				Expect(cnpg.Spec.Backup.VolumeSnapshot.Online).NotTo(BeNil())
				Expect(*cnpg.Spec.Backup.VolumeSnapshot.Online).To(BeTrue())
			})

			It("removes ScheduledBackup and condition when cluster overrides backup to disabled", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				// Make CNPG healthy and reconcile to create the ScheduledBackup.
				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGClusterHealthy(cnpg, clusterName, "")
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				reconcileNTimes(1)

				sbKey := types.NamespacedName{Name: clusterName + "-backup", Namespace: namespace}
				sb := &cnpgv1.ScheduledBackup{}
				Expect(k8sClient.Get(ctx, sbKey, sb)).To(Succeed())

				// Now disable backup on the cluster.
				current := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
				current.Spec.Backup = &platformv1alpha1.BackupConfig{Enabled: ptr.To(false)}
				Expect(k8sClient.Update(ctx, current)).To(Succeed())
				reconcileNTimes(2)

				// ScheduledBackup should be deleted.
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, sbKey, sb))).To(BeTrue())

				// BackupReady condition should indicate disabled.
				pc := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				backupCond := meta.FindStatusCondition(pc.Status.Conditions, "BackupReady")
				Expect(backupCond).NotTo(BeNil())
				Expect(backupCond.Status).To(Equal(metav1.ConditionTrue))
				Expect(backupCond.Reason).To(Equal("BackupDisabled"))
				Expect(pc.Status.BackupStatus).To(BeNil())
			})
		})

		Context("with backup disabled (default class)", func() {
			It("does not create ScheduledBackup and writes BackupReady with reason BackupDisabled", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGClusterHealthy(cnpg, clusterName, "")
				cnpg.Status.ManagedRolesStatus = cnpgv1.ManagedRoles{
					ByStatus: map[cnpgv1.RoleStatus][]string{
						cnpgv1.RoleStatusReconciled: {"app_user", "app_user_rw"},
					},
				}
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				reconcileNTimes(1)

				sbKey := types.NamespacedName{Name: clusterName + "-backup", Namespace: namespace}
				sb := &cnpgv1.ScheduledBackup{}
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, sbKey, sb))).To(BeTrue())

				pc := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				backupCond := meta.FindStatusCondition(pc.Status.Conditions, "BackupReady")
				Expect(backupCond).NotTo(BeNil())
				Expect(backupCond.Status).To(Equal(metav1.ConditionTrue))
				Expect(backupCond.Reason).To(Equal("BackupDisabled"))
				Expect(pc.Status.BackupStatus).To(BeNil())
			})
		})

		// Object-storage (barman) backup: a full apply -> reconcile -> status loop that
		// mirrors the volume-snapshot specs above but exercises the managed ObjectStore,
		// the barman-cloud WAL-archiver plugin on the CNPG Cluster, the plugin-method
		// ScheduledBackup, and the objectStore BackupStatus/ObjectStoreReady condition.
		Context("with object-storage (barman) backup enabled in class", func() {
			var classNameObjectStore string

			objectStoreKey := func() types.NamespacedName {
				return types.NamespacedName{Name: clusterName + "-object-store", Namespace: namespace}
			}
			getObjectStore := func() (*unstructured.Unstructured, error) {
				obj := &unstructured.Unstructured{}
				obj.SetGroupVersionKind(core.ObjectStoreGVK)
				err := k8sClient.Get(ctx, objectStoreKey(), obj)
				return obj, err
			}

			BeforeEach(func() {
				classNameObjectStore = classNamePrefix + "objectstore-" + fmt.Sprintf(
					"%d-%d-%d",
					GinkgoParallelProcess(),
					GinkgoRandomSeed(),
					CurrentSpecReport().LeafNodeLocation.LineNumber,
				)

				pgClassObjectStore := &platformv1alpha1.PostgresClusterClass{
					ObjectMeta: metav1.ObjectMeta{Name: classNameObjectStore},
					Spec: platformv1alpha1.PostgresClusterClassSpec{
						Provisioner: provisioner,
						Config: &platformv1alpha1.PostgresClusterClassConfig{
							Instances:       ptr.To(clusterMemberCount),
							Storage:         ptr.To(resource.MustParse(storageAmount)),
							PostgresVersion: ptr.To(postgresVersion),
							Backup: &platformv1alpha1.BackupConfig{
								Enabled:  ptr.To(true),
								Schedule: ptr.To("0 2 * * *"),
							},
						},
						CNPG: &platformv1alpha1.CNPGConfig{
							Backup: &platformv1alpha1.CNPGBackupConfig{
								Target: ptr.To("prefer-standby"),
								BarmanObjectStore: &platformv1alpha1.CNPGBarmanObjectStoreConfig{
									DestinationPath: "s3://test-bucket/clusters/",
									EndpointURL:     ptr.To("https://s3.us-east-1.amazonaws.com"),
									RetentionPolicy: ptr.To("30d"),
									S3Credentials: platformv1alpha1.CNPGBarmanS3Credentials{
										AccessKeyId: v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{Name: "s3-credentials"},
											Key:                  "accessKeyId",
										},
										SecretAccessKey: v1.SecretKeySelector{
											LocalObjectReference: v1.LocalObjectReference{Name: "s3-credentials"},
											Key:                  "secretAccessKey",
										},
									},
									WAL: &platformv1alpha1.CNPGBarmanWALConfig{Compression: ptr.To("gzip")},
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, pgClassObjectStore)).To(Succeed())

				pgCluster.Spec.Class = classNameObjectStore
			})

			AfterEach(func() {
				existing := &platformv1alpha1.PostgresClusterClass{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: classNameObjectStore}, existing)
				if err == nil {
					Expect(k8sClient.Delete(ctx, existing)).To(Succeed())
				} else {
					Expect(apierrors.IsNotFound(err)).To(BeTrue())
				}
			})

			It("reconciles the managed ObjectStore, barman plugin, plugin ScheduledBackup, and status", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				By("creating the managed ObjectStore owned by the PostgresCluster")
				obj, err := getObjectStore()
				Expect(err).NotTo(HaveOccurred())
				dest, found, err := unstructured.NestedString(obj.Object, "spec", "configuration", "destinationPath")
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(dest).To(Equal("s3://test-bucket/clusters/"))
				owner := metav1.GetControllerOf(obj)
				Expect(owner).NotTo(BeNil())
				Expect(owner.Kind).To(Equal("PostgresCluster"))
				Expect(owner.Name).To(Equal(clusterName))

				By("configuring the barman-cloud WAL archiver plugin on the CNPG Cluster")
				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				var barmanPlugin *cnpgv1.PluginConfiguration
				for i := range cnpg.Spec.Plugins {
					if cnpg.Spec.Plugins[i].Name == "barman-cloud.cloudnative-pg.io" {
						barmanPlugin = &cnpg.Spec.Plugins[i]
						break
					}
				}
				Expect(barmanPlugin).NotTo(BeNil(), "barman-cloud plugin should be set on the CNPG Cluster")
				Expect(barmanPlugin.IsWALArchiver).NotTo(BeNil())
				Expect(*barmanPlugin.IsWALArchiver).To(BeTrue())
				Expect(barmanPlugin.Parameters).To(HaveKeyWithValue("barmanObjectName", clusterName+"-object-store"))

				By("publishing the ObjectStoreReady condition")
				pc := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				objStoreCond := meta.FindStatusCondition(pc.Status.Conditions, "ObjectStoreReady")
				Expect(objStoreCond).NotTo(BeNil())
				Expect(objStoreCond.Status).To(Equal(metav1.ConditionTrue))
				Expect(objStoreCond.Reason).To(Equal("ObjectStoreConfigured"))

				By("creating a plugin-method ScheduledBackup once CNPG is healthy")
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGClusterHealthy(cnpg, clusterName, "")
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				reconcileNTimes(1)

				sbKey := types.NamespacedName{Name: clusterName + "-backup-objectstore", Namespace: namespace}
				sb := &cnpgv1.ScheduledBackup{}
				Expect(k8sClient.Get(ctx, sbKey, sb)).To(Succeed())
				Expect(sb.Spec.Method).To(Equal(cnpgv1.BackupMethodPlugin))
				Expect(sb.Spec.PluginConfiguration).NotTo(BeNil())
				Expect(sb.Spec.PluginConfiguration.Name).To(Equal("barman-cloud.cloudnative-pg.io"))
				Expect(sb.Spec.Cluster.Name).To(Equal(clusterName))

				By("populating objectStore BackupStatus and BackupReady condition")
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				backupCond := meta.FindStatusCondition(pc.Status.Conditions, "BackupReady")
				Expect(backupCond).NotTo(BeNil())
				Expect(backupCond.Status).To(Equal(metav1.ConditionTrue))
				Expect(backupCond.Reason).To(Equal("BackupConfigured"))
				Expect(pc.Status.BackupStatus).NotTo(BeNil())
				Expect(pc.Status.BackupStatus.ObjectStore).NotTo(BeNil())
				Expect(pc.Status.BackupStatus.ObjectStore.Enabled).To(BeTrue())

				By("garbage-collecting the ObjectStore when the cluster disables backup")
				current := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
				current.Spec.Backup = &platformv1alpha1.BackupConfig{Enabled: ptr.To(false)}
				Expect(k8sClient.Update(ctx, current)).To(Succeed())
				reconcileNTimes(2)

				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, sbKey, sb))).To(BeTrue())
				_, err = getObjectStore()
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			})
		})
	})

	When("deleting a PostgresCluster", func() {
		// PC-03
		Context("and clusterDeletionPolicy is set to Delete", func() {
			It("removes children and finalizer", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				pc := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				Expect(k8sClient.Delete(ctx, pc)).To(Succeed())

				Eventually(func() bool {
					_, err := reconciler.Reconcile(ctx, req)
					if err != nil {
						return false
					}
					getErr := k8sClient.Get(ctx, pgClusterKey, &platformv1alpha1.PostgresCluster{})
					return apierrors.IsNotFound(getErr)
				}, "30s", "250ms").Should(BeTrue())
			})
		})

		// PC-04
		Context("when clusterDeletionPolicy is set to Retain", func() {
			It("preserves retained resources and removes owner refs", func() {
				pgCluster.Spec.ClusterDeletionPolicy = ptr.To(retainPolicy)
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				pc := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				Expect(k8sClient.Delete(ctx, pc)).To(Succeed())

				Eventually(func() bool {
					_, err := reconciler.Reconcile(ctx, req)
					if err != nil {
						return false
					}
					getErr := k8sClient.Get(ctx, pgClusterKey, &platformv1alpha1.PostgresCluster{})
					return apierrors.IsNotFound(getErr)
				}, "30s", "250ms").Should(BeTrue())

				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed(), "CNPG cluster should be preserved under Retain policy")
				Expect(cnpg.OwnerReferences).To(BeEmpty(), "owner references should be removed from retained CNPG cluster")
			})
		})
	})

	When("reconciling with invalid or drifted dependencies", func() {
		// PC-05
		Context("when referenced class does not exist", func() {
			It("fails with class-not-found condition and emits a warning event", func() {
				badName := "bad-" + clusterName
				badKey := types.NamespacedName{Name: badName, Namespace: namespace}

				bad := &platformv1alpha1.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: badName, Namespace: namespace},
					Spec:       platformv1alpha1.PostgresClusterSpec{Class: "missing-class"},
				}
				Expect(k8sClient.Create(ctx, bad)).To(Succeed())
				DeferCleanup(func() { _ = k8sClient.Delete(ctx, bad) })

				// pass 1 adds finalizer, pass 2 reaches class lookup and sets failure condition.
				_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: badKey})
				Expect(err).NotTo(HaveOccurred())
				_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: badKey})
				Expect(err).To(HaveOccurred())

				Eventually(func() bool {
					current := &platformv1alpha1.PostgresCluster{}
					if err := k8sClient.Get(ctx, badKey, current); err != nil {
						return false
					}
					cond := meta.FindStatusCondition(current.Status.Conditions, "ClusterReady")
					return cond != nil && cond.Reason == "ClusterClassNotFound"
				}, "20s", "250ms").Should(BeTrue())

				received := make([]string, 0, 8)
				CollectEvents(&received, fakeRecorder)
				Expect(ContainsEvent(
					received,
					v1.EventTypeWarning, core.EventClusterClassNotFound,
				)).To(BeTrue(), "events seen: %v", received)
			})
		})

		// PC-06
		Context("when managed child spec drifts from desired state", func() {
			It("restores drifted managed spec", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				cnpg.Spec.Instances = 8
				Expect(k8sClient.Update(ctx, cnpg)).To(Succeed())

				reconcileNTimes(2)
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				Expect(cnpg.Spec.Instances).To(Equal(int(clusterMemberCount)))
			})

			It("removes stale PostgreSQL parameters and preserves externally owned parameters", func() {
				pgCluster.Spec.PostgreSQLConfig = map[string]string{
					"shared_buffers":  "256MB",
					"max_connections": "200",
				}
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				Expect(cnpg.Spec.PostgresConfiguration.Parameters).To(HaveKeyWithValue("shared_buffers", "256MB"))
				Expect(cnpg.Spec.PostgresConfiguration.Parameters).To(HaveKeyWithValue("max_connections", "200"))

				applyCNPGPostgreSQLParameters(ctx, k8sClient, clusterName, namespace, "external-postgresql-parameters", map[string]string{
					"application_name": "keep-me",
				})

				current := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
				current.Spec.PostgreSQLConfig = map[string]string{
					"shared_buffers": "256MB",
				}
				Expect(k8sClient.Update(ctx, current)).To(Succeed())

				Eventually(func(g Gomega) map[string]string {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())

					updated := &cnpgv1.Cluster{}
					g.Expect(k8sClient.Get(ctx, pgClusterKey, updated)).To(Succeed())
					return updated.Spec.PostgresConfiguration.Parameters
				}, "20s", "100ms").Should(SatisfyAll(
					HaveKeyWithValue("shared_buffers", "256MB"),
					HaveKeyWithValue("application_name", "keep-me"),
					Not(HaveKey("max_connections")),
				))
			})
		})

		Context("when scaling instances on PostgresCluster", func() {
			It("propagates scale-out to the underlying CNPG cluster", func() {
				pgCluster.Spec.Instances = ptr.To(int32(2))
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				Expect(cnpg.Spec.Instances).To(Equal(2))
				caSecretName := seedCNPGClusterServerCASecret(ctx, k8sClient, clusterName, namespace)
				markCNPGHealthy(cnpg, 2)
				cnpg.Status.Certificates.CertificatesConfiguration.ServerCASecret = caSecretName
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())

				Eventually(func(g Gomega) {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())

					current := &platformv1alpha1.PostgresCluster{}
					g.Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
					g.Expect(current.Status.Phase).NotTo(BeNil())
					g.Expect(*current.Status.Phase).To(Equal("Ready"))
				}, "20s", "250ms").Should(Succeed())

				pc := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				pc.Spec.Instances = ptr.To(int32(3))
				Expect(k8sClient.Update(ctx, pc)).To(Succeed())

				Eventually(func(g Gomega) {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())

					g.Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
					g.Expect(cnpg.Spec.Instances).To(Equal(3))
				}, "20s", "250ms").Should(Succeed())

				// CNPG keeps its own phase=Healthy during scale-up while it
				// builds the new replica; the only signal that the change is
				// in progress is that ReadyInstances trails the desired count.
				// Assert the PostgresCluster reflects this as Provisioning so
				// consumers don't miss the in-progress state.
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				cnpg.Status.Phase = cnpgv1.PhaseHealthy
				cnpg.Status.Instances = 3
				cnpg.Status.ReadyInstances = 2
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())

				Eventually(func(g Gomega) {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())

					current := &platformv1alpha1.PostgresCluster{}
					g.Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
					g.Expect(current.Status.Phase).NotTo(BeNil())
					g.Expect(*current.Status.Phase).To(Equal("Provisioning"))
				}, "20s", "250ms").Should(Succeed())
			})

			It("holds phase=Provisioning while CNPG is resizing PVCs", func() {
				pgCluster.Spec.Instances = ptr.To(int32(1))
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				caSecretName := seedCNPGClusterServerCASecret(ctx, k8sClient, clusterName, namespace)
				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGHealthy(cnpg, 1)
				cnpg.Status.Certificates.CertificatesConfiguration.ServerCASecret = caSecretName
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())

				Eventually(func(g Gomega) {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())

					current := &platformv1alpha1.PostgresCluster{}
					g.Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
					g.Expect(current.Status.Phase).NotTo(BeNil())
					g.Expect(*current.Status.Phase).To(Equal("Ready"))
				}, "20s", "250ms").Should(Succeed())

				// Simulate a storage resize: CNPG has applied the new size but
				// PVCs are still expanding. CNPG reports this via Status.ResizingPVC.
				// The operator should hold phase=Provisioning until it is empty.
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				cnpg.Status.ResizingPVC = []string{cnpg.Name + "-1"}
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())

				Eventually(func(g Gomega) {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())

					current := &platformv1alpha1.PostgresCluster{}
					g.Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
					g.Expect(current.Status.Phase).NotTo(BeNil())
					g.Expect(*current.Status.Phase).To(Equal("Provisioning"))

					cond := meta.FindStatusCondition(current.Status.Conditions, "ClusterReady")
					g.Expect(cond).NotTo(BeNil())
					g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
					g.Expect(cond.Reason).To(Equal("CNPGClusterProvisioning"))
					g.Expect(cond.Message).To(Equal("Resizing storage: 1/1 PVCs pending"))
				}, "20s", "250ms").Should(Succeed())

				// Once all PVCs have resized (ResizingPVC cleared), phase should return to Ready.
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				cnpg.Status.ResizingPVC = nil
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())

				Eventually(func(g Gomega) {
					_, err := reconciler.Reconcile(ctx, req)
					g.Expect(err).NotTo(HaveOccurred())

					current := &platformv1alpha1.PostgresCluster{}
					g.Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
					g.Expect(current.Status.Phase).NotTo(BeNil())
					g.Expect(*current.Status.Phase).To(Equal("Ready"))
				}, "20s", "250ms").Should(Succeed())
			})

			It("publishes empty read-only endpoint values when running with a single instance", func() {
				pgCluster.Spec.Instances = ptr.To(int32(1))
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				caSecretName := seedCNPGClusterServerCASecret(ctx, k8sClient, clusterName, namespace)
				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGHealthy(cnpg, 1)
				cnpg.Status.Certificates.CertificatesConfiguration.ServerCASecret = caSecretName
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				reconcileNTimes(2)

				pc := &platformv1alpha1.PostgresCluster{}
				Eventually(func() bool {
					if err := k8sClient.Get(ctx, pgClusterKey, pc); err != nil {
						return false
					}
					return pc.Status.Resources != nil && pc.Status.Resources.ConfigMapRef != nil
				}, "5s", "100ms").Should(BeTrue())

				cm := &v1.ConfigMap{}
				cmKey := types.NamespacedName{Name: pc.Status.Resources.ConfigMapRef.Name, Namespace: namespace}
				Expect(k8sClient.Get(ctx, cmKey, cm)).To(Succeed())
				Expect(cm.Data).To(HaveKey("CLUSTER_RO_ENDPOINT"))
				Expect(cm.Data["CLUSTER_RO_ENDPOINT"]).To(BeEmpty())
				Expect(cm.Data["CLUSTER_RW_ENDPOINT"]).NotTo(BeEmpty())
			})

			It("populates the read-only endpoint when scaling 1->2", func() {
				pgCluster.Spec.Instances = ptr.To(int32(1))
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				caSecretName := seedCNPGClusterServerCASecret(ctx, k8sClient, clusterName, namespace)
				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGHealthy(cnpg, 1)
				cnpg.Status.Certificates.CertificatesConfiguration.ServerCASecret = caSecretName
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				reconcileNTimes(2)

				// Confirm RO endpoint is suppressed before scaling out.
				pc := &platformv1alpha1.PostgresCluster{}
				Eventually(func() bool {
					if err := k8sClient.Get(ctx, pgClusterKey, pc); err != nil {
						return false
					}
					return pc.Status.Resources != nil && pc.Status.Resources.ConfigMapRef != nil
				}, "5s", "100ms").Should(BeTrue())

				cmKey := types.NamespacedName{Name: pc.Status.Resources.ConfigMapRef.Name, Namespace: namespace}
				cm := &v1.ConfigMap{}
				Expect(k8sClient.Get(ctx, cmKey, cm)).To(Succeed())
				Expect(cm.Data["CLUSTER_RO_ENDPOINT"]).To(BeEmpty())

				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				pc.Spec.Instances = ptr.To(int32(2))
				Expect(k8sClient.Update(ctx, pc)).To(Succeed())

				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGHealthy(cnpg, 2)
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				reconcileNTimes(3)

				Expect(k8sClient.Get(ctx, cmKey, cm)).To(Succeed())
				Expect(cm.Data["CLUSTER_RO_ENDPOINT"]).NotTo(BeEmpty())
			})

			It("allows clearing spec.instances to fall back to the class default", func() {
				pgCluster.Spec.Instances = ptr.To(clusterMemberCount)
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())

				pc := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				pc.Spec.Instances = nil
				Expect(k8sClient.Update(ctx, pc)).To(Succeed())

				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				Expect(pc.Spec.Instances).To(BeNil())
			})
		})

		Context("when a configmap spec changes", func() {
			It("does not let unrelated unpublished database status block requested custom metrics indefinitely", func() {
				const (
					queryKey  = "queries.yaml"
					queryName = "cluster_metric_with_unrelated_database_failure"
				)
				sourceName := clusterName + "-cluster-metrics-source"
				databaseResourceName := "unrelated-" + strings.TrimPrefix(clusterName, clusterNamePrefix)
				generatedKey := types.NamespacedName{Name: clusterName + "-metrics", Namespace: namespace}

				source := &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: sourceName, Namespace: namespace},
					Data: map[string]string{queryKey: fmt.Sprintf(`%s:
  type: gauge
  help: "Cluster metric independent of database provisioning"
  query: "SELECT 1 AS value"
  value: value
`, queryName)},
				}
				Expect(k8sClient.Create(ctx, source)).To(Succeed())
				DeferCleanup(func() {
					err := k8sClient.Delete(context.Background(), source)
					Expect(err == nil || apierrors.IsNotFound(err)).To(BeTrue())
				})

				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				caSecretName := seedCNPGClusterServerCASecret(ctx, k8sClient, clusterName, namespace)
				DeferCleanup(func() {
					err := k8sClient.Delete(context.Background(), &v1.Secret{
						ObjectMeta: metav1.ObjectMeta{Name: caSecretName, Namespace: namespace},
					})
					Expect(err == nil || apierrors.IsNotFound(err)).To(BeTrue())
				})
				markCNPGHealthy(cnpg, clusterMemberCount)
				cnpg.Status.Certificates.CertificatesConfiguration.ServerCASecret = caSecretName
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				reconcileAfterCNPGHealthyOrPatch()

				unrelatedDatabase := &platformv1alpha1.PostgresDatabase{
					ObjectMeta: metav1.ObjectMeta{Name: databaseResourceName, Namespace: namespace},
					Spec: platformv1alpha1.PostgresDatabaseSpec{
						ClusterRef: v1.LocalObjectReference{Name: clusterName},
						Databases: []platformv1alpha1.DatabaseDefinition{{
							Name: "unrelated",
							PasswordConfig: &platformv1alpha1.PasswordConfig{
								ExternalAdminSecretRef: v1.LocalObjectReference{Name: databaseResourceName + "-missing-admin"},
								ExternalRWSecretRef:    v1.LocalObjectReference{Name: databaseResourceName + "-missing-rw"},
							},
						}},
					},
				}
				Expect(k8sClient.Create(ctx, unrelatedDatabase)).To(Succeed())
				unrelatedDatabaseKey := types.NamespacedName{Name: databaseResourceName, Namespace: namespace}
				DeferCleanup(func() {
					current := &platformv1alpha1.PostgresDatabase{}
					if err := k8sClient.Get(context.Background(), unrelatedDatabaseKey, current); err != nil {
						Expect(apierrors.IsNotFound(err)).To(BeTrue())
						return
					}
					controllerutil.RemoveFinalizer(current, postgresDatabaseFinalizer)
					Expect(k8sClient.Update(context.Background(), current)).To(Succeed())
					err := k8sClient.Delete(context.Background(), current)
					Expect(err == nil || apierrors.IsNotFound(err)).To(BeTrue())
				})

				By("driving the database into an unrelated provisioning failure")
				Eventually(func(g Gomega) {
					_, _ = reconcilePostgresDatabase(ctx, unrelatedDatabaseKey)
					current := &platformv1alpha1.PostgresDatabase{}
					g.Expect(k8sClient.Get(ctx, unrelatedDatabaseKey, current)).To(Succeed())
					condition := meta.FindStatusCondition(current.Status.Conditions, "SecretsReady")
					g.Expect(condition).NotTo(BeNil())
					g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
					g.Expect(condition.Reason).To(Equal("ExternalSecretMissing"))
				}, "5s", "100ms").Should(Succeed())

				currentCluster := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, currentCluster)).To(Succeed())
				currentCluster.Spec.Monitoring = &platformv1alpha1.PostgresClusterMonitoring{
					CustomQueriesConfigMap: []v1.ConfigMapKeySelector{{
						LocalObjectReference: v1.LocalObjectReference{Name: sourceName},
						Key:                  queryKey,
					}},
				}
				Expect(k8sClient.Update(ctx, currentCluster)).To(Succeed())
				reconcileAfterCNPGHealthyOrPatch()
				acknowledgeCNPGMetricsConfigMap(true)
				reconcileNTimes(1)

				currentCluster = &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, currentCluster)).To(Succeed())
				condition := meta.FindStatusCondition(currentCluster.Status.Conditions, "CustomMetricsReady")
				Expect(condition).NotTo(BeNil())
				Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				Expect(condition.Reason).To(Equal("CustomMetricsReady"))

				generated := &v1.ConfigMap{}
				Expect(k8sClient.Get(ctx, generatedKey, generated)).To(Succeed())
				Expect(generated.Data[queryKey]).To(ContainSubstring(queryName))
			})

			It("consumes only committed database contributions and publishes acknowledgements", func() {
				const (
					queryKey  = "queries.yaml"
					queryName = "controller_database_handshake"
					database  = "appdb"
				)
				sourceName := clusterName + "-database-metrics-source"
				databaseResourceName := clusterName + "-databases"
				generatedKey := types.NamespacedName{Name: clusterName + "-metrics", Namespace: namespace}
				source := &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: sourceName, Namespace: namespace},
					Data: map[string]string{queryKey: fmt.Sprintf(`%s:
  type: gauge
  help: "Controller database handshake metric"
  query: "SELECT 1 AS value"
  value: value
`, queryName)},
				}
				Expect(k8sClient.Create(ctx, source)).To(Succeed())
				postgresDatabase := &platformv1alpha1.PostgresDatabase{
					ObjectMeta: metav1.ObjectMeta{Name: databaseResourceName, Namespace: namespace},
					Spec: platformv1alpha1.PostgresDatabaseSpec{
						ClusterRef: v1.LocalObjectReference{Name: clusterName},
						Databases: []platformv1alpha1.DatabaseDefinition{{
							Name: database,
							Monitoring: &platformv1alpha1.DatabaseMonitoring{
								CustomQueriesConfigMap: []v1.ConfigMapKeySelector{{
									LocalObjectReference: v1.LocalObjectReference{Name: sourceName},
									Key:                  queryKey,
								}},
							},
						}},
					},
				}
				Expect(k8sClient.Create(ctx, postgresDatabase)).To(Succeed())

				By("ignoring raw database spec before its controller publishes status")
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)
				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGHealthy(cnpg, clusterMemberCount)
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				reconcileAfterCNPGHealthyOrPatch()

				currentCluster := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, currentCluster)).To(Succeed())
				condition := meta.FindStatusCondition(currentCluster.Status.Conditions, "CustomMetricsReady")
				Expect(condition).NotTo(BeNil())
				Expect(condition.Status).To(Equal(metav1.ConditionFalse))
				Expect(condition.Reason).To(Equal("CustomMetricsPending"))
				Expect(currentCluster.Status.CustomMetricsStatus).To(BeNil())
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, generatedKey, &v1.ConfigMap{}))).To(BeTrue())

				By("aggregating the committed status contribution")
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: databaseResourceName, Namespace: namespace}, postgresDatabase)).To(Succeed())
				revision := mtypes.ContributionRevision(database, true, []mtypes.QuerySelector{{
					ConfigMapName: sourceName,
					ConfigMapKey:  queryKey,
				}})
				postgresDatabase.Status.CustomMetricsPublication = &platformv1alpha1.PostgresDatabaseCustomMetricsPublication{
					ObservedGeneration: postgresDatabase.Generation,
					Contributions: []platformv1alpha1.DatabaseCustomMetricsContribution{{
						DatabaseName: database,
						Revision:     revision,
						Exists:       true,
						CustomQueriesConfigMap: []v1.ConfigMapKeySelector{{
							LocalObjectReference: v1.LocalObjectReference{Name: sourceName},
							Key:                  queryKey,
						}},
					}},
				}
				Expect(k8sClient.Status().Update(ctx, postgresDatabase)).To(Succeed())
				reconcileNTimes(1)

				generated := &v1.ConfigMap{}
				Expect(k8sClient.Get(ctx, generatedKey, generated)).To(Succeed())
				acknowledgeCNPGMetricsConfigMap(true)
				reconcileNTimes(1)
				Expect(k8sClient.Get(ctx, generatedKey, generated)).To(Succeed())
				Expect(generated.Data[queryKey]).To(ContainSubstring(database + ":" + queryName))
				Expect(k8sClient.Get(ctx, pgClusterKey, currentCluster)).To(Succeed())
				Expect(currentCluster.Status.CustomMetricsStatus).NotTo(BeNil())
				Expect(currentCluster.Status.CustomMetricsStatus.DatabaseContributions).To(HaveLen(1))
				ack := currentCluster.Status.CustomMetricsStatus.DatabaseContributions[0]
				Expect(ack.PostgresDatabaseName).To(Equal(databaseResourceName))
				Expect(ack.PostgresDatabaseUID).To(Equal(string(postgresDatabase.UID)))
				Expect(ack.DatabaseName).To(Equal(database))
				Expect(ack.DesiredRevision).To(Equal(revision))
				Expect(ack.AppliedRevision).To(Equal(revision))
				Expect(ack.Status).To(Equal(metav1.ConditionTrue))

				By("retaining the applied revision while negatively acknowledging invalid source data")
				baseline := generated.DeepCopy()
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: sourceName, Namespace: namespace}, source)).To(Succeed())
				source.Data[queryKey] = fmt.Sprintf(`%s:
  type: histogram
  help: "Invalid database handshake metric"
  query: "SELECT 1 AS value"
  value: value
`, queryName)
				Expect(k8sClient.Update(ctx, source)).To(Succeed())
				reconcileNTimes(1)
				Expect(k8sClient.Get(ctx, generatedKey, generated)).To(Succeed())
				Expect(generated.UID).To(Equal(baseline.UID))
				Expect(generated.Data).To(Equal(baseline.Data))
				Expect(k8sClient.Get(ctx, pgClusterKey, currentCluster)).To(Succeed())
				ack = currentCluster.Status.CustomMetricsStatus.DatabaseContributions[0]
				Expect(ack.DesiredRevision).To(Equal(revision))
				Expect(ack.AppliedRevision).To(Equal(revision))
				Expect(ack.Status).To(Equal(metav1.ConditionFalse))
				Expect(ack.Reason).To(Equal("InvalidQueryDefinition"))

				By("applying an explicit disabled contribution")
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: databaseResourceName, Namespace: namespace}, postgresDatabase)).To(Succeed())
				disabledRevision := mtypes.ContributionRevision(database, false, nil)
				postgresDatabase.Status.CustomMetricsPublication.Contributions[0] = platformv1alpha1.DatabaseCustomMetricsContribution{
					DatabaseName: database,
					Revision:     disabledRevision,
					Exists:       false,
				}
				Expect(k8sClient.Status().Update(ctx, postgresDatabase)).To(Succeed())
				reconcileNTimes(1)
				acknowledgeCNPGMetricsConfigMap(false)
				reconcileNTimes(1)
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, generatedKey, &v1.ConfigMap{}))).To(BeTrue())
				Expect(k8sClient.Get(ctx, pgClusterKey, currentCluster)).To(Succeed())
				ack = currentCluster.Status.CustomMetricsStatus.DatabaseContributions[0]
				Expect(ack.DesiredRevision).To(Equal(disabledRevision))
				Expect(ack.AppliedRevision).To(Equal(disabledRevision))
				Expect(ack.Status).To(Equal(metav1.ConditionTrue))
				Expect(ack.Reason).To(Equal("CustomMetricsDisabled"))
			})

			It("reconciles custom metrics through invalid sources and ownership collisions", func() {
				const (
					generatedHashAnnotation = "platform.splunk.com/monitoring-config-hash"
					queryKey                = "queries.yaml"
					queryName               = "controller_database_count"
					uniqueLosingQuery       = "controller_losing_package_marker"
				)

				sourceName := clusterName + "-custom-metrics-source"
				collisionSourceName := clusterName + "-custom-metrics-collision"
				generatedName := clusterName + "-metrics"
				generatedKey := types.NamespacedName{Name: generatedName, Namespace: namespace}
				safetyKey := types.NamespacedName{Name: generatedName + "-lkg", Namespace: namespace}
				validQuery := fmt.Sprintf(`%s:
  type: gauge
  help: "Controller integration metric"
  query: "SELECT count(*) AS db_count FROM pg_database"
  value: db_count
`, queryName)

				defer func() {
					for _, name := range []string{sourceName, collisionSourceName} {
						_ = k8sClient.Delete(ctx, &v1.ConfigMap{
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
						})
					}
				}()

				var foreignUID types.UID
				defer func() {
					if foreignUID == "" {
						return
					}
					cm := &v1.ConfigMap{}
					if err := k8sClient.Get(ctx, generatedKey, cm); err == nil && cm.UID == foreignUID {
						_ = k8sClient.Delete(ctx, cm)
					}
				}()

				monitoringCondition := func(status metav1.ConditionStatus, reason string, messageParts ...string) *metav1.Condition {
					GinkgoHelper()
					current := &platformv1alpha1.PostgresCluster{}
					Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
					condition := meta.FindStatusCondition(current.Status.Conditions, "CustomMetricsReady")
					Expect(condition).NotTo(BeNil())
					Expect(condition.Status).To(Equal(status))
					Expect(condition.Reason).To(Equal(reason))
					Expect(condition.ObservedGeneration).To(Equal(current.Generation))
					for _, part := range messageParts {
						Expect(condition.Message).To(ContainSubstring(part))
					}
					return condition
				}

				getGenerated := func() *v1.ConfigMap {
					GinkgoHelper()
					cm := &v1.ConfigMap{}
					Expect(k8sClient.Get(ctx, generatedKey, cm)).To(Succeed())
					return cm
				}

				hasGeneratedSelector := func() bool {
					GinkgoHelper()
					cnpg := &cnpgv1.Cluster{}
					Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
					if cnpg.Spec.Monitoring == nil {
						return false
					}
					for _, selector := range cnpg.Spec.Monitoring.CustomQueriesConfigMap {
						if selector.Name == generatedName && selector.Key == queryKey {
							return true
						}
					}
					return false
				}

				assertLastKnownGood := func(baseline *v1.ConfigMap) {
					GinkgoHelper()
					current := getGenerated()
					Expect(current.UID).To(Equal(baseline.UID))
					Expect(current.Data).To(Equal(baseline.Data))
					Expect(current.BinaryData).To(Equal(baseline.BinaryData))
					Expect(current.Annotations).To(Equal(baseline.Annotations))
					Expect(current.OwnerReferences).To(Equal(baseline.OwnerReferences))
					Expect(hasGeneratedSelector()).To(BeTrue())
				}

				By("creating a valid source and reconciling the happy path")
				source := &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: sourceName, Namespace: namespace},
					Data:       map[string]string{queryKey: validQuery},
				}
				Expect(k8sClient.Create(ctx, source)).To(Succeed())
				pgCluster.Spec.Monitoring = &platformv1alpha1.PostgresClusterMonitoring{
					CustomQueriesConfigMap: []v1.ConfigMapKeySelector{{
						LocalObjectReference: v1.LocalObjectReference{Name: sourceName},
						Key:                  queryKey,
					}},
				}
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGHealthy(cnpg, clusterMemberCount)
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				reconcileAfterCNPGHealthyOrPatch()
				acknowledgeCNPGMetricsConfigMap(true)
				reconcileNTimes(1)

				monitoringCondition(metav1.ConditionTrue, "CustomMetricsReady")
				baseline := getGenerated().DeepCopy()
				Expect(baseline.Data[queryKey]).To(ContainSubstring(queryName))
				Expect(baseline.Data[queryKey]).To(ContainSubstring("usage: GAUGE"))
				Expect(baseline.Annotations[generatedHashAnnotation]).NotTo(BeEmpty())
				Expect(hasGeneratedSelector()).To(BeTrue())
				controllerOwner := metav1.GetControllerOf(baseline)
				Expect(controllerOwner).NotTo(BeNil())
				Expect(controllerOwner.APIVersion).To(Equal(cnpgv1.SchemeGroupVersion.String()))
				Expect(controllerOwner.Kind).To(Equal("Cluster"))
				Expect(controllerOwner.Name).To(Equal(clusterName))
				Expect(controllerOwner.UID).To(Equal(cnpg.UID))
				safety := &v1.ConfigMap{}
				Expect(k8sClient.Get(ctx, safetyKey, safety)).To(Succeed())
				safetyOwner := metav1.GetControllerOf(safety)
				Expect(safetyOwner).NotTo(BeNil())
				Expect(safetyOwner.APIVersion).To(Equal(platformv1alpha1.GroupVersion.String()))
				Expect(safetyOwner.Kind).To(Equal("PostgresCluster"))
				Expect(safetyOwner.Name).To(Equal(clusterName))
				Expect(safetyOwner.UID).To(Equal(pgCluster.UID))

				received := make([]string, 0, 16)
				CollectEvents(&received, fakeRecorder)

				By("deleting the source before removing its reference")
				Expect(k8sClient.Delete(ctx, source)).To(Succeed())
				_, err := reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				monitoringCondition(metav1.ConditionFalse, "CustomMetricsConfigMapNotFound", sourceName, queryKey)
				assertLastKnownGood(baseline)
				received = received[:0]
				CollectEvents(&received, fakeRecorder)
				Expect(ContainsEvent(received, v1.EventTypeWarning, core.EventCustomMetricsConfigMapNotFound)).To(
					BeTrue(), "events seen: %v", received)

				By("restoring the confirmed payload after the generated ConfigMap is deleted while the source remains invalid")
				Expect(k8sClient.Delete(ctx, getGenerated())).To(Succeed())
				_, err = reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				acknowledgeCNPGMetricsConfigMap(true)
				reconcileNTimes(1)
				monitoringCondition(metav1.ConditionFalse, "CustomMetricsConfigMapNotFound", sourceName, queryKey)
				restoredAfterDeletion := getGenerated().DeepCopy()
				Expect(restoredAfterDeletion.UID).NotTo(Equal(baseline.UID))
				Expect(restoredAfterDeletion.Data).To(Equal(baseline.Data))
				Expect(restoredAfterDeletion.Annotations).To(Equal(baseline.Annotations))
				Expect(hasGeneratedSelector()).To(BeTrue())
				baseline = restoredAfterDeletion
				received = received[:0]
				CollectEvents(&received, fakeRecorder)
				Expect(ContainsEvent(received, v1.EventTypeNormal, core.EventCustomMetricsQueryRepaired)).To(
					BeTrue(), "events seen: %v", received)

				By("recreating the source with invalid but readable query YAML")
				source = &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: sourceName, Namespace: namespace},
					Data: map[string]string{queryKey: fmt.Sprintf(`%s:
  type: histogram
  help: "Unsupported metric type"
  query: "SELECT 1 AS value"
  value: value
`, queryName)},
				}
				Expect(k8sClient.Create(ctx, source)).To(Succeed())
				_, err = reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				monitoringCondition(metav1.ConditionFalse, "InvalidQueryDefinition", sourceName, "histogram")
				assertLastKnownGood(baseline)
				received = received[:0]
				CollectEvents(&received, fakeRecorder)
				Expect(ContainsEvent(received, v1.EventTypeWarning, core.EventCustomMetricsInvalidQuery)).To(
					BeTrue(), "events seen: %v", received)

				By("repairing generated ConfigMap data drift while the source remains invalid")
				drifted := getGenerated()
				drifted.Data[queryKey] = "drifted"
				Expect(k8sClient.Update(ctx, drifted)).To(Succeed())
				_, err = reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				acknowledgeCNPGMetricsConfigMap(true)
				reconcileNTimes(1)
				monitoringCondition(metav1.ConditionFalse, "InvalidQueryDefinition", sourceName, "histogram")
				assertLastKnownGood(baseline)
				received = received[:0]
				CollectEvents(&received, fakeRecorder)
				Expect(ContainsEvent(received, v1.EventTypeNormal, core.EventCustomMetricsQueryRepaired)).To(
					BeTrue(), "events seen: %v", received)

				By("restoring the valid source")
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: sourceName, Namespace: namespace}, source)).To(Succeed())
				source.Data[queryKey] = validQuery
				Expect(k8sClient.Update(ctx, source)).To(Succeed())
				_, err = reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				monitoringCondition(metav1.ConditionTrue, "CustomMetricsReady")
				assertLastKnownGood(baseline)

				By("adding a later source package with a duplicate and a unique sibling")
				collisionSource := &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: collisionSourceName, Namespace: namespace},
					Data: map[string]string{queryKey: fmt.Sprintf(`%s:
  type: gauge
  help: "Duplicate metric"
  query: "SELECT 2 AS db_count"
  value: db_count
%s:
  type: gauge
  help: "Must be dropped with its package"
  query: "SELECT 1 AS marker"
  value: marker
`, queryName, uniqueLosingQuery)},
				}
				Expect(k8sClient.Create(ctx, collisionSource)).To(Succeed())
				current := &platformv1alpha1.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
				current.Spec.Monitoring.CustomQueriesConfigMap = append(
					current.Spec.Monitoring.CustomQueriesConfigMap,
					v1.ConfigMapKeySelector{
						LocalObjectReference: v1.LocalObjectReference{Name: collisionSourceName},
						Key:                  queryKey,
					},
				)
				Expect(k8sClient.Update(ctx, current)).To(Succeed())
				_, err = reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				monitoringCondition(
					metav1.ConditionFalse,
					"MetricNameCollision",
					queryName,
					sourceName,
					collisionSourceName,
				)
				assertLastKnownGood(baseline)
				Expect(getGenerated().Data[queryKey]).NotTo(ContainSubstring(uniqueLosingQuery))
				received = received[:0]
				CollectEvents(&received, fakeRecorder)
				Expect(ContainsEvent(received, v1.EventTypeWarning, core.EventCustomMetricsCollision)).To(
					BeTrue(), "events seen: %v", received)

				By("removing the colliding package and recovering")
				Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
				current.Spec.Monitoring.CustomQueriesConfigMap = current.Spec.Monitoring.CustomQueriesConfigMap[:1]
				Expect(k8sClient.Update(ctx, current)).To(Succeed())
				_, err = reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				monitoringCondition(metav1.ConditionTrue, "CustomMetricsReady")
				assertLastKnownGood(baseline)

				By("disabling custom metrics before introducing a foreign generated ConfigMap")
				Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
				current.Spec.Monitoring.CustomQueriesConfigMap = nil
				Expect(k8sClient.Update(ctx, current)).To(Succeed())
				_, err = reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				acknowledgeCNPGMetricsConfigMap(false)
				reconcileNTimes(1)
				monitoringCondition(metav1.ConditionTrue, "CustomMetricsDisabled")
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, generatedKey, &v1.ConfigMap{}))).To(BeTrue())
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, safetyKey, &v1.ConfigMap{}))).To(BeTrue())
				Expect(hasGeneratedSelector()).To(BeFalse())

				foreign := &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:        generatedName,
						Namespace:   namespace,
						Annotations: map[string]string{"integration-test": "do-not-adopt"},
					},
					Data: map[string]string{queryKey: "foreign-sentinel"},
				}
				Expect(k8sClient.Create(ctx, foreign)).To(Succeed())
				foreignUID = foreign.UID
				foreignBaseline := foreign.DeepCopy()

				Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
				current.Spec.Monitoring.CustomQueriesConfigMap = []v1.ConfigMapKeySelector{{
					LocalObjectReference: v1.LocalObjectReference{Name: sourceName},
					Key:                  queryKey,
				}}
				Expect(k8sClient.Update(ctx, current)).To(Succeed())
				_, err = reconciler.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				monitoringCondition(
					metav1.ConditionFalse,
					"GeneratedResourceOwnershipConflict",
					generatedName,
					"is not controlled by CNPG Cluster",
				)
				Expect(hasGeneratedSelector()).To(BeFalse())

				foreignCurrent := &v1.ConfigMap{}
				Expect(k8sClient.Get(ctx, generatedKey, foreignCurrent)).To(Succeed())
				Expect(foreignCurrent.UID).To(Equal(foreignBaseline.UID))
				Expect(foreignCurrent.Data).To(Equal(foreignBaseline.Data))
				Expect(foreignCurrent.BinaryData).To(Equal(foreignBaseline.BinaryData))
				Expect(foreignCurrent.Annotations).To(Equal(foreignBaseline.Annotations))
				Expect(foreignCurrent.OwnerReferences).To(Equal(foreignBaseline.OwnerReferences))

				By("deleting the foreign ConfigMap and reconciling recovery")
				Expect(k8sClient.Delete(ctx, foreignCurrent)).To(Succeed())
				foreignUID = ""
				reconcileAfterCNPGHealthyOrPatch()
				acknowledgeCNPGMetricsConfigMap(true)
				reconcileNTimes(1)
				monitoringCondition(metav1.ConditionTrue, "CustomMetricsReady")
				recovered := getGenerated()
				Expect(recovered.Data[queryKey]).To(ContainSubstring(queryName))
				Expect(recovered.UID).NotTo(Equal(foreignBaseline.UID))
				Expect(metav1.IsControlledBy(recovered, cnpg)).To(BeTrue())
				Expect(hasGeneratedSelector()).To(BeTrue())
			})

			It("emits ConfigMapReconciled event on configmap update", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				caSecretName := seedCNPGClusterServerCASecret(ctx, k8sClient, clusterName, namespace)
				cnpg := &cnpgv1.Cluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
				markCNPGClusterHealthy(cnpg, clusterName, caSecretName)
				Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
				reconcileAfterCNPGHealthyOrPatch()

				// Drain baseline events so we don't match the initial "created" event.
				received := make([]string, 0, 16)
				CollectEvents(&received, fakeRecorder)
				received = received[:0]

				// Drift the managed ConfigMap.
				pc := &platformv1alpha1.PostgresCluster{}
				Eventually(func() bool {
					if _, err := reconciler.Reconcile(ctx, req); err != nil {
						return false
					}
					if err := k8sClient.Get(ctx, pgClusterKey, pc); err != nil {
						return false
					}
					return pc.Status.Resources != nil && pc.Status.Resources.ConfigMapRef != nil
				}, "45s", "100ms").Should(BeTrue())

				cmKey := types.NamespacedName{
					Name:      pc.Status.Resources.ConfigMapRef.Name,
					Namespace: namespace,
				}
				cm := &v1.ConfigMap{}
				Expect(k8sClient.Get(ctx, cmKey, cm)).To(Succeed())
				delete(cm.Data, "CLUSTER_RW_ENDPOINT") // force reconciliation update
				Expect(k8sClient.Update(ctx, cm)).To(Succeed())

				// Reconcile and assert updated event.
				reconcileAfterCNPGHealthyOrPatch()

				Eventually(func() bool {
					if _, err := reconciler.Reconcile(ctx, req); err != nil {
						return false
					}
					CollectEvents(&received, fakeRecorder)

					// reason match
					if !ContainsEvent(received, v1.EventTypeNormal, core.EventConfigMapReconciled) {
						return false
					}
					// message-level match for update (not create)
					for _, e := range received {
						if strings.Contains(e, v1.EventTypeNormal) &&
							strings.Contains(e, core.EventConfigMapReconciled) &&
							strings.Contains(e, "updated") {
							return true
						}
					}
					return false
				}, "15s", "100ms").Should(BeTrue(), "events seen: %v", received)
			})
		})
		Context("when applying postgrescluster resource", func() {
			It("should catch password config being empty", func() {
				pgCluster.Spec.PasswordConfig = &platformv1alpha1.SuperuserPasswordConfig{
					SuperuserExternalSecretRef: v1.LocalObjectReference{
						Name: "",
					},
				}
				Expect(k8sClient.Create(ctx, pgCluster)).NotTo(Succeed())
			})
		})
	})

	When("restoring a cluster from a volume snapshot", func() {
		// PC-10: while CNPG not yet healthy, ClusterReady stays False (provisioning)
		It("keeps ClusterReady=False/Provisioning while CNPG is not yet healthy", func() {
			pgCluster.Spec.BootstrapFrom = &platformv1alpha1.BootstrapFrom{
				VolumeSnapshot: &platformv1alpha1.VolumeSnapshotSource{
					Storage: "source-pg-backup-20260501120000",
				},
			}
			Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
			// pass 1: finalizer; pass 2: CNPG cluster created, provisioner returns pending
			reconcileNTimes(2)

			pc := &platformv1alpha1.PostgresCluster{}
			Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())

			clusterReadyCond := meta.FindStatusCondition(pc.Status.Conditions, "ClusterReady")
			Expect(clusterReadyCond).NotTo(BeNil())
			Expect(clusterReadyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(*pc.Status.Phase).NotTo(Equal("Ready"))
		})

		// PC-11: sweep blocks Phase=Ready while CNPG is healthy but sweep has not run
		It("keeps Phase out of Ready while sweep is incomplete, even after CNPG is healthy", func() {
			pgCluster.Spec.BootstrapFrom = &platformv1alpha1.BootstrapFrom{
				VolumeSnapshot: &platformv1alpha1.VolumeSnapshotSource{
					Storage: "source-pg-backup-20260501120000",
				},
			}
			Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
			reconcileNTimes(2)

			// Simulate CNPG becoming healthy with all instances ready, so the cluster
			// component settles to ClusterReady=True and reconciliation proceeds to the
			// managedRoles component — where the sweep gate is exercised.
			cnpg := &cnpgv1.Cluster{}
			Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
			cnpg.Status.Phase = cnpgv1.PhaseHealthy
			cnpg.Status.Instances = int(clusterMemberCount)
			cnpg.Status.ReadyInstances = int(clusterMemberCount)
			Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())
			reconcileNTimes(1)

			pc := &platformv1alpha1.PostgresCluster{}
			Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())

			// Primary assertion: ManagedRolesReady is False — envtest has no real DB, so the
			// connection attempt inside runCredentialSweep fails and the sweep gate stays pending.
			managedRolesCond := meta.FindStatusCondition(pc.Status.Conditions, "ManagedRolesReady")
			Expect(managedRolesCond).NotTo(BeNil())
			Expect(managedRolesCond.Status).To(Equal(metav1.ConditionFalse))

			// Because the sweep gate is pending, the aggregate Phase must not reach Ready.
			Expect(pc.Status.Phase).NotTo(BeNil())
			Expect(*pc.Status.Phase).NotTo(Equal("Ready"))

			// ClusterReady tracks only CNPG provisioner health, so it is True here even though
			// the cluster is not Ready overall — the sweep gates Phase, not ClusterReady.
			clusterReadyCond := meta.FindStatusCondition(pc.Status.Conditions, "ClusterReady")
			Expect(clusterReadyCond).NotTo(BeNil())
			Expect(clusterReadyCond.Status).To(Equal(metav1.ConditionTrue))
		})

		// PC-12: once sweep is marked complete (simulated via status patch), cluster proceeds to Ready
		It("proceeds to Ready once status.restore.credentialSweep.completed is true", func() {
			snapName := "source-pg-backup-20260501120000"
			pgCluster.Spec.BootstrapFrom = &platformv1alpha1.BootstrapFrom{
				VolumeSnapshot: &platformv1alpha1.VolumeSnapshotSource{
					Storage: snapName,
				},
			}
			Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
			reconcileNTimes(2)

			// Simulate CNPG becoming healthy and mark sweep done directly on status
			// (as runSweepIfNeeded would do after a real DB connection).
			caSecretName := seedCNPGClusterServerCASecret(ctx, k8sClient, clusterName, namespace)
			cnpg := &cnpgv1.Cluster{}
			Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
			markCNPGClusterHealthy(cnpg, clusterName, caSecretName)
			Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())

			pc := &platformv1alpha1.PostgresCluster{}
			Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
			pc.Status.Restore = &platformv1alpha1.RestoreStatus{
				Source:          platformv1alpha1.RestoreSourceStatus{VolumeSnapshot: &snapName},
				CredentialSweep: platformv1alpha1.RestoreCredentialSweepStatus{Completed: true},
			}
			Expect(k8sClient.Status().Update(ctx, pc)).To(Succeed())

			reconcileNTimes(1)

			Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
			// With sweep completed and no managed roles, cluster should be fully Ready.
			clusterReadyCond := meta.FindStatusCondition(pc.Status.Conditions, "ClusterReady")
			Expect(clusterReadyCond).NotTo(BeNil())
			Expect(clusterReadyCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(clusterReadyCond.Reason).To(Equal("CNPGClusterHealthy"))

			Expect(pc.Status.Phase).NotTo(BeNil())
			Expect(*pc.Status.Phase).To(Equal("Ready"))
		})
	})

	When("restoring a cluster from an object-store archive (PITR)", func() {
		const originExternalCluster = "origin"
		const barmanPluginName = "barman-cloud.cloudnative-pg.io"

		var classNameRestore string

		objectStoreKey := func() types.NamespacedName {
			return types.NamespacedName{Name: clusterName + "-object-store", Namespace: namespace}
		}

		findExternalCluster := func(cnpg *cnpgv1.Cluster, name string) *cnpgv1.ExternalCluster {
			for i := range cnpg.Spec.ExternalClusters {
				if cnpg.Spec.ExternalClusters[i].Name == name {
					return &cnpg.Spec.ExternalClusters[i]
				}
			}
			return nil
		}

		BeforeEach(func() {
			classNameRestore = classNamePrefix + "restore-" + fmt.Sprintf(
				"%d-%d-%d",
				GinkgoParallelProcess(),
				GinkgoRandomSeed(),
				CurrentSpecReport().LeafNodeLocation.LineNumber,
			)

			// Object store defined but backup writing left disabled: the restore only
			// reads the archive, exercising the recovery-only ObjectStore path.
			pgClassRestore := &platformv1alpha1.PostgresClusterClass{
				ObjectMeta: metav1.ObjectMeta{Name: classNameRestore},
				Spec: platformv1alpha1.PostgresClusterClassSpec{
					Provisioner: provisioner,
					Config: &platformv1alpha1.PostgresClusterClassConfig{
						Instances:       ptr.To(clusterMemberCount),
						Storage:         ptr.To(resource.MustParse(storageAmount)),
						PostgresVersion: ptr.To(postgresVersion),
					},
					CNPG: &platformv1alpha1.CNPGConfig{
						Backup: &platformv1alpha1.CNPGBackupConfig{
							BarmanObjectStore: &platformv1alpha1.CNPGBarmanObjectStoreConfig{
								DestinationPath: "s3://test-bucket/clusters/",
								EndpointURL:     ptr.To("https://s3.us-east-1.amazonaws.com"),
								S3Credentials: platformv1alpha1.CNPGBarmanS3Credentials{
									AccessKeyId: v1.SecretKeySelector{
										LocalObjectReference: v1.LocalObjectReference{Name: "s3-credentials"},
										Key:                  "accessKeyId",
									},
									SecretAccessKey: v1.SecretKeySelector{
										LocalObjectReference: v1.LocalObjectReference{Name: "s3-credentials"},
										Key:                  "secretAccessKey",
									},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pgClassRestore)).To(Succeed())

			pgCluster.Spec.Class = classNameRestore
		})

		AfterEach(func() {
			existing := &platformv1alpha1.PostgresClusterClass{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: classNameRestore}, existing)
			if err == nil {
				Expect(k8sClient.Delete(ctx, existing)).To(Succeed())
			} else {
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}
		})

		// PITR-01
		It("wires objectStorage + type=time target into the CNPG recovery spec, normalizing the timestamp", func() {
			pgCluster.Spec.BootstrapFrom = &platformv1alpha1.BootstrapFrom{
				ObjectStorage: &platformv1alpha1.ObjectStorageSource{ServerName: "pitr-src"},
				RecoveryTarget: &platformv1alpha1.RecoveryTarget{
					Type:  platformv1alpha1.RecoveryTargetTime,
					Value: "2026-05-01T13:30:00Z",
				},
			}
			Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
			// pass 1: finalizer; pass 2: CNPG cluster + managed ObjectStore created.
			reconcileNTimes(2)

			cnpg := &cnpgv1.Cluster{}
			Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
			Expect(cnpg.Spec.Bootstrap).NotTo(BeNil())
			Expect(cnpg.Spec.Bootstrap.Recovery).NotTo(BeNil())
			Expect(cnpg.Spec.Bootstrap.InitDB).To(BeNil())
			Expect(cnpg.Spec.Bootstrap.Recovery.Source).To(Equal(originExternalCluster))
			Expect(cnpg.Spec.Bootstrap.Recovery.VolumeSnapshots).To(BeNil())

			// PostgreSQL's recovery_target_time GUC rejects the RFC 3339 "Z" designator.
			Expect(cnpg.Spec.Bootstrap.Recovery.RecoveryTarget).NotTo(BeNil())
			targetTime := cnpg.Spec.Bootstrap.Recovery.RecoveryTarget.TargetTime
			Expect(targetTime).To(Equal("2026-05-01 13:30:00+00:00"))
			Expect(targetTime).NotTo(ContainSubstring("Z"))

			origin := findExternalCluster(cnpg, originExternalCluster)
			Expect(origin).NotTo(BeNil())
			Expect(origin.PluginConfiguration).NotTo(BeNil())
			Expect(origin.PluginConfiguration.Name).To(Equal(barmanPluginName))
			Expect(origin.PluginConfiguration.Parameters).To(HaveKeyWithValue("barmanObjectName", clusterName+"-object-store"))
			Expect(origin.PluginConfiguration.Parameters).To(HaveKeyWithValue("serverName", "pitr-src"))

			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(core.ObjectStoreGVK)
			Expect(k8sClient.Get(ctx, objectStoreKey(), obj)).To(Succeed())
			owner := metav1.GetControllerOf(obj)
			Expect(owner).NotTo(BeNil())
			Expect(owner.Name).To(Equal(clusterName))
		})

		// PITR-02
		It("sets recovery.source and externalClusters for a volumeSnapshot base with a WAL archive", func() {
			pgCluster.Spec.BootstrapFrom = &platformv1alpha1.BootstrapFrom{
				VolumeSnapshot: &platformv1alpha1.VolumeSnapshotSource{
					Storage:    "source-pg-backup-20260501120000",
					WalArchive: &platformv1alpha1.ObjectStorageSource{ServerName: "pitr-src"},
				},
				RecoveryTarget: &platformv1alpha1.RecoveryTarget{
					Type:  platformv1alpha1.RecoveryTargetLSN,
					Value: "0/16D68D0",
				},
			}
			Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
			reconcileNTimes(2)

			cnpg := &cnpgv1.Cluster{}
			Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
			Expect(cnpg.Spec.Bootstrap).NotTo(BeNil())
			Expect(cnpg.Spec.Bootstrap.Recovery).NotTo(BeNil())
			Expect(cnpg.Spec.Bootstrap.Recovery.VolumeSnapshots).NotTo(BeNil())
			Expect(cnpg.Spec.Bootstrap.Recovery.VolumeSnapshots.Storage.Name).To(Equal("source-pg-backup-20260501120000"))
			Expect(cnpg.Spec.Bootstrap.Recovery.Source).To(Equal(originExternalCluster))

			Expect(cnpg.Spec.Bootstrap.Recovery.RecoveryTarget).NotTo(BeNil())
			Expect(cnpg.Spec.Bootstrap.Recovery.RecoveryTarget.TargetLSN).To(Equal("0/16D68D0"))

			origin := findExternalCluster(cnpg, originExternalCluster)
			Expect(origin).NotTo(BeNil())
			Expect(origin.PluginConfiguration.Parameters).To(HaveKeyWithValue("serverName", "pitr-src"))
		})
	})
})
