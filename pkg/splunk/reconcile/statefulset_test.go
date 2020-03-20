// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
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
	"errors"
	"fmt"
	"testing"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestApplyStatefulSet(t *testing.T) {
	funcCalls := []mockFuncCall{{metaName: "*v1.StatefulSet-test-splunk-stack1-indexer"}}
	createCalls := map[string][]mockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]mockFuncCall{"Get": funcCalls, "Update": funcCalls}
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
	reconcile := func(c *mockClient, cr interface{}) error {
		_, err := ApplyStatefulSet(c, cr.(*appsv1.StatefulSet))
		return err
	}
	reconcileTester(t, "TestApplyStatefulSet", current, revised, createCalls, updateCalls, reconcile)
}

func podManagerUpdateTester(t *testing.T, method string, mgr StatefulSetPodManager,
	desiredReplicas int32, wantPhase enterprisev1.ResourcePhase, statefulSet *appsv1.StatefulSet,
	wantCalls map[string][]mockFuncCall, wantError error, initObjects ...runtime.Object) {

	// initialize client
	c := newMockClient()
	for _, obj := range initObjects {
		c.state[getStateKey(obj)] = obj
	}

	// test update
	gotPhase, err := mgr.Update(c, statefulSet, desiredReplicas)
	if (err == nil && wantError != nil) ||
		(err != nil && wantError == nil) ||
		(err != nil && wantError != nil && err.Error() != wantError.Error()) {
		t.Errorf("%s returned error %v; want %v", method, err, wantError)
	}
	if gotPhase != wantPhase {
		t.Errorf("%s returned phase=%s; want %s", method, gotPhase, wantPhase)
	}

	// check calls
	c.checkCalls(t, method, wantCalls)
}

func podManagerTester(t *testing.T, method string, mgr StatefulSetPodManager) {
	// test create
	funcCalls := []mockFuncCall{{metaName: "*v1.StatefulSet-test-splunk-stack1"}}
	createCalls := map[string][]mockFuncCall{"Get": funcCalls, "Create": funcCalls}
	var replicas int32 = 1
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1",
			Namespace: "test",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:        replicas,
			ReadyReplicas:   replicas,
			UpdatedReplicas: replicas,
			UpdateRevision:  "v1",
		},
	}
	podManagerUpdateTester(t, method, mgr, 1, enterprisev1.PhasePending, current, createCalls, nil)

	// test update
	revised := current.DeepCopy()
	revised.Spec.Template.ObjectMeta.Labels = map[string]string{"one": "two"}
	updateCalls := map[string][]mockFuncCall{"Get": funcCalls, "Update": funcCalls}
	methodPlus := fmt.Sprintf("%s(%s)", method, "Update StatefulSet")
	podManagerUpdateTester(t, methodPlus, mgr, 1, enterprisev1.PhaseUpdating, revised, updateCalls, nil, current)

	// test scale up (zero ready so far; wait for ready)
	revised = current.DeepCopy()
	current.Status.ReadyReplicas = 0
	scaleUpCalls := map[string][]mockFuncCall{"Get": funcCalls}
	methodPlus = fmt.Sprintf("%s(%s)", method, "ScalingUp, 0 ready")
	podManagerUpdateTester(t, methodPlus, mgr, 1, enterprisev1.PhasePending, revised, scaleUpCalls, nil, current)

	// test scale up (1 ready scaling to 2; wait for ready)
	replicas = 2
	current.Status.Replicas = 2
	current.Status.ReadyReplicas = 1
	methodPlus = fmt.Sprintf("%s(%s)", method, "ScalingUp, 1/2 ready")
	podManagerUpdateTester(t, methodPlus, mgr, 2, enterprisev1.PhaseScalingUp, revised, scaleUpCalls, nil, current)

	// test scale up (1 ready scaling to 2)
	replicas = 1
	current.Status.Replicas = 1
	current.Status.ReadyReplicas = 1
	methodPlus = fmt.Sprintf("%s(%s)", method, "ScalingUp, Update Replicas 1=>2")
	podManagerUpdateTester(t, methodPlus, mgr, 2, enterprisev1.PhaseScalingUp, revised, updateCalls, nil, current)

	// test scale down (2 ready, 1 desired)
	replicas = 1
	current.Status.Replicas = 1
	current.Status.ReadyReplicas = 2
	methodPlus = fmt.Sprintf("%s(%s)", method, "ScalingDown, Ready > Replicas")
	podManagerUpdateTester(t, methodPlus, mgr, 1, enterprisev1.PhaseScalingDown, revised, scaleUpCalls, nil, current)

	// test scale down (2 ready scaling down to 1)
	pvcCalls := []mockFuncCall{
		{metaName: "*v1.PersistentVolumeClaim-test-pvc-etc-splunk-stack1-1"},
		{metaName: "*v1.PersistentVolumeClaim-test-pvc-var-splunk-stack1-1"},
	}
	scaleDownCalls := map[string][]mockFuncCall{
		"Get":    {funcCalls[0], pvcCalls[0], pvcCalls[1]},
		"Update": {funcCalls[0]},
		"Delete": pvcCalls,
	}
	pvcList := []*corev1.PersistentVolumeClaim{
		{ObjectMeta: metav1.ObjectMeta{Name: "pvc-etc-splunk-stack1-1", Namespace: "test"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "pvc-var-splunk-stack1-1", Namespace: "test"}},
	}
	replicas = 2
	current.Status.Replicas = 2
	current.Status.ReadyReplicas = 2
	methodPlus = fmt.Sprintf("%s(%s)", method, "ScalingDown, Update Replicas 2=>1")
	podManagerUpdateTester(t, methodPlus, mgr, 1, enterprisev1.PhaseScalingDown, revised, scaleDownCalls, nil, current, pvcList[0], pvcList[1])

	// test pod not found
	replicas = 1
	current.Status.Replicas = 1
	current.Status.ReadyReplicas = 1
	podCalls := []mockFuncCall{funcCalls[0], {metaName: "*v1.Pod-test-splunk-stack1-0"}}
	getPodCalls := map[string][]mockFuncCall{"Get": podCalls}
	methodPlus = fmt.Sprintf("%s(%s)", method, "Pod not found")
	podManagerUpdateTester(t, methodPlus, mgr, 1, enterprisev1.PhaseError, revised, getPodCalls, errors.New("NotFound"), current)

	// test pod updated
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-0",
			Namespace: "test",
			Labels: map[string]string{
				"controller-revision-hash": "v0",
			},
		},
	}
	updatePodCalls := map[string][]mockFuncCall{"Get": podCalls, "Delete": {podCalls[1]}}
	methodPlus = fmt.Sprintf("%s(%s)", method, "Recycle pod")
	podManagerUpdateTester(t, methodPlus, mgr, 1, enterprisev1.PhaseUpdating, revised, updatePodCalls, nil, current, pod)

	// test all pods ready
	pod.ObjectMeta.Labels["controller-revision-hash"] = "v1"
	methodPlus = fmt.Sprintf("%s(%s)", method, "All pods ready")
	podManagerUpdateTester(t, methodPlus, mgr, 1, enterprisev1.PhaseReady, revised, getPodCalls, nil, current, pod)
}

func TestDefaultStatefulSetPodManager(t *testing.T) {
	// test for updating
	mgr := DefaultStatefulSetPodManager{}
	method := "DefaultStatefulSetPodManager.Update"
	podManagerTester(t, method, &mgr)
}
