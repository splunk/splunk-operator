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

package controller

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func TestApplyDeployment(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{{MetaName: "*v1.Deployment-test-splunk-stack1-spark-worker"}}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": funcCalls}
	var replicas int32 = 1
	current := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-spark-worker",
			Namespace: "test",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
		},
		Status: appsv1.DeploymentStatus{
			Replicas:        1,
			ReadyReplicas:   1,
			UpdatedReplicas: 1,
		},
	}
	wantPhases := []splcommon.Phase{
		splcommon.PhasePending,
		splcommon.PhaseReady,
		splcommon.PhaseUpdating,
	}
	wantPhaseNum := 0
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		gotPhase, err := ApplyDeployment(c, cr.(*appsv1.Deployment))
		if gotPhase != wantPhases[wantPhaseNum] {
			t.Errorf("TestApplyDeployment() got phase[%d] = %s; want %s", wantPhaseNum, gotPhase, wantPhases[wantPhaseNum])
		}
		wantPhaseNum++
		return err
	}

	// test update
	revised := current.DeepCopy()
	revised.Spec.Template.ObjectMeta.Labels = map[string]string{"one": "two"}
	//spltest.ReconcileTester(t, "TestApplyDeployment", &current, revised, createCalls, updateCalls, reconcile)

	// test scale up
	revised = current.DeepCopy()
	*revised.Spec.Replicas = 3
	wantPhases = []splcommon.Phase{
		splcommon.PhasePending,
		splcommon.PhaseReady,
		splcommon.PhaseScalingUp,
	}
	wantPhaseNum = 0
	spltest.ReconcileTester(t, "TestApplyDeployment", &current, revised, createCalls, updateCalls, reconcile, false)

	// test scale down
	*current.Spec.Replicas = 5
	current.Status.Replicas = 5
	current.Status.ReadyReplicas = 5
	current.Status.UpdatedReplicas = 5
	revised = current.DeepCopy()
	*revised.Spec.Replicas = 3
	wantPhases = []splcommon.Phase{
		splcommon.PhasePending,
		splcommon.PhaseReady,
		splcommon.PhaseScalingDown,
	}
	wantPhaseNum = 0
	spltest.ReconcileTester(t, "TestApplyDeployment", &current, revised, createCalls, updateCalls, reconcile, false)

	// check for no updates, except pending pod updates (in progress)
	c := spltest.NewMockClient()
	c.AddObject(&current)
	current.Status.Replicas = 5
	current.Status.ReadyReplicas = 5
	current.Status.UpdatedReplicas = 3
	wantPhase := splcommon.PhaseUpdating
	gotPhase, err := ApplyDeployment(c, &current)
	if gotPhase != wantPhase {
		t.Errorf("TestApplyDeployment() got phase = %s; want %s", gotPhase, wantPhase)
	}
	if err != nil {
		t.Errorf("TestApplyDeployment() returned error = %v; want nil", err)
	}

	// check for no updates, except waiting for pods to become ready
	c = spltest.NewMockClient()
	c.AddObject(&current)
	*current.Spec.Replicas = 5
	current.Status.Replicas = 5
	current.Status.ReadyReplicas = 3
	current.Status.UpdatedReplicas = 5
	wantPhase = splcommon.PhaseScalingUp
	gotPhase, err = ApplyDeployment(c, &current)
	if gotPhase != wantPhase {
		t.Errorf("TestApplyDeployment() got phase = %s; want %s", gotPhase, wantPhase)
	}
	if err != nil {
		t.Errorf("TestApplyDeployment() returned error = %v; want nil", err)
	}
}
