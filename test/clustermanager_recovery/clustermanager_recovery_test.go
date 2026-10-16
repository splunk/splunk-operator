// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package clustermanagerrecoverytest contains end-to-end tests for Cluster Manager
// node failure recovery (CSPL-5632). Tests run against a real cluster with the
// operator deployed in-cluster and the ClusterManagerNodeRecovery feature gate enabled.
//
// Two test scenarios are covered:
//
//  1. Provider-neutral: node condition is patched to NotReady via the Kubernetes API
//     while the operator watches and drives the recovery state machine.
//
//  2. Real-provider (RKE2): the rke2-agent service is stopped on the CM node via SSM,
//     producing a genuine ConditionUnknown node. See docs/operate/NodeRecovery.md for
//     the manual test procedure.
package clustermanagerrecoverytest

import (
	"context"
	"fmt"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/test/testenv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// recoveryGracePeriod is kept short for test runs. Production default is 300s.
const recoveryGracePeriod = int32(30)

var _ = Describe("Cluster Manager Node Recovery", Label("tier:e2e-recovery"), func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment

	BeforeEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		var err error
		testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
		Expect(err).To(Succeed(), "Failed to setup test case environment")
	})

	AfterEach(func(ctx SpecContext) {
		if CurrentSpecReport().Failed() {
			testenv.DumpGetPods(testcaseEnvInst.GetName())
		}
		Expect(deployment.Teardown()).To(Succeed())
	}, NodeTimeout(testenv.SetupTeardownTimeout))

	// Provider-neutral: patch the node condition to simulate a sustained failure.
	// The patch loop runs until the grace period expires and pod deletion is requested.
	Context("Provider-neutral: node condition patched to NotReady", func() {
		It("should recover the Cluster Manager pod when node stays NotReady beyond grace period",
			Label("tier:e2e-recovery", "provider:neutral"),
			NodeTimeout(15*time.Minute), func(ctx SpecContext) {

				ns := testcaseEnvInst.GetName()
				cmName := deployment.GetName()

				By("deploying a Cluster Manager with recovery enabled and emptyDir storage")
				cmSpec := enterpriseApi.ClusterManagerSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						EtcVolumeStorageConfig: enterpriseApi.StorageClassSpec{EphemeralStorage: true},
						VarVolumeStorageConfig: enterpriseApi.StorageClassSpec{EphemeralStorage: true},
					},
					Recovery: &enterpriseApi.ClusterManagerRecoveryConfig{
						Enabled:            true,
						GracePeriodSeconds: recoveryGracePeriod,
					},
				}
				_, err := deployment.DeployClusterManagerWithGivenSpec(ctx, cmName, cmSpec)
				Expect(err).To(Succeed())

				By("waiting for Cluster Manager to reach PhaseReady")
				Eventually(func() enterpriseApi.Phase {
					return getCMPhase(ctx, deployment, cmName, ns)
				}, deployment.GetTimeout(), PollInterval).Should(Equal(enterpriseApi.PhaseReady))

				originalPodUID := getCMPodUID(ctx, testenvInstance, cmName, ns)
				nodeName := getCMPodNodeName(ctx, testenvInstance, cmName, ns)
				Expect(nodeName).NotTo(BeEmpty(), "CM pod must be scheduled on a node")

				By(fmt.Sprintf("patching node %s to NotReady", nodeName))
				stopPatch := make(chan struct{})
				go patchNodeNotReady(ctx, testenvInstance, nodeName, stopPatch)

				By("waiting for recovery to enter WaitingForGracePeriod")
				Eventually(func() enterpriseApi.RecoveryPhase {
					return getCMRecoveryPhase(ctx, deployment, cmName, ns)
				}, 2*time.Minute, PollInterval).Should(Equal(enterpriseApi.RecoveryPhaseWaitingForGracePeriod))

				By("waiting for recovery to request pod deletion after grace period")
				Eventually(func() enterpriseApi.RecoveryPhase {
					return getCMRecoveryPhase(ctx, deployment, cmName, ns)
				}, time.Duration(recoveryGracePeriod+60)*time.Second, PollInterval).Should(
					BeElementOf(enterpriseApi.RecoveryPhaseRequested, enterpriseApi.RecoveryPhaseReplacementPending))

				By("stopping the node patch loop")
				close(stopPatch)

				By("waiting for the replacement pod to appear with a new UID")
				Eventually(func() types.UID {
					return getCMPodUID(ctx, testenvInstance, cmName, ns)
				}, 5*time.Minute, PollInterval).ShouldNot(Equal(originalPodUID))

				By("waiting for Cluster Manager to return to PhaseReady after recovery")
				Eventually(func() enterpriseApi.Phase {
					return getCMPhase(ctx, deployment, cmName, ns)
				}, deployment.GetTimeout(), PollInterval).Should(Equal(enterpriseApi.PhaseReady))

				By("verifying recovery phase transitions to ReadyAfterRecovery")
				Eventually(func() enterpriseApi.RecoveryPhase {
					return getCMRecoveryPhase(ctx, deployment, cmName, ns)
				}, 2*time.Minute, PollInterval).Should(Equal(enterpriseApi.RecoveryPhaseReadyAfterRecovery))
			})
	})

	// Transient failure: node recovers before grace period expires.
	// Verifies the controller does not delete the pod for a short-lived failure.
	Context("Transient failure: node recovers before grace period expires", func() {
		It("should not delete the pod when the node recovers within the grace period",
			Label("tier:e2e-recovery", "provider:neutral"),
			NodeTimeout(10*time.Minute), func(ctx SpecContext) {

				ns := testcaseEnvInst.GetName()
				cmName := deployment.GetName()

				By("deploying a Cluster Manager with recovery enabled")
				cmSpec := enterpriseApi.ClusterManagerSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						EtcVolumeStorageConfig: enterpriseApi.StorageClassSpec{EphemeralStorage: true},
						VarVolumeStorageConfig: enterpriseApi.StorageClassSpec{EphemeralStorage: true},
					},
					Recovery: &enterpriseApi.ClusterManagerRecoveryConfig{
						Enabled:            true,
						GracePeriodSeconds: recoveryGracePeriod,
					},
				}
				_, err := deployment.DeployClusterManagerWithGivenSpec(ctx, cmName, cmSpec)
				Expect(err).To(Succeed())

				By("waiting for Cluster Manager to reach PhaseReady")
				Eventually(func() enterpriseApi.Phase {
					return getCMPhase(ctx, deployment, cmName, ns)
				}, deployment.GetTimeout(), PollInterval).Should(Equal(enterpriseApi.PhaseReady))

				originalPodUID := getCMPodUID(ctx, testenvInstance, cmName, ns)
				nodeName := getCMPodNodeName(ctx, testenvInstance, cmName, ns)

				By("patching node to NotReady briefly")
				stopPatch := make(chan struct{})
				go patchNodeNotReady(ctx, testenvInstance, nodeName, stopPatch)

				By("waiting for grace period to start")
				Eventually(func() enterpriseApi.RecoveryPhase {
					return getCMRecoveryPhase(ctx, deployment, cmName, ns)
				}, 2*time.Minute, PollInterval).Should(Equal(enterpriseApi.RecoveryPhaseWaitingForGracePeriod))

				By("restoring node before grace period expires")
				close(stopPatch)

				By("verifying no pod deletion occurred")
				Consistently(func() types.UID {
					return getCMPodUID(ctx, testenvInstance, cmName, ns)
				}, time.Duration(recoveryGracePeriod)*time.Second, PollInterval).Should(Equal(originalPodUID))

				By("verifying recovery state resets to Healthy")
				Eventually(func() enterpriseApi.RecoveryPhase {
					return getCMRecoveryPhase(ctx, deployment, cmName, ns)
				}, 2*time.Minute, PollInterval).Should(
					BeElementOf(enterpriseApi.RecoveryPhaseHealthy, enterpriseApi.RecoveryPhase("")))
			})
	})
})

func getCMPhase(ctx context.Context, d *testenv.Deployment, name, ns string) enterpriseApi.Phase {
	cm := &enterpriseApi.ClusterManager{}
	if err := d.GetInstance(ctx, name, cm); err != nil {
		return ""
	}
	return cm.Status.Phase
}

func getCMRecoveryPhase(ctx context.Context, d *testenv.Deployment, name, ns string) enterpriseApi.RecoveryPhase {
	cm := &enterpriseApi.ClusterManager{}
	if err := d.GetInstance(ctx, name, cm); err != nil {
		return ""
	}
	return cm.Status.Recovery.Phase
}

func getCMPodUID(ctx context.Context, te *testenv.TestEnv, cmName, ns string) types.UID {
	podName := fmt.Sprintf("splunk-%s-cluster-manager-0", cmName)
	pod := &corev1.Pod{}
	if err := te.GetKubeClient().Get(ctx, client.ObjectKey{Name: podName, Namespace: ns}, pod); err != nil {
		return ""
	}
	return pod.UID
}

func getCMPodNodeName(ctx context.Context, te *testenv.TestEnv, cmName, ns string) string {
	podName := fmt.Sprintf("splunk-%s-cluster-manager-0", cmName)
	pod := &corev1.Pod{}
	if err := te.GetKubeClient().Get(ctx, client.ObjectKey{Name: podName, Namespace: ns}, pod); err != nil {
		return ""
	}
	return pod.Spec.NodeName
}

// patchNodeNotReady continuously patches the node's Ready condition to False
// until the stop channel is closed. Simulates a sustained node failure without
// stopping the actual kubelet.
func patchNodeNotReady(ctx context.Context, te *testenv.TestEnv, nodeName string, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		default:
			node := &corev1.Node{}
			if err := te.GetKubeClient().Get(ctx, client.ObjectKey{Name: nodeName}, node); err != nil {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			for i, c := range node.Status.Conditions {
				if c.Type == corev1.NodeReady {
					node.Status.Conditions[i].Status = corev1.ConditionFalse
					node.Status.Conditions[i].LastHeartbeatTime = metav1.Now()
					node.Status.Conditions[i].LastTransitionTime = metav1.Now()
					break
				}
			}
			_ = te.GetKubeClient().Status().Update(ctx, node)
			time.Sleep(500 * time.Millisecond)
		}
	}
}
