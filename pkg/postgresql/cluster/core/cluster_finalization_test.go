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

package core

import (
	"context"
	"errors"
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type clusterFinalizerFunc func(context.Context, *platformv1alpha1.PostgresCluster) (bool, error)

func (f clusterFinalizerFunc) AdvanceFinalization(ctx context.Context, parent *platformv1alpha1.PostgresCluster) (bool, error) {
	return f(ctx, parent)
}

func TestClusterFinalizationPrecedesClassValidationAndBindsCompletionVersion(t *testing.T) {
	for _, changes := range []bool{false, true} {
		t.Run(map[bool]string{false: "verified completion", true: "concurrent history write"}[changes], func(t *testing.T) {
			parent := newTestCluster("orders", "default")
			parent.Spec.Class = "missing-class"
			parent.Finalizers = []string{PostgresClusterFinalizerName}
			parent.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt"}}}
			c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithStatusSubresource(parent).WithObjects(parent).Build()
			require.NoError(t, c.Delete(t.Context(), parent))
			called := false
			rc := &ReconcileContext{Client: c, APIReader: c, Recorder: record.NewFakeRecorder(5), Finalizer: clusterFinalizerFunc(func(ctx context.Context, parent *platformv1alpha1.PostgresCluster) (bool, error) {
				called = true
				if changes {
					parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.AttemptID = "new-history"
					require.NoError(t, c.Status().Update(ctx, parent))
				}
				return true, nil
			})}
			// A port must not mutate its caller's proof version when another writer acts.
			if changes {
				rc.Finalizer = clusterFinalizerFunc(func(ctx context.Context, parent *platformv1alpha1.PostgresCluster) (bool, error) {
					called = true
					copy := parent.DeepCopy()
					copy.Status.PostgresMajorUpgradeStatus[0].BlueGreen.AttemptID = "new-history"
					require.NoError(t, c.Status().Update(ctx, copy))
					return true, nil
				})
			}
			result, err := PostgresClusterService(t.Context(), rc, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(parent)}, nil, nil, nil, nil)
			require.NoError(t, err)
			assert.True(t, called)
			err = c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent)
			if changes {
				require.NoError(t, err)
				assert.Contains(t, parent.Finalizers, PostgresClusterFinalizerName)
				assert.Positive(t, result.RequeueAfter)
			} else {
				assert.True(t, apierrors.IsNotFound(err))
			}
		})
	}
}

func TestClusterFinalizationPreservesPendingAndBlockedOwnership(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "blocked"}[blocked], func(t *testing.T) {
			parent := newTestCluster("orders", "default")
			parent.Finalizers = []string{PostgresClusterFinalizerName}
			parent.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt"}}}
			c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithStatusSubresource(parent).WithObjects(parent).Build()
			require.NoError(t, c.Delete(t.Context(), parent))
			rc := &ReconcileContext{Client: c, APIReader: c, Recorder: record.NewFakeRecorder(5), Finalizer: clusterFinalizerFunc(func(context.Context, *platformv1alpha1.PostgresCluster) (bool, error) {
				if blocked {
					return false, errors.New("missing SQL proof")
				}
				return false, nil
			})}
			result, err := PostgresClusterService(t.Context(), rc, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(parent)}, nil, nil, nil, nil)
			if blocked {
				require.ErrorContains(t, err, "missing SQL proof")
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
			assert.Contains(t, parent.Finalizers, PostgresClusterFinalizerName)
			assert.Positive(t, result.RequeueAfter)
			condition := meta.FindStatusCondition(parent.Status.Conditions, "Finalization")
			require.NotNil(t, condition)
			assert.Equal(t, map[bool]string{false: "FinalizationPending", true: "FinalizationBlocked"}[blocked], condition.Reason)
		})
	}
}
