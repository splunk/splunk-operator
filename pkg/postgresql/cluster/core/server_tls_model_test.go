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
	"context"
	"crypto/x509"
	"errors"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgcconstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	tlsport "github.com/splunk/splunk-operator/pkg/postgresql/cluster/ports/tls"
	identitytypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

type serverTLSManagerStub struct {
	existingDNSNames []string
	existingErr      error
	err              error
}

func (s serverTLSManagerStub) ExistingCertificateDNSNames(context.Context, tlsport.Identity) ([]string, error) {
	return s.existingDNSNames, s.existingErr
}

func (s serverTLSManagerStub) Reconcile(context.Context, tlsport.Request) error {
	return s.err
}

func (serverTLSManagerStub) Finalize(context.Context, tlsport.FinalizeRequest) error {
	return nil
}

type capturingServerTLSManager struct {
	serverTLSManagerStub
	request tlsport.Request
}

func (s *capturingServerTLSManager) Reconcile(_ context.Context, request tlsport.Request) error {
	s.request = request
	return s.err
}

func TestServerTLSNames(t *testing.T) {
	t.Parallel()
	enabled := true
	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "payments"},
		Spec:       platformv1alpha1.PostgresClusterSpec{TLS: &platformv1alpha1.PostgresClusterTLS{ServerAltDNSNames: []string{"db.example", "orders-rw"}}},
	}
	cfg := &MergedConfig{Spec: &platformv1alpha1.PostgresClusterSpec{ConnectionPooler: &platformv1alpha1.ConnectionPoolerEnableConfig{Enabled: &enabled}}}
	names := serverTLSNames(cluster, cfg, cluster.Name)
	assert.Contains(t, names, "orders-rw")
	assert.Contains(t, names, "orders-rw.payments.svc.cluster.local")
	assert.Contains(t, names, "orders-ro.payments.svc.cluster.local")
	assert.Contains(t, names, "orders-r.payments.svc.cluster.local")
	assert.Contains(t, names, "orders-pooler-rw.payments.svc.cluster.local")
	assert.Contains(t, names, "orders-pooler-ro.payments.svc.cluster.local")
	assert.Contains(t, names, "db.example")
	assert.Equal(t, 1, occurrences(names, "orders-rw"))
}

func TestServerTLSModelUsesAuthoritativeEnvironmentName(t *testing.T) {
	t.Parallel()
	mode := platformv1alpha1.PostgresCertificateModeCertManager
	enabled := true
	cluster := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "payments"}}
	class := &platformv1alpha1.PostgresClusterClass{Spec: platformv1alpha1.PostgresClusterClassSpec{
		TLS: &platformv1alpha1.PostgresClusterClassTLS{Certificates: &platformv1alpha1.PostgresCertificateConfig{
			Mode: &mode, IssuerRef: &platformv1alpha1.CertificateIssuerReference{Name: "platform-ca"},
		}},
	}}
	config := &MergedConfig{Spec: &platformv1alpha1.PostgresClusterSpec{
		ConnectionPooler: &platformv1alpha1.ConnectionPoolerEnableConfig{Enabled: &enabled},
	}}
	manager := &capturingServerTLSManager{}
	contracts := &reconcileContracts{
		Authority:        identitytypes.ClusterCard{Authoritative: identitytypes.Environment{Identity: identitytypes.ObjectIdentity{Name: "orders-green"}}},
		EnvironmentNamer: testEnvironmentNamer,
	}
	model := newServerTLSModel(manager, noopEventEmitter{}, nil, cluster, class, config, contracts)

	require.NoError(t, model.CheckContracts())
	plan, err := model.reconcilePlan(t.Context())
	require.NoError(t, err)
	assert.Contains(t, manager.request.DNSNames, "orders-green-rw.payments.svc.cluster.local")
	assert.Contains(t, manager.request.DNSNames, "orders-green-pooler-rw.payments.svc.cluster.local")
	assert.NotContains(t, manager.request.DNSNames, "orders-pooler-rw.payments.svc.cluster.local")
	assert.True(t, plan.ValidatesPoolerLeaf(&x509.Certificate{DNSNames: certManagerPoolerSANs(cluster, config, "orders-green")}))
}

func TestCompleteServerTLSNamesRetainsOnlyGeneratedPoolerNames(t *testing.T) {
	t.Parallel()
	desired := []string{"orders-green-rw.payments.svc"}
	generated := "orders-green-pooler-ro.payments.svc"
	removedCustomName := "legacy.orders-green-pooler-rw.example.com"

	all, retained := completeServerTLSNames(desired, []string{generated, removedCustomName}, "orders-green", "payments")
	assert.Contains(t, all, generated)
	assert.NotContains(t, all, removedCustomName)
	assert.Equal(t, []string{generated}, retained)
}

func TestResolvedServerTLSUsages(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"digital signature", "key encipherment", "server auth"}, resolvedServerTLSUsages(nil))
	assert.Equal(t, []string{"client auth"}, resolvedServerTLSUsages([]string{"client auth"}))
}

func TestCertManagerPlanAppliesServerTLS(t *testing.T) {
	t.Parallel()
	spec := &cnpgv1.ClusterSpec{Certificates: &cnpgv1.CertificatesConfiguration{ServerAltDNSNames: []string{"must-be-cleared"}}}
	plan := newCertManagerServerTLSPlan(nil, nil, tlsport.Identity{ServerTLSSecret: "orders-server-tls", ServerCASecret: "orders-server-ca"}, "orders")
	plan.ApplyToCNPG(spec, "orders", "payments")
	require.NotNil(t, spec.Certificates)
	assert.Equal(t, "orders-server-tls", spec.Certificates.ServerTLSSecret)
	assert.Equal(t, "orders-server-ca", spec.Certificates.ServerCASecret)
	assert.Empty(t, spec.Certificates.ServerAltDNSNames)
}

func TestServerTLSPlanObservesCNPGStatus(t *testing.T) {
	t.Parallel()

	cluster := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "payments"}}
	status := cnpgv1.ClusterStatus{Certificates: cnpgv1.CertificatesStatus{CertificatesConfiguration: cnpgv1.CertificatesConfiguration{
		ServerTLSSecret: "orders-server-tls",
		ServerCASecret:  "orders-server-ca",
	}}}

	t.Run("cert-manager requires CNPG to adopt both planned Secrets", func(t *testing.T) {
		plan := newCertManagerServerTLSPlan(cluster, nil, serverTLSIdentity(cluster), cluster.Name)
		backend := plan.ObserveCNPG(status)
		assert.True(t, backend.Converged)
		assert.Equal(t, "orders-server-tls", backend.ServerTLSSecret)
		require.NotNil(t, backend.ConnectionCARef)
		assert.Equal(t, "orders-server-ca", backend.ConnectionCARef.Name)
		assert.Equal(t, defaultServerCACertKey, backend.ConnectionCARef.Key)

		status.Certificates.ServerCASecret = "unexpected-ca"
		assert.False(t, plan.ObserveCNPG(status).Converged)
	})

	t.Run("CNPG default resolves CNPG-selected backend references", func(t *testing.T) {
		status.Certificates.ServerCASecret = "orders-server-ca"
		plan := newCNPGDefaultServerTLSPlan(cluster, nil, cluster.Name)
		backend := plan.ObserveCNPG(status)
		assert.True(t, backend.Converged)
		assert.Equal(t, "orders-server-tls", backend.ServerTLSSecret)
		require.NotNil(t, backend.ConnectionCARef)
		assert.Equal(t, "orders-server-ca", backend.ConnectionCARef.Name)
	})
}

func TestCNPGDefaultServerTLSPlanAppliesPoolerIntentAndValidatesLeaf(t *testing.T) {
	t.Parallel()

	enabled := true
	cluster := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "payments"}}
	config := &MergedConfig{Spec: &platformv1alpha1.PostgresClusterSpec{
		ConnectionPooler: &platformv1alpha1.ConnectionPoolerEnableConfig{Enabled: ptr.To(enabled)},
	}}
	plan := newCNPGDefaultServerTLSPlan(cluster, config, cluster.Name)
	spec := &cnpgv1.ClusterSpec{}
	plan.ApplyToCNPG(spec, cluster.Name, cluster.Namespace)

	require.NotNil(t, spec.Certificates)
	assert.Contains(t, spec.Certificates.ServerAltDNSNames, "orders-pooler-rw.payments.svc.cluster.local")
	assert.Contains(t, spec.Certificates.ServerAltDNSNames, "orders-pooler-ro.payments.svc.cluster.local")
	assert.True(t, plan.ValidatesPoolerLeaf(&x509.Certificate{DNSNames: spec.Certificates.ServerAltDNSNames}))
	assert.False(t, plan.ValidatesPoolerLeaf(&x509.Certificate{DNSNames: []string{"orders-pooler-rw.payments.svc.cluster.local"}}))
}

func TestCertManagerServerTLSPlanValidatesTheIssuedPoolerSANSet(t *testing.T) {
	t.Parallel()

	enabled := true
	readOnly := false
	cluster := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "payments"}}
	config := &MergedConfig{Spec: &platformv1alpha1.PostgresClusterSpec{
		ConnectionPooler: &platformv1alpha1.ConnectionPoolerEnableConfig{Enabled: ptr.To(enabled), ReadOnly: ptr.To(readOnly)},
	}}
	plan := newCertManagerServerTLSPlan(cluster, config, serverTLSIdentity(cluster), cluster.Name)
	leaf := &x509.Certificate{DNSNames: certManagerPoolerSANs(cluster, config, cluster.Name)}

	assert.True(t, plan.ValidatesPoolerLeaf(leaf))
	assert.NotContains(t, leaf.DNSNames, "orders-pooler-ro.payments.svc.cluster.local")
}

func TestServerTLSModelStopsPipelineOnTerminalError(t *testing.T) {
	t.Parallel()
	cluster := &platformv1alpha1.PostgresCluster{}
	model := newServerTLSModel(nil, noopEventEmitter{}, nil, cluster, nil, nil, &reconcileContracts{})
	health, err := model.Observe(context.Background(), errors.New("invalid TLS configuration"))
	assert.Equal(t, certificatesReady, health.Condition)
	assert.Equal(t, reasonCertificateConfigError, health.Reason)
	assert.Error(t, err)
}

func TestServerTLSModelEmitsEventsOnlyForCertificateConditionTransitions(t *testing.T) {
	t.Parallel()
	mode := platformv1alpha1.PostgresCertificateModeCertManager
	cluster := &platformv1alpha1.PostgresCluster{}
	class := &platformv1alpha1.PostgresClusterClass{Spec: platformv1alpha1.PostgresClusterClassSpec{
		TLS: &platformv1alpha1.PostgresClusterClassTLS{Certificates: &platformv1alpha1.PostgresCertificateConfig{Mode: &mode}},
	}}
	events := &captureEventEmitter{}
	updateStatus := func(_ *platformv1alpha1.PostgresClusterStatus, health componentHealth) error {
		status := metav1.ConditionFalse
		if health.State == pgcconstants.Ready {
			status = metav1.ConditionTrue
		}
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{Type: string(certificatesReady), Status: status, Reason: string(health.Reason), Message: health.Message})
		return nil
	}
	model := newServerTLSModel(nil, events, updateStatus, cluster, class, nil, &reconcileContracts{})

	_, err := model.Observe(context.Background(), nil)
	require.NoError(t, err)
	assert.Len(t, events.normals, 1)
	_, err = model.Observe(context.Background(), nil)
	require.NoError(t, err)
	assert.Len(t, events.normals, 1)

	_, err = model.Observe(context.Background(), tlsport.ErrCertificatePending)
	require.NoError(t, err)
	assert.Len(t, events.warnings, 1)
	_, err = model.Observe(context.Background(), tlsport.ErrCertificatePending)
	require.NoError(t, err)
	assert.Len(t, events.warnings, 1)
}

func TestServerTLSModelReconcilePlan(t *testing.T) {
	t.Parallel()

	mode := platformv1alpha1.PostgresCertificateModeCertManager
	cluster := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "payments"}}
	class := &platformv1alpha1.PostgresClusterClass{Spec: platformv1alpha1.PostgresClusterClassSpec{
		TLS: &platformv1alpha1.PostgresClusterClassTLS{Certificates: &platformv1alpha1.PostgresCertificateConfig{
			Mode:      &mode,
			IssuerRef: &platformv1alpha1.CertificateIssuerReference{Name: "platform-ca"},
		}},
	}}

	tests := []struct {
		name     string
		manager  serverTLSManagerStub
		wantPlan bool
		wantErr  error
	}{
		{
			name:    "certificate issuance is pending",
			manager: serverTLSManagerStub{err: tlsport.ErrCertificatePending},
			wantErr: tlsport.ErrCertificatePending,
		},
		{
			name:    "configuration is blocked",
			manager: serverTLSManagerStub{err: tlsport.ErrConfiguration},
			wantErr: tlsport.ErrConfiguration,
		},
		{
			name:     "successful reconciliation produces a ready plan",
			manager:  serverTLSManagerStub{},
			wantPlan: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newServerTLSModel(tt.manager, noopEventEmitter{}, nil, cluster, class, nil, &reconcileContracts{})
			plan, err := model.reconcilePlan(t.Context())
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, tt.wantPlan, plan.initialized)
		})
	}
}

func occurrences(values []string, sought string) int {
	count := 0
	for _, value := range values {
		if value == sought {
			count++
		}
	}
	return count
}
