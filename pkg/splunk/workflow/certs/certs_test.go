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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// --- helpers ---

func scheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = enterpriseApi.AddToScheme(s)
	return s
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

func TestReconcileCerts_MissingSecret_Skipped(t *testing.T) {
	cr := standaloneWithCerts("ns", "s1", []enterpriseApi.CertSpec{
		{SecretRef: corev1.LocalObjectReference{Name: "missing-cert"}},
	})
	c := buildClient(cr)
	cfg, err := ReconcileCerts(context.Background(), c, cr, specToCertEntries(cr.Spec.Certs))
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Fatal("expected nil config — missing secret should be skipped")
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
