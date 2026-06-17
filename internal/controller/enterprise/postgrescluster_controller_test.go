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

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	pgprometheus "github.com/splunk/splunk-operator/pkg/postgresql/shared/adapter/prometheus"
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
	if caSecretName != "" {
		cnpg.Status.Certificates.CertificatesConfiguration.ServerCASecret = caSecretName
	}
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
		pgCluster         *enterprisev4.PostgresCluster
		pgClusterClass    *enterprisev4.PostgresClusterClass
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

		pgClusterClass = &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: className},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Provisioner: provisioner,
				Config: &enterprisev4.PostgresClusterClassConfig{
					Instances:       ptr.To(clusterMemberCount),
					Storage:         ptr.To(resource.MustParse(storageAmount)),
					PostgresVersion: ptr.To(postgresVersion),
					ConnectionPooler: &enterprisev4.ConnectionPoolerEnableConfig{
						Enabled: ptr.To(poolerEnabled),
					},
				},
				CNPG: &enterprisev4.CNPGConfig{},
			},
		}

		pgClassPostgresMetrics := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: classNameMetrics},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Provisioner: provisioner,
				Config: &enterprisev4.PostgresClusterClassConfig{
					Instances:       ptr.To(clusterMemberCount),
					Storage:         ptr.To(resource.MustParse(storageAmount)),
					PostgresVersion: ptr.To(postgresVersion),
					Monitoring: &enterprisev4.PostgresMonitoringClassConfig{
						PostgreSQLMetrics: &enterprisev4.MetricsClassConfig{Enabled: ptr.To(true)},
					},
				},
				CNPG: &enterprisev4.CNPGConfig{},
			},
		}

		pgClassPoolerMetrics := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: classNamePooler},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Provisioner: provisioner,
				Config: &enterprisev4.PostgresClusterClassConfig{
					Instances:       ptr.To(clusterMemberCount),
					Storage:         ptr.To(resource.MustParse(storageAmount)),
					PostgresVersion: ptr.To(postgresVersion),
					ConnectionPooler: &enterprisev4.ConnectionPoolerEnableConfig{
						Enabled: ptr.To(true),
					},
					Monitoring: &enterprisev4.PostgresMonitoringClassConfig{
						ConnectionPoolerMetrics: &enterprisev4.MetricsClassConfig{Enabled: ptr.To(true)},
					},
				},
				CNPG: &enterprisev4.CNPGConfig{
					ConnectionPooler: &enterprisev4.ConnectionPoolerConfig{
						Instances: ptr.To(int32(2)),
						Mode:      ptr.To(enterprisev4.ConnectionPoolerModeTransaction),
					},
				},
			},
		}

		pgClassBackup := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: classNameBackup},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Provisioner: provisioner,
				Config: &enterprisev4.PostgresClusterClassConfig{
					Instances:       ptr.To(clusterMemberCount),
					Storage:         ptr.To(resource.MustParse(storageAmount)),
					PostgresVersion: ptr.To(postgresVersion),
					Backup: &enterprisev4.BackupConfig{
						Enabled:  ptr.To(true),
						Schedule: ptr.To("0 2 * * *"),
					},
				},
				CNPG: &enterprisev4.CNPGConfig{
					Backup: &enterprisev4.CNPGBackupConfig{
						Target: ptr.To("prefer-standby"),
						VolumeSnapshot: &enterprisev4.CNPGVolumeSnapshotConfig{
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

		pgCluster = &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
			Spec: enterprisev4.PostgresClusterSpec{
				Class:                 className,
				ClusterDeletionPolicy: ptr.To(deletePolicy),
				ManagedRoles: []enterprisev4.ManagedRole{
					{Name: "app_user", Exists: true},
					{Name: "app_user_rw", Exists: true},
				},
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
					current := &enterprisev4.PostgresCluster{}
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
			getErr := k8sClient.Get(ctx, pgClusterKey, &enterprisev4.PostgresCluster{})
			return apierrors.IsNotFound(getErr)
		}, "10s", "500ms").Should(BeTrue())

		By("Cleaning up PostgresClusterClass fixtures")
		for _, key := range []types.NamespacedName{
			pgClusterClassKey,
			{Name: classNameMetrics},
			{Name: classNamePooler},
			{Name: classNameBackup},
		} {
			existing := &enterprisev4.PostgresClusterClass{}
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

				pc := &enterprisev4.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				Expect(controllerutil.ContainsFinalizer(pc, core.PostgresClusterFinalizerName)).To(BeTrue())
			})

			// PC-01
			It("creates managed resources and status refs", func() {
				received := make([]string, 0, 16)

				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				// pass 1: add finalizer; pass 2: create CNPG cluster/secret/status.
				reconcileNTimes(2)

				pc := &enterprisev4.PostgresCluster{}
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

			It("reconciles external superuser secret and creates managed resources w/ status refs", func() {
				received := make([]string, 0, 16)

				pgCluster.Spec.PasswordConfig = &enterprisev4.SuperuserPasswordConfig{
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
				// pass 1: add finalizer; pass 2: create CNPG cluster/secret/status.
				reconcileNTimes(2)

				pc := &enterprisev4.PostgresCluster{}
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

				pgCluster.Spec.PasswordConfig = &enterprisev4.SuperuserPasswordConfig{
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

				pc := &enterprisev4.PostgresCluster{}
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

				pc := &enterprisev4.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				cond := meta.FindStatusCondition(pc.Status.Conditions, "ClusterReady")
				Expect(cond).NotTo(BeNil())
				Expect(cond.ObservedGeneration).To(Equal(pc.Generation))
			})

			It("patches the CNPG image and reports configuring state during a minor PostgreSQL upgrade", func() {
				const upgradedPostgresVersion = "15.13"

				pgCluster.Spec.ManagedRoles = nil
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

				current := &enterprisev4.PostgresCluster{}
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
				pgCluster.Spec.ManagedRoles = nil
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

				current := &enterprisev4.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
				current.Spec.Monitoring = &enterprisev4.PostgresClusterMonitoring{
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
				pgCluster.Spec.ManagedRoles = nil
			})

			It("adds scrape annotations to poolers only after the CNPG cluster becomes healthy", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				rwKey := types.NamespacedName{Name: clusterName + "-pooler-rw", Namespace: namespace}
				roKey := types.NamespacedName{Name: clusterName + "-pooler-ro", Namespace: namespace}

				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, rwKey, &cnpgv1.Pooler{}))).To(BeTrue())
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, roKey, &cnpgv1.Pooler{}))).To(BeTrue())

				pc := &enterprisev4.PostgresCluster{}
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

					updated := &enterprisev4.PostgresCluster{}
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

				pc := &enterprisev4.PostgresCluster{}
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

				pc := &enterprisev4.PostgresCluster{}
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
				pgCluster.Spec.ConnectionPooler = &enterprisev4.ConnectionPoolerEnableConfig{
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

				pc := &enterprisev4.PostgresCluster{}
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

				pc := &enterprisev4.PostgresCluster{}
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

				pcAfter := &enterprisev4.PostgresCluster{}
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
				pgCluster.Spec.ManagedRoles = nil
			})

			It("creates a ScheduledBackup and sets BackupReady condition after CNPG becomes healthy", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				// Before CNPG is healthy, provisioner blocks and backup component is not reached.
				pc := &enterprisev4.PostgresCluster{}
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
				current := &enterprisev4.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
				current.Spec.Backup = &enterprisev4.BackupConfig{Enabled: ptr.To(false)}
				Expect(k8sClient.Update(ctx, current)).To(Succeed())
				reconcileNTimes(2)

				// ScheduledBackup should be deleted.
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, sbKey, sb))).To(BeTrue())

				// BackupReady condition should indicate disabled.
				pc := &enterprisev4.PostgresCluster{}
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

				pc := &enterprisev4.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				backupCond := meta.FindStatusCondition(pc.Status.Conditions, "BackupReady")
				Expect(backupCond).NotTo(BeNil())
				Expect(backupCond.Status).To(Equal(metav1.ConditionTrue))
				Expect(backupCond.Reason).To(Equal("BackupDisabled"))
				Expect(pc.Status.BackupStatus).To(BeNil())
			})
		})
	})

	When("deleting a PostgresCluster", func() {
		// PC-03
		Context("and clusterDeletionPolicy is set to Delete", func() {
			It("removes children and finalizer", func() {
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
				reconcileNTimes(2)

				pc := &enterprisev4.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				Expect(k8sClient.Delete(ctx, pc)).To(Succeed())

				Eventually(func() bool {
					_, err := reconciler.Reconcile(ctx, req)
					if err != nil {
						return false
					}
					getErr := k8sClient.Get(ctx, pgClusterKey, &enterprisev4.PostgresCluster{})
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

				pc := &enterprisev4.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				Expect(k8sClient.Delete(ctx, pc)).To(Succeed())

				Eventually(func() bool {
					_, err := reconciler.Reconcile(ctx, req)
					if err != nil {
						return false
					}
					getErr := k8sClient.Get(ctx, pgClusterKey, &enterprisev4.PostgresCluster{})
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

				bad := &enterprisev4.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: badName, Namespace: namespace},
					Spec:       enterprisev4.PostgresClusterSpec{Class: "missing-class"},
				}
				Expect(k8sClient.Create(ctx, bad)).To(Succeed())
				DeferCleanup(func() { _ = k8sClient.Delete(ctx, bad) })

				// pass 1 adds finalizer, pass 2 reaches class lookup and sets failure condition.
				_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: badKey})
				Expect(err).NotTo(HaveOccurred())
				_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: badKey})
				Expect(err).To(HaveOccurred())

				Eventually(func() bool {
					current := &enterprisev4.PostgresCluster{}
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
				pgCluster.Spec.ManagedRoles = nil
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

				current := &enterprisev4.PostgresCluster{}
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
				pgCluster.Spec.ManagedRoles = nil
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

					current := &enterprisev4.PostgresCluster{}
					g.Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
					g.Expect(current.Status.Phase).NotTo(BeNil())
					g.Expect(*current.Status.Phase).To(Equal(string(enterprisev4.PhaseReady)))
				}, "20s", "250ms").Should(Succeed())

				pc := &enterprisev4.PostgresCluster{}
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

					current := &enterprisev4.PostgresCluster{}
					g.Expect(k8sClient.Get(ctx, pgClusterKey, current)).To(Succeed())
					g.Expect(current.Status.Phase).NotTo(BeNil())
					g.Expect(*current.Status.Phase).To(Equal("Provisioning"))
				}, "20s", "250ms").Should(Succeed())
			})

			It("publishes empty read-only endpoint values when running with a single instance", func() {
				pgCluster.Spec.ManagedRoles = nil
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

				pc := &enterprisev4.PostgresCluster{}
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
				pgCluster.Spec.ManagedRoles = nil
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
				pc := &enterprisev4.PostgresCluster{}
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
				pgCluster.Spec.ManagedRoles = nil
				pgCluster.Spec.Instances = ptr.To(clusterMemberCount)
				Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())

				pc := &enterprisev4.PostgresCluster{}
				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				pc.Spec.Instances = nil
				Expect(k8sClient.Update(ctx, pc)).To(Succeed())

				Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
				Expect(pc.Spec.Instances).To(BeNil())
			})
		})

		Context("when a configmap spec changes", func() {
			BeforeEach(func() {
				// Keep this test focused on ConfigMap behavior; otherwise reconcile can
				// stop on ManagedRolesPending before ConfigMap status is written.
				pgCluster.Spec.ManagedRoles = nil
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
				pc := &enterprisev4.PostgresCluster{}
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
				pgCluster.Spec.PasswordConfig = &enterprisev4.SuperuserPasswordConfig{
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
			pgCluster.Spec.BootstrapFrom = &enterprisev4.BootstrapFrom{
				VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{
					Storage: "source-pg-backup-20260501120000",
				},
			}
			pgCluster.Spec.ManagedRoles = nil
			Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
			// pass 1: finalizer; pass 2: CNPG cluster created, provisioner returns pending
			reconcileNTimes(2)

			pc := &enterprisev4.PostgresCluster{}
			Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())

			clusterReadyCond := meta.FindStatusCondition(pc.Status.Conditions, "ClusterReady")
			Expect(clusterReadyCond).NotTo(BeNil())
			Expect(clusterReadyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(*pc.Status.Phase).NotTo(Equal("Ready"))
		})

		// PC-11: sweep blocks Phase=Ready while CNPG is healthy but sweep has not run
		It("keeps Phase out of Ready while sweep is incomplete, even after CNPG is healthy", func() {
			pgCluster.Spec.BootstrapFrom = &enterprisev4.BootstrapFrom{
				VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{
					Storage: "source-pg-backup-20260501120000",
				},
			}
			pgCluster.Spec.ManagedRoles = nil
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

			pc := &enterprisev4.PostgresCluster{}
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
			pgCluster.Spec.BootstrapFrom = &enterprisev4.BootstrapFrom{
				VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{
					Storage: snapName,
				},
			}
			pgCluster.Spec.ManagedRoles = nil
			Expect(k8sClient.Create(ctx, pgCluster)).To(Succeed())
			reconcileNTimes(2)

			// Simulate CNPG becoming healthy and mark sweep done directly on status
			// (as runSweepIfNeeded would do after a real DB connection).
			caSecretName := seedCNPGClusterServerCASecret(ctx, k8sClient, clusterName, namespace)
			cnpg := &cnpgv1.Cluster{}
			Expect(k8sClient.Get(ctx, pgClusterKey, cnpg)).To(Succeed())
			markCNPGClusterHealthy(cnpg, clusterName, caSecretName)
			Expect(k8sClient.Status().Update(ctx, cnpg)).To(Succeed())

			pc := &enterprisev4.PostgresCluster{}
			Expect(k8sClient.Get(ctx, pgClusterKey, pc)).To(Succeed())
			pc.Status.Restore = &enterprisev4.RestoreStatus{
				Source:          enterprisev4.RestoreSourceStatus{VolumeSnapshot: &snapName},
				CredentialSweep: enterprisev4.RestoreCredentialSweepStatus{Completed: true},
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
})
