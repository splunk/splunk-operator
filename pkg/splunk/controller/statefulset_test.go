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

package controller

import (
	"context"
	"errors"
	"testing"

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
	statefulSet.Status.CurrentRevision = "v1"
	phase, err = updateStatefulSetPodsTester(t, &mgr, statefulSet, 1 /*desiredReplicas*/, statefulSet, pod)
	if err == nil && phase != enterpriseApi.PhaseScalingUp {
		t.Errorf("UpdateStatefulSetPods should have returned error or phase should have been PhaseError, but we got phase=%s", phase)
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
		t.Errorf(err.Error())
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
