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

package config

import (
	"errors"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestResolveNoahRuntime(t *testing.T) {
	client := spltest.NewMockClient()
	cluster := validNoahCluster()
	cacheWarmEnabled := false
	cluster.Spec.CacheWarmScaleOutEnabled = &cacheWarmEnabled
	secret := validNoahSecret()
	require.NoError(t, client.Create(t.Context(), cluster))
	require.NoError(t, client.Create(t.Context(), secret))

	resolved, err := ResolveNoahRuntime(t.Context(), client, "test", corev1.LocalObjectReference{Name: "noah"})

	require.NoError(t, err)
	assert.Equal(t, cluster.Spec, resolved.Spec())
	assert.Equal(t, secret.Data[NoahAuthSecretKey], resolved.Credential())
	returnedSpec := resolved.Spec()
	*returnedSpec.CacheWarmScaleOutEnabled = true
	assert.False(t, *resolved.Spec().CacheWarmScaleOutEnabled)
	returnedCredential := resolved.Credential()
	returnedCredential[0] = 'X'
	assert.Equal(t, secret.Data[NoahAuthSecretKey], resolved.Credential())
	assert.Nil(t, resolved.client, "dependency resolution must not derive the HMAC key")
	firstClient, err := resolved.Client()
	require.NoError(t, err)
	secondClient, err := resolved.Client()
	require.NoError(t, err)
	assert.Same(t, firstClient, secondClient)
}

func TestNoahCredentialTracksSecretUpdates(t *testing.T) {
	client := spltest.NewMockClient()
	cluster := validNoahCluster()
	secret := validNoahSecret()
	secret.ResourceVersion = "1"
	require.NoError(t, client.Create(t.Context(), cluster))
	require.NoError(t, client.Create(t.Context(), secret))

	first, err := ResolveNoahRuntime(t.Context(), client, "test", corev1.LocalObjectReference{Name: "noah"})
	require.NoError(t, err)
	firstCredential := append([]byte(nil), secret.Data[NoahAuthSecretKey]...)
	secret.ResourceVersion = "2"
	secret.Annotations = map[string]string{"refresh": "metadata-only"}
	require.NoError(t, client.Update(t.Context(), secret))
	metadataUpdate, err := ResolveNoahRuntime(t.Context(), client, "test", corev1.LocalObjectReference{Name: "noah"})
	require.NoError(t, err)
	assert.Equal(t, first.Credential(), metadataUpdate.Credential())

	secret.ResourceVersion = "3"
	rotatedCredential := append([]byte(nil), firstCredential...)
	rotatedCredential[0] = 'X'
	secret.Data[NoahAuthSecretKey] = rotatedCredential
	require.NoError(t, client.Update(t.Context(), secret))
	second, err := ResolveNoahRuntime(t.Context(), client, "test", corev1.LocalObjectReference{Name: "noah"})
	require.NoError(t, err)
	assert.Equal(t, firstCredential, first.Credential())
	assert.Equal(t, secret.Data[NoahAuthSecretKey], second.Credential())
}

func TestResolveNoahClassifiesMissingDependencies(t *testing.T) {
	t.Run("NoahCluster", func(t *testing.T) {
		resolved, err := ResolveNoahRuntime(t.Context(), spltest.NewMockClient(), "test", corev1.LocalObjectReference{Name: "missing"})
		assert.Nil(t, resolved)
		assert.True(t, k8serrors.IsNotFound(err))
		assertNoahDependencyError(t, err, NoahDependencyMissing)
	})

	t.Run("Secret", func(t *testing.T) {
		client := spltest.NewMockClient()
		require.NoError(t, client.Create(t.Context(), validNoahCluster()))
		resolved, err := ResolveNoahRuntime(t.Context(), client, "test", corev1.LocalObjectReference{Name: "noah"})
		assert.Nil(t, resolved)
		assert.True(t, k8serrors.IsNotFound(err))
		assertNoahDependencyError(t, err, NoahDependencyMissing)
	})
}

func TestResolveNoahPreservesKubernetesReadFailures(t *testing.T) {
	client := spltest.NewMockClient()
	readErr := errors.New("API unavailable")
	client.InduceErrorKind[splcommon.MockClientInduceErrorGet] = readErr

	resolved, err := ResolveNoahRuntime(t.Context(), client, "test", corev1.LocalObjectReference{Name: "noah"})

	assert.Nil(t, resolved)
	assert.ErrorIs(t, err, readErr)
	var dependencyErr *NoahDependencyError
	assert.False(t, errors.As(err, &dependencyErr))
}

func TestResolveNoahRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name       string
		endpoint   string
		tenant     string
		secretData map[string][]byte
	}{
		{name: "missing secret key", endpoint: "https://noah.test.svc", tenant: "tenant", secretData: map[string][]byte{}},
		{name: "wrong case secret key", endpoint: "https://noah.test.svc", tenant: "tenant", secretData: map[string][]byte{"Pass4SymmKey": []byte("unit-test-noah-key")}},
		{name: "empty secret", endpoint: "https://noah.test.svc", tenant: "tenant", secretData: map[string][]byte{NoahAuthSecretKey: []byte("")}},
		{name: "short secret", endpoint: "https://noah.test.svc", tenant: "tenant", secretData: map[string][]byte{NoahAuthSecretKey: []byte("short")}},
		{name: "multiline secret", endpoint: "https://noah.test.svc", tenant: "tenant", secretData: map[string][]byte{NoahAuthSecretKey: []byte("unit-test-key\nsecond-line")}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := spltest.NewMockClient()
			cluster := validNoahCluster()
			cluster.Spec.Endpoint = test.endpoint
			cluster.Spec.Tenant = test.tenant
			secret := validNoahSecret()
			secret.Data = test.secretData
			require.NoError(t, client.Create(t.Context(), cluster))
			require.NoError(t, client.Create(t.Context(), secret))

			resolved, err := ResolveNoahRuntime(t.Context(), client, "test", corev1.LocalObjectReference{Name: "noah"})

			assert.Nil(t, resolved)
			assertNoahDependencyError(t, err, NoahDependencyInvalid)
			assert.NotContains(t, err.Error(), "unit-test-noah-key")
		})
	}
}

func assertNoahDependencyError(t *testing.T, err error, kind NoahDependencyErrorKind) {
	t.Helper()
	var dependencyErr *NoahDependencyError
	require.ErrorAs(t, err, &dependencyErr)
	assert.Equal(t, kind, dependencyErr.Kind())
}

func validNoahCluster() *enterpriseApi.NoahCluster {
	return &enterpriseApi.NoahCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: "test"},
		Spec: enterpriseApi.NoahClusterSpec{
			Endpoint:      "https://noah.test.svc:8080",
			Tenant:        "tenant",
			AuthSecretRef: corev1.LocalObjectReference{Name: "noah-auth"},
		},
	}
}

func validNoahSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: "test", ResourceVersion: "1"},
		Data:       map[string][]byte{NoahAuthSecretKey: []byte("unit-test-noah-key")},
	}
}
