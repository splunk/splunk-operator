/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package k8s

import (
	"context"
	"errors"
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestClusterReaderRead(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	ready := "Ready"
	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "primary", Namespace: "dbs"},
		Status: platformv1alpha1.PostgresClusterStatus{
			Phase:          &ready,
			ProvisionerRef: &corev1.ObjectReference{Name: "primary-cnpg", Namespace: "dbs"},
		},
	}

	snapshot, err := NewClusterReader(fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()).Read(t.Context(), "dbs", "primary")
	require.NoError(t, err)
	assert.Equal(t, "primary", snapshot.Name)
	require.NotNil(t, snapshot.Phase)
	assert.Equal(t, "Ready", *snapshot.Phase)
	require.NotNil(t, snapshot.ProvisionerRef)
	assert.Equal(t, "primary-cnpg", snapshot.ProvisionerRef.Name)
}

func TestClusterReaderPreservesAPIErrors(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))

	t.Run("not found", func(t *testing.T) {
		_, err := NewClusterReader(fake.NewClientBuilder().WithScheme(scheme).Build()).Read(t.Context(), "dbs", "missing")
		assert.True(t, apierrors.IsNotFound(err))
	})

	t.Run("transient", func(t *testing.T) {
		transient := errors.New("apiserver unavailable")
		reader := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
				return transient
			},
		}).Build()
		_, err := NewClusterReader(reader).Read(t.Context(), "dbs", "primary")
		assert.ErrorIs(t, err, transient)
	})
}
