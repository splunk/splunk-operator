/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationtypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	usecases "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestCleanupRunsBeforeOrdinaryFailureAndPreservesItsProgress(t *testing.T) {
	for _, fails := range []bool{false, true} {
		t.Run(map[bool]string{true: "cleanup error", false: "cleanup progress"}[fails], func(t *testing.T) {
			parent := newTestCluster("orders", "default")
			parent.Finalizers = []string{PostgresClusterFinalizerName}
			parent.Status.Phase = new("Ready")
			parent.Spec.Class = "missing-class"
			parent.Spec.PostgresMajorUpgradeConfig = &platformv1alpha1.PostgresMajorUpgradeConfig{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeConfig{Cleanup: new(true)}}
			parent.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{Phase: new("Cancelled"), Strategy: new(mvutypes.MajorUpgradeFlowBlueGreen), BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt"}}}
			acts, ordinaryReads := 0, 0
			c := fake.NewClientBuilder().WithScheme(newTestScheme()).WithStatusSubresource(parent).WithObjects(parent).WithInterceptorFuncs(interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
				if _, class := object.(*platformv1alpha1.PostgresClusterClass); class {
					ordinaryReads++
					assert.Equal(t, 1, acts, "cleanup must precede class resolution")
				}
				return c.Get(ctx, key, object, options...)
			}}).Build()
			uc := &recoveryPriorityUseCase{active: true, act: func(ctx context.Context) (reconciliationtypes.Report, error) {
				acts++
				fresh := &platformv1alpha1.PostgresCluster{}
				require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(parent), fresh))
				fresh.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Cleanup = &platformv1alpha1.BlueGreenCleanupStatus{State: platformv1alpha1.BlueGreenCleanupStateCleaning, Failure: new("durable retirement progress")}
				require.NoError(t, c.Status().Update(ctx, fresh))
				if fails {
					return reconciliationtypes.Report{Retry: true}, errors.New("cleanup retry")
				}
				return reconciliationtypes.Report{Retry: true}, nil
			}}
			rc := &ReconcileContext{Client: c, APIReader: c, Scheme: c.Scheme(), Recorder: record.NewFakeRecorder(20), UseCaseRegistryProvider: func(types.NamespacedName, *platformv1alpha1.PostgresCluster, *MergedConfig) map[string]usecases.Factory {
				return map[string]usecases.Factory{mvutypes.UseCaseName: func() (usecases.UseCase, error) { return uc, nil }}
			}}
			_, err := PostgresClusterService(t.Context(), rc, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(parent)}, nil, nil, nil, nil)
			require.Error(t, err, "ordinary configuration is unavailable")
			if fails {
				assert.ErrorContains(t, err, "cleanup retry")
			}
			assert.Equal(t, 1, acts)
			assert.Positive(t, ordinaryReads, "ordinary maintenance must still run")
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(parent), parent))
			assert.Equal(t, "durable retirement progress", *parent.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Cleanup.Failure)
		})
	}
}
