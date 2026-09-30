// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package testenv

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

func readyConditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{Type: string(enterpriseApi.ConditionReady), Status: metav1.ConditionTrue, ObservedGeneration: generation},
		{Type: string(enterpriseApi.ConditionProgressing), Status: metav1.ConditionFalse, ObservedGeneration: generation},
		{Type: string(enterpriseApi.ConditionPaused), Status: metav1.ConditionFalse, ObservedGeneration: generation},
		{Type: string(enterpriseApi.ConditionStalled), Status: metav1.ConditionFalse, ObservedGeneration: generation},
		{Type: string(enterpriseApi.ConditionNoahDependencyResolved), Status: metav1.ConditionTrue, ObservedGeneration: generation},
		{Type: string(enterpriseApi.ConditionNoahPeersReady), Status: metav1.ConditionTrue, ObservedGeneration: generation},
	}
}

func newReadyIndexerCluster() *enterpriseApi.IndexerCluster {
	return &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c3", Generation: 3},
		Spec:       enterpriseApi.IndexerClusterSpec{Replicas: 2},
		Status: enterpriseApi.IndexerClusterStatus{
			Phase:              enterpriseApi.PhaseReady,
			ObservedGeneration: 3,
			Replicas:           2,
			ReadyReplicas:      2,
			Conditions:         readyConditions(3),
		},
	}
}

func newReadySearchHeadCluster() *enterpriseApi.SearchHeadCluster {
	return &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c3", Generation: 3},
		Spec:       enterpriseApi.SearchHeadClusterSpec{Replicas: 3},
		Status: enterpriseApi.SearchHeadClusterStatus{
			Phase:              enterpriseApi.PhaseReady,
			ObservedGeneration: 3,
			DeployerPhase:      enterpriseApi.PhaseReady,
			Replicas:           3,
			ReadyReplicas:      3,
			Captain:            "splunk-c3-search-head-0",
			CaptainReady:       true,
			Initialized:        true,
			MinPeersJoined:     true,
			Conditions:         readyConditions(3),
		},
	}
}

func newReadyPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-c3-indexer-0", Namespace: "splunk-operator"},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "sidecar", Ready: true},
				{Name: "splunk", Ready: true},
			},
		},
	}
}

func newReadyDeployment() *appsv1.Deployment {
	replicas := int32(3)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "noah", Namespace: "splunk-operator", Generation: 4},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration:  4,
			Replicas:            3,
			UpdatedReplicas:     3,
			ReadyReplicas:       3,
			AvailableReplicas:   3,
			UnavailableReplicas: 0,
			Conditions: []appsv1.DeploymentCondition{{
				Type:   appsv1.DeploymentAvailable,
				Status: corev1.ConditionTrue,
			}},
		},
	}
}

func newIndexerClusterWithPeers(replicas int32) *enterpriseApi.IndexerCluster {
	idxc := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c3"},
		Spec:       enterpriseApi.IndexerClusterSpec{Replicas: replicas},
	}
	for ordinal := replicas - 1; ordinal >= 0; ordinal-- {
		idxc.Status.Peers = append(idxc.Status.Peers, enterpriseApi.IndexerClusterMemberStatus{
			ID:         fmt.Sprintf("guid-%d", ordinal),
			Name:       fmt.Sprintf("splunk-c3-indexer-%d", ordinal),
			Status:     "Up",
			Searchable: true,
		})
	}
	return idxc
}

func newSearchHeadClusterWithMembers(replicas int32) *enterpriseApi.SearchHeadCluster {
	shc := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c3"},
		Spec:       enterpriseApi.SearchHeadClusterSpec{Replicas: replicas},
	}
	for ordinal := replicas - 1; ordinal >= 0; ordinal-- {
		shc.Status.Members = append(shc.Status.Members, enterpriseApi.SearchHeadClusterMemberStatus{
			Name:       fmt.Sprintf("splunk-c3-search-head-%d", ordinal),
			Status:     "Up",
			Registered: true,
		})
	}
	return shc
}

func expectErrorFragment(err error, fragment string) {
	GinkgoHelper()
	if fragment == "" {
		Expect(err).To(Succeed())
		return
	}
	Expect(err).To(MatchError(ContainSubstring(fragment)))
}

var _ = Describe("Noah readiness verification helpers", func() {
	DescribeTable("verifies a condition for the current generation",
		func(conditions []metav1.Condition, wantError string) {
			err := VerifyCRConditionForGeneration(
				"IndexerCluster",
				"c3",
				conditions,
				enterpriseApi.ConditionReady,
				metav1.ConditionTrue,
				3,
			)
			expectErrorFragment(err, wantError)
		},
		Entry("accepts a current ready condition", []metav1.Condition{{
			Type: string(enterpriseApi.ConditionReady), Status: metav1.ConditionTrue, ObservedGeneration: 3,
		}}, ""),
		Entry("rejects a stale ready condition", []metav1.Condition{{
			Type: string(enterpriseApi.ConditionReady), Status: metav1.ConditionTrue, ObservedGeneration: 2,
		}}, "condition is not current"),
		Entry("rejects a condition from an unexpected future generation", []metav1.Condition{{
			Type: string(enterpriseApi.ConditionReady), Status: metav1.ConditionTrue, ObservedGeneration: 4,
		}}, "condition is not current"),
		Entry("rejects the wrong condition status", []metav1.Condition{{
			Type: string(enterpriseApi.ConditionReady), Status: metav1.ConditionFalse, ObservedGeneration: 3,
		}}, "condition is False, expected True"),
		Entry("rejects a missing condition", nil, "condition not found"),
	)

	DescribeTable("verifies IndexerCluster readiness",
		func(mutate func(*enterpriseApi.IndexerCluster), wantError string) {
			idxc := newReadyIndexerCluster()
			mutate(idxc)
			err := VerifyIndexerClusterReadyStatus(
				idxc,
				enterpriseApi.ConditionNoahDependencyResolved,
				enterpriseApi.ConditionNoahPeersReady,
			)
			expectErrorFragment(err, wantError)
		},
		Entry("accepts generation-current Noah readiness", func(*enterpriseApi.IndexerCluster) {}, ""),
		Entry("rejects stale status", func(idxc *enterpriseApi.IndexerCluster) {
			idxc.Status.ObservedGeneration = 2
		}, "status is not current"),
		Entry("rejects incomplete replicas", func(idxc *enterpriseApi.IndexerCluster) {
			idxc.Status.ReadyReplicas = 1
		}, "is not ready"),
		Entry("rejects a current stalled condition", func(idxc *enterpriseApi.IndexerCluster) {
			for i := range idxc.Status.Conditions {
				if idxc.Status.Conditions[i].Type == string(enterpriseApi.ConditionStalled) {
					idxc.Status.Conditions[i].Status = metav1.ConditionTrue
				}
			}
		}, "is stalled"),
		Entry("rejects a missing mode-specific condition", func(idxc *enterpriseApi.IndexerCluster) {
			idxc.Status.Conditions = idxc.Status.Conditions[:4]
		}, "NoahDependencyResolved condition not found"),
	)

	DescribeTable("verifies SearchHeadCluster readiness",
		func(mutate func(*enterpriseApi.SearchHeadCluster), wantError string) {
			shc := newReadySearchHeadCluster()
			mutate(shc)
			expectErrorFragment(
				VerifySearchHeadClusterReadyStatus(shc, enterpriseApi.ConditionNoahDependencyResolved),
				wantError,
			)
		},
		Entry("accepts generation-current Noah readiness", func(*enterpriseApi.SearchHeadCluster) {}, ""),
		Entry("rejects an unready deployer", func(shc *enterpriseApi.SearchHeadCluster) {
			shc.Status.DeployerPhase = enterpriseApi.PhasePending
		}, "deployerPhase=Pending"),
		Entry("rejects a missing captain", func(shc *enterpriseApi.SearchHeadCluster) {
			shc.Status.Captain = ""
		}, "captain=\"\""),
		Entry("rejects a cluster without a ready captain", func(shc *enterpriseApi.SearchHeadCluster) {
			shc.Status.CaptainReady = false
		}, "captainReady=false"),
	)

	It("verifies LicenseManager readiness", func() {
		lm := &enterpriseApi.LicenseManager{
			ObjectMeta: metav1.ObjectMeta{Name: "c3", Generation: 3},
			Status: enterpriseApi.LicenseManagerStatus{
				Phase:              enterpriseApi.PhaseReady,
				ObservedGeneration: 3,
				Conditions:         readyConditions(3),
			},
		}

		Expect(VerifyLicenseManagerReadyStatus(lm)).To(Succeed())
		lm.Status.ObservedGeneration = 2
		Expect(VerifyLicenseManagerReadyStatus(lm)).To(MatchError(ContainSubstring("status is not current")))
	})

	DescribeTable("verifies Pod readiness",
		func(mutate func(*corev1.Pod), wantError string) {
			pod := newReadyPod()
			mutate(pod)
			expectErrorFragment(VerifyPodReady(pod, "splunk"), wantError)
		},
		Entry("accepts a ready application container", func(*corev1.Pod) {}, ""),
		Entry("rejects a terminating Pod", func(pod *corev1.Pod) {
			now := metav1.Now()
			pod.DeletionTimestamp = &now
		}, "is terminating"),
		Entry("rejects a non-running Pod", func(pod *corev1.Pod) {
			pod.Status.Phase = corev1.PodPending
		}, "expected Running"),
		Entry("rejects an unready Pod condition", func(pod *corev1.Pod) {
			pod.Status.Conditions[0].Status = corev1.ConditionFalse
		}, "does not have Ready=True"),
		Entry("rejects an unready application container", func(pod *corev1.Pod) {
			pod.Status.ContainerStatuses[1].Ready = false
		}, "container splunk is not ready"),
		Entry("rejects a missing application container", func(pod *corev1.Pod) {
			pod.Status.ContainerStatuses = pod.Status.ContainerStatuses[:1]
		}, "has no splunk container status"),
	)

	DescribeTable("verifies Deployment readiness",
		func(mutate func(*appsv1.Deployment), wantError string) {
			deployment := newReadyDeployment()
			mutate(deployment)
			expectErrorFragment(VerifyDeploymentReady(deployment), wantError)
		},
		Entry("accepts a fully rolled out Deployment", func(*appsv1.Deployment) {}, ""),
		Entry("rejects stale status", func(deployment *appsv1.Deployment) {
			deployment.Status.ObservedGeneration = 3
		}, "status is not current"),
		Entry("rejects old-revision replicas", func(deployment *appsv1.Deployment) {
			deployment.Status.UpdatedReplicas = 2
		}, "is not fully rolled out"),
		Entry("rejects unavailable replicas", func(deployment *appsv1.Deployment) {
			deployment.Status.AvailableReplicas = 2
			deployment.Status.UnavailableReplicas = 1
		}, "is not fully rolled out"),
		Entry("rejects a missing Available condition", func(deployment *appsv1.Deployment) {
			deployment.Status.Conditions = nil
		}, "does not have Available=True"),
	)

	DescribeTable("returns ready IndexerCluster Pods",
		func(mutate func(*enterpriseApi.IndexerCluster), wantError string) {
			idxc := newIndexerClusterWithPeers(11)
			mutate(idxc)
			pods, err := ReadyIndexerClusterPods(idxc)
			if wantError == "" {
				Expect(err).To(Succeed())
				Expect(pods).To(HaveLen(11))
				return
			}
			Expect(err).To(MatchError(ContainSubstring(wantError)))
		},
		Entry("accepts an unordered eleven-peer status", func(*enterpriseApi.IndexerCluster) {}, ""),
		Entry("rejects a missing peer entry", func(idxc *enterpriseApi.IndexerCluster) {
			idxc.Status.Peers = idxc.Status.Peers[1:]
		}, "reports 10 peers, expected 11"),
		Entry("rejects a missing peer ID", func(idxc *enterpriseApi.IndexerCluster) {
			idxc.Status.Peers[0].ID = ""
		}, "peer is not ready"),
		Entry("rejects a duplicate peer ID", func(idxc *enterpriseApi.IndexerCluster) {
			idxc.Status.Peers[1].ID = idxc.Status.Peers[0].ID
		}, "duplicate peer ID"),
		Entry("rejects a non-searchable peer", func(idxc *enterpriseApi.IndexerCluster) {
			idxc.Status.Peers[0].Searchable = false
		}, "peer is not ready"),
		Entry("rejects the wrong status contract", func(idxc *enterpriseApi.IndexerCluster) {
			idxc.Status.Peers[0].Status = "up"
		}, "peer is not ready"),
		Entry("rejects a Pod outside the StatefulSet identity", func(idxc *enterpriseApi.IndexerCluster) {
			idxc.Status.Peers[0].Name = "replacement-indexer"
		}, "unexpected peer Pod"),
	)

	DescribeTable("returns ready SearchHeadCluster Pods",
		func(mutate func(*enterpriseApi.SearchHeadCluster), wantError string) {
			shc := newSearchHeadClusterWithMembers(11)
			mutate(shc)
			pods, err := ReadySearchHeadClusterPods(shc)
			if wantError == "" {
				Expect(err).To(Succeed())
				Expect(pods).To(HaveLen(11))
				return
			}
			Expect(err).To(MatchError(ContainSubstring(wantError)))
		},
		Entry("accepts an unordered eleven-member status", func(*enterpriseApi.SearchHeadCluster) {}, ""),
		Entry("rejects an unregistered member", func(shc *enterpriseApi.SearchHeadCluster) {
			shc.Status.Members[0].Registered = false
		}, "member is not ready"),
		Entry("rejects an active lifecycle operation", func(shc *enterpriseApi.SearchHeadCluster) {
			shc.Status.Members[0].CurrentOperation = enterpriseApi.MemberOperationRecycling
		}, "operation=\"Recycling\""),
		Entry("rejects a duplicate member", func(shc *enterpriseApi.SearchHeadCluster) {
			shc.Status.Members[1].Name = shc.Status.Members[0].Name
		}, "duplicate member"),
		Entry("rejects a Pod outside the StatefulSet identity", func(shc *enterpriseApi.SearchHeadCluster) {
			shc.Status.Members[0].Name = "replacement-search-head"
		}, "unexpected member Pod"),
	)

	DescribeTable("detects a current stalled condition",
		func(conditions []metav1.Condition, wantError bool) {
			err := VerifyCRNotStalledForGeneration("IndexerCluster", "c3", conditions, 3)
			if wantError {
				Expect(err).To(MatchError(ContainSubstring("is stalled at generation 3")))
				return
			}
			Expect(err).To(Succeed())
		},
		Entry("accepts a missing stalled condition", nil, false),
		Entry("accepts a current not-stalled condition", []metav1.Condition{{
			Type: string(enterpriseApi.ConditionStalled), Status: metav1.ConditionFalse, ObservedGeneration: 3,
		}}, false),
		Entry("ignores a stale stalled condition", []metav1.Condition{{
			Type: string(enterpriseApi.ConditionStalled), Status: metav1.ConditionTrue, ObservedGeneration: 2,
		}}, false),
		Entry("rejects a current stalled condition", []metav1.Condition{{
			Type:               string(enterpriseApi.ConditionStalled),
			Status:             metav1.ConditionTrue,
			ObservedGeneration: 3,
			Reason:             "PVCUnbound",
			Message:            "indexer PVC cannot bind",
		}}, true),
	)
})
