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

package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgtestfixture "github.com/splunk/splunk-operator/internal/testutil/postgres"
	clustercore "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCutoverRecoveryFactoryUsesLiveIntentAfterClassOrImageFailure(t *testing.T) {
	for _, missingClass := range []bool{true, false} {
		t.Run(fmt.Sprintf("missing class %t", missingClass), func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, platformv1alpha1.AddToScheme(scheme))
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, cnpgv1.AddToScheme(scheme))
			started := metav1.NewTime(time.Now().Add(-2 * time.Minute))
			deadline := metav1.NewTime(time.Now().Add(-time.Minute))
			strategy := mvutypes.MajorUpgradeFlowBlueGreen
			parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default", UID: "parent-uid", Generation: 5, Annotations: map[string]string{mvutypes.AnnotationMajorUpgradeRetryAt: "malformed-after-fencing"}}, Spec: platformv1alpha1.PostgresClusterSpec{Class: "deleted-class"}}
			bg := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt", Blue: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "blue", Namespace: parent.Namespace, UID: "blue-uid"}}, Green: &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{Name: "green", Namespace: parent.Namespace, UID: "green-uid"}}, Gates: &platformv1alpha1.BlueGreenGateStatus{Switchover: &platformv1alpha1.BlueGreenGateConsumptionStatus{Generation: parent.Generation, ConsumedAt: &started}}, Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1, StartedAt: &started, DeadlineAt: &deadline, LatchedTimeout: &metav1.Duration{Duration: time.Minute}, FenceAppliedAt: &started}}
			parent.Status.Resources = &platformv1alpha1.PostgresClusterResources{ConfigMapRef: &corev1.LocalObjectReference{Name: "access"}}
			parent.Status.ProvisionerRef = bg.Blue.Ref.DeepCopy()
			parent.Status.Phase = new("Ready")
			parent.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ready"}}
			parent.Status.PostgresMajorUpgradeStatus = []platformv1alpha1.PostgresMajorUpgradeStatus{{Strategy: &strategy, SourcePgVersion: new("17"), TargetPgVersion: new("18"), Phase: new(string(mvutypes.SwitchingOver)), BlueGreen: bg}}
			cached := parent.DeepCopy()
			cached.Status.PostgresMajorUpgradeStatus[0].Phase = new(string(mvutypes.ReadyForSwitchover))
			cached.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Gates = nil
			cached.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Switchover = nil
			objects := []client.Object{parent}
			if !missingClass {
				objects = append(objects, &platformv1alpha1.PostgresClusterClass{Name: parent.Spec.Class, Spec: platformv1alpha1.PostgresClusterClassSpec{Config: &platformv1alpha1.PostgresClusterClassConfig{PostgresVersion: new("18")}}})
			}
			live := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(parent).WithObjects(objects...).Build()
			// Simulate controller-runtime: informer reads lag, all writes hit the API.
			writer := &staleParentClient{Client: live, parent: cached}
			r := &PostgresClusterReconciler{Client: writer, APIReader: live, Scheme: scheme, Recorder: record.NewFakeRecorder(10)}
			result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(parent)})
			require.NoError(t, err)
			assert.True(t, result.Requeue || result.RequeueAfter > 0)
			fresh := new(platformv1alpha1.PostgresCluster)
			require.NoError(t, live.Get(t.Context(), client.ObjectKeyFromObject(parent), fresh))
			require.Len(t, fresh.Status.PostgresMajorUpgradeStatus, 1)
			restored := fresh.Status.PostgresMajorUpgradeStatus[0].BlueGreen.Switchover
			require.NotNil(t, restored)
			assert.NotNil(t, restored.TimedOutAt, "the real factory/runtime must observe the consumed window and start restoration")
			assert.NotNil(t, restored.Failure)
			assert.Equal(t, "attempt", fresh.Status.PostgresMajorUpgradeStatus[0].BlueGreen.AttemptID)
			assert.Equal(t, string(mvutypes.SwitchingOver), *fresh.Status.PostgresMajorUpgradeStatus[0].Phase)
			assert.Equal(t, "Configuring", *fresh.Status.Phase)
			assert.True(t, meta.IsStatusConditionFalse(fresh.Status.Conditions, "Ready"), "fenced production access must not be advertised as Ready")
		})
	}
}

type staleParentClient struct {
	client.Client
	parent *platformv1alpha1.PostgresCluster
}

func (c *staleParentClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if parent, ok := obj.(*platformv1alpha1.PostgresCluster); ok && key == client.ObjectKeyFromObject(c.parent) {
		*parent = *c.parent.DeepCopy()
		return nil
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func TestMajorUpgradeFactoryReturnsConstructionErrors(t *testing.T) {
	c := fake.NewClientBuilder().Build()
	parent := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default"}}
	cfg := &clustercore.MergedConfig{Spec: &platformv1alpha1.PostgresClusterSpec{
		PostgresVersion: new(platformv1alpha1.PostgresMajorVersion18),
		Images:          pgtestfixture.Images(),
	}}
	for _, test := range []struct {
		name         string
		writer       client.Client
		reader       client.Reader
		key          client.ObjectKey
		errorContext string
	}{
		{"missing writer", nil, c, client.ObjectKeyFromObject(parent), "construct major-upgrade state store"},
		{"missing parent key", c, c, client.ObjectKey{}, "construct major-upgrade state store"},
		{"missing reader", c, nil, client.ObjectKeyFromObject(parent), "construct major-upgrade state store"},
		{"mismatched parent identity", c, c, client.ObjectKey{Name: "other", Namespace: parent.Namespace}, "construct blue/green switchover"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := &PostgresClusterReconciler{Client: test.writer, APIReader: test.reader}
			useCase, err := r.newMajorUpgradeUseCase(test.key, parent, cfg)()
			require.ErrorContains(t, err, test.errorContext)
			assert.Nil(t, useCase)
		})
	}
}
