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

package noah

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

func TestResolveConnection(t *testing.T) {
	client := spltest.NewMockClient()
	cluster := validNoahCluster()
	cacheWarmEnabled := false
	cluster.Spec.CacheWarmScaleOutEnabled = &cacheWarmEnabled
	secret := validNoahSecret()
	require.NoError(t, client.Create(t.Context(), cluster))
	require.NoError(t, client.Create(t.Context(), secret))

	connection, err := ResolveConnection(t.Context(), client, "test", corev1.LocalObjectReference{Name: "noah"})

	require.NoError(t, err)
	assert.Equal(t, cluster.Spec, connection.Spec())
	assert.Equal(t, secret.Name, connection.AuthSecretName())
	assert.Equal(t, secret.Data[AuthSecretKey], connection.Credential())
	assert.Equal(t, secret.ResourceVersion, connection.AuthSecretResourceVersion())
	returnedSpec := connection.Spec()
	*returnedSpec.CacheWarmScaleOutEnabled = true
	assert.False(t, *connection.Spec().CacheWarmScaleOutEnabled)
	returnedCredential := connection.Credential()
	returnedCredential[0] = 'X'
	assert.Equal(t, secret.Data[AuthSecretKey], connection.Credential())
	assert.Nil(t, connection.client, "dependency resolution must not derive the HMAC key")
	firstClient, err := connection.Client()
	require.NoError(t, err)
	secondClient, err := connection.Client()
	require.NoError(t, err)
	assert.Same(t, firstClient, secondClient)
}

func TestAuthSecretResourceVersionTracksSecretUpdates(t *testing.T) {
	client := spltest.NewMockClient()
	cluster := validNoahCluster()
	secret := validNoahSecret()
	secret.ResourceVersion = "1"
	require.NoError(t, client.Create(t.Context(), cluster))
	require.NoError(t, client.Create(t.Context(), secret))

	first, err := ResolveConnection(t.Context(), client, "test", corev1.LocalObjectReference{Name: "noah"})
	require.NoError(t, err)
	secret.ResourceVersion = "2"
	secret.Annotations = map[string]string{"refresh": "metadata-only"}
	require.NoError(t, client.Update(t.Context(), secret))
	second, err := ResolveConnection(t.Context(), client, "test", corev1.LocalObjectReference{Name: "noah"})
	require.NoError(t, err)
	assert.Equal(t, "1", first.AuthSecretResourceVersion())
	assert.Equal(t, "2", second.AuthSecretResourceVersion())
}

func TestResolveConnectionClassifiesMissingDependencies(t *testing.T) {
	t.Run("NoahCluster", func(t *testing.T) {
		connection, err := ResolveConnection(t.Context(), spltest.NewMockClient(), "test", corev1.LocalObjectReference{Name: "missing"})
		assert.Nil(t, connection)
		assert.True(t, k8serrors.IsNotFound(err))
		assertDependencyError(t, err, DependencyMissing)
	})

	t.Run("Secret", func(t *testing.T) {
		client := spltest.NewMockClient()
		require.NoError(t, client.Create(t.Context(), validNoahCluster()))
		connection, err := ResolveConnection(t.Context(), client, "test", corev1.LocalObjectReference{Name: "noah"})
		assert.Nil(t, connection)
		assert.True(t, k8serrors.IsNotFound(err))
		assertDependencyError(t, err, DependencyMissing)
	})
}

func TestResolveConnectionValidatesCoordinatesBeforeResolvingSecret(t *testing.T) {
	client := spltest.NewMockClient()
	cluster := validNoahCluster()
	cluster.Spec.Endpoint = "https://noah.test.svc/api"
	cluster.Spec.AuthSecretRef.Name = "missing-auth"
	require.NoError(t, client.Create(t.Context(), cluster))

	connection, err := ResolveConnection(t.Context(), client, "test", corev1.LocalObjectReference{Name: "noah"})

	assert.Nil(t, connection)
	assertDependencyError(t, err, DependencyInvalid)
	assert.NotContains(t, err.Error(), "missing-auth")
}

func TestResolveConnectionPreservesKubernetesReadFailures(t *testing.T) {
	client := spltest.NewMockClient()
	readErr := errors.New("API unavailable")
	client.InduceErrorKind[splcommon.MockClientInduceErrorGet] = readErr

	connection, err := ResolveConnection(t.Context(), client, "test", corev1.LocalObjectReference{Name: "noah"})

	assert.Nil(t, connection)
	assert.ErrorIs(t, err, readErr)
	var dependencyErr *DependencyError
	assert.False(t, errors.As(err, &dependencyErr))
}

func TestResolveConnectionRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name       string
		endpoint   string
		tenant     string
		secretData map[string][]byte
	}{
		{name: "endpoint path", endpoint: "https://noah.test.svc/api", tenant: "tenant", secretData: map[string][]byte{AuthSecretKey: []byte("unit-test-noah-key")}},
		{name: "endpoint query", endpoint: "https://noah.test.svc?x=y", tenant: "tenant", secretData: map[string][]byte{AuthSecretKey: []byte("unit-test-noah-key")}},
		{name: "endpoint credentials", endpoint: "https://user:password@noah.test.svc", tenant: "tenant", secretData: map[string][]byte{AuthSecretKey: []byte("unit-test-noah-key")}},
		{name: "unsupported scheme", endpoint: "ftp://noah.test.svc", tenant: "tenant", secretData: map[string][]byte{AuthSecretKey: []byte("unit-test-noah-key")}},
		{name: "missing host", endpoint: "https://", tenant: "tenant", secretData: map[string][]byte{AuthSecretKey: []byte("unit-test-noah-key")}},
		{name: "blank tenant", endpoint: "https://noah.test.svc", secretData: map[string][]byte{AuthSecretKey: []byte("unit-test-noah-key")}},
		{name: "padded tenant", endpoint: "https://noah.test.svc", tenant: " tenant ", secretData: map[string][]byte{AuthSecretKey: []byte("unit-test-noah-key")}},
		{name: "missing secret key", endpoint: "https://noah.test.svc", tenant: "tenant", secretData: map[string][]byte{}},
		{name: "short secret", endpoint: "https://noah.test.svc", tenant: "tenant", secretData: map[string][]byte{AuthSecretKey: []byte("short")}},
		{name: "multiline secret", endpoint: "https://noah.test.svc", tenant: "tenant", secretData: map[string][]byte{AuthSecretKey: []byte("unit-test-key\nsecond-line")}},
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

			connection, err := ResolveConnection(t.Context(), client, "test", corev1.LocalObjectReference{Name: "noah"})

			assert.Nil(t, connection)
			assertDependencyError(t, err, DependencyInvalid)
			assert.NotContains(t, err.Error(), "unit-test-noah-key")
		})
	}
}

func assertDependencyError(t *testing.T, err error, kind DependencyErrorKind) {
	t.Helper()
	var dependencyErr *DependencyError
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
		Data:       map[string][]byte{AuthSecretKey: []byte("unit-test-noah-key")},
	}
}
