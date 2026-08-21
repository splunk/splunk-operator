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

package k8sops

import (
	"context"
	"errors"
	"reflect"
	"testing"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func TestApplyService(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{{MetaName: "*v1.Service-test-svc"}}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls}
	current := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "test",
		},
	}
	revised := current.DeepCopy()
	revised.Spec.ClusterIP = "8.8.8.8"
	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		return ApplyService(context.TODO(), c, cr.(*corev1.Service))
	}
	spltest.ReconcileTester(t, "TestApplyService", &current, revised, createCalls, updateCalls, reconcile, false)

	// Negative testing
	c := spltest.NewMockClient()
	rerr := errors.New(splcommon.Rerr)
	ctx := context.TODO()
	c.InduceErrorKind[splcommon.MockClientInduceErrorGet] = errors.New(splcommon.Rerr)
	err := ApplyService(ctx, c, &current)
	if err == nil {
		t.Errorf("Expected error")
	}

	current.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
	c.Update(ctx, &current)
	c.InduceErrorKind[splcommon.MockClientInduceErrorGet] = nil
	c.InduceErrorKind[splcommon.MockClientInduceErrorUpdate] = rerr
	revised = current.DeepCopy()
	revised.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeCluster
	err = ApplyService(ctx, c, revised)
	if err == nil {
		t.Errorf("Expected error")
	}
}

func TestMergeServiceSpecUpdates(t *testing.T) {
	ctx := context.TODO()
	var current, revised corev1.ServiceSpec
	name := "test-svc"
	matcher := func() bool { return false }

	svcUpdateTester := func(param string) {
		if !MergeServiceSpecUpdates(ctx, &current, &revised, name) {
			t.Errorf("MergeServiceSpecUpdates() returned %t; want %t", false, true)
		}
		if !matcher() {
			t.Errorf("MergeServiceSpecUpdates() to detect change: %s", param)
		}
		if MergeServiceSpecUpdates(ctx, &current, &revised, name) {
			t.Errorf("MergeServiceSpecUpdates() re-run returned %t; want %t", true, false)
		}
	}

	// should be no updates to merge if they are empty
	if MergeServiceSpecUpdates(ctx, &current, &revised, name) {
		t.Errorf("MergeServiceSpecUpdates() returned %t; want %t", true, false)
	}

	// check new Port added
	revised.Ports = []corev1.ServicePort{{Name: "new-port-added", Port: 32000}}
	matcher = func() bool { return reflect.DeepEqual(current.Ports, revised.Ports) }
	svcUpdateTester("Service Ports added")

	// check Port changed
	current.Ports = []corev1.ServicePort{{Name: "port-changed", Port: 32320}}
	revised.Ports = []corev1.ServicePort{{Name: "port-changed", Port: 32000}}
	matcher = func() bool { return reflect.DeepEqual(current.Ports, revised.Ports) }
	svcUpdateTester("Service Ports change")

	// new ExternalIPs
	revised.ExternalIPs = []string{"1.2.3.4"}
	matcher = func() bool { return reflect.DeepEqual(current.ExternalIPs, revised.ExternalIPs) }
	svcUpdateTester("Service ExternalIPs added")

	// updated ExternalIPs
	current.ExternalIPs = []string{"1.2.3.4"}
	revised.ExternalIPs = []string{"1.1.3.4"}
	matcher = func() bool { return reflect.DeepEqual(current.ExternalIPs, revised.ExternalIPs) }
	svcUpdateTester("Service ExternalIPs changed")

	// Type change
	current.Type = corev1.ServiceTypeClusterIP
	revised.Type = corev1.ServiceTypeNodePort
	matcher = func() bool { return current.Type == revised.Type }
	svcUpdateTester("Service Type changed")

	current.ExternalName = "splunk.example.com"
	revised.ExternalName = "splunk2.example.com"
	matcher = func() bool { return current.ExternalName == revised.ExternalName }
	svcUpdateTester("Service ExternalName changed")

	current.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeLocal
	revised.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeCluster
	matcher = func() bool { return current.ExternalTrafficPolicy == revised.ExternalTrafficPolicy }
	svcUpdateTester("Service ExternalTrafficPolicy changed")
}

func TestMergeServiceSpecUpdatesEmptyRevisedType(t *testing.T) {
	ctx := context.TODO()

	// Case 1: current is already ClusterIP and revised has no Type set
	// (the common steady-state case). Must not report a diff and must
	// not overwrite the current value.
	current := corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP}
	revised := corev1.ServiceSpec{}
	if MergeServiceSpecUpdates(ctx, &current, &revised, "test-svc") {
		t.Errorf("MergeServiceSpecUpdates() reported a diff for empty revised.Type against ClusterIP; want no diff")
	}
	if current.Type != corev1.ServiceTypeClusterIP {
		t.Errorf("current.Type was overwritten to %q; want it preserved as ClusterIP", current.Type)
	}

	// Case 2: rollback path. The CR previously customized the service to
	// LoadBalancer/NodePort via spec.serviceTemplate; the override is now
	// removed so revised.Type is empty. The merge must drive the live
	// Service back to the default ClusterIP.
	for _, fromType := range []corev1.ServiceType{corev1.ServiceTypeLoadBalancer, corev1.ServiceTypeNodePort} {
		current := corev1.ServiceSpec{Type: fromType}
		revised := corev1.ServiceSpec{}
		if !MergeServiceSpecUpdates(ctx, &current, &revised, "test-svc") {
			t.Errorf("MergeServiceSpecUpdates() did not detect rollback from %q to default ClusterIP", fromType)
		}
		if current.Type != corev1.ServiceTypeClusterIP {
			t.Errorf("rollback from %q: current.Type = %q; want ClusterIP", fromType, current.Type)
		}
	}
}
