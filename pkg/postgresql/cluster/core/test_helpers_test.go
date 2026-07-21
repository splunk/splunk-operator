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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	backuptypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/backup"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	client "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stretchr/testify/require"
)

type configMapNotFoundClient struct {
	client.Client
}

type getErrorClient struct {
	client.Client
	err        error
	matcher    func(client.Object) bool
	keyMatcher func(client.ObjectKey) bool
}

func (c getErrorClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if c.keyMatcher != nil && c.keyMatcher(key) {
		return c.err
	}
	if c.matcher != nil && c.matcher(obj) {
		return c.err
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

type createErrorClient struct {
	client.Client
	err     error
	matcher func(client.Object) bool
}

func (c createErrorClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if c.matcher != nil && c.matcher(obj) {
		return c.err
	}
	return c.Client.Create(ctx, obj, opts...)
}

type patchErrorClient struct {
	client.Client
	err error
}

func (c patchErrorClient) Patch(_ context.Context, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
	return c.err
}

// makePoolerReadyCNPG returns a healthy CNPG cluster with the full converged pooler SAN set
// and a matching TLS secret whose leaf cert covers those SANs. Both must be seeded into the
// fake client for poolerModel.Observe to reach Ready state with poolerEnabled=true.
func makePoolerReadyCNPG(t *testing.T, name, ns string) (*cnpgv1.Cluster, *corev1.Secret) {
	t.Helper()
	tlsSecretName := name + "-server-tls"
	sans := computeDesiredPoolerSANSet(true, nil, name, ns)
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: cnpgv1.ClusterSpec{
			Certificates: &cnpgv1.CertificatesConfiguration{ServerAltDNSNames: sans},
		},
		Status: cnpgv1.ClusterStatus{
			Phase: cnpgv1.PhaseHealthy,
			Certificates: cnpgv1.CertificatesStatus{
				CertificatesConfiguration: cnpgv1.CertificatesConfiguration{ServerTLSSecret: tlsSecretName},
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: tlsSecretName, Namespace: ns},
		Data:       map[string][]byte{corev1.TLSCertKey: testSelfSignedLeafCertPEM(t, sans)},
	}
	return cnpg, secret
}

func testSelfSignedLeafCertPEM(t *testing.T, dnsNames []string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)
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
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

type noopEventEmitter struct{}

func (noopEventEmitter) emitNormal(_ client.Object, _, _ string)                            {}
func (noopEventEmitter) emitWarning(_ client.Object, _, _ string)                           {}
func (noopEventEmitter) emitPoolerReadyTransition(_ client.Object, _ []metav1.Condition)    {}
func (noopEventEmitter) emitPoolerCreationTransition(_ client.Object, _ []metav1.Condition) {}
func (noopEventEmitter) emitBackupReadyTransition(_ client.Object, _ []metav1.Condition)    {}

// noopBackupBackend is a test stub that satisfies BackupBackend with no-op behaviour.
// Use it wherever a model test only needs the interface satisfied, not the backup logic.
type noopBackupBackend struct{}

func (noopBackupBackend) EnsureScheduled(_ context.Context, _ client.Object, _ backuptypes.ScheduleSpec) (bool, error) {
	return false, nil
}
func (noopBackupBackend) DeleteScheduled(_ context.Context, _ client.Object, _, _ string) (bool, error) {
	return false, nil
}
func (noopBackupBackend) GetSchedule(_ context.Context, _, _ string) (backuptypes.ScheduleResult, error) {
	return backuptypes.ScheduleResult{}, nil
}
func (noopBackupBackend) BackupNow(_ context.Context, _ client.Object, _ backuptypes.BackupRequest) (bool, error) {
	return false, nil
}
func (noopBackupBackend) GetBackup(_ context.Context, _ client.Object, _, _ string) (backuptypes.BackupResult, bool, error) {
	return backuptypes.BackupResult{}, false, nil
}
func (noopBackupBackend) ListBackups(_ context.Context, _ client.Object, _, _ string) ([]backuptypes.BackupResult, error) {
	return nil, nil
}

type captureEventEmitter struct {
	normals  []string
	warnings []string
}

func (c *captureEventEmitter) emitNormal(_ client.Object, reason, message string) {
	c.normals = append(c.normals, reason+":"+message)
}

func (c *captureEventEmitter) emitWarning(_ client.Object, reason, message string) {
	c.warnings = append(c.warnings, reason+":"+message)
}

func (c *captureEventEmitter) emitPoolerReadyTransition(_ client.Object, conditions []metav1.Condition) {
	if !meta.IsStatusConditionTrue(conditions, string(poolerReady)) {
		c.normals = append(c.normals, EventPoolerReady+":Connection poolers are ready")
	}
}

func (c *captureEventEmitter) emitPoolerCreationTransition(_ client.Object, conditions []metav1.Condition) {
	cond := meta.FindStatusCondition(conditions, string(poolerReady))
	if cond != nil && cond.Status == metav1.ConditionFalse && cond.Reason == string(reasonPoolerCreating) {
		return
	}
	c.normals = append(c.normals, EventPoolerCreationStarted+":Connection poolers created, waiting for readiness")
}

func (c configMapNotFoundClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*corev1.ConfigMap); ok {
		return apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, key.Name)
	}
	return c.Client.Get(ctx, key, obj, opts...)
}
