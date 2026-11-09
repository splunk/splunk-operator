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

package majorupgradeadapter

import (
	"context"
	"errors"
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCommittedRecoveryHandlesMissingBlueWithoutAdoptingSurvivors(t *testing.T) {
	for _, state := range []string{"absent", "owned pod", "retagged owned pod", "unready pod", "terminating pod", "orphaned labelled pod", "replacement cluster", "source read unavailable", "inventory unavailable", "missing fence proof"} {
		t.Run(state, func(t *testing.T) {
			f := newCutoverFixture(t)
			f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool { return bg.CommitReceipt != nil })
			blue := new(cnpgv1.Cluster)
			require.NoError(t, f.runtime.reader.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "blue"}, blue))
			require.NoError(t, f.runtime.client.Delete(t.Context(), blue))
			pod := &corev1.Pod{Name: "blue-1", Namespace: "default", UID: "source-pod", Labels: map[string]string{"cnpg.io/cluster": blue.Name}, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(blue, cnpgv1.SchemeGroupVersion.WithKind("Cluster"))}}
			switch state {
			case "retagged owned pod":
				pod.Labels = nil
			case "unready pod":
				pod.Status.Phase = corev1.PodRunning
			case "terminating pod":
				pod.DeletionTimestamp, pod.Finalizers = new(metav1.Now()), []string{"test-finalizer"}
			case "orphaned labelled pod":
				pod.OwnerReferences = nil
			case "replacement cluster":
				blue.UID, blue.ResourceVersion = "replacement-uid", ""
				require.NoError(t, f.runtime.client.Create(t.Context(), blue))
			case "inventory unavailable":
				f.runtime.reader = &unavailableRetiredInventory{Reader: f.runtime.reader}
			case "source read unavailable":
				f.runtime.reader = &cutoverFailOnceReader{Reader: f.runtime.reader, name: blue.Name}
			case "missing fence proof":
				f.mutate(func(_ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
					bg.Switchover.DrainCompletedAt = nil
				})
			}
			if state == "owned pod" || state == "retagged owned pod" || state == "unready pod" || state == "terminating pod" || state == "orphaned labelled pod" {
				require.NoError(t, f.runtime.client.Create(t.Context(), pod))
			}
			f.calls = nil
			if state == "absent" {
				f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
					return bg.Switchover.TargetAvailableAt != nil
				})
				assert.Contains(t, f.calls, "open-green")
				assert.NotContains(t, f.calls, "fence-blue", "no provider object may be fabricated for an absent source")
			} else {
				_, err := f.step()
				require.Error(t, err)
				assert.Nil(t, f.status().Switchover.TargetAvailableAt)
				assert.NotContains(t, f.calls, "open-green")
				assert.NotContains(t, f.calls, "publish-endpoints")
				if state == "owned pod" {
					require.NoError(t, f.runtime.client.Delete(t.Context(), pod))
					f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
						return bg.Switchover.TargetAvailableAt != nil
					})
					assert.Equal(t, 1, f.endpoints.commits, "recovery after instance removal must reuse the committed attempt")
				}
			}
			assert.NotContains(t, f.calls, "restore-blue")
			assert.NotContains(t, f.calls, "restore-canonical-blue")
		})
	}
}

type unavailableRetiredInventory struct{ client.Reader }

func (r *unavailableRetiredInventory) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if _, ok := list.(*corev1.PodList); ok {
		return errors.New("pod inventory unavailable")
	}
	return r.Reader.List(ctx, list, opts...)
}

type blockedRecoveryAccess struct {
	cutoverAccess
	inspect func(context.Context)
}

func (a blockedRecoveryAccess) FenceBlue(ctx context.Context, _ *cnpgv1.Cluster, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	a.inspect(ctx)
	return false, ctx.Err()
}

func TestCommittedRecoveryBoundsEachAttemptAndRetriesForward(t *testing.T) {
	f := newCutoverFixture(t)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool { return bg.CommitReceipt != nil })
	f.clock = f.status().Switchover.DeadlineAt.Add(time.Minute)
	_, err := f.step() // Record the exceeded availability target.
	require.NoError(t, err)
	f.calls = nil
	f.runtime.access = blockedRecoveryAccess{cutoverAccess: f.access, inspect: func(ctx context.Context) {
		deadline, bounded := ctx.Deadline()
		require.True(t, bounded)
		assert.WithinDuration(t, time.Now().Add(cutoverRecoveryTimeout), deadline, time.Second)
		assert.NoError(t, ctx.Err(), "the expired interruption deadline must not cancel a fresh recovery attempt")
	}}
	_, err = f.step()
	require.NoError(t, err)
	for _, cancellation := range []string{"caller cancellation", "earlier caller deadline"} {
		t.Run(cancellation, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if cancellation == "earlier caller deadline" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 25*time.Millisecond)
				defer stop()
			}
			f.runtime.access = blockedRecoveryAccess{cutoverAccess: f.access, inspect: func(operation context.Context) {
				if cancellation == "caller cancellation" {
					cancel()
				}
				<-operation.Done()
			}}
			_, err := f.runtime.AdvanceSwitchover(ctx, mvutypes.Intent{AttemptID: "attempt"}, f.status())
			require.ErrorIs(t, err, ctx.Err())
			assert.Nil(t, f.status().Switchover.TargetAvailableAt)
		})
	}
	f.runtime.access = f.access
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.TargetAvailableAt != nil
	})
	assert.NotContains(t, f.calls, "restore-blue")
	assert.NotContains(t, f.calls, "restore-canonical-blue")
	assert.Equal(t, 1, f.endpoints.commits)
}
