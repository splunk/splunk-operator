// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package certmanager

import (
	"context"
	"errors"
	"testing"

	cmapi "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// --- helpers ---

func scheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = cmapi.AddToScheme(s)
	return s
}

// restMapperWithCertificateCRD returns a RESTMapper that resolves the
// cert-manager Certificate GVK, simulating a cluster with the CRD installed.
func restMapperWithCertificateCRD() apimeta.RESTMapper {
	m := apimeta.NewDefaultRESTMapper([]schema.GroupVersion{cmapi.SchemeGroupVersion})
	m.Add(cmapi.SchemeGroupVersion.WithKind(cmapi.CertificateKind), apimeta.RESTScopeNamespace)
	return m
}

// emptyRESTMapper returns a RESTMapper with no registered types, simulating
// a cluster without the cert-manager CRDs installed.
func emptyRESTMapper() apimeta.RESTMapper {
	return apimeta.NewDefaultRESTMapper(nil)
}

func buildClient(mapper apimeta.RESTMapper, objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(scheme()).
		WithRESTMapper(mapper).
		WithObjects(objs...).
		Build()
}

func namespacedIssuer(ns, name string) *cmapi.Issuer {
	return &cmapi.Issuer{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Status: cmapi.IssuerStatus{
			Conditions: []cmapi.IssuerCondition{
				{Type: cmapi.IssuerConditionReady, Status: cmmeta.ConditionTrue},
			},
		},
	}
}

func notReadyNamespacedIssuer(ns, name string) *cmapi.Issuer {
	return &cmapi.Issuer{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Status: cmapi.IssuerStatus{
			Conditions: []cmapi.IssuerCondition{
				{Type: cmapi.IssuerConditionReady, Status: cmmeta.ConditionFalse},
			},
		},
	}
}

func clusterIssuer(name string) *cmapi.ClusterIssuer {
	return &cmapi.ClusterIssuer{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: cmapi.IssuerStatus{
			Conditions: []cmapi.IssuerCondition{
				{Type: cmapi.IssuerConditionReady, Status: cmmeta.ConditionTrue},
			},
		},
	}
}

func notReadyClusterIssuer(name string) *cmapi.ClusterIssuer {
	return &cmapi.ClusterIssuer{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: cmapi.IssuerStatus{
			Conditions: []cmapi.IssuerCondition{
				{Type: cmapi.IssuerConditionReady, Status: cmmeta.ConditionFalse},
			},
		},
	}
}

// defaultDesiredSpec mirrors the CertificateSpec EnsureCertificate builds for
// a call with only WithIssuerRef(IssuerRef{Name: issuerName}) set (no
// DNSNames/CommonName/etc. overrides), so fixtures can be constructed with a
// spec that already matches the desired state and won't be seen as a diff by
// CreateOrUpdate.
func defaultDesiredSpec(secretName, issuerName string) cmapi.CertificateSpec {
	return cmapi.CertificateSpec{
		SecretName: secretName,
		IssuerRef:  cmmeta.IssuerReference{Name: issuerName, Kind: cmapi.IssuerKind},
		CommonName: defaultCommonName,
		Usages:     defaultUsages,
	}
}

func readyCertificate(ns, name string, spec cmapi.CertificateSpec) *cmapi.Certificate {
	return &cmapi.Certificate{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec:       spec,
		Status: cmapi.CertificateStatus{
			Conditions: []cmapi.CertificateCondition{
				{Type: cmapi.CertificateConditionReady, Status: cmmeta.ConditionTrue},
			},
		},
	}
}

func notReadyCertificate(ns, name string, spec cmapi.CertificateSpec) *cmapi.Certificate {
	return &cmapi.Certificate{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}, Spec: spec}
}

// --- CertManagerInstalled ---

func TestCertManagerInstalled_CRDPresent(t *testing.T) {
	ok, err := CertManagerInstalled(restMapperWithCertificateCRD())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected installed=true")
	}
}

func TestCertManagerInstalled_CRDAbsent(t *testing.T) {
	ok, err := CertManagerInstalled(emptyRESTMapper())
	if err != nil {
		t.Fatalf("expected nil error for no-match, got: %v", err)
	}
	if ok {
		t.Fatal("expected installed=false")
	}
}

// --- EnsureCertificate ---

func TestEnsureCertificate_CertManagerNotInstalled(t *testing.T) {
	c := buildClient(emptyRESTMapper())
	err := EnsureCertificate(context.Background(), c, "my-secret", "ns",
		WithIssuerRef(IssuerRef{Name: "my-issuer"}))
	if !errors.Is(err, ErrCertManagerNotInstalled) {
		t.Fatalf("expected ErrCertManagerNotInstalled, got: %v", err)
	}
}

func TestEnsureCertificate_IssuerRefRequired(t *testing.T) {
	c := buildClient(restMapperWithCertificateCRD())
	err := EnsureCertificate(context.Background(), c, "my-secret", "ns")
	if !errors.Is(err, ErrIssuerRefRequired) {
		t.Fatalf("expected ErrIssuerRefRequired, got: %v", err)
	}
}

func TestEnsureCertificate_IssuerNotFound(t *testing.T) {
	c := buildClient(restMapperWithCertificateCRD())
	err := EnsureCertificate(context.Background(), c, "my-secret", "ns",
		WithIssuerRef(IssuerRef{Name: "missing-issuer"}))
	if !errors.Is(err, ErrIssuerNotFound) {
		t.Fatalf("expected ErrIssuerNotFound, got: %v", err)
	}
}

func TestEnsureCertificate_ClusterIssuerNotFound(t *testing.T) {
	c := buildClient(restMapperWithCertificateCRD())
	err := EnsureCertificate(context.Background(), c, "my-secret", "ns",
		WithIssuerRef(IssuerRef{Name: "missing-cluster-issuer", Kind: cmapi.ClusterIssuerKind}))
	if !errors.Is(err, ErrIssuerNotFound) {
		t.Fatalf("expected ErrIssuerNotFound, got: %v", err)
	}
}

func TestEnsureCertificate_IssuerNotReady(t *testing.T) {
	issuer := notReadyNamespacedIssuer("ns", "my-issuer")
	c := buildClient(restMapperWithCertificateCRD(), issuer)
	err := EnsureCertificate(context.Background(), c, "my-secret", "ns",
		WithIssuerRef(IssuerRef{Name: "my-issuer"}))
	if !errors.Is(err, ErrIssuerNotReady) {
		t.Fatalf("expected ErrIssuerNotReady, got: %v", err)
	}
}

func TestEnsureCertificate_IssuerNoConditions_NotReady(t *testing.T) {
	issuer := &cmapi.Issuer{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "my-issuer"}}
	c := buildClient(restMapperWithCertificateCRD(), issuer)
	err := EnsureCertificate(context.Background(), c, "my-secret", "ns",
		WithIssuerRef(IssuerRef{Name: "my-issuer"}))
	if !errors.Is(err, ErrIssuerNotReady) {
		t.Fatalf("expected ErrIssuerNotReady, got: %v", err)
	}
}

func TestEnsureCertificate_ClusterIssuerNotReady(t *testing.T) {
	issuer := notReadyClusterIssuer("my-cluster-issuer")
	c := buildClient(restMapperWithCertificateCRD(), issuer)
	err := EnsureCertificate(context.Background(), c, "my-secret", "ns",
		WithIssuerRef(IssuerRef{Name: "my-cluster-issuer", Kind: cmapi.ClusterIssuerKind}))
	if !errors.Is(err, ErrIssuerNotReady) {
		t.Fatalf("expected ErrIssuerNotReady, got: %v", err)
	}
}

func TestEnsureCertificate_CreatesCertificate_ReturnsNotReady(t *testing.T) {
	issuer := namespacedIssuer("ns", "my-issuer")
	c := buildClient(restMapperWithCertificateCRD(), issuer)

	err := EnsureCertificate(context.Background(), c, "my-secret", "ns",
		WithIssuerRef(IssuerRef{Name: "my-issuer"}))
	if !errors.Is(err, ErrCertificateNotReady) {
		t.Fatalf("expected ErrCertificateNotReady, got: %v", err)
	}

	created := &cmapi.Certificate{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "my-secret"}, created); err != nil {
		t.Fatalf("expected Certificate CR to be created: %v", err)
	}
	if created.Spec.SecretName != "my-secret" {
		t.Errorf("SecretName = %q, want %q", created.Spec.SecretName, "my-secret")
	}
	if created.Spec.IssuerRef.Name != "my-issuer" || created.Spec.IssuerRef.Kind != cmapi.IssuerKind {
		t.Errorf("IssuerRef = %+v, want name=my-issuer kind=Issuer", created.Spec.IssuerRef)
	}
	if len(created.Spec.Usages) == 0 {
		t.Error("expected default usages to be set")
	}
}

func TestEnsureCertificate_ClusterIssuer_Resolved(t *testing.T) {
	issuer := clusterIssuer("my-cluster-issuer")
	c := buildClient(restMapperWithCertificateCRD(), issuer)

	err := EnsureCertificate(context.Background(), c, "my-secret", "ns",
		WithIssuerRef(IssuerRef{Name: "my-cluster-issuer", Kind: cmapi.ClusterIssuerKind}))
	if !errors.Is(err, ErrCertificateNotReady) {
		t.Fatalf("expected ErrCertificateNotReady, got: %v", err)
	}

	created := &cmapi.Certificate{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "my-secret"}, created); err != nil {
		t.Fatalf("expected Certificate CR to be created: %v", err)
	}
	if created.Spec.IssuerRef.Kind != cmapi.ClusterIssuerKind {
		t.Errorf("IssuerRef.Kind = %q, want %q", created.Spec.IssuerRef.Kind, cmapi.ClusterIssuerKind)
	}
}

func TestEnsureCertificate_AlreadyExists_NotReady(t *testing.T) {
	issuer := namespacedIssuer("ns", "my-issuer")
	existing := notReadyCertificate("ns", "my-secret", defaultDesiredSpec("my-secret", "my-issuer"))
	c := buildClient(restMapperWithCertificateCRD(), issuer, existing)

	err := EnsureCertificate(context.Background(), c, "my-secret", "ns",
		WithIssuerRef(IssuerRef{Name: "my-issuer"}))
	if !errors.Is(err, ErrCertificateNotReady) {
		t.Fatalf("expected ErrCertificateNotReady, got: %v", err)
	}
}

func TestEnsureCertificate_AlreadyExists_Ready(t *testing.T) {
	issuer := namespacedIssuer("ns", "my-issuer")
	existing := readyCertificate("ns", "my-secret", defaultDesiredSpec("my-secret", "my-issuer"))
	c := buildClient(restMapperWithCertificateCRD(), issuer, existing)

	err := EnsureCertificate(context.Background(), c, "my-secret", "ns",
		WithIssuerRef(IssuerRef{Name: "my-issuer"}))
	if err != nil {
		t.Fatalf("expected nil error for ready certificate, got: %v", err)
	}
}

// TestEnsureCertificate_AlreadyExists_SpecChanged_Updates verifies the fix for
// the "create-only" gap: when a Certificate CR already exists but its spec
// (e.g. DNSNames, reflecting a replica scale-out) no longer matches the
// caller's desired state, EnsureCertificate must patch the existing object's
// spec rather than silently leaving the stale spec in place.
func TestEnsureCertificate_AlreadyExists_SpecChanged_Updates(t *testing.T) {
	issuer := namespacedIssuer("ns", "my-issuer")
	staleSpec := defaultDesiredSpec("my-secret", "my-issuer")
	staleSpec.DNSNames = []string{"old.example.com"}
	existing := readyCertificate("ns", "my-secret", staleSpec)
	c := buildClient(restMapperWithCertificateCRD(), issuer, existing)

	err := EnsureCertificate(context.Background(), c, "my-secret", "ns",
		WithIssuerRef(IssuerRef{Name: "my-issuer"}),
		WithDNSNames([]string{"new-0.example.com", "new-1.example.com"}),
	)
	if !errors.Is(err, ErrCertificateNotReady) {
		t.Fatalf("expected ErrCertificateNotReady after spec update, got: %v", err)
	}

	updated := &cmapi.Certificate{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "my-secret"}, updated); err != nil {
		t.Fatalf("failed to fetch certificate: %v", err)
	}
	want := []string{"new-0.example.com", "new-1.example.com"}
	if len(updated.Spec.DNSNames) != len(want) || updated.Spec.DNSNames[0] != want[0] || updated.Spec.DNSNames[1] != want[1] {
		t.Errorf("DNSNames = %v, want %v (existing Certificate spec was not reconciled)", updated.Spec.DNSNames, want)
	}
}

func TestEnsureCertificate_DNSNamesAndOptionsApplied(t *testing.T) {
	issuer := namespacedIssuer("ns", "my-issuer")
	c := buildClient(restMapperWithCertificateCRD(), issuer)

	duration := metav1.Duration{Duration: 0}
	err := EnsureCertificate(context.Background(), c, "my-secret", "ns",
		WithIssuerRef(IssuerRef{Name: "my-issuer"}),
		WithDNSNames([]string{"foo.example.com", "bar.example.com"}),
		WithDuration(duration),
		WithRotationPolicy(cmapi.RotationPolicyAlways),
	)
	if !errors.Is(err, ErrCertificateNotReady) {
		t.Fatalf("expected ErrCertificateNotReady, got: %v", err)
	}

	created := &cmapi.Certificate{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "my-secret"}, created); err != nil {
		t.Fatalf("expected Certificate CR to be created: %v", err)
	}
	if len(created.Spec.DNSNames) != 2 {
		t.Errorf("DNSNames = %v, want 2 entries", created.Spec.DNSNames)
	}
	if created.Spec.PrivateKey == nil || created.Spec.PrivateKey.RotationPolicy != cmapi.RotationPolicyAlways {
		t.Errorf("PrivateKey.RotationPolicy not applied: %+v", created.Spec.PrivateKey)
	}
}

func TestEnsureCertificate_CommonNameDefaultsToFixedConst(t *testing.T) {
	issuer := namespacedIssuer("ns", "my-issuer")
	c := buildClient(restMapperWithCertificateCRD(), issuer)

	err := EnsureCertificate(context.Background(), c, "my-secret", "ns",
		WithIssuerRef(IssuerRef{Name: "my-issuer"}),
		WithDNSNames([]string{"foo.example.com", "bar.example.com"}),
	)
	if !errors.Is(err, ErrCertificateNotReady) {
		t.Fatalf("expected ErrCertificateNotReady, got: %v", err)
	}

	created := &cmapi.Certificate{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "my-secret"}, created); err != nil {
		t.Fatalf("expected Certificate CR to be created: %v", err)
	}
	if created.Spec.CommonName != defaultCommonName {
		t.Errorf("CommonName = %q, want %q", created.Spec.CommonName, defaultCommonName)
	}
}

func TestEnsureCertificate_CommonNameExplicitOverridesDefault(t *testing.T) {
	issuer := namespacedIssuer("ns", "my-issuer")
	c := buildClient(restMapperWithCertificateCRD(), issuer)

	err := EnsureCertificate(context.Background(), c, "my-secret", "ns",
		WithIssuerRef(IssuerRef{Name: "my-issuer"}),
		WithDNSNames([]string{"foo.example.com"}),
		WithCommonName("explicit-cn"),
	)
	if !errors.Is(err, ErrCertificateNotReady) {
		t.Fatalf("expected ErrCertificateNotReady, got: %v", err)
	}

	created := &cmapi.Certificate{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "my-secret"}, created); err != nil {
		t.Fatalf("expected Certificate CR to be created: %v", err)
	}
	if created.Spec.CommonName != "explicit-cn" {
		t.Errorf("CommonName = %q, want %q", created.Spec.CommonName, "explicit-cn")
	}
}

func TestEnsureCertificate_CommonNameDefaultsToFixedConstWhenNoDNSNames(t *testing.T) {
	issuer := namespacedIssuer("ns", "my-issuer")
	c := buildClient(restMapperWithCertificateCRD(), issuer)

	err := EnsureCertificate(context.Background(), c, "my-secret", "ns",
		WithIssuerRef(IssuerRef{Name: "my-issuer"}),
	)
	if !errors.Is(err, ErrCertificateNotReady) {
		t.Fatalf("expected ErrCertificateNotReady, got: %v", err)
	}

	created := &cmapi.Certificate{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "my-secret"}, created); err != nil {
		t.Fatalf("expected Certificate CR to be created: %v", err)
	}
	if created.Spec.CommonName != defaultCommonName {
		t.Errorf("CommonName = %q, want %q", created.Spec.CommonName, defaultCommonName)
	}
}

func TestEnsureCertificate_OwnerReferenceSet(t *testing.T) {
	issuer := namespacedIssuer("ns", "my-issuer")
	c := buildClient(restMapperWithCertificateCRD(), issuer)

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "owner-cm", UID: "test-uid"},
	}

	err := EnsureCertificate(context.Background(), c, "my-secret", "ns",
		WithIssuerRef(IssuerRef{Name: "my-issuer"}),
		WithOwner(owner),
	)
	if !errors.Is(err, ErrCertificateNotReady) {
		t.Fatalf("expected ErrCertificateNotReady, got: %v", err)
	}

	created := &cmapi.Certificate{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "my-secret"}, created); err != nil {
		t.Fatalf("expected Certificate CR to be created: %v", err)
	}
	if len(created.OwnerReferences) != 1 || created.OwnerReferences[0].Name != "owner-cm" {
		t.Errorf("expected owner reference to owner-cm, got: %+v", created.OwnerReferences)
	}
}
