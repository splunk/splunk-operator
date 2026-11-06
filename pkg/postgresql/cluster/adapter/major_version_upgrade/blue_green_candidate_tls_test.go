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

package majorupgradeadapter

import (
	"context"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	tlsport "github.com/splunk/splunk-operator/pkg/postgresql/cluster/ports/tls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type recordingCandidateTLSManager struct {
	request          tlsport.Request
	existingDNSNames []string
	err              error
}

func (m *recordingCandidateTLSManager) ExistingCertificateDNSNames(context.Context, tlsport.Identity) ([]string, error) {
	return m.existingDNSNames, nil
}

func (m *recordingCandidateTLSManager) Reconcile(_ context.Context, request tlsport.Request) error {
	m.request = request
	return m.err
}

func (*recordingCandidateTLSManager) Finalize(context.Context, tlsport.FinalizeRequest) error {
	return nil
}

func TestCandidateCertManagerLeafReusesFrozenBlueCA(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	mode := platformv1alpha1.PostgresCertificateModeCertManager
	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default", UID: "parent-uid"}, Spec: platformv1alpha1.PostgresClusterSpec{Class: "gold"}}
	class := &platformv1alpha1.PostgresClusterClass{ObjectMeta: metav1.ObjectMeta{Name: "gold"}, Spec: platformv1alpha1.PostgresClusterClassSpec{TLS: &platformv1alpha1.PostgresClusterClassTLS{Certificates: &platformv1alpha1.PostgresCertificateConfig{
		Mode: &mode, IssuerRef: &platformv1alpha1.CertificateIssuerReference{Name: "platform-ca", Kind: "Issuer"},
	}}}}
	source := &cnpgv1.Cluster{Spec: cnpgv1.ClusterSpec{Certificates: &cnpgv1.CertificatesConfiguration{ServerTLSSecret: "orders-server-tls"}}, Status: cnpgv1.ClusterStatus{Certificates: cnpgv1.CertificatesStatus{CertificatesConfiguration: cnpgv1.CertificatesConfiguration{ServerCASecret: "orders-ca"}}}}
	blueCA := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "orders-ca", Namespace: parent.Namespace}, Data: map[string][]byte{"ca.crt": []byte("same-ca")}}
	greenCA := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "orders-bg-server-ca", Namespace: parent.Namespace}, Data: map[string][]byte{"ca.crt": []byte("same-ca")}}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(class, blueCA, greenCA).Build()
	retainedPoolerName := "orders-bg-pooler-rw.default.svc.cluster.local"
	manager := &recordingCandidateTLSManager{existingDNSNames: []string{retainedPoolerName, "old.example.com"}}
	preparer := &BlueGreenCandidatePreparer{client: client, cluster: parent, key: types.NamespacedName{Name: parent.Name, Namespace: parent.Namespace}, plan: BlueGreenCandidatePlan{TargetConfiguration: &platformv1alpha1.PostgresClusterSpec{}}, tls: manager}
	leaf, err := preparer.reconcileCandidateServerTLS(t.Context(), source, "orders-bg", "attempt-1")
	require.NoError(t, err)
	assert.Equal(t, "orders-bg-server-tls", leaf)
	assert.Equal(t, "orders-bg-server-tls", manager.request.Identity.CertificateName)
	assert.Equal(t, "attempt-1", manager.request.Identity.Labels()[blueGreenAttemptLabel])
	assert.Contains(t, manager.request.DNSNames, "orders-bg-rw.default.svc.cluster.local")
	assert.Contains(t, manager.request.DNSNames, retainedPoolerName)
	assert.Equal(t, []string{retainedPoolerName}, manager.request.RetainedPoolerDNSNames)
	assert.NotContains(t, manager.request.DNSNames, "old.example.com")
	assert.Equal(t, "platform-ca", manager.request.IssuerRef.Name)

	greenCA.Data["ca.crt"] = []byte("different-ca")
	require.NoError(t, client.Update(t.Context(), greenCA))
	_, err = preparer.reconcileCandidateServerTLS(t.Context(), source, "orders-bg", "attempt-1")
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
}

func TestCNPGCandidateLeafRequiresSourceCASigningKey(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	source := &cnpgv1.Cluster{Status: cnpgv1.ClusterStatus{Certificates: cnpgv1.CertificatesStatus{CertificatesConfiguration: cnpgv1.CertificatesConfiguration{ServerCASecret: "orders-ca"}}}}
	ca := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "orders-ca", Namespace: "default"}, Data: map[string][]byte{"ca.crt": []byte("certificate")}}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ca).Build()
	preparer := &BlueGreenCandidatePreparer{client: client, key: types.NamespacedName{Name: "orders", Namespace: "default"}}
	_, err := preparer.reconcileCandidateServerTLS(t.Context(), source, "orders-bg", "attempt-1")
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	ca.Data["ca.key"] = []byte("signing-key")
	require.NoError(t, client.Update(t.Context(), ca))
	leaf, err := preparer.reconcileCandidateServerTLS(t.Context(), source, "orders-bg", "attempt-1")
	require.NoError(t, err)
	assert.Empty(t, leaf, "CNPG must issue the candidate leaf under the shared CA")
}

func TestCandidateTopologyWaitsForObservedReplicasAndRestoresSyncPolicy(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	source := &cnpgv1.Cluster{Spec: cnpgv1.ClusterSpec{Instances: 3, MinSyncReplicas: 1, MaxSyncReplicas: 2}}
	candidate := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "green", Namespace: "default"},
		Spec:       cnpgv1.ClusterSpec{Instances: 1},
		Status: cnpgv1.ClusterStatus{
			Phase: cnpgv1.PhaseHealthy, Instances: 1, ReadyInstances: 1, CurrentPrimary: "green-1",
			InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"green-1"}},
		},
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(candidate).Build()
	preparer := &BlueGreenCandidatePreparer{client: client, plan: BlueGreenCandidatePlan{TargetInstances: 3}}

	ready, err := preparer.reconcileCandidateTopology(t.Context(), candidate, source)
	require.NoError(t, err)
	assert.False(t, ready, "a spec patch is not proof of healthy replicas")
	assert.Equal(t, 3, candidate.Spec.Instances)
	assert.Equal(t, 1, candidate.Spec.MinSyncReplicas)
	assert.Equal(t, 2, candidate.Spec.MaxSyncReplicas)

	ready, err = preparer.reconcileCandidateTopology(t.Context(), candidate, source)
	require.NoError(t, err)
	assert.False(t, ready, "stale healthy 1/1 status must not certify a 3-instance target")

	candidate.Status.Instances = 3
	candidate.Status.ReadyInstances = 3
	candidate.Status.InstancesStatus[cnpgv1.PodHealthy] = []string{"green-1", "green-2", "green-3"}
	ready, err = preparer.reconcileCandidateTopology(t.Context(), candidate, source)
	require.NoError(t, err)
	assert.True(t, ready)
}
