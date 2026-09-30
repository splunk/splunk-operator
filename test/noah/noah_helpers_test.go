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

package noah

import (
	"context"
	"errors"
	"fmt"
	"io"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/test/testenv"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type c3Topology struct {
	LicenseManagerPod string
	IndexerPods       []string
	SearchHeadPods    []string
}

func (topology *c3Topology) splunkPods() []string {
	pods := make([]string, 0, 1+len(topology.IndexerPods)+len(topology.SearchHeadPods))
	pods = append(pods, topology.LicenseManagerPod)
	pods = append(pods, topology.IndexerPods...)
	return append(pods, topology.SearchHeadPods...)
}

type terminalReadinessError struct {
	err error
}

func (err *terminalReadinessError) Error() string {
	return err.err.Error()
}

func (err *terminalReadinessError) Unwrap() error {
	return err.err
}

func isTerminalReadinessError(err error) bool {
	var terminal *terminalReadinessError
	return errors.As(err, &terminal)
}

// inspectC3Ready makes one non-blocking observation. Gomega's Eventually owns
// retry timing; a current Stalled=True condition is returned as terminal so the
// test can stop immediately rather than consume the entire readiness timeout.
func inspectC3Ready(
	ctx context.Context,
	kubeClient client.Client,
	namespace string,
	name string,
	operatorDeployment string,
	noahDeployment string,
) (*c3Topology, error) {
	operator := &appsv1.Deployment{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: operatorDeployment}, operator); err != nil {
		return nil, fmt.Errorf("get operator Deployment %s/%s: %w", namespace, operatorDeployment, err)
	}
	if err := testenv.VerifyDeploymentReady(operator); err != nil {
		return nil, err
	}

	noah := &appsv1.Deployment{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: noahDeployment}, noah); err != nil {
		return nil, fmt.Errorf("get Noah Deployment %s/%s: %w", namespace, noahDeployment, err)
	}
	if err := testenv.VerifyDeploymentReady(noah); err != nil {
		return nil, err
	}

	idxc := &enterpriseApi.IndexerCluster{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, idxc); err != nil {
		return nil, fmt.Errorf("get IndexerCluster %s/%s: %w", namespace, name, err)
	}
	if err := testenv.VerifyCRNotStalledForGeneration("IndexerCluster", name, idxc.Status.Conditions, idxc.Generation); err != nil {
		return nil, &terminalReadinessError{err: err}
	}
	if !idxc.Spec.NoahEnabled() || idxc.Spec.NoahClusterRef.Name == "" {
		return nil, fmt.Errorf("IndexerCluster %s/%s is not Noah enabled", namespace, name)
	}
	if idxc.Spec.LicenseManagerRef.Name == "" {
		return nil, fmt.Errorf("IndexerCluster %s/%s has no LicenseManager reference", namespace, name)
	}
	if err := testenv.VerifyIndexerClusterReadyStatus(
		idxc,
		enterpriseApi.ConditionNoahDependencyResolved,
		enterpriseApi.ConditionNoahPeersReady,
	); err != nil {
		return nil, err
	}

	indexerPods, err := testenv.ReadyIndexerClusterPods(idxc)
	if err != nil {
		return nil, err
	}

	shc := &enterpriseApi.SearchHeadCluster{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, shc); err != nil {
		return nil, fmt.Errorf("get SearchHeadCluster %s/%s: %w", namespace, name, err)
	}
	if err := testenv.VerifyCRNotStalledForGeneration("SearchHeadCluster", name, shc.Status.Conditions, shc.Generation); err != nil {
		return nil, &terminalReadinessError{err: err}
	}
	if !shc.Spec.NoahEnabled() || shc.Spec.NoahClusterRef.Name == "" {
		return nil, fmt.Errorf("SearchHeadCluster %s/%s is not Noah enabled", namespace, name)
	}
	if shc.Spec.NoahClusterRef.Name != idxc.Spec.NoahClusterRef.Name {
		return nil, fmt.Errorf(
			"SearchHeadCluster %s/%s references NoahCluster %q; IndexerCluster references %q",
			namespace,
			name,
			shc.Spec.NoahClusterRef.Name,
			idxc.Spec.NoahClusterRef.Name,
		)
	}
	if shc.Spec.LicenseManagerRef.Name != idxc.Spec.LicenseManagerRef.Name {
		return nil, fmt.Errorf(
			"SearchHeadCluster %s/%s references LicenseManager %q; IndexerCluster references %q",
			namespace,
			name,
			shc.Spec.LicenseManagerRef.Name,
			idxc.Spec.LicenseManagerRef.Name,
		)
	}
	if err := testenv.VerifySearchHeadClusterReadyStatus(
		shc,
		enterpriseApi.ConditionNoahDependencyResolved,
	); err != nil {
		return nil, err
	}

	searchHeadPods, err := testenv.ReadySearchHeadClusterPods(shc)
	if err != nil {
		return nil, err
	}

	lmName := idxc.Spec.LicenseManagerRef.Name
	lm := &enterpriseApi.LicenseManager{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: lmName}, lm); err != nil {
		return nil, fmt.Errorf("get LicenseManager %s/%s: %w", namespace, lmName, err)
	}
	if err := testenv.VerifyCRNotStalledForGeneration("LicenseManager", lmName, lm.Status.Conditions, lm.Generation); err != nil {
		return nil, &terminalReadinessError{err: err}
	}
	if err := testenv.VerifyLicenseManagerReadyStatus(lm); err != nil {
		return nil, err
	}

	topology := &c3Topology{
		LicenseManagerPod: splutil.GetSplunkStatefulsetPodName(splcommon.SplunkLicenseManager, lmName, 0),
		IndexerPods:       indexerPods,
		SearchHeadPods:    searchHeadPods,
	}
	for _, podName := range topology.splunkPods() {
		pod := &corev1.Pod{}
		if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: podName}, pod); err != nil {
			return nil, fmt.Errorf("get Pod %s/%s: %w", namespace, podName, err)
		}
		if err := testenv.VerifyPodReady(pod, "splunk"); err != nil {
			return nil, err
		}
	}

	return topology, nil
}

func dumpC3FailureState(ctx context.Context, kubeClient client.Client, namespace, name string, writer io.Writer) {
	lmName := name
	idxc := &enterpriseApi.IndexerCluster{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, idxc); err != nil {
		fmt.Fprintf(writer, "failure diagnostics: get IndexerCluster %s/%s: %v\n", namespace, name, err)
	} else {
		fmt.Fprintf(writer, "IndexerCluster %s generation=%d status=%+v\n", name, idxc.Generation, idxc.Status)
		if idxc.Spec.LicenseManagerRef.Name != "" {
			lmName = idxc.Spec.LicenseManagerRef.Name
		}
	}

	shc := &enterpriseApi.SearchHeadCluster{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, shc); err != nil {
		fmt.Fprintf(writer, "failure diagnostics: get SearchHeadCluster %s/%s: %v\n", namespace, name, err)
	} else {
		fmt.Fprintf(writer, "SearchHeadCluster %s generation=%d status=%+v\n", name, shc.Generation, shc.Status)
	}

	lm := &enterpriseApi.LicenseManager{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: lmName}, lm); err != nil {
		fmt.Fprintf(writer, "failure diagnostics: get LicenseManager %s/%s: %v\n", namespace, lmName, err)
	} else {
		fmt.Fprintf(writer, "LicenseManager %s generation=%d status=%+v\n", lmName, lm.Generation, lm.Status)
	}

	pods := &corev1.PodList{}
	if err := kubeClient.List(ctx, pods, client.InNamespace(namespace)); err != nil {
		fmt.Fprintf(writer, "failure diagnostics: list Pods in %s: %v\n", namespace, err)
	} else {
		for i := range pods.Items {
			pod := &pods.Items[i]
			fmt.Fprintf(writer, "Pod %s phase=%s deleting=%t conditions=%+v containers=%+v\n",
				pod.Name, pod.Status.Phase, pod.DeletionTimestamp != nil, pod.Status.Conditions, pod.Status.ContainerStatuses)
		}
	}

	events := &corev1.EventList{}
	if err := kubeClient.List(ctx, events, client.InNamespace(namespace)); err != nil {
		fmt.Fprintf(writer, "failure diagnostics: list Events in %s: %v\n", namespace, err)
		return
	}
	for i := range events.Items {
		event := &events.Items[i]
		fmt.Fprintf(writer, "Event %s/%s type=%s reason=%s count=%d message=%q\n",
			event.InvolvedObject.Kind, event.InvolvedObject.Name, event.Type, event.Reason, event.Count, event.Message)
	}
}
