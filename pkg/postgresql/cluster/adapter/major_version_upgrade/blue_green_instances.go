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
	"fmt"
	"net/netip"
	"slices"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// healthyInstancePods resolves exact CNPG instance identities. CNPG does not
// provide stable per-pod DNS, so direct SQL uses these observed PodIPs while
// independently verifying the environment's Service certificate identity.
func healthyInstancePods(ctx context.Context, reader client.Reader, cluster *cnpgv1.Cluster) ([]string, map[string]corev1.Pod, error) {
	return observedInstancePods(ctx, reader, cluster, false)
}

// A retired source has a durable all-instance fence proof. Missing pods cannot
// accept connections, but every observed pod with an address must be inspected
// even while unready or terminating.
func retainedInstancePods(ctx context.Context, reader client.Reader, cluster *cnpgv1.Cluster) ([]string, map[string]corev1.Pod, error) {
	return observedInstancePods(ctx, reader, cluster, true)
}

func observedInstancePods(ctx context.Context, reader client.Reader, cluster *cnpgv1.Cluster, retained bool) ([]string, map[string]corev1.Pod, error) {
	if reader == nil || cluster == nil || cluster.UID == "" || cluster.Name == "" || cluster.Namespace == "" {
		return nil, nil, fmt.Errorf("%w: CNPG instance discovery requires the environment's persisted identity", mvutypes.ErrUpgradeFlowFailed)
	}
	names := slices.Clone(cluster.Status.InstancesStatus[cnpgv1.PodHealthy])
	slices.Sort(names)
	for index, name := range names {
		if name == "" || (index > 0 && names[index-1] == name) {
			return nil, nil, fmt.Errorf("%w: CNPG healthy instance inventory is invalid", mvutypes.ErrUpgradeFlowFailed)
		}
	}
	// Status can stop listing an instance before its old sessions stop, even
	// before commit. Controller ownership discovers every remaining instance;
	// a label selector could hide a retagged pod and cannot prove ownership.
	list := &corev1.PodList{}
	if err := reader.List(ctx, list, client.InNamespace(cluster.Namespace)); err != nil {
		return nil, nil, fmt.Errorf("listing owned CNPG instances: %w", err)
	}
	for _, pod := range list.Items {
		if owner := metav1.GetControllerOf(&pod); owner != nil && owner.UID == cluster.UID {
			names = append(names, pod.Name)
		}
	}
	slices.Sort(names)
	names = slices.Compact(names)
	pods := make(map[string]corev1.Pod, len(names))
	addresses := make(map[netip.Addr]string, len(names))
	observed := make([]string, 0, len(names))
	for _, name := range names {
		pod := &corev1.Pod{}
		if err := reader.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, pod); err != nil {
			if retained && apierrors.IsNotFound(err) {
				continue
			}
			return nil, nil, fmt.Errorf("%w: reading CNPG instance %q: %w", mvutypes.ErrUpgradeFlowPending, name, err)
		}
		owner := metav1.GetControllerOf(pod)
		if pod.UID == "" || pod.Name != name || pod.Namespace != cluster.Namespace || pod.Labels["cnpg.io/cluster"] != cluster.Name ||
			owner == nil || owner.UID != cluster.UID || owner.Name != cluster.Name || owner.Kind != "Cluster" || owner.APIVersion != cnpgv1.SchemeGroupVersion.String() {
			return nil, nil, fmt.Errorf("%w: CNPG instance %q does not belong to the recorded environment", mvutypes.ErrUpgradeFlowFailed, name)
		}
		if !retained && (!pod.DeletionTimestamp.IsZero() || pod.Status.Phase != corev1.PodRunning || !instancePodReady(pod)) {
			return nil, nil, fmt.Errorf("%w: CNPG instance %q is not currently ready", mvutypes.ErrUpgradeFlowPending, name)
		}
		if retained && pod.Status.PodIP == "" {
			continue
		}
		address, err := netip.ParseAddr(pod.Status.PodIP)
		if err != nil || address.IsUnspecified() || address.IsLoopback() || address.IsMulticast() {
			return nil, nil, fmt.Errorf("%w: CNPG instance %q has no valid direct address", mvutypes.ErrUpgradeFlowPending, name)
		}
		address = address.Unmap()
		if previous, found := addresses[address]; found {
			return nil, nil, fmt.Errorf("%w: CNPG instances %q and %q share a direct address", mvutypes.ErrUpgradeFlowFailed, previous, name)
		}
		addresses[address] = name
		pods[name] = *pod
		observed = append(observed, name)
	}
	return observed, pods, nil
}

func instancePodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

type cutoverInstanceTarget struct {
	name    string
	address string
}

func (s *postgresSwitchoverSQL) instanceHosts(ctx context.Context, cluster *cnpgv1.Cluster) ([]cutoverInstanceTarget, error) {
	if !sourceReadyForFailover(cluster) {
		return nil, nil
	}
	names, pods, err := healthyInstancePods(ctx, s.client, cluster)
	if err != nil {
		return nil, err
	}
	return cutoverInstanceTargets(names, pods), nil
}

// Retired source availability is independent of its former HA topology. The
// caller must first prove that the committed source retains its durable fence.
func retainedSourceHosts(ctx context.Context, reader client.Reader, cluster *cnpgv1.Cluster) ([]cutoverInstanceTarget, error) {
	names, pods, err := retainedInstancePods(ctx, reader, cluster)
	if err != nil {
		return nil, err
	}
	return cutoverInstanceTargets(names, pods), nil
}

func cutoverInstanceTargets(names []string, pods map[string]corev1.Pod) []cutoverInstanceTarget {
	targets := make([]cutoverInstanceTarget, 0, len(names))
	for _, name := range names {
		targets = append(targets, cutoverInstanceTarget{name: name, address: pods[name].Status.PodIP})
	}
	return targets
}
