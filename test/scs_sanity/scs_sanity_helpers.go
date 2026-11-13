// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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
package scssanity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// terminalError tells retryUntil that waiting longer cannot help.
type terminalError struct{ err error }

func (e *terminalError) Error() string { return e.err.Error() }
func (e *terminalError) Unwrap() error { return e.err }

// retryUntil polls check until it succeeds, surfacing check's own last error on timeout rather than
// a generic deadline-exceeded. Operator health right after a Helm upgrade is eventually-consistent,
// so a single sample is racy: the Deployment reports the rollout complete before the incoming pod
// has acquired the leader-election lease, and the outgoing pod's lease stays stale for up to its
// full duration in the meantime.
func retryUntil(ctx context.Context, timeout, pollInterval time.Duration, check func(context.Context) error) error {
	var lastErr error
	waitErr := wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		err := check(ctx)
		var terminal *terminalError
		if errors.As(err, &terminal) {
			lastErr = err
			return false, err
		}
		// A check cut short by the deadline would otherwise replace the last real observation with "context deadline exceeded".
		if err != nil && ctx.Err() != nil && lastErr != nil {
			return false, nil
		}
		lastErr = err
		return err == nil, nil
	})
	if waitErr != nil && lastErr != nil {
		return lastErr
	}
	return waitErr
}

// retryPhase is retryUntil with the failing phase named, so a CI log says which wait gave up and
// whether it stopped on a terminal condition or ran out of time.
func retryPhase(ctx context.Context, phase string, timeout, pollInterval time.Duration, check func(context.Context) error) error {
	err := retryUntil(ctx, timeout, pollInterval, check)
	if err == nil {
		return nil
	}
	var terminal *terminalError
	if errors.As(err, &terminal) {
		return fmt.Errorf("%s stopped early: %w", phase, err)
	}
	return fmt.Errorf("%s did not finish within %s: %w", phase, timeout, err)
}

// parseSuiteTimeout reads SCS_SANITY_SUITE_TIMEOUT. The gate sets it below its own hard timeout so
// Ginkgo can report and clean up before the process is killed.
func parseSuiteTimeout(raw string, fallback time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("SCS_SANITY_SUITE_TIMEOUT must be a positive Go duration such as 28m, got %q; the gate sets it, so unset it when running by hand", raw)
	}
	return d, nil
}

// operatorDeploymentHealthy fetches the operator Deployment and reports whether its rollout is
// fully healthy: all replicas ready, and (when targetImage is non-empty) the manager container
// running that exact image.
func operatorDeploymentHealthy(ctx context.Context, kubeClient client.Client, namespace, name, targetImage string) error {
	dep := &appsv1.Deployment{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, dep); err != nil {
		return fmt.Errorf("failed to get operator Deployment %s/%s: %w", namespace, name, err)
	}
	// ObservedGeneration/UpdatedReplicas guard against sampling mid-rollout, when ReadyReplicas can
	// still equal Replicas while the outgoing pods are counted and the new spec is not yet observed.
	if dep.Status.ObservedGeneration < dep.Generation {
		return fmt.Errorf("operator Deployment %s/%s rollout not observed yet: observedGeneration=%d generation=%d",
			namespace, name, dep.Status.ObservedGeneration, dep.Generation)
	}
	if dep.Status.ReadyReplicas != dep.Status.Replicas || dep.Status.Replicas == 0 {
		return fmt.Errorf("operator Deployment %s/%s not fully ready: readyReplicas=%d replicas=%d",
			namespace, name, dep.Status.ReadyReplicas, dep.Status.Replicas)
	}
	if dep.Status.UpdatedReplicas != dep.Status.Replicas {
		return fmt.Errorf("operator Deployment %s/%s still has stale replicas: updatedReplicas=%d replicas=%d",
			namespace, name, dep.Status.UpdatedReplicas, dep.Status.Replicas)
	}
	if dep.Status.UnavailableReplicas != 0 {
		return fmt.Errorf("operator Deployment %s/%s has %d unavailable replicas", namespace, name, dep.Status.UnavailableReplicas)
	}
	if targetImage == "" {
		return nil
	}
	for _, c := range dep.Spec.Template.Spec.Containers {
		if c.Name == "manager" {
			if c.Image != targetImage {
				return fmt.Errorf("operator Deployment %s/%s manager container image is %s, expected %s",
					namespace, name, c.Image, targetImage)
			}
			return nil
		}
	}
	return fmt.Errorf("operator Deployment %s/%s has no container named %q", namespace, name, "manager")
}

// operatorLeaderElected fetches the operator's leader-election Lease (id "270bec8c.splunk.com",
// see cmd/main.go's LeaderElectionID) and confirms it has a holder that is still actively
// renewing, not a stale holder left behind by a replica that stopped renewing during upgrade.
// holderIdentity itself is never cleared on its own, so staleness is checked the same way
// client-go's leaderelection.go does internally: renewTime + leaseDurationSeconds must still be
// in the future.
//
// Takes a client.Reader, not the cached client: the shared cache lists and watches Leases
// cluster-wide, which a namespace-scoped account is forbidden from, so a cached read hangs.
func operatorLeaderElected(ctx context.Context, kubeClient client.Reader, namespace string) error {
	lease := &coordinationv1.Lease{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Name: "270bec8c.splunk.com", Namespace: namespace}, lease); err != nil {
		return fmt.Errorf("failed to get operator leader-election lease in %s: %w", namespace, err)
	}
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "" {
		return fmt.Errorf("operator leader-election lease in %s has no holder", namespace)
	}
	if lease.Spec.RenewTime == nil || lease.Spec.LeaseDurationSeconds == nil {
		return fmt.Errorf("operator leader-election lease in %s (holder %s) is missing renewTime/leaseDurationSeconds, cannot confirm it is current",
			namespace, *lease.Spec.HolderIdentity)
	}
	leaseDuration := time.Duration(*lease.Spec.LeaseDurationSeconds) * time.Second
	expiry := lease.Spec.RenewTime.Time.Add(leaseDuration)
	if time.Now().After(expiry) {
		return fmt.Errorf("operator leader-election lease in %s (holder %s) is stale: last renewed at %s, lease duration %s, expired at %s",
			namespace, *lease.Spec.HolderIdentity, lease.Spec.RenewTime.Time, leaseDuration, expiry)
	}
	return nil
}
