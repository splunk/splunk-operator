// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//  http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package secret implements secret validation and versioned secret management.
package secret

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/secret"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/services/config"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/services/interfaces"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Resolver implements secret validation and versioned secret management.
type Resolver struct {
	client         client.Client
	configResolver interfaces.ConfigResolver
	config         *api.RuntimeConfig
	logger         logr.Logger
}

// NewResolver creates a new secret resolver.
func NewResolver(
	client client.Client,
	configResolver interfaces.ConfigResolver,
	cfg *api.RuntimeConfig,
	logger logr.Logger,
) *Resolver {
	return &Resolver{
		client:         client,
		configResolver: configResolver,
		config:         cfg,
		logger:         logger.WithName("secret-resolver"),
	}
}

// Resolve validates that a secret exists and has required keys.
func (r *Resolver) Resolve(ctx context.Context, binding secret.Binding) (*secret.Ref, error) {
	if binding.Name == "" && binding.Service == "" {
		return nil, fmt.Errorf("secret name or service/instance is required")
	}
	if len(binding.Keys) == 0 {
		return nil, fmt.Errorf("at least one key is required")
	}

	secretConfig, err := r.configResolver.ResolveSecretConfig(ctx, binding.Namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve secret configuration: %w", err)
	}

	// Handle CSI secrets if CSI is configured in binding or provider is "csi"
	if binding.CSI != nil || secretConfig.Provider == "csi" {
		return r.resolveCSISecret(ctx, binding, secretConfig)
	}

	// Handle versioned Splunk secrets
	if binding.Type == secret.SecretTypeSplunk {
		return r.resolveVersionedSecret(ctx, binding, secretConfig)
	}

	// Handle generic secrets
	return r.resolveGenericSecret(ctx, binding, secretConfig)
}

// resolveCSISecret resolves CSI-based secrets via SecretProviderClass.
func (r *Resolver) resolveCSISecret(ctx context.Context, binding secret.Binding, cfg interface{}) (*secret.Ref, error) {
	// Type assertion to get ResolvedSecretConfig
	secretConfig, ok := cfg.(*config.ResolvedSecretConfig)
	if !ok {
		return nil, fmt.Errorf("invalid secret config type for CSI resolution")
	}

	// Determine SecretProviderClass name
	providerClassName := ""
	if binding.CSI != nil && binding.CSI.ProviderClass != "" {
		// Explicitly provided
		providerClassName = binding.CSI.ProviderClass
	} else if binding.Service != "" && binding.Instance != "" {
		// Apply naming pattern
		var err error
		providerClassName, err = r.applyCSINamingPattern(secretConfig.CSINamingPattern, binding)
		if err != nil {
			return nil, fmt.Errorf("failed to apply CSI naming pattern: %w", err)
		}
	} else {
		return nil, fmt.Errorf("CSI secret requires either explicit ProviderClass or Service+Instance for pattern resolution")
	}

	// Check if SecretProviderClass exists
	providerClass := &unstructured.Unstructured{}
	providerClass.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "secrets-store.csi.x-k8s.io",
		Version: "v1",
		Kind:    "SecretProviderClass",
	})

	err := r.client.Get(ctx, types.NamespacedName{
		Name:      providerClassName,
		Namespace: binding.Namespace,
	}, providerClass)

	if err != nil {
		if apierrors.IsNotFound(err) {
			return &secret.Ref{
				SecretName: binding.Name,
				Namespace:  binding.Namespace,
				Ready:      false,
				Provider:   fmt.Sprintf("csi-%s", secretConfig.CSIProvider),
				Error:      fmt.Sprintf("SecretProviderClass %s not found in namespace %s", providerClassName, binding.Namespace),
			}, nil
		}
		return nil, fmt.Errorf("failed to get SecretProviderClass: %w", err)
	}

	// Determine mount path
	mountPath := secretConfig.CSIMountPath
	if binding.CSI != nil && binding.CSI.MountPath != "" {
		mountPath = binding.CSI.MountPath
	}
	if mountPath == "" {
		mountPath = "/mnt/secrets"
	}

	// Determine CSI provider
	csiProvider := secretConfig.CSIProvider
	if binding.CSI != nil && binding.CSI.Provider != "" {
		csiProvider = binding.CSI.Provider
	}
	if csiProvider == "" {
		csiProvider = "vault" // Default
	}

	// Determine CSI driver
	csiDriver := secretConfig.CSIDriver
	if csiDriver == "" {
		csiDriver = "secrets-store.csi.k8s.io"
	}

	// SecretProviderClass exists, return CSI reference
	return &secret.Ref{
		SecretName: binding.Name,
		Namespace:  binding.Namespace,
		Keys:       binding.Keys,
		Ready:      true,
		Provider:   fmt.Sprintf("csi-%s", csiProvider),
		CSI: &secret.CSIInfo{
			ProviderClass: providerClassName,
			Driver:        csiDriver,
			MountPath:     mountPath,
			Files:         binding.Keys, // Each key becomes a file in the mount
		},
	}, nil
}

// applyCSINamingPattern applies variable substitution to the CSI naming pattern.
func (r *Resolver) applyCSINamingPattern(pattern string, binding secret.Binding) (string, error) {
	if pattern == "" {
		pattern = "${service}-${instance}-secrets"
	}

	result := pattern
	result = strings.ReplaceAll(result, "${namespace}", binding.Namespace)
	result = strings.ReplaceAll(result, "${service}", binding.Service)
	result = strings.ReplaceAll(result, "${instance}", binding.Instance)

	// Check if all variables were replaced
	if strings.Contains(result, "${") {
		return "", fmt.Errorf("unresolved variables in pattern: %s", result)
	}

	return result, nil
}

// resolveGenericSecret validates a generic secret exists.
func (r *Resolver) resolveGenericSecret(ctx context.Context, binding secret.Binding, cfg interface{}) (*secret.Ref, error) {
	k8sSecret := &corev1.Secret{}
	err := r.client.Get(ctx, types.NamespacedName{
		Name:      binding.Name,
		Namespace: binding.Namespace,
	}, k8sSecret)

	if err != nil {
		if apierrors.IsNotFound(err) {
			return &secret.Ref{
				SecretName: binding.Name,
				Namespace:  binding.Namespace,
				Ready:      false,
				Error:      fmt.Sprintf("secret %s not found", binding.Name),
			}, nil
		}
		return nil, fmt.Errorf("failed to get secret: %w", err)
	}

	// Validate required keys
	missingKeys := []string{}
	existingKeys := []string{}
	for _, key := range binding.Keys {
		if _, ok := k8sSecret.Data[key]; ok {
			existingKeys = append(existingKeys, key)
		} else {
			missingKeys = append(missingKeys, key)
		}
	}

	if len(missingKeys) > 0 {
		return &secret.Ref{
			SecretName: binding.Name,
			Namespace:  binding.Namespace,
			Keys:       existingKeys,
			Ready:      false,
			Error:      fmt.Sprintf("missing required keys: %v", missingKeys),
			Provider:   r.detectProvider(k8sSecret),
		}, nil
	}

	return &secret.Ref{
		SecretName: binding.Name,
		Namespace:  binding.Namespace,
		Keys:       existingKeys,
		Ready:      true,
		Provider:   r.detectProvider(k8sSecret),
	}, nil
}

// resolveVersionedSecret manages versioned Splunk secrets.
func (r *Resolver) resolveVersionedSecret(ctx context.Context, binding secret.Binding, cfg interface{}) (*secret.Ref, error) {
	sourceSecretName := fmt.Sprintf("splunk-%s-secret", binding.Namespace)

	// Check if source secret exists
	sourceSecret := &corev1.Secret{}
	err := r.client.Get(ctx, types.NamespacedName{
		Name:      sourceSecretName,
		Namespace: binding.Namespace,
	}, sourceSecret)

	if err != nil {
		if apierrors.IsNotFound(err) {
			return &secret.Ref{
				SecretName: binding.Name,
				Namespace:  binding.Namespace,
				Ready:      false,
				Error:      fmt.Sprintf("source secret %s not found", sourceSecretName),
			}, nil
		}
		return nil, fmt.Errorf("failed to get source secret: %w", err)
	}

	// Validate source secret has required keys
	for _, key := range binding.Keys {
		if _, ok := sourceSecret.Data[key]; !ok {
			return &secret.Ref{
				SecretName:       binding.Name,
				Namespace:        binding.Namespace,
				Ready:            false,
				Error:            fmt.Sprintf("source secret missing key: %s", key),
				SourceSecretName: sourceSecretName,
			}, nil
		}
	}

	// Find or create versioned secret
	latestVersion, err := r.findLatestVersion(ctx, binding.Name, binding.Namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to find latest version: %w", err)
	}

	if latestVersion == 0 {
		// Create v1
		return r.createVersionedSecret(ctx, binding, sourceSecret, 1)
	}

	// Check if source has changed
	versionedName := fmt.Sprintf("%s-v%d", binding.Name, latestVersion)
	versionedSecret := &corev1.Secret{}
	err = r.client.Get(ctx, types.NamespacedName{
		Name:      versionedName,
		Namespace: binding.Namespace,
	}, versionedSecret)

	if err != nil {
		if apierrors.IsNotFound(err) {
			// Versioned secret deleted, recreate
			return r.createVersionedSecret(ctx, binding, sourceSecret, latestVersion)
		}
		return nil, fmt.Errorf("failed to get versioned secret: %w", err)
	}

	// Check if content has changed
	if r.secretsEqual(sourceSecret, versionedSecret) {
		// No change, return existing version
		version := int(latestVersion)
		return &secret.Ref{
			SecretName:       versionedName,
			Namespace:        binding.Namespace,
			Keys:             binding.Keys,
			Ready:            true,
			Provider:         r.detectProvider(sourceSecret),
			Version:          &version,
			SourceSecretName: sourceSecretName,
		}, nil
	}

	// Content changed, create new version
	return r.createVersionedSecret(ctx, binding, sourceSecret, latestVersion+1)
}

// findLatestVersion finds the latest version number for a secret.
func (r *Resolver) findLatestVersion(ctx context.Context, baseName, namespace string) (int, error) {
	secretList := &corev1.SecretList{}
	err := r.client.List(ctx, secretList, client.InNamespace(namespace))
	if err != nil {
		return 0, err
	}

	maxVersion := 0
	prefix := baseName + "-v"
	for _, s := range secretList.Items {
		if strings.HasPrefix(s.Name, prefix) {
			versionStr := strings.TrimPrefix(s.Name, prefix)
			if version, err := strconv.Atoi(versionStr); err == nil {
				if version > maxVersion {
					maxVersion = version
				}
			}
		}
	}

	return maxVersion, nil
}

// createVersionedSecret creates a new versioned secret.
func (r *Resolver) createVersionedSecret(ctx context.Context, binding secret.Binding, sourceSecret *corev1.Secret, version int) (*secret.Ref, error) {
	versionedName := fmt.Sprintf("%s-v%d", binding.Name, version)

	r.logger.Info("Creating versioned secret",
		"name", versionedName,
		"namespace", binding.Namespace,
		"version", version,
	)

	// Copy data from source secret
	data := make(map[string][]byte)
	for k, v := range sourceSecret.Data {
		data[k] = v
	}

	versionedSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      versionedName,
			Namespace: binding.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by":       "platform-sdk",
				"platform.splunk.com/component":      "secret",
				"platform.splunk.com/secret-base":    binding.Name,
				"platform.splunk.com/secret-version": fmt.Sprintf("v%d", version),
			},
			Annotations: map[string]string{
				"platform.splunk.com/source-secret": sourceSecret.Name,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}

	err := r.client.Create(ctx, versionedSecret)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Secret already exists, return it
			v := int(version)
			return &secret.Ref{
				SecretName:       versionedName,
				Namespace:        binding.Namespace,
				Keys:             binding.Keys,
				Ready:            true,
				Provider:         r.detectProvider(sourceSecret),
				Version:          &v,
				SourceSecretName: sourceSecret.Name,
			}, nil
		}
		return nil, fmt.Errorf("failed to create versioned secret: %w", err)
	}

	// Clean up old versions
	go r.cleanupOldVersions(context.Background(), binding.Name, binding.Namespace, version)

	v := int(version)
	return &secret.Ref{
		SecretName:       versionedName,
		Namespace:        binding.Namespace,
		Keys:             binding.Keys,
		Ready:            true,
		Provider:         r.detectProvider(sourceSecret),
		Version:          &v,
		SourceSecretName: sourceSecret.Name,
	}, nil
}

// secretsEqual checks if two secrets have the same data.
func (r *Resolver) secretsEqual(a, b *corev1.Secret) bool {
	if len(a.Data) != len(b.Data) {
		return false
	}

	for k, v := range a.Data {
		if bv, ok := b.Data[k]; !ok || string(v) != string(bv) {
			return false
		}
	}

	return true
}

// cleanupOldVersions removes old secret versions beyond retention limit.
func (r *Resolver) cleanupOldVersions(ctx context.Context, baseName, namespace string, currentVersion int) {
	// Default: keep last 3 versions
	keepVersions := 3

	secretList := &corev1.SecretList{}
	err := r.client.List(ctx, secretList, client.InNamespace(namespace))
	if err != nil {
		r.logger.Error(err, "Failed to list secrets for cleanup")
		return
	}

	// Find all versions
	versions := []int{}
	prefix := baseName + "-v"
	for _, s := range secretList.Items {
		if strings.HasPrefix(s.Name, prefix) {
			versionStr := strings.TrimPrefix(s.Name, prefix)
			if version, err := strconv.Atoi(versionStr); err == nil {
				versions = append(versions, version)
			}
		}
	}

	// Sort versions descending
	sort.Sort(sort.Reverse(sort.IntSlice(versions)))

	// Delete old versions
	for i, version := range versions {
		if i >= keepVersions {
			secretName := fmt.Sprintf("%s-v%d", baseName, version)
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
				},
			}
			err := r.client.Delete(ctx, secret)
			if err != nil && !apierrors.IsNotFound(err) {
				r.logger.Error(err, "Failed to delete old secret version",
					"name", secretName,
					"version", version,
				)
			} else {
				r.logger.Info("Deleted old secret version",
					"name", secretName,
					"version", version,
				)
			}
		}
	}
}

// detectProvider detects which provider created the secret.
func (r *Resolver) detectProvider(k8sSecret *corev1.Secret) string {
	// Check annotations for ESO
	if _, ok := k8sSecret.Annotations["external-secrets.io/created-by"]; ok {
		return "external-secrets"
	}

	// Check labels
	if managedBy, ok := k8sSecret.Labels["app.kubernetes.io/managed-by"]; ok {
		if managedBy == "external-secrets" {
			return "external-secrets"
		}
		if managedBy == "platform-sdk" {
			return "platform-sdk"
		}
	}

	return "kubernetes"
}
