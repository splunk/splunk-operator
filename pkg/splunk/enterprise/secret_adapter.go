// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

package enterprise

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/splunk/splunk-operator/pkg/platform-sdk/api"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/secret"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// SecretAdapter provides a unified interface for secret management,
// supporting both legacy Kubernetes secrets and Platform SDK secret resolution.
//
// This adapter allows gradual migration from the legacy secret management
// to the Platform SDK without breaking existing deployments.
type SecretAdapter struct {
	// sdkEnabled indicates whether to use Platform SDK for secret resolution
	sdkEnabled bool

	// rctx is the Platform SDK reconcile context (only set if sdkEnabled=true)
	rctx api.ReconcileContext

	// client is the Kubernetes client for direct secret access
	client client.Client

	// namespace is the namespace for secret resolution
	namespace string

	// crName is the name of the CR (for SDK secret naming)
	crName string
}

// NewSecretAdapter creates a new SecretAdapter.
//
// Parameters:
//   - sdkEnabled: whether to use Platform SDK (set to false for legacy mode)
//   - rctx: Platform SDK reconcile context (can be nil if sdkEnabled=false)
//   - client: Kubernetes client
//   - namespace: namespace for secrets
//   - crName: name of the CR
func NewSecretAdapter(
	sdkEnabled bool,
	rctx api.ReconcileContext,
	client client.Client,
	namespace string,
	crName string,
) *SecretAdapter {
	return &SecretAdapter{
		sdkEnabled: sdkEnabled,
		rctx:       rctx,
		client:     client,
		namespace:  namespace,
		crName:     crName,
	}
}

// GetSplunkSecret retrieves the Splunk secret, using either Platform SDK
// or legacy secret resolution based on configuration.
//
// The secret contains:
//   - password: Splunk admin password
//   - hec_token: HTTP Event Collector token
//   - pass4SymmKey: cluster security key
//   - idxc_secret: indexer cluster secret (optional)
//   - shc_secret: search head cluster secret (optional)
func (a *SecretAdapter) GetSplunkSecret(ctx context.Context) (*corev1.Secret, *secret.Ref, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("SecretAdapter.GetSplunkSecret")

	if a.sdkEnabled && a.rctx != nil {
		scopedLog.Info("Using Platform SDK for secret resolution", "cr", a.crName)
		return a.getSecretViaSDK(ctx)
	}

	scopedLog.Info("Using legacy secret resolution", "cr", a.crName)
	secret, err := a.getSecretLegacy(ctx)
	return secret, nil, err
}

// getSecretViaSDK uses the Platform SDK to resolve secrets.
//
// This provides:
//   - Pluggable providers (K8s native, ESO, Vault, etc.)
//   - Secret versioning for rolling updates
//   - Automatic secret rotation
//   - External secret synchronization detection
func (a *SecretAdapter) getSecretViaSDK(ctx context.Context) (*corev1.Secret, *secret.Ref, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getSecretViaSDK")

	// Define required secret keys for Splunk
	requiredKeys := []string{
		"password",     // Admin password
		"hec_token",    // HTTP Event Collector token
		"pass4SymmKey", // Cluster security key
	}

	// Resolve secret via Platform SDK
	secretRef, err := a.rctx.ResolveSecret(secret.Binding{
		Name:      fmt.Sprintf("%s-credentials", a.crName),
		Namespace: a.namespace,
		Type:      secret.SecretTypeSplunk,
		Keys:      requiredKeys,
	})
	if err != nil {
		scopedLog.Error(err, "Failed to resolve secret via Platform SDK",
			"cr", a.crName,
			"namespace", a.namespace)
		return nil, nil, fmt.Errorf("SDK secret resolution failed: %w", err)
	}

	// Check if secret is ready
	if !secretRef.Ready {
		scopedLog.Info("Secret not ready yet",
			"cr", a.crName,
			"secretName", secretRef.SecretName,
			"provider", secretRef.Provider,
			"error", secretRef.Error)
		return nil, secretRef, fmt.Errorf("secret not ready: %s", secretRef.Error)
	}

	scopedLog.Info("Secret resolved successfully",
		"cr", a.crName,
		"secretName", secretRef.SecretName,
		"provider", secretRef.Provider,
		"version", secretRef.Version,
		"keys", secretRef.Keys)

	// Emit event for secret resolution (if EventRecorder is available)
	if eventRecorder := a.rctx.EventRecorder(); eventRecorder != nil {
		eventRecorder.Event(
			&corev1.ObjectReference{
				Kind:      "Standalone",
				Namespace: a.namespace,
				Name:      a.crName,
			},
			corev1.EventTypeNormal,
			"SecretResolved",
			fmt.Sprintf("Secret %s resolved via %s (version: %v)", secretRef.SecretName, secretRef.Provider, secretRef.Version),
		)
	}

	// Get the actual Kubernetes secret
	k8sSecret := &corev1.Secret{}
	err = a.client.Get(ctx, types.NamespacedName{
		Name:      secretRef.SecretName,
		Namespace: secretRef.Namespace,
	}, k8sSecret)
	if err != nil {
		scopedLog.Error(err, "Failed to get Kubernetes secret",
			"secretName", secretRef.SecretName,
			"namespace", secretRef.Namespace)
		return nil, secretRef, fmt.Errorf("failed to get K8s secret: %w", err)
	}

	return k8sSecret, secretRef, nil
}

// getSecretLegacy uses the legacy secret resolution logic.
//
// This maintains backwards compatibility with existing deployments
// that don't use the Platform SDK.
func (a *SecretAdapter) getSecretLegacy(ctx context.Context) (*corev1.Secret, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getSecretLegacy")

	// Use existing naming convention: splunk-{namespace}-secret
	secretName := splcommon.GetNamespaceScopedSecretName(a.namespace)

	scopedLog.V(1).Info("Fetching legacy secret",
		"secretName", secretName,
		"namespace", a.namespace)

	k8sSecret := &corev1.Secret{}
	err := a.client.Get(ctx, types.NamespacedName{
		Name:      secretName,
		Namespace: a.namespace,
	}, k8sSecret)
	if err != nil {
		scopedLog.Error(err, "Failed to get legacy secret",
			"secretName", secretName,
			"namespace", a.namespace)
		return nil, fmt.Errorf("failed to get legacy secret: %w", err)
	}

	scopedLog.V(1).Info("Legacy secret fetched successfully",
		"secretName", secretName,
		"keys", getSecretKeys(k8sSecret))

	return k8sSecret, nil
}

// GetSecretVersion returns the secret version if using SDK, otherwise returns nil.
//
// This is useful for determining whether a rolling restart is needed
// due to secret rotation.
func (a *SecretAdapter) GetSecretVersion() *int {
	// Version tracking only available via SDK
	// For legacy mode, return nil
	return nil
}

// IsSDKEnabled returns whether Platform SDK is enabled for this adapter.
func (a *SecretAdapter) IsSDKEnabled() bool {
	return a.sdkEnabled
}

// getSecretKeys returns the list of keys in a secret (for logging).
func getSecretKeys(secret *corev1.Secret) []string {
	keys := make([]string, 0, len(secret.Data))
	for k := range secret.Data {
		keys = append(keys, k)
	}
	return keys
}
