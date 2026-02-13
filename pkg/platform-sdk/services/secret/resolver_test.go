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

package secret

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/secret"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/services/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestResolveGenericSecret tests basic K8s secret resolution
func TestResolveGenericSecret(t *testing.T) {
	tests := []struct {
		name          string
		secretExists  bool
		secretData    map[string][]byte
		requestedKeys []string
		expectedReady bool
		expectedError string
		expectedKeys  []string
	}{
		{
			name:         "secret exists with all keys",
			secretExists: true,
			secretData: map[string][]byte{
				"username": []byte("admin"),
				"password": []byte("changeme"),
			},
			requestedKeys: []string{"username", "password"},
			expectedReady: true,
			expectedKeys:  []string{"username", "password"},
		},
		{
			name:         "secret missing some keys",
			secretExists: true,
			secretData: map[string][]byte{
				"username": []byte("admin"),
			},
			requestedKeys: []string{"username", "password"},
			expectedReady: false,
			expectedError: "missing required keys: [password]",
			expectedKeys:  []string{"username"},
		},
		{
			name:          "secret does not exist",
			secretExists:  false,
			requestedKeys: []string{"username", "password"},
			expectedReady: false,
			expectedError: "secret postgres-credentials not found",
		},
		{
			name:         "secret exists with extra keys",
			secretExists: true,
			secretData: map[string][]byte{
				"username": []byte("admin"),
				"password": []byte("changeme"),
				"host":     []byte("localhost"),
			},
			requestedKeys: []string{"username", "password"},
			expectedReady: true,
			expectedKeys:  []string{"username", "password"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)

			// Create fake client
			objs := []runtime.Object{}
			if tt.secretExists {
				k8sSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "postgres-credentials",
						Namespace: "default",
					},
					Data: tt.secretData,
				}
				objs = append(objs, k8sSecret)
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objs...).
				Build()

			// Create mock config resolver
			mockConfigResolver := &mockConfigResolver{
				secretConfig: &config.ResolvedSecretConfig{
					Provider:          "kubernetes",
					VersioningEnabled: true,
					VersionsToKeep:    3,
				},
			}

			// Create resolver
			resolver := NewResolver(
				fakeClient,
				mockConfigResolver,
				&api.RuntimeConfig{},
				logr.Discard(),
			)

			// Resolve secret
			binding := secret.Binding{
				Name:      "postgres-credentials",
				Namespace: "default",
				Type:      secret.SecretTypeGeneric,
				Keys:      tt.requestedKeys,
			}

			ref, err := resolver.Resolve(ctx, binding)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}

			// Verify results
			if ref.Ready != tt.expectedReady {
				t.Errorf("Ready = %v, want %v", ref.Ready, tt.expectedReady)
			}

			if tt.expectedError != "" && ref.Error != tt.expectedError {
				t.Errorf("Error = %v, want %v", ref.Error, tt.expectedError)
			}

			if tt.expectedReady {
				if len(ref.Keys) != len(tt.expectedKeys) {
					t.Errorf("Keys length = %v, want %v", len(ref.Keys), len(tt.expectedKeys))
				}
				for _, key := range tt.expectedKeys {
					if !ref.HasKey(key) {
						t.Errorf("Missing expected key: %s", key)
					}
				}
			}

			if ref.SecretName != "postgres-credentials" {
				t.Errorf("SecretName = %v, want postgres-credentials", ref.SecretName)
			}

			if ref.Namespace != "default" {
				t.Errorf("Namespace = %v, want default", ref.Namespace)
			}
		})
	}
}

// TestResolveVersionedSecret tests Splunk secret versioning
func TestResolveVersionedSecret(t *testing.T) {
	tests := []struct {
		name            string
		sourceExists    bool
		sourceData      map[string][]byte
		requestedKeys   []string
		expectedVersion int
		expectedReady   bool
		expectedError   string
	}{
		{
			name:         "create initial version",
			sourceExists: true,
			sourceData: map[string][]byte{
				"password":     []byte("admin123"),
				"hec_token":    []byte("abc-123"),
				"pass4SymmKey": []byte("symm-key"),
			},
			requestedKeys:   []string{"password", "hec_token"},
			expectedVersion: 1,
			expectedReady:   true,
		},
		{
			name:          "source secret not found",
			sourceExists:  false,
			requestedKeys: []string{"password"},
			expectedReady: false,
			expectedError: "source secret splunk-default-secret not found",
		},
		{
			name:         "source missing required key",
			sourceExists: true,
			sourceData: map[string][]byte{
				"password": []byte("admin123"),
			},
			requestedKeys: []string{"password", "hec_token"},
			expectedReady: false,
			expectedError: "source secret missing key: hec_token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)

			// Create fake client
			objs := []runtime.Object{}
			if tt.sourceExists {
				sourceSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "splunk-default-secret",
						Namespace: "default",
					},
					Data: tt.sourceData,
				}
				objs = append(objs, sourceSecret)
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objs...).
				Build()

			// Create mock config resolver
			mockConfigResolver := &mockConfigResolver{
				secretConfig: &config.ResolvedSecretConfig{
					Provider:          "kubernetes",
					VersioningEnabled: true,
					VersionsToKeep:    3,
				},
			}

			// Create resolver
			resolver := NewResolver(
				fakeClient,
				mockConfigResolver,
				&api.RuntimeConfig{},
				logr.Discard(),
			)

			// Resolve Splunk secret
			binding := secret.Binding{
				Name:      "splunk-standalone-secret",
				Namespace: "default",
				Type:      secret.SecretTypeSplunk,
				Keys:      tt.requestedKeys,
			}

			ref, err := resolver.Resolve(ctx, binding)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}

			// Verify results
			if ref.Ready != tt.expectedReady {
				t.Errorf("Ready = %v, want %v", ref.Ready, tt.expectedReady)
			}

			if tt.expectedError != "" && ref.Error != tt.expectedError {
				t.Errorf("Error = %v, want %v", ref.Error, tt.expectedError)
			}

			if tt.expectedReady {
				if ref.Version == nil || *ref.Version != tt.expectedVersion {
					t.Errorf("Version = %v, want %v", ref.Version, tt.expectedVersion)
				}

				expectedSecretName := "splunk-standalone-secret-v1"
				if ref.SecretName != expectedSecretName {
					t.Errorf("SecretName = %v, want %v", ref.SecretName, expectedSecretName)
				}

				if ref.SourceSecretName != "splunk-default-secret" {
					t.Errorf("SourceSecretName = %v, want splunk-default-secret", ref.SourceSecretName)
				}
			}
		})
	}
}

// mockConfigResolver implements interfaces.ConfigResolver for testing
type mockConfigResolver struct {
	secretConfig *config.ResolvedSecretConfig
}

func (m *mockConfigResolver) StartWatches(ctx context.Context) error {
	return nil
}

func (m *mockConfigResolver) ResolveConfig(ctx context.Context, key string, namespace string) (interface{}, error) {
	return nil, nil
}

func (m *mockConfigResolver) ResolveSecretConfig(ctx context.Context, namespace string) (*config.ResolvedSecretConfig, error) {
	return m.secretConfig, nil
}

func (m *mockConfigResolver) ResolveCertificateConfig(ctx context.Context, namespace string) (*config.ResolvedCertConfig, error) {
	return nil, nil
}

func (m *mockConfigResolver) ResolveObservabilityConfig(ctx context.Context, namespace string) (*config.ResolvedObservabilityConfig, error) {
	return nil, nil
}
