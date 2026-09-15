// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package reconcile

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlreconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	configworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
)

func TestResolveNoahDependency(t *testing.T) {
	t.Run("missing dependency is retryable", func(t *testing.T) {
		client := spltest.NewMockClient()
		cr := noahDependencyTestIndexer("missing", 3)

		result := ResolveNoahDependency(t.Context(), client, cr, &cr.Status.Conditions, cr.Spec.NoahClusterRef)

		assert.Nil(t, result.Runtime)
		assert.Equal(t, enterpriseApi.PhasePending, result.Phase)
		assert.True(t, result.StateKnown)
		assert.NoError(t, result.ReconcileErr)
		assertNoahDependencyCondition(t, cr, metav1.ConditionUnknown, enterpriseApi.ReasonNoahDependencyMissing, 3)
	})

	t.Run("invalid dependency is terminal", func(t *testing.T) {
		client := noahDependencyTestClient(t, map[string][]byte{"wrong-key": []byte(t.Name())})
		cr := noahDependencyTestIndexer("noah", 4)

		result := ResolveNoahDependency(t.Context(), client, cr, &cr.Status.Conditions, cr.Spec.NoahClusterRef)

		assert.Nil(t, result.Runtime)
		assert.Equal(t, enterpriseApi.PhaseError, result.Phase)
		assert.True(t, result.StateKnown)
		assert.Error(t, result.ReconcileErr)
		assert.ErrorIs(t, result.ReconcileErr, ctrlreconcile.TerminalError(nil))
		assertNoahDependencyCondition(t, cr, metav1.ConditionFalse, enterpriseApi.ReasonNoahConfigurationInvalid, 4)
	})

	t.Run("resolved dependency is ready", func(t *testing.T) {
		client := noahDependencyTestClient(t, nil)
		cr := noahDependencyTestIndexer("noah", 5)

		result := ResolveNoahDependency(t.Context(), client, cr, &cr.Status.Conditions, cr.Spec.NoahClusterRef)

		require.NotNil(t, result.Runtime)
		assert.True(t, result.StateKnown)
		assert.NoError(t, result.ReconcileErr)
		assertNoahDependencyCondition(t, cr, metav1.ConditionTrue, enterpriseApi.ReasonNoahDependencyResolved, 5)
	})

	for _, test := range []struct {
		name string
		ref  *corev1.LocalObjectReference
	}{
		{name: "nil reference"},
		{name: "empty reference", ref: &corev1.LocalObjectReference{}},
	} {
		t.Run(test.name+" is invalid", func(t *testing.T) {
			cr := noahDependencyTestIndexer("", 6)

			result := ResolveNoahDependency(t.Context(), spltest.NewMockClient(), cr, &cr.Status.Conditions, test.ref)

			assert.Nil(t, result.Runtime)
			assert.Equal(t, enterpriseApi.PhaseError, result.Phase)
			assert.True(t, result.StateKnown)
			assert.ErrorIs(t, result.ReconcileErr, ctrlreconcile.TerminalError(nil))
			assertNoahDependencyCondition(t, cr, metav1.ConditionFalse, enterpriseApi.ReasonNoahConfigurationInvalid, 6)
		})
	}
}

func TestResolveNoahDependencyClearsStaleSuccessOnUnclassifiedError(t *testing.T) {
	client := noahForbiddenSecretReader{MockClient: noahDependencyTestClient(t, nil)}
	cr := noahDependencyTestIndexer("noah", 6)
	cr.Status.Conditions = splcommon.UpsertCondition(cr.Status.Conditions, metav1.Condition{
		Type:    string(enterpriseApi.ConditionNoahDependencyResolved),
		Status:  metav1.ConditionTrue,
		Reason:  string(enterpriseApi.ReasonNoahDependencyResolved),
		Message: "resolved",
	})

	result := ResolveNoahDependency(t.Context(), client, cr, &cr.Status.Conditions, cr.Spec.NoahClusterRef)

	assert.Nil(t, result.Runtime)
	assert.False(t, result.StateKnown)
	assert.Error(t, result.ReconcileErr)
	condition := assertNoahDependencyCondition(t, cr, metav1.ConditionUnknown, enterpriseApi.ReasonNoahDependencyUnknown, 6)
	assert.NotContains(t, condition.Message, "resolved")
}

func TestResolveNoahDependencyRedactsCredential(t *testing.T) {
	credential := t.Name()
	client := noahDependencyTestClient(t, map[string][]byte{
		configworkflow.NoahAuthSecretKey: []byte(credential + "\nsecond-line"),
	})
	cr := noahDependencyTestIndexer("noah", 7)

	result := ResolveNoahDependency(t.Context(), client, cr, &cr.Status.Conditions, cr.Spec.NoahClusterRef)

	assert.Error(t, result.ReconcileErr)
	condition := assertNoahDependencyCondition(t, cr, metav1.ConditionFalse, enterpriseApi.ReasonNoahConfigurationInvalid, 7)
	assert.NotContains(t, condition.Message, credential)
	assert.Contains(t, condition.Message, configworkflow.NoahAuthSecretKey)
}

func noahDependencyTestIndexer(noahName string, generation int64) *enterpriseApi.IndexerCluster {
	return &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "idx", Namespace: "test", Generation: generation},
		Spec: enterpriseApi.IndexerClusterSpec{
			NoahClusterRef: &corev1.LocalObjectReference{Name: noahName},
		},
	}
}

func noahDependencyTestClient(t *testing.T, secretData map[string][]byte) *spltest.MockClient {
	t.Helper()
	if secretData == nil {
		secretData = map[string][]byte{configworkflow.NoahAuthSecretKey: []byte(t.Name())}
	}
	client := spltest.NewMockClient()
	client.AddObject(&enterpriseApi.NoahCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: "test"},
		Spec: enterpriseApi.NoahClusterSpec{
			Endpoint:      "http://noah.example:8080",
			Tenant:        "linus-dev",
			AuthSecretRef: corev1.LocalObjectReference{Name: "noah-auth"},
		},
	})
	client.AddObject(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noah-auth", Namespace: "test"},
		Data:       secretData,
	})
	return client
}

func assertNoahDependencyCondition(t *testing.T, cr *enterpriseApi.IndexerCluster, status metav1.ConditionStatus, reason enterpriseApi.ConditionReason, generation int64) *metav1.Condition {
	t.Helper()
	condition := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionNoahDependencyResolved)
	require.NotNil(t, condition)
	assert.Equal(t, status, condition.Status)
	assert.Equal(t, string(reason), condition.Reason)
	assert.Equal(t, generation, condition.ObservedGeneration)
	return condition
}

type noahForbiddenSecretReader struct {
	*spltest.MockClient
}

func (reader noahForbiddenSecretReader) Get(ctx context.Context, key k8sclient.ObjectKey, obj k8sclient.Object, opts ...k8sclient.GetOption) error {
	if _, ok := obj.(*corev1.Secret); ok {
		return k8serrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, key.Name, assert.AnError)
	}
	return reader.MockClient.Get(ctx, key, obj, opts...)
}
