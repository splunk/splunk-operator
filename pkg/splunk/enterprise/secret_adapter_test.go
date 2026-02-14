// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

package enterprise

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/go-logr/logr"
	platformv4 "github.com/splunk/splunk-operator/api/platform/v4"
	sdk "github.com/splunk/splunk-operator/pkg/platform-sdk"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/config"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// TestSecretAdapter_LegacyMode tests secret adapter in legacy mode (SDK disabled).
func TestSecretAdapter_LegacyMode(t *testing.T) {
	ctx := context.Background()
	namespace := "test-ns"
	crName := "test-standalone"

	// Create scheme
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	// Create legacy secret: splunk-{namespace}-secret
	legacySecretName := splcommon.GetNamespaceScopedSecretName(namespace)
	legacySecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      legacySecretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"password":     []byte("admin123"),
			"hec_token":    []byte("token123"),
			"pass4SymmKey": []byte("key123"),
		},
	}

	// Create fake client with legacy secret
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(legacySecret).
		Build()

	// Create adapter in legacy mode (sdkEnabled=false)
	adapter := NewSecretAdapter(
		false, // SDK disabled
		nil,   // no rctx needed
		fakeClient,
		namespace,
		crName,
	)

	// Test: Get secret in legacy mode
	secret, secretRef, err := adapter.GetSplunkSecret(ctx)
	if err != nil {
		t.Fatalf("GetSplunkSecret() failed: %v", err)
	}

	// Verify: Secret should be returned
	if secret == nil {
		t.Fatal("Expected secret to be returned, got nil")
	}

	// Verify: SecretRef should be nil in legacy mode
	if secretRef != nil {
		t.Error("Expected secretRef to be nil in legacy mode")
	}

	// Verify: Secret name matches legacy naming
	if secret.Name != legacySecretName {
		t.Errorf("Secret name = %v, want %v", secret.Name, legacySecretName)
	}

	// Verify: Secret has required keys
	requiredKeys := []string{"password", "hec_token", "pass4SymmKey"}
	for _, key := range requiredKeys {
		if _, ok := secret.Data[key]; !ok {
			t.Errorf("Secret missing required key: %s", key)
		}
	}

	// Verify: SDK not enabled
	if adapter.IsSDKEnabled() {
		t.Error("IsSDKEnabled() should return false in legacy mode")
	}
}

// TestSecretAdapter_SDKMode tests secret adapter with Platform SDK enabled.
func TestSecretAdapter_SDKMode(t *testing.T) {
	ctx := context.Background()
	namespace := "test-ns"
	crName := "test-standalone"

	// Create scheme
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = config.AddToScheme(scheme)
	_ = platformv4.AddToScheme(scheme)

	// Create source secret (what admins create manually or via ESO)
	sourceSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-" + namespace + "-secret",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"password":     []byte("admin456"),
			"hec_token":    []byte("token456"),
			"pass4SymmKey": []byte("key456"),
		},
	}

	// Create fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(sourceSecret).
		Build()

	// Create SDK runtime
	eventRecorder := record.NewFakeRecorder(100)
	sdkRuntime, err := sdk.NewRuntime(
		fakeClient,
		sdk.WithClusterScoped(),
		sdk.WithLogger(logr.Discard()),
		sdk.WithEventRecorder(eventRecorder),
	)
	if err != nil {
		t.Fatalf("Failed to create SDK runtime: %v", err)
	}

	// Start SDK runtime
	if err := sdkRuntime.Start(ctx); err != nil {
		t.Fatalf("Failed to start SDK runtime: %v", err)
	}
	defer sdkRuntime.Stop()

	// Create reconcile context
	rctx := sdkRuntime.NewReconcileContext(ctx, namespace, crName)

	// Create adapter in SDK mode
	adapter := NewSecretAdapter(
		true, // SDK enabled
		rctx,
		fakeClient,
		namespace,
		crName,
	)

	// Test: Get secret via SDK
	secret, secretRef, err := adapter.GetSplunkSecret(ctx)
	if err != nil {
		t.Fatalf("GetSplunkSecret() failed: %v", err)
	}

	// Verify: Secret should be returned
	if secret == nil {
		t.Fatal("Expected secret to be returned, got nil")
	}

	// Verify: SecretRef should be returned in SDK mode
	if secretRef == nil {
		t.Fatal("Expected secretRef to be returned in SDK mode, got nil")
	}

	// Verify: SecretRef is ready
	if !secretRef.Ready {
		t.Errorf("SecretRef.Ready = false, want true. Error: %s", secretRef.Error)
	}

	// Verify: SecretRef has version (for Splunk secrets)
	if secretRef.Version == nil {
		t.Error("SecretRef.Version should not be nil for Splunk secrets")
	} else if *secretRef.Version != 1 {
		t.Errorf("SecretRef.Version = %v, want 1 (first version)", *secretRef.Version)
	}

	// Verify: Secret has required keys
	requiredKeys := []string{"password", "hec_token", "pass4SymmKey"}
	for _, key := range requiredKeys {
		if _, ok := secret.Data[key]; !ok {
			t.Errorf("Secret missing required key: %s", key)
		}
	}

	// Verify: SDK enabled
	if !adapter.IsSDKEnabled() {
		t.Error("IsSDKEnabled() should return true in SDK mode")
	}

	// Verify: Event was recorded
	select {
	case event := <-eventRecorder.Events:
		if event == "" {
			t.Error("Expected event to be recorded, got empty")
		}
		t.Logf("Event recorded: %s", event)
	default:
		t.Error("Expected event to be recorded, got none")
	}
}

// TestSecretAdapter_SDKMode_SecretNotReady tests behavior when secret is not yet synced.
func TestSecretAdapter_SDKMode_SecretNotReady(t *testing.T) {
	ctx := context.Background()
	namespace := "test-ns"
	crName := "test-standalone"

	// Create scheme
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = config.AddToScheme(scheme)
	_ = platformv4.AddToScheme(scheme)

	// Create fake client WITHOUT source secret (simulating ExternalSecret not yet synced)
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	// Create SDK runtime
	sdkRuntime, err := sdk.NewRuntime(
		fakeClient,
		sdk.WithClusterScoped(),
		sdk.WithLogger(logr.Discard()),
	)
	if err != nil {
		t.Fatalf("Failed to create SDK runtime: %v", err)
	}

	if err := sdkRuntime.Start(ctx); err != nil {
		t.Fatalf("Failed to start SDK runtime: %v", err)
	}
	defer sdkRuntime.Stop()

	// Create reconcile context
	rctx := sdkRuntime.NewReconcileContext(ctx, namespace, crName)

	// Create adapter in SDK mode
	adapter := NewSecretAdapter(
		true,
		rctx,
		fakeClient,
		namespace,
		crName,
	)

	// Test: Get secret when not ready
	secret, secretRef, err := adapter.GetSplunkSecret(ctx)

	// Verify: Error should be returned
	if err == nil {
		t.Fatal("Expected error when secret not ready, got nil")
	}

	// Verify: Secret should be nil
	if secret != nil {
		t.Error("Expected secret to be nil when not ready")
	}

	// Verify: SecretRef should be returned even when not ready
	if secretRef == nil {
		t.Fatal("Expected secretRef to be returned even when not ready")
	}

	// Verify: SecretRef.Ready should be false
	if secretRef.Ready {
		t.Error("SecretRef.Ready should be false")
	}

	// Verify: Error message in SecretRef
	if secretRef.Error == "" {
		t.Error("SecretRef.Error should contain error message")
	}

	t.Logf("Expected error: %v", err)
	t.Logf("SecretRef.Error: %s", secretRef.Error)
}

// TestSecretAdapter_SDKMode_SecretVersioning tests secret versioning.
func TestSecretAdapter_SDKMode_SecretVersioning(t *testing.T) {
	ctx := context.Background()
	namespace := "test-ns"
	crName := "test-standalone"

	// Create scheme
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = config.AddToScheme(scheme)
	_ = platformv4.AddToScheme(scheme)

	// Create source secret
	sourceSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-" + namespace + "-secret",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"password":     []byte("admin789"),
			"hec_token":    []byte("token789"),
			"pass4SymmKey": []byte("key789"),
		},
	}

	// Create fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(sourceSecret).
		Build()

	// Create SDK runtime
	sdkRuntime, err := sdk.NewRuntime(
		fakeClient,
		sdk.WithClusterScoped(),
		sdk.WithLogger(logr.Discard()),
	)
	if err != nil {
		t.Fatalf("Failed to create SDK runtime: %v", err)
	}

	if err := sdkRuntime.Start(ctx); err != nil {
		t.Fatalf("Failed to start SDK runtime: %v", err)
	}
	defer sdkRuntime.Stop()

	// Create reconcile context
	rctx := sdkRuntime.NewReconcileContext(ctx, namespace, crName)

	// Create adapter
	adapter := NewSecretAdapter(true, rctx, fakeClient, namespace, crName)

	// First resolution - should create v1
	_, secretRef1, err := adapter.GetSplunkSecret(ctx)
	if err != nil {
		t.Fatalf("First GetSplunkSecret() failed: %v", err)
	}

	if secretRef1.Version == nil || *secretRef1.Version != 1 {
		t.Errorf("First version = %v, want 1", secretRef1.Version)
	}

	// Second resolution with same content - should return same version
	_, secretRef2, err := adapter.GetSplunkSecret(ctx)
	if err != nil {
		t.Fatalf("Second GetSplunkSecret() failed: %v", err)
	}

	if secretRef2.Version == nil || *secretRef2.Version != 1 {
		t.Errorf("Second version = %v, want 1 (unchanged)", secretRef2.Version)
	}

	// Update source secret
	sourceSecret.Data["password"] = []byte("new-password")
	if err := fakeClient.Update(ctx, sourceSecret); err != nil {
		t.Fatalf("Failed to update source secret: %v", err)
	}

	// Third resolution with changed content - should create v2
	_, secretRef3, err := adapter.GetSplunkSecret(ctx)
	if err != nil {
		t.Fatalf("Third GetSplunkSecret() failed: %v", err)
	}

	if secretRef3.Version == nil || *secretRef3.Version != 2 {
		t.Errorf("Third version = %v, want 2 (changed)", secretRef3.Version)
	}

	// Verify versioned secret was created
	versionedSecretName := fmt.Sprintf("%s-credentials-v2", crName)
	versionedSecret := &corev1.Secret{}
	err = fakeClient.Get(ctx, types.NamespacedName{
		Name:      versionedSecretName,
		Namespace: namespace,
	}, versionedSecret)
	if err != nil {
		t.Errorf("Versioned secret not created: %v", err)
	}
}

// TestGetSecretKeys tests the getSecretKeys helper function.
func TestGetSecretKeys(t *testing.T) {
	secret := &corev1.Secret{
		Data: map[string][]byte{
			"key1": []byte("value1"),
			"key2": []byte("value2"),
			"key3": []byte("value3"),
		},
	}

	keys := getSecretKeys(secret)

	if len(keys) != 3 {
		t.Errorf("Expected 3 keys, got %d", len(keys))
	}

	// Verify all keys are present (order doesn't matter)
	keyMap := make(map[string]bool)
	for _, k := range keys {
		keyMap[k] = true
	}

	for _, expectedKey := range []string{"key1", "key2", "key3"} {
		if !keyMap[expectedKey] {
			t.Errorf("Expected key %s not found in keys list", expectedKey)
		}
	}
}
