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

package certmanager

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	cmacme "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	cmapi "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	tlsport "github.com/splunk/splunk-operator/pkg/postgresql/cluster/ports/tls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func serverTLSScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, cmapi.AddToScheme(scheme))
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	return scheme
}

func serverTLSRESTMapper() apimeta.RESTMapper {
	mapper := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{cmapi.SchemeGroupVersion})
	mapper.Add(cmapi.SchemeGroupVersion.WithKind(cmapi.CertificateKind), apimeta.RESTScopeNamespace)
	return mapper
}

func serverTLSClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	return fake.NewClientBuilder().
		WithScheme(serverTLSScheme(t)).
		WithRESTMapper(serverTLSRESTMapper()).
		WithObjects(objects...).
		Build()
}

func readyIssuer(namespace string) *cmapi.Issuer {
	return &cmapi.Issuer{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-ca", Namespace: namespace},
		Status: cmapi.IssuerStatus{Conditions: []cmapi.IssuerCondition{{
			Type: cmapi.IssuerConditionReady, Status: cmmeta.ConditionTrue,
		}}},
	}
}

func certManagerRequest(cluster *platformv1alpha1.PostgresCluster, dnsNames ...string) tlsport.Request {
	return tlsport.Request{
		Identity:  serverTLSIdentity(cluster),
		IssuerRef: tlsport.IssuerReference{Name: "platform-ca", Kind: "Issuer", Group: "cert-manager.io"},
		DNSNames:  dnsNames,
		Usages:    []string{string(cmapi.UsageDigitalSignature), string(cmapi.UsageKeyEncipherment), string(cmapi.UsageServerAuth)},
	}
}

func serverTLSIdentity(cluster *platformv1alpha1.PostgresCluster) tlsport.Identity {
	controller := true
	blockOwnerDeletion := true
	return tlsport.Identity{
		ClusterName: cluster.Name,
		Namespace:   cluster.Namespace,
		ClusterUID:  cluster.UID,
		OwnerReference: metav1.OwnerReference{
			APIVersion:         platformv1alpha1.GroupVersion.String(),
			Kind:               "PostgresCluster",
			Name:               cluster.Name,
			UID:                cluster.UID,
			Controller:         &controller,
			BlockOwnerDeletion: &blockOwnerDeletion,
		},
		CertificateName: cluster.Name + "-server-tls",
		ServerTLSSecret: cluster.Name + "-server-tls",
		ServerCASecret:  cluster.Name + "-server-ca",
	}
}

func serverTLSCluster() *platformv1alpha1.PostgresCluster {
	return &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "payments", UID: types.UID("cluster-uid")},
	}
}

func markCertificateReady(ctx context.Context, t *testing.T, c client.Client, cluster *platformv1alpha1.PostgresCluster) {
	t.Helper()
	certificate := &cmapi.Certificate{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: serverTLSIdentity(cluster).CertificateName}, certificate))
	certificate.Status.Conditions = []cmapi.CertificateCondition{{Type: cmapi.CertificateConditionReady, Status: cmmeta.ConditionTrue}}
	require.NoError(t, c.Update(ctx, certificate))
}

func certificatePEM(t *testing.T, dnsNames ...string) []byte {
	return certificatePEMWithExtKeyUsages(t, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, dnsNames...)
}

func certificatePEMWithExtKeyUsages(t *testing.T, extKeyUsages []x509.ExtKeyUsage, dnsNames ...string) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	certificate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     dnsNames,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  extKeyUsages,
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func prepareReadyCertificate(ctx context.Context, t *testing.T, manager *Manager, c client.Client, request tlsport.Request) {
	t.Helper()
	err := manager.Reconcile(ctx, request)
	require.ErrorIs(t, err, tlsport.ErrCertificatePending)
	markCertificateReady(ctx, t, c, &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: request.Identity.ClusterName, Namespace: request.Identity.Namespace}})
}

func TestReconcileRejectsInvalidIssuerConfiguration(t *testing.T) {
	t.Parallel()
	cluster := serverTLSCluster()

	tests := []struct {
		name        string
		request     tlsport.Request
		objects     []client.Object
		wantMessage string
	}{
		{
			name: "missing issuer reference",
			request: tlsport.Request{
				Identity: serverTLSIdentity(cluster),
			},
			wantMessage: "resolved TLS request is incomplete",
		},
		{
			name: "unsupported issuer group",
			request: tlsport.Request{
				Identity:  serverTLSIdentity(cluster),
				IssuerRef: tlsport.IssuerReference{Name: "platform-ca", Group: "example.io"},
			},
			wantMessage: "issuer group",
		},
		{
			name:    "ACME issuer",
			request: certManagerRequest(cluster),
			objects: []client.Object{&cmapi.Issuer{
				ObjectMeta: metav1.ObjectMeta{Name: "platform-ca", Namespace: cluster.Namespace},
				Spec:       cmapi.IssuerSpec{IssuerConfig: cmapi.IssuerConfig{ACME: &cmacme.ACMEIssuer{Server: "https://acme.example"}}},
			}},
			wantMessage: "ACME issuers are not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager(serverTLSClient(t, tt.objects...))
			err := manager.Reconcile(context.Background(), tt.request)
			assert.ErrorIs(t, err, tlsport.ErrConfiguration)
			assert.ErrorContains(t, err, tt.wantMessage)
		})
	}
}

func TestReconcileReturnsPendingUntilIssuerAndCertificateAreReady(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cluster := serverTLSCluster()

	t.Run("issuer is not ready", func(t *testing.T) {
		issuer := readyIssuer(cluster.Namespace)
		issuer.Status.Conditions[0].Status = cmmeta.ConditionFalse
		err := NewManager(serverTLSClient(t, issuer)).Reconcile(ctx, certManagerRequest(cluster, "orders-rw.payments.svc"))
		assert.ErrorIs(t, err, tlsport.ErrCertificatePending)
	})

	t.Run("certificate is newly created", func(t *testing.T) {
		c := serverTLSClient(t, readyIssuer(cluster.Namespace))
		err := NewManager(c).Reconcile(ctx, certManagerRequest(cluster, "orders-rw.payments.svc"))
		assert.ErrorIs(t, err, tlsport.ErrCertificatePending)

		certificate := &cmapi.Certificate{}
		require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: serverTLSIdentity(cluster).CertificateName}, certificate))
		assert.Equal(t, []string{"orders-rw.payments.svc"}, certificate.Spec.DNSNames)
		assert.Equal(t, []cmapi.KeyUsage{cmapi.UsageDigitalSignature, cmapi.UsageKeyEncipherment, cmapi.UsageServerAuth}, certificate.Spec.Usages)
		assert.Equal(t, "platform-ca", certificate.Spec.IssuerRef.Name)
		assert.Equal(t, certManagerRequest(cluster, "orders-rw.payments.svc").Identity.Labels(), certificate.Spec.SecretTemplate.Labels)
	})
}

func TestReconcilePublishesCAOnlySecretForValidLeaf(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cluster := serverTLSCluster()
	request := certManagerRequest(cluster, "orders-rw.payments.svc", "postgres.internal.example")
	c := serverTLSClient(t, readyIssuer(cluster.Namespace))
	manager := NewManager(c)
	prepareReadyCertificate(ctx, t, manager, c, request)
	caPEM := certificatePEM(t, "issuer-ca")

	leaf := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: request.Identity.ServerTLSSecret, Namespace: cluster.Namespace},
		Type:       corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       certificatePEM(t, request.DNSNames...),
			corev1.TLSPrivateKeyKey: []byte("private-key"),
			"ca.crt":                caPEM,
		},
	}
	require.NoError(t, c.Create(ctx, leaf))

	require.NoError(t, manager.Reconcile(ctx, request))

	ca := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: request.Identity.ServerCASecret}, ca))
	assert.Equal(t, corev1.SecretTypeOpaque, ca.Type)
	assert.Equal(t, map[string][]byte{"ca.crt": caPEM}, ca.Data)
	assert.Equal(t, request.Identity.Labels(), ca.Labels)
}

func TestReconcileDoesNotAdoptForeignCertificate(t *testing.T) {
	t.Parallel()
	cluster := serverTLSCluster()
	request := certManagerRequest(cluster, "orders-rw.payments.svc")
	foreign := &cmapi.Certificate{
		ObjectMeta: metav1.ObjectMeta{Name: request.Identity.CertificateName, Namespace: cluster.Namespace, Labels: map[string]string{"app": "foreign"}},
		Spec:       cmapi.CertificateSpec{SecretName: "foreign-secret"},
	}
	c := serverTLSClient(t, readyIssuer(cluster.Namespace), foreign)

	err := NewManager(c).Reconcile(context.Background(), request)
	assert.ErrorIs(t, err, tlsport.ErrConfiguration)
	assert.ErrorContains(t, err, "not managed by this PostgresCluster")

	actual := &cmapi.Certificate{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(foreign), actual))
	assert.Equal(t, "foreign-secret", actual.Spec.SecretName)
	assert.Equal(t, map[string]string{"app": "foreign"}, actual.Labels)
}

func TestReconcileRejectsInvalidLeafMaterial(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cluster := serverTLSCluster()
	request := certManagerRequest(cluster, "orders-rw.payments.svc")

	tests := []struct {
		name string
		leaf *corev1.Secret
	}{
		{
			name: "missing CA certificate",
			leaf: &corev1.Secret{Type: corev1.SecretTypeTLS, Data: map[string][]byte{
				corev1.TLSCertKey:       certificatePEM(t, request.DNSNames...),
				corev1.TLSPrivateKeyKey: []byte("private-key"),
			}},
		},
		{
			name: "required DNS name absent",
			leaf: &corev1.Secret{Type: corev1.SecretTypeTLS, Data: map[string][]byte{
				corev1.TLSCertKey:       certificatePEM(t, "other.example"),
				corev1.TLSPrivateKeyKey: []byte("private-key"),
				"ca.crt":                []byte("issuer-ca"),
			}},
		},
		{
			name: "server authentication key usage absent",
			leaf: &corev1.Secret{Type: corev1.SecretTypeTLS, Data: map[string][]byte{
				corev1.TLSCertKey:       certificatePEMWithExtKeyUsages(t, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, request.DNSNames...),
				corev1.TLSPrivateKeyKey: []byte("private-key"),
				"ca.crt":                certificatePEM(t, "issuer-ca"),
			}},
		},
		{
			name: "malformed CA certificate",
			leaf: &corev1.Secret{Type: corev1.SecretTypeTLS, Data: map[string][]byte{
				corev1.TLSCertKey:       certificatePEM(t, request.DNSNames...),
				corev1.TLSPrivateKeyKey: []byte("private-key"),
				"ca.crt":                []byte("not-a-certificate"),
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := serverTLSClient(t, readyIssuer(cluster.Namespace))
			manager := NewManager(c)
			prepareReadyCertificate(ctx, t, manager, c, request)
			tt.leaf.Name = request.Identity.ServerTLSSecret
			tt.leaf.Namespace = cluster.Namespace
			require.NoError(t, c.Create(ctx, tt.leaf))

			err := manager.Reconcile(ctx, request)
			assert.ErrorIs(t, err, tlsport.ErrInvalidMaterial)
		})
	}
}

func TestReconcileRejectsForeignCASecret(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cluster := serverTLSCluster()
	request := certManagerRequest(cluster, "orders-rw.payments.svc")
	c := serverTLSClient(t, readyIssuer(cluster.Namespace))
	manager := NewManager(c)
	prepareReadyCertificate(ctx, t, manager, c, request)
	caPEM := certificatePEM(t, "issuer-ca")
	leaf := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: request.Identity.ServerTLSSecret, Namespace: cluster.Namespace}, Type: corev1.SecretTypeTLS, Data: map[string][]byte{
		corev1.TLSCertKey: certificatePEM(t, request.DNSNames...), corev1.TLSPrivateKeyKey: []byte("private-key"), "ca.crt": caPEM,
	}}
	require.NoError(t, c.Create(ctx, leaf))
	require.NoError(t, c.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: request.Identity.ServerCASecret, Namespace: cluster.Namespace}}))

	err := manager.Reconcile(ctx, request)
	assert.ErrorIs(t, err, tlsport.ErrConfiguration)
	assert.ErrorContains(t, err, "is not managed by this PostgresCluster")
	foreign := &corev1.Secret{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: request.Identity.ServerCASecret}, foreign))
	assert.Empty(t, foreign.Data)
	assert.Empty(t, foreign.Labels)
}

func TestReconcileRejectsIncompleteRequest(t *testing.T) {
	t.Parallel()
	err := NewManager(nil).Reconcile(context.Background(), tlsport.Request{})
	assert.ErrorIs(t, err, tlsport.ErrConfiguration)
}

func TestFinalizePreservesCASecretUnlessItIsManagedAndPolicyIsDelete(t *testing.T) {
	t.Parallel()
	cluster := serverTLSCluster()
	identity := serverTLSIdentity(cluster)

	tests := []struct {
		name   string
		labels map[string]string
	}{
		{name: "cleanup not requested", labels: identity.Labels()},
		{name: "unmanaged Secret", labels: map[string]string{"app": "other"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Name: identity.ServerCASecret, Namespace: cluster.Namespace, Labels: tt.labels,
			}}
			c := serverTLSClient(t, secret)

			require.NoError(t, NewManager(c).Finalize(context.Background(), tlsport.FinalizeRequest{Identity: identity}))
			assert.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(secret), &corev1.Secret{}))
		})
	}
}

func TestRetainAllowsAnAlreadyOrphanedManagedCertificate(t *testing.T) {
	t.Parallel()
	cluster := serverTLSCluster()
	identity := serverTLSIdentity(cluster)
	certificate := &cmapi.Certificate{ObjectMeta: metav1.ObjectMeta{
		Name: identity.CertificateName, Namespace: cluster.Namespace, Labels: identity.Labels(),
	}}

	err := NewManager(serverTLSClient(t, certificate)).Finalize(context.Background(), tlsport.FinalizeRequest{Identity: identity, Retain: true})
	assert.NoError(t, err)
}

func TestRetainRejectsACertificateWithAForeignController(t *testing.T) {
	t.Parallel()
	cluster := serverTLSCluster()
	identity := serverTLSIdentity(cluster)
	controller := true
	certificate := &cmapi.Certificate{ObjectMeta: metav1.ObjectMeta{
		Name: identity.CertificateName, Namespace: cluster.Namespace, Labels: identity.Labels(),
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: "example.io/v1", Kind: "Foreign", Name: "foreign", UID: "foreign-uid", Controller: &controller,
		}},
	}}

	err := NewManager(serverTLSClient(t, certificate)).Finalize(context.Background(), tlsport.FinalizeRequest{Identity: identity, Retain: true})
	assert.ErrorContains(t, err, "is not managed by this PostgresCluster")
}

func TestValidateDNSNamesRejectsInvalidOrIncompleteCertificates(t *testing.T) {
	t.Parallel()
	for _, certificate := range [][]byte{
		[]byte("not PEM"),
		certificatePEM(t, "other.example"),
	} {
		assert.Error(t, validateDNSNames(certificate, []string{"orders-rw.payments.svc"}))
	}
}

func TestExistingCertificateDNSNames(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	require.NoError(t, cmapi.AddToScheme(scheme))
	certificate := &cmapi.Certificate{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-server-tls", Namespace: "payments"},
		Spec: cmapi.CertificateSpec{DNSNames: []string{
			"orders-rw.payments.svc", "orders-pooler-rw.payments.svc", "orders-pooler-ro.payments.svc",
		}},
	}
	manager := NewManager(fake.NewClientBuilder().WithScheme(scheme).WithObjects(certificate).Build())
	names, err := manager.ExistingCertificateDNSNames(context.Background(), tlsport.Identity{ClusterName: "orders", Namespace: "payments", CertificateName: "orders-server-tls"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"orders-rw.payments.svc", "orders-pooler-rw.payments.svc", "orders-pooler-ro.payments.svc"}, names)
}

func TestCleanupDeletesOnlyVerifiedCertManagerCASecret(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	cluster := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "payments", UID: "cluster-uid"}}
	identity := serverTLSIdentity(cluster)
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: identity.ServerCASecret, Namespace: "payments", Labels: identity.Labels()}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	manager := NewManager(c)
	require.NoError(t, manager.Finalize(context.Background(), tlsport.FinalizeRequest{Identity: identity, DeleteCASecret: true}))
	assert.Error(t, c.Get(context.Background(), client.ObjectKeyFromObject(secret), &corev1.Secret{}))
}

func TestRetainOrphansOnlyTheManagedCertificate(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	require.NoError(t, cmapi.AddToScheme(scheme))
	cluster := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "payments", UID: types.UID("cluster-uid")}}
	identity := serverTLSIdentity(cluster)
	controller := true
	foreignController := false
	certificate := &cmapi.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      identity.CertificateName,
			Namespace: "payments",
			Labels:    identity.Labels(),
			OwnerReferences: []metav1.OwnerReference{
				{APIVersion: platformv1alpha1.GroupVersion.String(), Kind: "PostgresCluster", Name: cluster.Name, UID: cluster.UID, Controller: &controller},
				{APIVersion: "example.io/v1", Kind: "Audit", Name: "audit", UID: "audit-uid", Controller: &foreignController},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(certificate).Build()

	require.NoError(t, NewManager(c).Finalize(context.Background(), tlsport.FinalizeRequest{Identity: identity, Retain: true}))
	actual := &cmapi.Certificate{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(certificate), actual))
	assert.Len(t, actual.OwnerReferences, 1)
	assert.Equal(t, "Audit", actual.OwnerReferences[0].Kind)
	require.NoError(t, NewManager(c).Finalize(context.Background(), tlsport.FinalizeRequest{Identity: identity, Retain: true}))
}

func TestKeyUsagesConvertsResolvedUsages(t *testing.T) {
	t.Parallel()
	assert.Empty(t, keyUsages(nil))
	assert.Equal(t, []cmapi.KeyUsage{cmapi.UsageClientAuth}, keyUsages([]string{string(cmapi.UsageClientAuth)}))
}
