// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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

package splkcontroller

import (
	"context"
	"errors"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// errTestPodManager is used for UT negative testing
type errTestPodManager struct {
	c splcommon.ControllerClient
}

// Update for DefaultStatefulSetPodManager handles all updates for a statefulset of standard pods
func (mgr *errTestPodManager) Update(ctx context.Context, client splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (enterpriseApi.Phase, error) {
	return enterpriseApi.PhaseInstall, nil
}

// PrepareScaleDown for DefaultStatefulSetPodManager does nothing and returns true
func (mgr *errTestPodManager) PrepareScaleDown(ctx context.Context, n int32) (bool, error) {
	// Induce not ready error
	if ctx.Value("errKey") == "errVal" {
		return false, nil
	}

	return true, errors.New(splcommon.Rerr)
}

// PrepareRecycle for DefaultStatefulSetPodManager does nothing and returns true
func (mgr *errTestPodManager) PrepareRecycle(ctx context.Context, n int32) (bool, error) {
	// Induce not ready error
	if ctx.Value("errKey") == "errVal" {
		return false, nil
	}

	return true, errors.New(splcommon.Rerr)
}

// FinishRecycle for DefaultStatefulSetPodManager does nothing and returns false
func (mgr *errTestPodManager) FinishRecycle(ctx context.Context, n int32) (bool, error) {
	// Induce not ready error
	if ctx.Value("errKey") == "errVal" {
		return false, nil
	}

	return true, errors.New(splcommon.Rerr)
}

func (mgr *errTestPodManager) FinishUpgrade(ctx context.Context, n int32) error {
	// Induce not ready error
	if ctx.Value("errKey") == "errVal" {
		return nil
	}

	return errors.New(splcommon.Rerr)
}

func TestApplyStatefulSet(t *testing.T) {
	ctx := context.TODO()
	funcCalls := []spltest.MockFuncCall{{MetaName: "*v1.StatefulSet-test-splunk-stack1-indexer"}}
	getFuncCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-indexer"},
		{MetaName: "*v1.StatefulSet-test-splunk-stack1-indexer"},
	}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": getFuncCalls, "Update": funcCalls}
	var replicas int32 = 1
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-indexer",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
	}
	revised := current.DeepCopy()
	revised.Spec.Template.ObjectMeta.Labels = map[string]string{"one": "two"}
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyStatefulSet(ctx, c, cr.(*appsv1.StatefulSet))
		return err
	}
	spltest.ReconcileTester(t, "TestApplyStatefulSet", current, revised, createCalls, updateCalls, reconcile, false)

	// Negative testing
	c := spltest.NewMockClient()
	ctx = context.TODO()
	rerr := errors.New(splcommon.Rerr)
	current.Spec.Template.Spec.Containers = []corev1.Container{{Image: "abcd"}}
	c.Create(ctx, current)

	revised = current.DeepCopy()
	revised.Spec.Template.Spec.Containers = []corev1.Container{{Image: "efgh"}}
	c.InduceErrorKind[splcommon.MockClientInduceErrorUpdate] = rerr
	_, err := ApplyStatefulSet(ctx, c, revised)
	if err == nil {
		t.Errorf("Expected error")
	}
}

func TestDefaultStatefulSetPodManager(t *testing.T) {

	// test for updating
	mgr := DefaultStatefulSetPodManager{}
	method := "DefaultStatefulSetPodManager.Update"
	spltest.PodManagerTester(t, method, &mgr)
}

func updateStatefulSetPodsTester(t *testing.T, mgr splcommon.StatefulSetPodManager, statefulSet *appsv1.StatefulSet, desiredReplicas int32, initObjects ...client.Object) (enterpriseApi.Phase, error) {
	// initialize client
	ctx := context.TODO()
	c := spltest.NewMockClient()
	c.AddObjects(initObjects)
	phase, err := UpdateStatefulSetPods(ctx, c, statefulSet, mgr, desiredReplicas)
	return phase, err
}

func TestUpdateStatefulSetPods(t *testing.T) {
	mgr := DefaultStatefulSetPodManager{}
	var replicas int32 = 1
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{ObjectMeta: metav1.ObjectMeta{Name: "pvc-etc", Namespace: "test"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pvc-var", Namespace: "test"}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:        replicas,
			ReadyReplicas:   replicas,
			UpdatedReplicas: replicas,
			UpdateRevision:  "v1",
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-0",
			Namespace: "test",
			Labels: map[string]string{
				"controller-revision-hash": "v0",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Ready: true},
			},
		},
	}

	var phase enterpriseApi.Phase
	phase, err := updateStatefulSetPodsTester(t, &mgr, statefulSet, 1 /*desiredReplicas*/, statefulSet, pod)
	if err != nil && phase != enterpriseApi.PhaseUpdating {
		t.Errorf("UpdateStatefulSetPods should not have returned error=%s with phase=%s", err, phase)
	}

	// readyReplicas < replicas
	replicas = 3
	statefulSet.Status.ReadyReplicas = 2
	statefulSet.Spec.Replicas = &replicas
	phase, err = updateStatefulSetPodsTester(t, &mgr, statefulSet, 1 /*desiredReplicas*/, statefulSet, pod)
	if err != nil && phase != enterpriseApi.PhaseUpdating {
		t.Errorf("UpdateStatefulSetPods should not have returned error=%s with phase=%s", err, phase)
	}

	// CurrentRevision = UpdateRevision
	// With refactored logic, scale-down is prioritized over waiting for scale-up
	// readyReplicas=2 > desiredReplicas=1, so we expect PhaseScalingDown
	statefulSet.Status.CurrentRevision = "v1"
	phase, err = updateStatefulSetPodsTester(t, &mgr, statefulSet, 1 /*desiredReplicas*/, statefulSet, pod)
	if err == nil && phase != enterpriseApi.PhaseScalingDown {
		t.Errorf("UpdateStatefulSetPods should have returned PhaseScalingDown, but we got phase=%s", phase)
	}

	// readyReplicas > replicas
	replicas = 2
	statefulSet.Status.ReadyReplicas = 3
	statefulSet.Spec.Replicas = &replicas
	statefulSet.Status.CurrentRevision = ""
	phase, err = updateStatefulSetPodsTester(t, &mgr, statefulSet, 1 /*desiredReplicas*/, statefulSet, pod)
	if err == nil && phase != enterpriseApi.PhaseScalingDown {
		t.Errorf("UpdateStatefulSetPods should have returned error or phase should have been PhaseError, but we got phase=%s", phase)
	}

	// CurrentRevision = UpdateRevision
	statefulSet.Status.CurrentRevision = "v1"
	phase, err = updateStatefulSetPodsTester(t, &mgr, statefulSet, 1 /*desiredReplicas*/, statefulSet, pod)
	if err == nil && phase != enterpriseApi.PhaseScalingDown {
		t.Errorf("UpdateStatefulSetPods should have returned error or phase should have been PhaseError, but we got phase=%s", phase)
	}

	// Negative testing
	ctx := context.TODO()
	replicas = 3
	rerr := errors.New(splcommon.Rerr)
	c := spltest.NewMockClient()
	statefulSet = &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{ObjectMeta: metav1.ObjectMeta{Name: "pvc-etc", Namespace: "test"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pvc-var", Namespace: "test"}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:        replicas,
			ReadyReplicas:   replicas,
			UpdatedReplicas: replicas,
			UpdateRevision:  "v1",
		},
	}
	statefulSet.Status.ReadyReplicas = 3
	// Add statefulSet to mock client so UpdateStatefulSetPods can re-fetch it
	c.AddObject(statefulSet)
	c.InduceErrorKind[splcommon.MockClientInduceErrorUpdate] = rerr
	_, err = UpdateStatefulSetPods(ctx, c, statefulSet, &mgr, 1)
	if err == nil {
		t.Errorf("Expected error")
	}

	// Prepare scale down errors
	replicas = 3
	errPodMgr := errTestPodManager{
		c: c,
	}
	// Create the pod that will be scaled down so PrepareScaleDown is called
	podToScaleDown := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-2",
			Namespace: "test",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}
	c.Create(ctx, podToScaleDown)

	_, err = UpdateStatefulSetPods(ctx, c, statefulSet, &errPodMgr, 1)
	if err == nil {
		t.Errorf("Expected error")
	}
	replicas = 3
	ctx = context.WithValue(ctx, "errKey", "errVal")
	_, err = UpdateStatefulSetPods(ctx, c, statefulSet, &errPodMgr, 1)
	if err != nil {
		t.Errorf("scale down not ready, don't expect error")
	}

	// Scaling down errors
	c.InduceErrorKind[splcommon.MockClientInduceErrorUpdate] = nil
	c.InduceErrorKind[splcommon.MockClientInduceErrorGet] = rerr
	_, err = UpdateStatefulSetPods(ctx, c, statefulSet, &mgr, 1)
	if err == nil {
		t.Errorf("Expected error")
	}

	replicas = 3
	c.InduceErrorKind[splcommon.MockClientInduceErrorGet] = nil
	c.InduceErrorKind[splcommon.MockClientInduceErrorDelete] = rerr
	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pvc-etc-splunk-stack1-2",
			Namespace: "test",
		},
	}
	c.Create(ctx, &pvc)
	_, err = UpdateStatefulSetPods(ctx, c, statefulSet, &mgr, 1)
	if err == nil {
		t.Errorf("Expected error")
	}

	// Pod revision different errors
	c.InduceErrorKind[splcommon.MockClientInduceErrorDelete] = nil
	replicas = 3
	pod.Name = "splunk-stack1-2"
	pod.Status.Phase = corev1.PodRunning
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name:  "splunkcontiner",
			Ready: true,
		},
	}
	pod.ObjectMeta.Labels = make(map[string]string)
	pod.ObjectMeta.Labels["controller-revision-hash"] = "v2"
	c.Create(ctx, pod)
	_, err = UpdateStatefulSetPods(ctx, c, statefulSet, &errPodMgr, 3)
	if err != nil {
		t.Errorf("Ready fail for prepareRecycle pod revision hash different, no expected error")
	}

	ctx = context.WithValue(ctx, "errKey", "newVal")
	_, err = UpdateStatefulSetPods(ctx, c, statefulSet, &errPodMgr, 3)
	if err == nil {
		t.Errorf("Expected error")
	}

	c.InduceErrorKind[splcommon.MockClientInduceErrorDelete] = rerr
	_, err = UpdateStatefulSetPods(ctx, c, statefulSet, &mgr, 3)
	if err == nil {
		t.Errorf("Expected error")
	}

	c.InduceErrorKind[splcommon.MockClientInduceErrorDelete] = nil
	pod.ObjectMeta.Labels["controller-revision-hash"] = "v1"
	c.Update(ctx, pod)

	_, err = UpdateStatefulSetPods(ctx, c, statefulSet, &errPodMgr, 3)
	if err == nil {
		t.Errorf("Expected error")
	}

	ctx = context.WithValue(ctx, "errKey", "errVal")
	_, err = UpdateStatefulSetPods(ctx, c, statefulSet, &errPodMgr, 3)
	if err != nil {
		t.Errorf("Don't expected error, finish recyle complete flag failure")
	}

	newCtx := context.WithValue(context.TODO(), "errVal", "newVal")
	_, err = UpdateStatefulSetPods(newCtx, c, statefulSet, &mgr, 3)

}

func TestSetStatefulSetOwnerRef(t *testing.T) {

	ctx := context.TODO()
	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	current := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console",
			Namespace: "test",
		},
	}
	namespacedName := types.NamespacedName{Namespace: "test", Name: "splunk-test-monitoring-console"}

	err := SetStatefulSetOwnerRef(ctx, c, &cr, namespacedName)
	if !k8serrors.IsNotFound(err) {
		t.Errorf("Couldn't detect resource %s", current.GetName())
	}

	// Create statefulset
	err = splutil.CreateResource(ctx, c, &current)
	if err != nil {
		t.Errorf("Failed to create owner reference  %s", current.GetName())
	}

	// Test existing owner reference
	err = SetStatefulSetOwnerRef(ctx, c, &cr, namespacedName)
	if err != nil {
		t.Errorf("Couldn't set owner ref for resource %s", current.GetName())
	}

	// Try adding same owner again
	err = SetStatefulSetOwnerRef(ctx, c, &cr, namespacedName)
	if err != nil {
		t.Errorf("Couldn't set owner ref for statefulset %s", current.GetName())
	}
}

func TestGetStatefulSetByName(t *testing.T) {

	ctx := context.TODO()
	c := spltest.NewMockClient()

	current := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console",
			Namespace: "test",
		},
	}

	_, err := ApplyStatefulSet(ctx, c, &current)
	if err != nil {
		return
	}

	namespacedName := types.NamespacedName{Namespace: "test", Name: "splunk-test-monitoring-console"}
	_, err = GetStatefulSetByName(ctx, c, namespacedName)
	if err != nil {
		t.Error(err.Error())
	}
}

func TestDeleteReferencesToAutomatedMCIfExists(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	cr1 := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack2",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	current := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console",
			Namespace: "test",
		},
	}
	namespacedName := types.NamespacedName{Namespace: "test", Name: "splunk-test-monitoring-console"}

	err := SetStatefulSetOwnerRef(ctx, c, &cr, namespacedName)
	if !k8serrors.IsNotFound(err) {
		t.Errorf("Couldn't detect resource %s", current.GetName())
	}

	// Create statefulset
	err = splutil.CreateResource(ctx, c, &current)
	if err != nil {
		t.Errorf("Failed to create owner reference  %s", current.GetName())
	}

	// Test existing owner reference
	err = SetStatefulSetOwnerRef(ctx, c, &cr, namespacedName)
	if err != nil {
		t.Errorf("Couldn't set owner ref for resource %s", current.GetName())
	}

	err = SetStatefulSetOwnerRef(ctx, c, &cr1, namespacedName)
	if err != nil {
		t.Errorf("Couldn't set owner ref for resource %s", current.GetName())
	}

	configmap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console",
			Namespace: "test",
		},
	}

	// Create configmap
	err = splutil.CreateResource(ctx, c, &configmap)
	if err != nil {
		t.Errorf("Failed to create resource  %s", current.GetName())
	}

	// multiple owner ref
	err = DeleteReferencesToAutomatedMCIfExists(ctx, c, &cr, namespacedName)
	if err != nil {
		t.Errorf("Couldn't delete resource %s", current.GetName())
	}

	//single owner
	// Create statefulset
	err = splutil.CreateResource(ctx, c, &current)
	if err != nil {
		t.Errorf("Failed to create owner reference  %s", current.GetName())
	}

	//set owner reference
	err = SetStatefulSetOwnerRef(ctx, c, &cr1, namespacedName)
	if err != nil {
		t.Errorf("Couldn't set owner ref for resource %s", current.GetName())
	}

	// Create configmap
	err = splutil.CreateResource(ctx, c, &configmap)
	if err != nil {
		t.Errorf("Failed to create resource  %s", current.GetName())
	}

	// multiple owner ref
	err = DeleteReferencesToAutomatedMCIfExists(ctx, c, &cr1, namespacedName)
	if err != nil {
		t.Errorf("Couldn't delete resource %s", current.GetName())
	}

	// Negative testing
	c = spltest.NewMockClient()
	err = DeleteReferencesToAutomatedMCIfExists(ctx, c, &cr, namespacedName)
	if err != nil {
		t.Errorf("MC ss doesn't exist, don't expected error")
	}

	c.Create(ctx, &current)
	err = SetStatefulSetOwnerRef(ctx, c, &cr, namespacedName)
	if err != nil {
		t.Errorf("Couldn't set OR resource %s", current.GetName())
	}

	rerr := errors.New(splcommon.Rerr)
	c.InduceErrorKind[splcommon.MockClientInduceErrorDelete] = rerr
	err = DeleteReferencesToAutomatedMCIfExists(ctx, c, &cr, namespacedName)
	if err == nil {
		t.Errorf("expected error")
	}

	c.InduceErrorKind[splcommon.MockClientInduceErrorDelete] = nil
	err = DeleteReferencesToAutomatedMCIfExists(ctx, c, &cr, namespacedName)
	if err != nil {
		t.Errorf("didn't expect error")
	}

	or := []metav1.OwnerReference{}
	current.SetOwnerReferences(or)
	c.Update(ctx, &current)
	err = DeleteReferencesToAutomatedMCIfExists(ctx, c, &cr, namespacedName)
	if err != nil {
		t.Errorf("didn't expect error")
	}
}

func TestIsStatefulSetScalingUp(t *testing.T) {

	ctx := context.TODO()
	var replicas int32 = 1
	statefulSetName := "splunk-stand1-standalone"

	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stand1",
			Namespace: "test",
		},
	}

	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      statefulSetName,
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
	}

	c := spltest.NewMockClient()

	*current.Spec.Replicas = 2
	_, err := IsStatefulSetScalingUpOrDown(ctx, c, &cr, statefulSetName, replicas)
	if err == nil {
		t.Errorf("IsStatefulSetScalingUp should have returned error as we have not yet added statefulset to client.")
	}

	c.AddObject(current)
	_, err = IsStatefulSetScalingUpOrDown(ctx, c, &cr, statefulSetName, replicas)
	if err != nil {
		t.Errorf("IsStatefulSetScalingUp should not have returned error")
	}

	var higherRep int32 = 3
	var lowerRef int32 = 0
	_, err = IsStatefulSetScalingUpOrDown(ctx, c, &cr, statefulSetName, higherRep)
	if err != nil {
		t.Errorf("IsStatefulSetScalingUp should not have returned error")
	}
	_, err = IsStatefulSetScalingUpOrDown(ctx, c, &cr, statefulSetName, lowerRef)
	if err != nil {
		t.Errorf("IsStatefulSetScalingUp should not have returned error")
	}
}

func TestRemoveUnwantedOwnerRefSs(t *testing.T) {
	ctx := context.TODO()
	cr := enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	current := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-test-monitoring-console",
			Namespace: "test",
		},
	}
	namespacedName := types.NamespacedName{Namespace: "test", Name: "splunk-test-monitoring-console"}

	err := RemoveUnwantedOwnerRefSs(ctx, c, namespacedName, &cr)
	if err == nil {
		t.Errorf("Expected an error for statefulSet not found")
	}

	c.AddObject(&current)
	err = RemoveUnwantedOwnerRefSs(ctx, c, namespacedName, &cr)
	if err != nil {
		t.Errorf("Unexpected error")
	}

	c.InduceErrorKind[splcommon.MockClientInduceErrorUpdate] = errors.New(splcommon.Rerr)
	err = RemoveUnwantedOwnerRefSs(ctx, c, namespacedName, &cr)
	if err == nil {
		t.Errorf("Expected error")
	}
}

func TestGetScaleUpReadyWaitTimeout(t *testing.T) {
	tests := []struct {
		name        string
		annotation  string
		expected    string // Use string for easier comparison
		expectError bool
	}{
		// Standard valid durations
		{
			name:       "valid 10m timeout",
			annotation: "10m",
			expected:   "10m0s",
		},
		{
			name:       "valid 5m30s timeout",
			annotation: "5m30s",
			expected:   "5m30s",
		},
		{
			name:       "valid 1h timeout",
			annotation: "1h",
			expected:   "1h0m0s",
		},
		// Short durations (previously rejected, now accepted)
		{
			name:       "short timeout 1s",
			annotation: "1s",
			expected:   "1s",
		},
		{
			name:       "short timeout 5s",
			annotation: "5s",
			expected:   "5s",
		},
		{
			name:       "short timeout 10s",
			annotation: "10s",
			expected:   "10s",
		},
		{
			name:       "short timeout 29s (just under old min)",
			annotation: "29s",
			expected:   "29s",
		},
		// Long durations (previously capped at 24h, now accepted as-is)
		{
			name:       "long timeout 48h",
			annotation: "48h",
			expected:   "48h0m0s",
		},
		{
			name:       "long timeout 72h (3 days)",
			annotation: "72h",
			expected:   "72h0m0s",
		},
		{
			name:       "long timeout 168h (7 days)",
			annotation: "168h",
			expected:   "168h0m0s",
		},
		{
			name:       "long timeout 720h (30 days)",
			annotation: "720h",
			expected:   "720h0m0s",
		},
		// Zero timeouts (bypass wait)
		{
			name:       "zero timeout",
			annotation: "0s",
			expected:   "0s",
		},
		{
			name:       "zero timeout alternate",
			annotation: "0",
			expected:   "0s",
		},
		// Default/error cases - default is now 0 (no wait)
		{
			name:       "missing annotation returns 0 (no wait)",
			annotation: "",
			expected:   "0s",
		},
		{
			name:       "invalid format returns 0 (no wait)",
			annotation: "invalid",
			expected:   "0s",
		},
		// Negative values mean wait forever
		{
			name:       "negative value -5m returns -5m (wait forever)",
			annotation: "-5m",
			expected:   "-5m0s",
		},
		{
			name:       "negative value -1ns returns -1ns (wait forever)",
			annotation: "-1ns",
			expected:   "-1ns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-statefulset",
					Namespace: "test",
				},
			}

			if tt.annotation != "" {
				statefulSet.ObjectMeta.Annotations = map[string]string{
					ScaleUpReadyWaitTimeoutAnnotation: tt.annotation,
				}
			}

			result := getScaleUpReadyWaitTimeout(statefulSet)
			if result.String() != tt.expected {
				t.Errorf("getScaleUpReadyWaitTimeout() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetScaleUpWaitStarted(t *testing.T) {
	now := "2025-12-10T10:00:00Z"

	tests := []struct {
		name       string
		annotation string
		expectOk   bool
	}{
		{
			name:       "valid RFC3339 timestamp",
			annotation: now,
			expectOk:   true,
		},
		{
			name:       "missing annotation",
			annotation: "",
			expectOk:   false,
		},
		{
			name:       "invalid format",
			annotation: "invalid-timestamp",
			expectOk:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statefulSet := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-statefulset",
					Namespace: "test",
				},
			}

			if tt.annotation != "" {
				statefulSet.ObjectMeta.Annotations = map[string]string{
					ScaleUpWaitStartedAnnotation: tt.annotation,
				}
			}

			_, ok := getScaleUpWaitStarted(statefulSet)
			if ok != tt.expectOk {
				t.Errorf("getScaleUpWaitStarted() ok = %v, want %v", ok, tt.expectOk)
			}
		})
	}
}

func TestSetAndClearScaleUpWaitStarted(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: func() *int32 { r := int32(3); return &r }(),
		},
	}

	c.AddObject(statefulSet)

	// Test setScaleUpWaitStarted
	err := setScaleUpWaitStarted(ctx, c, statefulSet)
	if err != nil {
		t.Errorf("setScaleUpWaitStarted() error = %v", err)
	}

	// Verify timestamp was set
	_, ok := getScaleUpWaitStarted(statefulSet)
	if !ok {
		t.Errorf("Expected timestamp to be set")
	}

	// Test clearScaleUpWaitStarted
	err = clearScaleUpWaitStarted(ctx, c, statefulSet)
	if err != nil {
		t.Errorf("clearScaleUpWaitStarted() error = %v", err)
	}

	// Verify timestamp was cleared
	_, ok = getScaleUpWaitStarted(statefulSet)
	if ok {
		t.Errorf("Expected timestamp to be cleared")
	}
}

// TestHandleScaleUpNegativeTimeout verifies that handleScaleUp waits indefinitely
// when a negative timeout is explicitly set via annotation
func TestHandleScaleUpNegativeTimeout(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 3
	var readyReplicas int32 = 2 // Not all pods are ready
	var desiredReplicas int32 = 5

	// StatefulSet WITH the scale-up-ready-wait-timeout annotation set to -1 (wait forever)
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test",
			Annotations: map[string]string{
				ScaleUpReadyWaitTimeoutAnnotation: "-1ns",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:      replicas,
			ReadyReplicas: readyReplicas,
		},
	}

	c.AddObject(statefulSet)

	// Verify that getScaleUpReadyWaitTimeout returns -1ns for negative annotation
	timeout := getScaleUpReadyWaitTimeout(statefulSet)
	if timeout != time.Duration(-1) {
		t.Errorf("Expected timeout = -1ns for negative annotation, got %v", timeout)
	}

	// Call handleScaleUp - it should wait indefinitely (return PhaseScalingUp, not proceed with scale-up)
	phase, err := handleScaleUp(ctx, c, statefulSet, replicas, readyReplicas, desiredReplicas)

	// Should not return an error
	if err != nil {
		t.Errorf("handleScaleUp() error = %v, expected nil", err)
	}

	// Should return PhaseScalingUp (since readyReplicas > 0) indicating it's waiting
	// and not proceeding with scale-up
	if phase != enterpriseApi.PhaseScalingUp {
		t.Errorf("Expected PhaseScalingUp while waiting indefinitely, got %v", phase)
	}

	// Verify that replicas was NOT changed (scale-up was not initiated)
	if *statefulSet.Spec.Replicas != replicas {
		t.Errorf("Expected replicas to remain %d, but got %d", replicas, *statefulSet.Spec.Replicas)
	}

	// Verify that the wait start annotation was set
	_, hasStartTime := getScaleUpWaitStarted(statefulSet)
	if !hasStartTime {
		t.Errorf("Expected scale-up wait start time to be set")
	}

	// Call handleScaleUp again - should continue waiting (not bypass due to timeout)
	phase, err = handleScaleUp(ctx, c, statefulSet, replicas, readyReplicas, desiredReplicas)

	if err != nil {
		t.Errorf("handleScaleUp() second call error = %v, expected nil", err)
	}

	// Should still be waiting
	if phase != enterpriseApi.PhaseScalingUp {
		t.Errorf("Expected PhaseScalingUp on second call (still waiting), got %v", phase)
	}

	// Verify replicas still unchanged
	if *statefulSet.Spec.Replicas != replicas {
		t.Errorf("Expected replicas to remain %d after second call, but got %d", replicas, *statefulSet.Spec.Replicas)
	}
}

// TestHandleScaleUpNegativeTimeoutPhasePending verifies that handleScaleUp returns PhasePending
// when waiting indefinitely (via negative timeout annotation) and there are no ready replicas
func TestHandleScaleUpNegativeTimeoutPhasePending(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 3
	var readyReplicas int32 = 0 // No pods are ready
	var desiredReplicas int32 = 5

	// StatefulSet WITH the scale-up-ready-wait-timeout annotation set to -1 (wait forever)
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset-pending",
			Namespace: "test",
			Annotations: map[string]string{
				ScaleUpReadyWaitTimeoutAnnotation: "-1ns",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:      replicas,
			ReadyReplicas: readyReplicas,
		},
	}

	c.AddObject(statefulSet)

	// Call handleScaleUp - should return PhasePending when no replicas are ready
	phase, err := handleScaleUp(ctx, c, statefulSet, replicas, readyReplicas, desiredReplicas)

	if err != nil {
		t.Errorf("handleScaleUp() error = %v, expected nil", err)
	}

	// Should return PhasePending (since readyReplicas == 0)
	if phase != enterpriseApi.PhasePending {
		t.Errorf("Expected PhasePending while waiting with no ready replicas, got %v", phase)
	}

	// Verify that replicas was NOT changed
	if *statefulSet.Spec.Replicas != replicas {
		t.Errorf("Expected replicas to remain %d, but got %d", replicas, *statefulSet.Spec.Replicas)
	}
}

func TestScaleDownBugFix(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	// Create test scenario: replicas=5, readyReplicas=2, desiredReplicas=3
	// This tests the bug fix where we check replicas > desiredReplicas instead of readyReplicas > desiredReplicas
	var replicas int32 = 5
	var readyReplicas int32 = 2
	var desiredReplicas int32 = 3

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "test",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "test:latest",
						},
					},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:      replicas,
			ReadyReplicas: readyReplicas,
		},
	}

	c.AddObject(statefulSet)
	mgr := &DefaultStatefulSetPodManager{}

	// Call UpdateStatefulSetPods
	phase, err := UpdateStatefulSetPods(ctx, c, statefulSet, mgr, desiredReplicas)

	if err != nil {
		t.Errorf("UpdateStatefulSetPods() error = %v", err)
	}

	// Should return PhaseScalingDown since replicas(5) > desiredReplicas(3)
	if phase != enterpriseApi.PhaseScalingDown {
		t.Errorf("Expected PhaseScalingDown, got %v", phase)
	}

	// Verify the scale-down logic would target the correct pod (replicas-1 = pod-4, not readyReplicas-1 = pod-1)
	// The function should attempt to decommission pod index 4 (replicas-1)
}

// TestPrepareScaleDownAlwaysCalled verifies that PrepareScaleDown is called regardless of pod state
func TestPrepareScaleDownAlwaysCalled(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 3
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-indexer",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{ObjectMeta: metav1.ObjectMeta{Name: "pvc-etc", Namespace: "test"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pvc-var", Namespace: "test"}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:        replicas,
			ReadyReplicas:   replicas,
			UpdatedReplicas: replicas,
		},
	}

	c.AddObject(statefulSet)

	// Track whether PrepareScaleDown was called
	prepareScaleDownCalled := false
	var calledWithIndex int32 = -1

	// Custom pod manager that tracks PrepareScaleDown calls
	mgr := &testTrackingPodManager{
		onPrepareScaleDown: func(n int32) (bool, error) {
			prepareScaleDownCalled = true
			calledWithIndex = n
			return true, nil
		},
	}

	// Test 1: Pod exists and is running - PrepareScaleDown should be called
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-indexer-2",
			Namespace: "test",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Ready: true},
			},
		},
	}
	c.Create(ctx, pod)

	prepareScaleDownCalled = false
	phase, err := UpdateStatefulSetPods(ctx, c, statefulSet, mgr, 2)
	if err != nil {
		t.Errorf("UpdateStatefulSetPods() error = %v", err)
	}
	if phase != enterpriseApi.PhaseScalingDown {
		t.Errorf("Expected PhaseScalingDown, got %v", phase)
	}
	if !prepareScaleDownCalled {
		t.Errorf("PrepareScaleDown was not called for running pod")
	}
	if calledWithIndex != 2 {
		t.Errorf("PrepareScaleDown called with wrong index: got %d, want 2", calledWithIndex)
	}

	// Clean up for next test
	c.Delete(ctx, pod)
}

// TestScaleDownPodPending verifies scale-down works when pod is in Pending state
func TestScaleDownPodPending(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 3
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-indexer",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{ObjectMeta: metav1.ObjectMeta{Name: "pvc-etc", Namespace: "test"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pvc-var", Namespace: "test"}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:        replicas,
			ReadyReplicas:   2, // Only 2 ready, one is pending
			UpdatedReplicas: replicas,
		},
	}

	c.AddObject(statefulSet)

	// Pod exists but is in Pending state (e.g., after manual deletion and recreation)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-indexer-2",
			Namespace: "test",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			// No container statuses since pod is pending
		},
	}
	c.Create(ctx, pod)

	prepareScaleDownCalled := false
	mgr := &testTrackingPodManager{
		onPrepareScaleDown: func(n int32) (bool, error) {
			prepareScaleDownCalled = true
			return true, nil
		},
	}

	phase, err := UpdateStatefulSetPods(ctx, c, statefulSet, mgr, 2)
	if err != nil {
		t.Errorf("UpdateStatefulSetPods() error = %v", err)
	}
	if phase != enterpriseApi.PhaseScalingDown {
		t.Errorf("Expected PhaseScalingDown, got %v", phase)
	}
	if !prepareScaleDownCalled {
		t.Errorf("PrepareScaleDown was not called for pending pod")
	}
}

// TestScaleDownPodNotExists verifies scale-down works when pod doesn't exist at all
func TestScaleDownPodNotExists(t *testing.T) {
	ctx := context.TODO()
	c := spltest.NewMockClient()

	var replicas int32 = 3
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-indexer",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{ObjectMeta: metav1.ObjectMeta{Name: "pvc-etc", Namespace: "test"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pvc-var", Namespace: "test"}},
			},
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:        replicas,
			ReadyReplicas:   2, // Only 2 ready, one was deleted
			UpdatedReplicas: 2,
		},
	}

	c.AddObject(statefulSet)

	// Pod doesn't exist at all (manually deleted)
	// Don't create the pod, so it's not found

	prepareScaleDownCalled := false
	mgr := &testTrackingPodManager{
		onPrepareScaleDown: func(n int32) (bool, error) {
			prepareScaleDownCalled = true
			return true, nil
		},
	}

	phase, err := UpdateStatefulSetPods(ctx, c, statefulSet, mgr, 2)
	if err != nil {
		t.Errorf("UpdateStatefulSetPods() error = %v", err)
	}
	if phase != enterpriseApi.PhaseScalingDown {
		t.Errorf("Expected PhaseScalingDown, got %v", phase)
	}
	if !prepareScaleDownCalled {
		t.Errorf("PrepareScaleDown was not called even though pod doesn't exist")
	}
}

// testTrackingPodManager is a test helper that tracks method calls
type testTrackingPodManager struct {
	onPrepareScaleDown func(n int32) (bool, error)
	onPrepareRecycle   func(n int32) (bool, error)
	onFinishRecycle    func(n int32) (bool, error)
	onFinishUpgrade    func(n int32) error
}

func (mgr *testTrackingPodManager) Update(ctx context.Context, client splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, desiredReplicas int32) (enterpriseApi.Phase, error) {
	return enterpriseApi.PhaseReady, nil
}

func (mgr *testTrackingPodManager) PrepareScaleDown(ctx context.Context, n int32) (bool, error) {
	if mgr.onPrepareScaleDown != nil {
		return mgr.onPrepareScaleDown(n)
	}
	return true, nil
}

func (mgr *testTrackingPodManager) PrepareRecycle(ctx context.Context, n int32) (bool, error) {
	if mgr.onPrepareRecycle != nil {
		return mgr.onPrepareRecycle(n)
	}
	return true, nil
}

func (mgr *testTrackingPodManager) FinishRecycle(ctx context.Context, n int32) (bool, error) {
	if mgr.onFinishRecycle != nil {
		return mgr.onFinishRecycle(n)
	}
	return true, nil
}

func (mgr *testTrackingPodManager) FinishUpgrade(ctx context.Context, n int32) error {
	if mgr.onFinishUpgrade != nil {
		return mgr.onFinishUpgrade(n)
	}
	return nil
}
