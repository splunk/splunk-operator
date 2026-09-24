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

package certs

import (
	"context"
	"errors"
	"fmt"
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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	certlib "github.com/splunk/splunk-operator/pkg/splunk/client/certmanager"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// --- helpers ---

func scheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = enterpriseApi.AddToScheme(s)
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

func buildClientWithMapper(mapper apimeta.RESTMapper, objs ...client.Object) client.Client {
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

func makeSecret(ns, name string, data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Data:       data,
	}
}

func fullSecret(ns, name string) *corev1.Secret {
	return makeSecret(ns, name, map[string][]byte{
		CertTLSCRTKey: []byte("CERT"),
		CertTLSKeyKey: []byte("KEY"),
		CertCAKey:     []byte("CA"),
	})
}

func noCASecret(ns, name string) *corev1.Secret {
	return makeSecret(ns, name, map[string][]byte{
		CertTLSCRTKey: []byte("CERT"),
		CertTLSKeyKey: []byte("KEY"),
	})
}

func caOnlySecret(ns, name string) *corev1.Secret {
	return makeSecret(ns, name, map[string][]byte{
		CertCAKey: []byte("CA"),
	})
}

func standaloneWithCerts(ns, name string, certs []enterpriseApi.CertSpec) *enterpriseApi.Standalone {
	return &enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec: enterpriseApi.StandaloneSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Certs: certs},
		},
	}
}

// specToCertEntries converts enterpriseApi.CertSpec slice to CertEntry for tests.
func specToCertEntries(specs []enterpriseApi.CertSpec) []CertEntry {
	if len(specs) == 0 {
		return nil
	}
	entries := make([]CertEntry, len(specs))
	for i, s := range specs {
		entries[i] = CertEntry{SecretName: s.SecretRef.Name, Role: string(s.Role)}
	}
	return entries
}

// --- ValidateCertSecret ---

func TestValidateCertSecret_Missing(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(scheme()).Build()
	_, err := ValidateCertSecret(context.Background(), c, "ns", "missing")
	if err == nil {
		t.Fatal("expected not-found error, got nil")
	}
}

func TestValidateCertSecret_MissingTLSCrt(t *testing.T) {
	s := makeSecret("ns", "s", map[string][]byte{CertTLSKeyKey: []byte("k")})
	c := fake.NewClientBuilder().WithScheme(scheme()).WithObjects(s).Build()
	_, err := ValidateCertSecret(context.Background(), c, "ns", "s")
	if err == nil || err.Error() == "" {
		t.Fatal("expected error for missing tls.crt")
	}
}

func TestValidateCertSecret_MissingTLSKey(t *testing.T) {
	s := makeSecret("ns", "s", map[string][]byte{CertTLSCRTKey: []byte("c")})
	c := fake.NewClientBuilder().WithScheme(scheme()).WithObjects(s).Build()
	_, err := ValidateCertSecret(context.Background(), c, "ns", "s")
	if err == nil {
		t.Fatal("expected error for missing tls.key")
	}
}

func TestValidateCertSecret_CAOptional(t *testing.T) {
	// ca.crt absent → should succeed (ACME issuers omit it)
	s := noCASecret("ns", "s")
	c := fake.NewClientBuilder().WithScheme(scheme()).WithObjects(s).Build()
	got, err := ValidateCertSecret(context.Background(), c, "ns", "s")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "s" {
		t.Fatalf("wrong secret returned: %s", got.Name)
	}
}

func TestValidateCertSecret_CAOnly(t *testing.T) {
	// only ca.crt present, tls.crt/tls.key both absent → should succeed
	// (e.g. mounting an externally-managed postgres CA cert).
	s := caOnlySecret("ns", "s")
	c := fake.NewClientBuilder().WithScheme(scheme()).WithObjects(s).Build()
	got, err := ValidateCertSecret(context.Background(), c, "ns", "s")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "s" {
		t.Fatalf("wrong secret returned: %s", got.Name)
	}
}

func TestValidateCertSecret_AllKeys(t *testing.T) {
	s := fullSecret("ns", "s")
	c := fake.NewClientBuilder().WithScheme(scheme()).WithObjects(s).Build()
	got, err := ValidateCertSecret(context.Background(), c, "ns", "s")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected secret, got nil")
	}
}

// --- ReconcileCerts ---

func buildClient(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(scheme()).WithObjects(objs...).Build()
}

func TestReconcileCerts_GateDisabled_NoCertsMounted(t *testing.T) {
	config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.CertManagement): false})
	t.Cleanup(func() {
		config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.CertManagement): true})
	})

	secret := noCASecret("ns", "my-server-cert")
	cr := standaloneWithCerts("ns", "s1", []enterpriseApi.CertSpec{
		{SecretRef: corev1.LocalObjectReference{Name: "my-server-cert"}, Role: enterpriseApi.CertRoleServer},
	})
	c := buildClient(cr, secret)

	cfg, err := ReconcileCerts(context.Background(), c, cr, specToCertEntries(cr.Spec.Certs))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil CertMountConfig when gate disabled, got: %+v", cfg)
	}
}

func TestReconcileCerts_GateEnabled_CertsMounted(t *testing.T) {
	config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.CertManagement): true})

	secret := noCASecret("ns", "my-server-cert")
	cr := standaloneWithCerts("ns", "s1", []enterpriseApi.CertSpec{
		{SecretRef: corev1.LocalObjectReference{Name: "my-server-cert"}, Role: enterpriseApi.CertRoleServer},
	})
	c := buildClient(cr, secret)

	cfg, err := ReconcileCerts(context.Background(), c, cr, specToCertEntries(cr.Spec.Certs))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil CertMountConfig when gate enabled")
	}
	if len(cfg.Volumes) == 0 {
		t.Fatal("expected at least one volume to be mounted when gate enabled")
	}
}

func TestReconcileCerts_NoCerts(t *testing.T) {
	cr := standaloneWithCerts("ns", "s1", nil)
	c := buildClient(cr)
	cfg, err := ReconcileCerts(context.Background(), c, cr, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Fatal("expected nil config when no certs declared")
	}
}

func TestReconcileCerts_MissingSecret_CertManagerNotInstalled_ReturnsError(t *testing.T) {
	cr := standaloneWithCerts("ns", "s1", []enterpriseApi.CertSpec{
		{SecretRef: corev1.LocalObjectReference{Name: "missing-cert"}},
	})
	c := buildClient(cr)
	cfg, err := ReconcileCerts(context.Background(), c, cr, specToCertEntries(cr.Spec.Certs))
	if !errors.Is(err, certlib.ErrCertManagerNotInstalled) {
		t.Fatalf("expected ErrCertManagerNotInstalled, got: %v", err)
	}
	if cfg != nil {
		t.Fatal("expected nil config on error")
	}
}

func TestReconcileCerts_NoRole_AsIsMountPath(t *testing.T) {
	secret := noCASecret("ns", "my-cert")
	cr := standaloneWithCerts("ns", "s1", []enterpriseApi.CertSpec{
		{SecretRef: corev1.LocalObjectReference{Name: "my-cert"}},
	})
	c := buildClient(cr, secret)
	cfg, err := ReconcileCerts(context.Background(), c, cr, specToCertEntries(cr.Spec.Certs))
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	wantMount := CertMountRoot + "/my-cert"
	if cfg.VolumeMounts[0].MountPath != wantMount {
		t.Errorf("mount path = %s, want %s", cfg.VolumeMounts[0].MountPath, wantMount)
	}
}

func TestReconcileCerts_RoleServer_FixedMountPath(t *testing.T) {
	secret := noCASecret("ns", "server-cert")
	cr := standaloneWithCerts("ns", "s1", []enterpriseApi.CertSpec{
		{SecretRef: corev1.LocalObjectReference{Name: "server-cert"}, Role: enterpriseApi.CertRoleServer},
	})
	c := buildClient(cr, secret)
	cfg, err := ReconcileCerts(context.Background(), c, cr, specToCertEntries(cr.Spec.Certs))
	if err != nil {
		t.Fatal(err)
	}
	wantMount := fmt.Sprintf(RoleMountFmt, "server")
	if cfg.VolumeMounts[0].MountPath != wantMount {
		t.Errorf("mount path = %s, want %s", cfg.VolumeMounts[0].MountPath, wantMount)
	}
}

func TestReconcileCerts_AnnotationContainsCertHash(t *testing.T) {
	secret := noCASecret("ns", "my-cert")
	cr := standaloneWithCerts("ns", "s1", []enterpriseApi.CertSpec{
		{SecretRef: corev1.LocalObjectReference{Name: "my-cert"}},
	})
	c := buildClient(cr, secret)
	cfg, err := ReconcileCerts(context.Background(), c, cr, specToCertEntries(cr.Spec.Certs))
	if err != nil {
		t.Fatal(err)
	}
	annotKey := certRevAnnotKey("my-cert")
	if _, ok := cfg.Annotations[annotKey]; !ok {
		t.Errorf("expected annotation %s, not found", annotKey)
	}
}

func TestVolumeName_CollisionSafe(t *testing.T) {
	// "foo.bar" and "foo-bar" both used to normalize to "cert-foo-bar"; now they must differ.
	n1 := volumeName("foo.bar")
	n2 := volumeName("foo-bar")
	if n1 == n2 {
		t.Errorf("volumeName collision: %q == %q", n1, n2)
	}
	// Both must be ≤ 63 chars.
	for _, n := range []string{n1, n2} {
		if len(n) > 63 {
			t.Errorf("volumeName %q exceeds 63 chars (%d)", n, len(n))
		}
	}
}

func TestVolumeName_LongName(t *testing.T) {
	long := "a-very-long-secret-name-that-exceeds-forty-nine-characters-in-total"
	n := volumeName(long)
	if len(n) > 63 {
		t.Errorf("volumeName %q exceeds 63 chars (%d)", n, len(n))
	}
}

func TestCertRevAnnotKey_ShortName(t *testing.T) {
	key := certRevAnnotKey("my-cert")
	// Should use the plain format for short names.
	want := fmt.Sprintf(CertRevAnnotFmt, "my-cert")
	if key != want {
		t.Errorf("certRevAnnotKey = %q, want %q", key, want)
	}
}

func TestCertRevAnnotKey_LongName_BoundedAndUnique(t *testing.T) {
	long := "this-secret-name-is-way-too-long-for-a-kubernetes-annotation-key-suffix"
	key := certRevAnnotKey(long)
	// Extract the name part after "enterprise.splunk.com/"
	const prefix = "enterprise.splunk.com/"
	namePart := key[len(prefix):]
	if len(namePart) > 63 {
		t.Errorf("annotation name part %q exceeds 63 chars (%d)", namePart, len(namePart))
	}
	// Two different long names must produce different keys.
	long2 := long + "-extra"
	key2 := certRevAnnotKey(long2)
	if key == key2 {
		t.Errorf("certRevAnnotKey collision for different long names: %q == %q", key, key2)
	}
}

func TestReconcileCerts_UserDeclaredWinsOverOperatorDriven(t *testing.T) {
	// CR implements CertificateRequester returning "shared-cert",
	// and also declares "shared-cert" in spec.certs with a role.
	// User-declared should win (role-based mount path).
	secret := noCASecret("ns", "shared-cert")

	type mockCR struct {
		*enterpriseApi.Standalone
	}
	// embed CertificateRequester
	cr := standaloneWithCerts("ns", "s1", []enterpriseApi.CertSpec{
		{SecretRef: corev1.LocalObjectReference{Name: "shared-cert"}, Role: enterpriseApi.CertRoleServer},
	})
	c := buildClient(cr, secret)

	// Wrap cr with a CertificateRequester that also returns "shared-cert"
	wrapped := &certRequesterStandalone{Standalone: cr, secrets: []string{"shared-cert"}}
	cfg, err := ReconcileCerts(context.Background(), c, wrapped, specToCertEntries(cr.Spec.Certs))
	if err != nil {
		t.Fatal(err)
	}
	// Should have exactly one mount (not two)
	if len(cfg.VolumeMounts) != 1 {
		t.Errorf("expected 1 mount, got %d", len(cfg.VolumeMounts))
	}
	// Mount path should be the role-based one (user-declared wins)
	wantMount := fmt.Sprintf(RoleMountFmt, "server")
	if cfg.VolumeMounts[0].MountPath != wantMount {
		t.Errorf("mount path = %s, want %s", cfg.VolumeMounts[0].MountPath, wantMount)
	}
}

func TestReconcileCerts_OperatorDriven_MissingSecret_Skipped(t *testing.T) {
	cr := standaloneWithCerts("ns", "s1", nil)
	c := buildClient(cr) // secret not present
	wrapped := &certRequesterStandalone{Standalone: cr, secrets: []string{"operator-cert"}}
	cfg, err := ReconcileCerts(context.Background(), c, wrapped, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Fatal("expected nil — missing operator-driven secret should be skipped")
	}
}

func TestReconcileCerts_OperatorDriven_Mounted(t *testing.T) {
	secret := noCASecret("ns", "op-cert")
	cr := standaloneWithCerts("ns", "s1", nil)
	c := buildClient(cr, secret)
	wrapped := &certRequesterStandalone{Standalone: cr, secrets: []string{"op-cert"}}
	cfg, err := ReconcileCerts(context.Background(), c, wrapped, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	// Operator-driven certs always use as-is path
	wantMount := CertMountRoot + "/op-cert"
	if cfg.VolumeMounts[0].MountPath != wantMount {
		t.Errorf("mount path = %s, want %s", cfg.VolumeMounts[0].MountPath, wantMount)
	}
}

func TestReconcileCerts_MultipleCerts(t *testing.T) {
	s1 := noCASecret("ns", "cert1")
	s2 := noCASecret("ns", "cert2")
	cr := standaloneWithCerts("ns", "s1", []enterpriseApi.CertSpec{
		{SecretRef: corev1.LocalObjectReference{Name: "cert1"}},
		{SecretRef: corev1.LocalObjectReference{Name: "cert2"}, Role: enterpriseApi.CertRoleServer},
	})
	c := buildClient(cr, s1, s2)
	cfg, err := ReconcileCerts(context.Background(), c, cr, specToCertEntries(cr.Spec.Certs))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Volumes) != 2 {
		t.Errorf("expected 2 volumes, got %d", len(cfg.Volumes))
	}
}

func TestReconcileCerts_AutoGenerate_MissingSecret_CreatesCertificate(t *testing.T) {
	issuer := namespacedIssuer("ns", "my-issuer")
	cr := standaloneWithCerts("ns", "s1", nil)
	c := buildClientWithMapper(restMapperWithCertificateCRD(), cr, issuer)

	entries := []CertEntry{
		{
			SecretName: "auto-cert",
			Role:       string(enterpriseApi.CertRoleServer),
			IssuerRef:  &certlib.IssuerRef{Name: "my-issuer"},
			DNSNames:   []string{"svc.ns.svc.cluster.local"},
		},
	}

	cfg, err := ReconcileCerts(context.Background(), c, cr, entries)
	if !errors.Is(err, certlib.ErrCertificateNotReady) {
		t.Fatalf("expected ErrCertificateNotReady while cert-manager issues the cert, got: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config while cert-manager issues the cert, got: %+v", cfg)
	}

	created := &cmapi.Certificate{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "auto-cert"}, created); err != nil {
		t.Fatalf("expected Certificate CR to be created: %v", err)
	}
	if created.Spec.SecretName != "auto-cert" {
		t.Errorf("SecretName = %q, want %q", created.Spec.SecretName, "auto-cert")
	}
	if created.Spec.IssuerRef.Name != "my-issuer" {
		t.Errorf("IssuerRef.Name = %q, want %q", created.Spec.IssuerRef.Name, "my-issuer")
	}
	if len(created.OwnerReferences) != 1 || created.OwnerReferences[0].Name != "s1" {
		t.Errorf("expected owner reference to s1, got: %+v", created.OwnerReferences)
	}
}

func TestReconcileCerts_AutoGenerate_NoIssuerRef_ReturnsError(t *testing.T) {
	cr := standaloneWithCerts("ns", "s1", nil)
	c := buildClientWithMapper(restMapperWithCertificateCRD(), cr)

	entries := []CertEntry{
		{SecretName: "auto-cert", Role: string(enterpriseApi.CertRoleServer)},
	}

	cfg, err := ReconcileCerts(context.Background(), c, cr, entries)
	if err == nil {
		t.Fatal("expected error when issuerRef is missing")
	}
	if !errors.Is(err, certlib.ErrIssuerRefRequired) {
		t.Errorf("expected ErrIssuerRefRequired, got: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config on error, got: %+v", cfg)
	}
}

func TestReconcileCerts_AutoGenerate_IssuerNotFound_ReturnsError(t *testing.T) {
	cr := standaloneWithCerts("ns", "s1", nil)
	c := buildClientWithMapper(restMapperWithCertificateCRD(), cr)

	entries := []CertEntry{
		{SecretName: "auto-cert", IssuerRef: &certlib.IssuerRef{Name: "missing-issuer"}},
	}

	_, err := ReconcileCerts(context.Background(), c, cr, entries)
	if !errors.Is(err, certlib.ErrIssuerNotFound) {
		t.Fatalf("expected ErrIssuerNotFound, got: %v", err)
	}
}

func TestReconcileCerts_AutoGenerate_CertManagerNotInstalled_ReturnsError(t *testing.T) {
	cr := standaloneWithCerts("ns", "s1", nil)
	c := buildClientWithMapper(emptyRESTMapper(), cr)

	entries := []CertEntry{
		{SecretName: "auto-cert", IssuerRef: &certlib.IssuerRef{Name: "my-issuer"}},
	}

	cfg, err := ReconcileCerts(context.Background(), c, cr, entries)
	if !errors.Is(err, certlib.ErrCertManagerNotInstalled) {
		t.Fatalf("expected ErrCertManagerNotInstalled, got: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config on error, got: %+v", cfg)
	}
}

func TestReconcileCerts_AutoGenerate_CertGenerationGateDisabled_ReturnsError(t *testing.T) {
	config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.CertManagerCertGeneration): false})
	t.Cleanup(func() {
		config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.CertManagerCertGeneration): true})
	})

	cr := standaloneWithCerts("ns", "s1", nil)
	c := buildClientWithMapper(restMapperWithCertificateCRD(), cr)

	entries := []CertEntry{
		{SecretName: "auto-cert", IssuerRef: &certlib.IssuerRef{Name: "my-issuer"}},
	}

	cfg, err := ReconcileCerts(context.Background(), c, cr, entries)
	if !errors.Is(err, ErrCertGenerationDisabled) {
		t.Fatalf("expected ErrCertGenerationDisabled, got: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config on error, got: %+v", cfg)
	}

	created := &cmapi.Certificate{}
	getErr := c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "auto-cert"}, created)
	if getErr == nil {
		t.Fatal("expected no Certificate CR to be created when gate is disabled")
	}
}

func TestReconcileCerts_AutoGenerate_AlreadyExistsNotReady_ReturnsError(t *testing.T) {
	issuer := namespacedIssuer("ns", "my-issuer")
	cr := standaloneWithCerts("ns", "s1", nil)
	existing := &cmapi.Certificate{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "auto-cert"}}
	c := buildClientWithMapper(restMapperWithCertificateCRD(), cr, issuer, existing)

	entries := []CertEntry{
		{SecretName: "auto-cert", IssuerRef: &certlib.IssuerRef{Name: "my-issuer"}},
	}

	cfg, err := ReconcileCerts(context.Background(), c, cr, entries)
	if !errors.Is(err, certlib.ErrCertificateNotReady) {
		t.Fatalf("expected ErrCertificateNotReady while Certificate is not yet ready, got: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config while Certificate is not yet ready, got: %+v", cfg)
	}
}

func TestReconcileCerts_AutoGenerate_StampsGeneratedForUIDAnnotation(t *testing.T) {
	issuer := namespacedIssuer("ns", "my-issuer")
	cr := standaloneWithCerts("ns", "s1", nil)
	cr.UID = "cr-uid-1"
	c := buildClientWithMapper(restMapperWithCertificateCRD(), cr, issuer)

	entries := []CertEntry{
		{SecretName: "auto-cert", IssuerRef: &certlib.IssuerRef{Name: "my-issuer"}, DNSNames: []string{"svc.ns.svc.cluster.local"}},
	}

	if _, err := ReconcileCerts(context.Background(), c, cr, entries); !errors.Is(err, certlib.ErrCertificateNotReady) {
		t.Fatalf("expected ErrCertificateNotReady, got: %v", err)
	}

	created := &cmapi.Certificate{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "auto-cert"}, created); err != nil {
		t.Fatalf("expected Certificate CR to be created: %v", err)
	}
	if created.Spec.SecretTemplate == nil {
		t.Fatal("expected SecretTemplate to be set on the generated Certificate")
	}
	if got := created.Spec.SecretTemplate.Annotations[CertGeneratedForUIDAnnotation]; got != "cr-uid-1" {
		t.Errorf("SecretTemplate annotation %s = %q, want %q", CertGeneratedForUIDAnnotation, got, "cr-uid-1")
	}
}

func TestReconcileCerts_AutoGeneratedSecret_RejectedForDifferentCR(t *testing.T) {
	// Simulates a Secret that cert-manager populated from a Certificate whose
	// SecretTemplate stamped it as generated for a different CR's UID.
	secret := noCASecret("ns", "auto-cert")
	secret.Annotations = map[string]string{CertGeneratedForUIDAnnotation: "other-cr-uid"}
	cr := standaloneWithCerts("ns", "s1", []enterpriseApi.CertSpec{
		{SecretRef: corev1.LocalObjectReference{Name: "auto-cert"}},
	})
	cr.UID = "cr-uid-1"
	c := buildClient(cr, secret)

	cfg, err := ReconcileCerts(context.Background(), c, cr, specToCertEntries(cr.Spec.Certs))
	if err == nil {
		t.Fatal("expected error when reusing another CR's auto-generated secret")
	}
	if reason, ok := splcommon.TerminalReason(err); !ok || reason != EventReasonCertSecretWrongOwner {
		t.Errorf("expected TerminalError with reason %s, got: %v", EventReasonCertSecretWrongOwner, err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config on error, got: %+v", cfg)
	}
}

func TestReconcileCerts_AutoGeneratedSecret_AllowedForOwningCR(t *testing.T) {
	secret := noCASecret("ns", "auto-cert")
	secret.Annotations = map[string]string{CertGeneratedForUIDAnnotation: "cr-uid-1"}
	cr := standaloneWithCerts("ns", "s1", []enterpriseApi.CertSpec{
		{SecretRef: corev1.LocalObjectReference{Name: "auto-cert"}},
	})
	cr.UID = "cr-uid-1"
	c := buildClient(cr, secret)

	cfg, err := ReconcileCerts(context.Background(), c, cr, specToCertEntries(cr.Spec.Certs))
	if err != nil {
		t.Fatalf("unexpected error mounting own auto-generated secret: %v", err)
	}
	if cfg == nil || len(cfg.Volumes) != 1 {
		t.Fatalf("expected 1 volume mounted, got: %+v", cfg)
	}
}

func TestReconcileCerts_CustomerProvidedSecret_SharedAcrossCRs(t *testing.T) {
	// A customer-provided secret (no CertGeneratedForUIDAnnotation) must remain
	// mountable by any CR, regardless of UID.
	secret := noCASecret("ns", "shared-customer-cert")
	certSpec := []enterpriseApi.CertSpec{
		{SecretRef: corev1.LocalObjectReference{Name: "shared-customer-cert"}},
	}

	crA := standaloneWithCerts("ns", "a", certSpec)
	crA.UID = "cr-uid-a"
	cA := buildClient(crA, secret)
	if _, err := ReconcileCerts(context.Background(), cA, crA, specToCertEntries(certSpec)); err != nil {
		t.Fatalf("unexpected error for crA: %v", err)
	}

	crB := standaloneWithCerts("ns", "b", certSpec)
	crB.UID = "cr-uid-b"
	cB := buildClient(crB, secret)
	if _, err := ReconcileCerts(context.Background(), cB, crB, specToCertEntries(certSpec)); err != nil {
		t.Fatalf("unexpected error for crB: %v", err)
	}
}

func TestReconcileCerts_OperatorDriven_AutoGeneratedSecret_RejectedForDifferentCR(t *testing.T) {
	secret := noCASecret("ns", "op-cert")
	secret.Annotations = map[string]string{CertGeneratedForUIDAnnotation: "other-cr-uid"}
	cr := standaloneWithCerts("ns", "s1", nil)
	cr.UID = "cr-uid-1"
	c := buildClient(cr, secret)
	wrapped := &certRequesterStandalone{Standalone: cr, secrets: []string{"op-cert"}}

	cfg, err := ReconcileCerts(context.Background(), c, wrapped, nil)
	if err == nil {
		t.Fatal("expected error when reusing another CR's auto-generated secret via operator-driven path")
	}
	if reason, ok := splcommon.TerminalReason(err); !ok || reason != EventReasonCertSecretWrongOwner {
		t.Errorf("expected TerminalError with reason %s, got: %v", EventReasonCertSecretWrongOwner, err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config on error, got: %+v", cfg)
	}
}

// --- InjectCertMounts ---

func TestInjectCertMounts_Nil_NoOp(t *testing.T) {
	pod := &corev1.PodTemplateSpec{}
	InjectCertMounts(pod, nil)
	if len(pod.Spec.Volumes) != 0 {
		t.Error("expected no volumes after nil inject")
	}
}

func TestInjectCertMounts_InjectsVolumesAndMounts(t *testing.T) {
	pod := &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "splunk"}},
		},
	}
	cfg := &CertMountConfig{
		Volumes: []corev1.Volume{{Name: "cert-foo"}},
		VolumeMounts: []corev1.VolumeMount{{
			Name:      "cert-foo",
			MountPath: "/mnt/tls/foo",
		}},
		Annotations: map[string]string{"certRev/foo": "abc123"},
	}
	InjectCertMounts(pod, cfg)

	if len(pod.Spec.Volumes) != 1 {
		t.Errorf("expected 1 volume, got %d", len(pod.Spec.Volumes))
	}
	if len(pod.Spec.Containers[0].VolumeMounts) != 1 {
		t.Errorf("expected 1 volumeMount, got %d", len(pod.Spec.Containers[0].VolumeMounts))
	}
	if pod.Annotations["certRev/foo"] != "abc123" {
		t.Error("annotation not injected")
	}
}

func TestInjectCertMounts_AllContainersGetMounts(t *testing.T) {
	pod := &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "c1"}, {Name: "c2"}},
		},
	}
	cfg := &CertMountConfig{
		Volumes:      []corev1.Volume{{Name: "cert-foo"}},
		VolumeMounts: []corev1.VolumeMount{{Name: "cert-foo", MountPath: "/mnt/tls/foo"}},
		Annotations:  map[string]string{},
	}
	InjectCertMounts(pod, cfg)
	for _, c := range pod.Spec.Containers {
		if len(c.VolumeMounts) != 1 {
			t.Errorf("container %s: expected 1 mount, got %d", c.Name, len(c.VolumeMounts))
		}
	}
}

// --- helper type for CertificateRequester tests ---

type certRequesterStandalone struct {
	*enterpriseApi.Standalone
	secrets []string
}

func (c *certRequesterStandalone) Certificates() []string { return c.secrets }

// TestReconcileCerts_MalformedSecret_ErrorChain verifies that when a cert secret
// exists but is missing a required key, ReconcileCerts returns an error whose
// chain satisfies BOTH splcommon.TerminalMessage (controller sets Stalled) AND
// errors.Is(reconcile.TerminalError(nil)) (controller stops requeueing).
//
// Regression test: Apply* functions previously re-wrapped the ReconcileCerts
// error as reconcile.TerminalError(err), stripping the *splcommon.TerminalError
// from the chain. The controller tail's splcommon.TerminalMessage(err) then
// returned ok=false, causing ClearStalledCondition to run and losing the signal.
func TestReconcileCerts_MalformedSecret_ErrorChain(t *testing.T) {
	config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.CertManagement): true})

	secret := makeSecret("ns", "my-cert", map[string][]byte{
		CertTLSCRTKey: []byte("cert"), // tls.key missing → malformed
	})
	cr := standaloneWithCerts("ns", "s1", []enterpriseApi.CertSpec{
		{SecretRef: corev1.LocalObjectReference{Name: "my-cert"}},
	})
	c := buildClient(cr, secret)

	_, err := ReconcileCerts(context.Background(), c, cr, specToCertEntries(cr.Spec.Certs))
	if err == nil {
		t.Fatal("expected error for malformed cert secret, got nil")
	}

	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Errorf("errors.Is(err, reconcile.TerminalError(nil)) = false; controller would requeue a non-retryable failure: %v", err)
	}

	msg, ok := splcommon.TerminalMessage(err)
	if !ok {
		t.Errorf("splcommon.TerminalMessage returned ok=false; controller would call ClearStalledCondition and lose the Stalled signal: %v", err)
	}
	if msg == "" {
		t.Error("splcommon.TerminalMessage returned empty message")
	}
}

// TestValidateCertSecret_MissingKey_ErrorChain verifies that a missing-key error
// satisfies both splcommon.TerminalMessage (so the controller can surface a
// user-facing condition message) and errors.Is(reconcile.TerminalError(nil))
// (so the controller stops requeueing).
func TestValidateCertSecret_MissingKey_ErrorChain(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-certs", Namespace: "test"},
		Data:       map[string][]byte{CertTLSCRTKey: []byte("cert")}, // tls.key missing
	}
	c := fake.NewClientBuilder().WithScheme(scheme()).WithObjects(secret).Build()

	_, err := ValidateCertSecret(context.Background(), c, "test", "my-certs")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, reconcile.TerminalError(nil)) {
		t.Error("errors.Is(err, reconcile.TerminalError(nil)) = false; controller would requeue a non-retryable failure")
	}

	msg, ok := splcommon.TerminalMessage(err)
	if !ok {
		t.Error("splcommon.TerminalMessage returned ok=false; controller cannot surface condition message")
	}
	if msg == "" {
		t.Error("TerminalMessage returned empty message")
	}
}
