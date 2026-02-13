// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

package sdk_test

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	platformv4 "github.com/splunk/splunk-operator/api/platform/v4"
	sdk "github.com/splunk/splunk-operator/pkg/platform-sdk"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/certificate"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/config"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/discovery"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/secret"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestSDKIntegration_BasicWorkflow tests the complete SDK workflow.
func TestSDKIntegration_BasicWorkflow(t *testing.T) {
	// Create fake client with scheme
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = config.AddToScheme(scheme)
	_ = platformv4.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	// Create event recorder
	eventRecorder := record.NewFakeRecorder(100)

	// Create SDK runtime
	runtime, err := sdk.NewRuntime(
		fakeClient,
		sdk.WithClusterScoped(),
		sdk.WithLogger(logr.Discard()),
		sdk.WithEventRecorder(eventRecorder),
	)
	if err != nil {
		t.Fatalf("NewRuntime() failed: %v", err)
	}

	// Start the runtime
	ctx := context.Background()
	if err := runtime.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Create reconcile context
	rctx := runtime.NewReconcileContext(ctx, "default", "test-standalone")

	// Verify context properties
	if rctx.Namespace() != "default" {
		t.Errorf("Namespace() = %v, want default", rctx.Namespace())
	}
	if rctx.Name() != "test-standalone" {
		t.Errorf("Name() = %v, want test-standalone", rctx.Name())
	}
	if rctx.Context() != ctx {
		t.Error("Context() should return original context")
	}
	// Logger is a zero-value struct if nil, so just verify we can call it
	_ = rctx.Logger()
	if rctx.EventRecorder() == nil {
		t.Error("EventRecorder() should not be nil")
	}

	// Test certificate resolution (self-signed fallback)
	certRef, err := rctx.ResolveCertificate(certificate.Binding{
		Name: "test-tls",
		DNSNames: []string{
			"test.default.svc",
			"test.default.svc.cluster.local",
		},
	})
	if err != nil {
		t.Fatalf("ResolveCertificate() failed: %v", err)
	}

	// Self-signed should be ready immediately
	if !certRef.Ready {
		t.Error("Self-signed certificate should be ready immediately")
	}
	if certRef.Provider != "self-signed" {
		t.Errorf("Provider = %v, want self-signed", certRef.Provider)
	}
	if certRef.SecretName != "test-tls" {
		t.Errorf("SecretName = %v, want test-tls", certRef.SecretName)
	}

	// Verify certificate secret was created
	certSecret := &corev1.Secret{}
	err = fakeClient.Get(ctx, client.ObjectKey{
		Name:      "test-tls",
		Namespace: "default",
	}, certSecret)
	if err != nil {
		t.Errorf("Certificate secret not created: %v", err)
	}

	// Test secret resolution
	// First, create a source secret
	sourceSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-default-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"password":  []byte("admin123"),
			"hec_token": []byte("token123"),
		},
	}
	if err := fakeClient.Create(ctx, sourceSecret); err != nil {
		t.Fatalf("Failed to create source secret: %v", err)
	}

	secretRef, err := rctx.ResolveSecret(secret.Binding{
		Name: "test-credentials",
		Type: secret.SecretTypeSplunk,
		Keys: []string{"password", "hec_token"},
	})
	if err != nil {
		t.Fatalf("ResolveSecret() failed: %v", err)
	}

	if !secretRef.Ready {
		t.Error("Secret should be ready")
	}
	if secretRef.Version == nil {
		t.Error("Versioned secret should have version")
	} else if *secretRef.Version != 1 {
		t.Errorf("First version should be 1, got %v", *secretRef.Version)
	}

	// Test service discovery
	// Create a test service
	testService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-indexer",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "splunk-operator",
				"app.kubernetes.io/component":  "IndexerCluster",
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.0.0.1",
			Ports: []corev1.ServicePort{
				{Name: "mgmt", Port: 8089},
			},
		},
	}
	if err := fakeClient.Create(ctx, testService); err != nil {
		t.Fatalf("Failed to create test service: %v", err)
	}

	endpoints, err := rctx.DiscoverSplunk(discovery.SplunkSelector{
		Type:      discovery.SplunkTypeIndexerCluster,
		Namespace: "default",
	})
	if err != nil {
		t.Fatalf("DiscoverSplunk() failed: %v", err)
	}

	if len(endpoints) != 1 {
		t.Errorf("Expected 1 endpoint, got %v", len(endpoints))
	}
	if len(endpoints) > 0 {
		if endpoints[0].Name != "test-indexer" {
			t.Errorf("Endpoint name = %v, want test-indexer", endpoints[0].Name)
		}
		if endpoints[0].Type != discovery.SplunkTypeIndexerCluster {
			t.Errorf("Endpoint type = %v, want IndexerCluster", endpoints[0].Type)
		}
	}

	// Test builders
	sts, err := rctx.BuildStatefulSet().
		WithName("test-standalone").
		WithImage("splunk/splunk:9.1.0").
		WithReplicas(3).
		WithPorts([]corev1.ContainerPort{
			{Name: "web", ContainerPort: 8000},
			{Name: "mgmt", ContainerPort: 8089},
		}).
		WithCertificate(certRef).
		WithSecret(secretRef).
		Build()
	if err != nil {
		t.Fatalf("BuildStatefulSet() failed: %v", err)
	}

	if sts.Name != "test-standalone" {
		t.Errorf("StatefulSet name = %v, want test-standalone", sts.Name)
	}
	if *sts.Spec.Replicas != 3 {
		t.Errorf("Replicas = %v, want 3", *sts.Spec.Replicas)
	}

	// Verify certificate and secret volumes were created
	foundCertVol := false
	foundSecretVol := false
	for _, vol := range sts.Spec.Template.Spec.Volumes {
		if vol.Name == certRef.SecretName {
			foundCertVol = true
		}
		if vol.Name == secretRef.SecretName {
			foundSecretVol = true
		}
	}
	if !foundCertVol {
		t.Error("Certificate volume not created")
	}
	if !foundSecretVol {
		t.Error("Secret volume not created")
	}

	// Test service builder
	svc, err := rctx.BuildService().
		WithName("test-standalone").
		WithType(corev1.ServiceTypeClusterIP).
		WithPorts([]corev1.ServicePort{
			{Name: "web", Port: 8000, TargetPort: intstr.FromInt(8000)},
			{Name: "mgmt", Port: 8089, TargetPort: intstr.FromInt(8089)},
		}).
		WithDiscoveryLabels().
		Build()
	if err != nil {
		t.Fatalf("BuildService() failed: %v", err)
	}

	if svc.Labels["splunk.com/discoverable"] != "true" {
		t.Error("Discovery label not set")
	}

	// Test configmap builder
	cm, err := rctx.BuildConfigMap().
		WithName("test-config").
		WithData(map[string]string{
			"server.conf": "[general]\nserverName = test",
		}).
		Build()
	if err != nil {
		t.Fatalf("BuildConfigMap() failed: %v", err)
	}

	if cm.Data["server.conf"] == "" {
		t.Error("ConfigMap data not set")
	}

	// Stop the runtime
	if err := runtime.Stop(); err != nil {
		t.Errorf("Stop() failed: %v", err)
	}
}

// TestSDKIntegration_ConfigurationHierarchy tests configuration resolution.
func TestSDKIntegration_ConfigurationHierarchy(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = config.AddToScheme(scheme)
	_ = platformv4.AddToScheme(scheme)

	// Create PlatformConfig
	duration := metav1.Duration{Duration: 90 * 24 * 3600}
	platformConfig := &config.PlatformConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "platform-default",
		},
		Spec: config.PlatformConfigSpec{
			Certificates: config.CertificateConfig{
				Provider: "self-signed",
				Duration: &duration,
			},
			Observability: config.ObservabilityConfig{
				Enabled: true,
				PrometheusAnnotations: config.PrometheusAnnotations{
					Scrape: true,
					Port:   9090,
					Path:   "/metrics",
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(platformConfig).
		Build()

	runtime, err := sdk.NewRuntime(
		fakeClient,
		sdk.WithClusterScoped(),
		sdk.WithLogger(logr.Discard()),
	)
	if err != nil {
		t.Fatalf("NewRuntime() failed: %v", err)
	}

	ctx := context.Background()
	if err := runtime.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer runtime.Stop()

	rctx := runtime.NewReconcileContext(ctx, "default", "test")

	// Test certificate with platform config
	certRef, err := rctx.ResolveCertificate(certificate.Binding{
		Name:     "test-cert",
		DNSNames: []string{"test.svc"},
	})
	if err != nil {
		t.Fatalf("ResolveCertificate() failed: %v", err)
	}

	if certRef.Provider != "self-signed" {
		t.Errorf("Provider = %v, want self-signed (from PlatformConfig)", certRef.Provider)
	}
}

// TestSDKIntegration_SecretVersioning tests secret version management.
func TestSDKIntegration_SecretVersioning(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = config.AddToScheme(scheme)
	_ = platformv4.AddToScheme(scheme)

	// Create source secret
	sourceSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-default-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"password": []byte("initial-password"),
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(sourceSecret).
		Build()

	runtime, err := sdk.NewRuntime(
		fakeClient,
		sdk.WithClusterScoped(),
		sdk.WithLogger(logr.Discard()),
	)
	if err != nil {
		t.Fatalf("NewRuntime() failed: %v", err)
	}

	ctx := context.Background()
	if err := runtime.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer runtime.Stop()

	rctx := runtime.NewReconcileContext(ctx, "default", "test")

	// First resolution creates v1
	secret1, err := rctx.ResolveSecret(secret.Binding{
		Name: "test-secret",
		Type: secret.SecretTypeSplunk,
		Keys: []string{"password"},
	})
	if err != nil {
		t.Fatalf("First ResolveSecret() failed: %v", err)
	}

	if *secret1.Version != 1 {
		t.Errorf("First version = %v, want 1", *secret1.Version)
	}

	// Second resolution with same content returns same version
	secret2, err := rctx.ResolveSecret(secret.Binding{
		Name: "test-secret",
		Type: secret.SecretTypeSplunk,
		Keys: []string{"password"},
	})
	if err != nil {
		t.Fatalf("Second ResolveSecret() failed: %v", err)
	}

	if *secret2.Version != 1 {
		t.Errorf("Second version = %v, want 1 (no change)", *secret2.Version)
	}

	// Update source secret
	sourceSecret.Data["password"] = []byte("new-password")
	if err := fakeClient.Update(ctx, sourceSecret); err != nil {
		t.Fatalf("Failed to update source secret: %v", err)
	}

	// Third resolution with changed content creates v2
	secret3, err := rctx.ResolveSecret(secret.Binding{
		Name: "test-secret",
		Type: secret.SecretTypeSplunk,
		Keys: []string{"password"},
	})
	if err != nil {
		t.Fatalf("Third ResolveSecret() failed: %v", err)
	}

	if *secret3.Version != 2 {
		t.Errorf("Third version = %v, want 2 (changed content)", *secret3.Version)
	}
}

// TestSDKIntegration_MultipleContexts tests multiple concurrent reconcile contexts.
func TestSDKIntegration_MultipleContexts(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = config.AddToScheme(scheme)
	_ = platformv4.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	runtime, err := sdk.NewRuntime(
		fakeClient,
		sdk.WithClusterScoped(),
		sdk.WithLogger(logr.Discard()),
	)
	if err != nil {
		t.Fatalf("NewRuntime() failed: %v", err)
	}

	ctx := context.Background()
	if err := runtime.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer runtime.Stop()

	// Create multiple contexts
	rctx1 := runtime.NewReconcileContext(ctx, "ns1", "resource1")
	rctx2 := runtime.NewReconcileContext(ctx, "ns2", "resource2")
	rctx3 := runtime.NewReconcileContext(ctx, "ns1", "resource3")

	// Verify contexts are independent
	if rctx1.Namespace() != "ns1" || rctx1.Name() != "resource1" {
		t.Error("Context 1 has wrong namespace/name")
	}
	if rctx2.Namespace() != "ns2" || rctx2.Name() != "resource2" {
		t.Error("Context 2 has wrong namespace/name")
	}
	if rctx3.Namespace() != "ns1" || rctx3.Name() != "resource3" {
		t.Error("Context 3 has wrong namespace/name")
	}

	// All contexts should share the same runtime
	if rctx1.Context() != ctx || rctx2.Context() != ctx || rctx3.Context() != ctx {
		t.Error("Contexts should share the same Go context")
	}
}

// TestSDKIntegration_EventRecording tests event emission.
func TestSDKIntegration_EventRecording(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = config.AddToScheme(scheme)
	_ = platformv4.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	eventRecorder := record.NewFakeRecorder(10)

	runtime, err := sdk.NewRuntime(
		fakeClient,
		sdk.WithClusterScoped(),
		sdk.WithLogger(logr.Discard()),
		sdk.WithEventRecorder(eventRecorder),
	)
	if err != nil {
		t.Fatalf("NewRuntime() failed: %v", err)
	}

	ctx := context.Background()
	if err := runtime.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer runtime.Stop()

	rctx := runtime.NewReconcileContext(ctx, "default", "test")

	// Create a dummy object
	obj := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	// Record an event
	rctx.EventRecorder().Event(obj, corev1.EventTypeNormal, "TestReason", "Test message")

	// Verify event was recorded
	select {
	case event := <-eventRecorder.Events:
		if event != "Normal TestReason Test message" {
			t.Errorf("Event = %v, want 'Normal TestReason Test message'", event)
		}
	case <-time.After(1 * time.Second):
		t.Error("Event was not recorded")
	}
}
