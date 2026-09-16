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

package adapter

import (
	"context"
	"errors"
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestConnectionMetadataPublisherAppliesOwnerAndPayload(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	target := dbtypes.ConnectionMetadataTarget{Name: "tenant", Namespace: "dbs", UID: "database-uid"}
	publication := dbtypes.ConnectionMetadataPublication{
		Name: "tenant-payments-config", Data: map[string]string{"DATABASE_NAME": "payments"},
	}

	err := NewConnectionMetadataPublisher(c, scheme).Apply(t.Context(), target, publication)
	require.NoError(t, err)

	actual := &corev1.ConfigMap{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKey{Name: publication.Name, Namespace: target.Namespace}, actual))
	assert.Equal(t, publication.Data, actual.Data)
	controller := metav1.GetControllerOf(actual)
	require.NotNil(t, controller)
	assert.Equal(t, target.Name, controller.Name)
	assert.Equal(t, target.UID, string(controller.UID))
}

func TestConnectionMetadataPublisherClassifiesConflictAndPreservesIdentity(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	conflict := apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, "tenant-payments-config", errors.New("write conflict"))
	c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(context.Context, client.WithWatch, client.Object, ...client.CreateOption) error {
			return conflict
		},
	}).Build()

	err := NewConnectionMetadataPublisher(c, scheme).Apply(
		t.Context(),
		dbtypes.ConnectionMetadataTarget{Name: "tenant", Namespace: "dbs", UID: "database-uid"},
		dbtypes.ConnectionMetadataPublication{Name: "tenant-payments-config", Data: map[string]string{"DATABASE_NAME": "payments"}},
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, dbtypes.ErrConnectionMetadataConflict)
	assert.ErrorIs(t, err, conflict)
	assert.True(t, apierrors.IsConflict(err))
}
