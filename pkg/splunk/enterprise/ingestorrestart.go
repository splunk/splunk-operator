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

package enterprise

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// restartCheckConcurrency is the maximum number of pods polled for restart_required simultaneously.
	restartCheckConcurrency = 5
	// restartCheckTimeout is the per-pod HTTP timeout for the restart_required REST call.
	restartCheckTimeout = 5 * time.Second
	// restartPollInterval is the requeue interval when no pods need a restart (steady-state polling cadence).
	restartPollInterval = 1 * time.Minute
	// restartRetryInterval is the requeue interval when eviction is deferred or a check failed
	// (e.g. rollout in progress, pod unreachable) — short enough to recover quickly.
	restartRetryInterval = 15 * time.Second
	// pdbRetryInterval is the requeue interval when the PDB blocked an eviction; longer than
	// restartRetryInterval to give the evicted pod time to recover and restore disruption budget.
	pdbRetryInterval = 30 * time.Second
)

// RestartRequiredChecker polls a single pod for restart_required.
// Abstracted so tests can inject a mock without a real Splunk client.
type RestartRequiredChecker func(ctx context.Context, podIndex int32) (bool, error)

type restartCheckResult struct {
	pod             corev1.Pod
	restartRequired bool
	err             error
}

// MakeRestartRequiredChecker is a package-level var so tests can override it without
// standing up a real Splunk HTTP server.
var MakeRestartRequiredChecker = func(c splcommon.ControllerClient, cr *enterpriseApi.IngestorCluster) RestartRequiredChecker {
	mgr := newIngestorClusterPodManager(slog.Default(), cr, nil, splclient.NewSplunkClient, c)
	return func(ctx context.Context, podIndex int32) (bool, error) {
		splunkClient := mgr.getClient(ctx, podIndex)
		return splunkClient.GetRestartRequired(ctx)
	}
}

// RunRollingEviction checks all ingestor pods for restart_required (concurrently) and evicts
// candidates in ordinal order until the PDB blocks. Unready or terminating pods fail the
// restart_required check and are counted as failedChecks. It returns a reconcile.Result with
// an appropriate requeue interval so the caller can return it directly.
func RunRollingEviction(
	ctx context.Context,
	c splcommon.ControllerClient,
	cr *enterpriseApi.IngestorCluster,
	logger *slog.Logger,
) (reconcile.Result, error) {
	log := logger.With("cr", types.NamespacedName{Name: cr.GetName(), Namespace: cr.GetNamespace()}.String())

	// Fetch the StatefulSet to get the current UpdateRevision.
	sts := &appsv1.StatefulSet{}
	stsName := GetSplunkStatefulsetName(SplunkIngestor, cr.GetName())
	if err := c.Get(ctx, types.NamespacedName{Name: stsName, Namespace: cr.GetNamespace()}, sts); err != nil {
		return reconcile.Result{}, fmt.Errorf("get ingestor StatefulSet %s: %w", stsName, err)
	}

	var podList corev1.PodList
	if err := c.List(ctx, &podList,
		client.InNamespace(cr.GetNamespace()),
		client.MatchingLabels{"app.kubernetes.io/instance": "splunk-" + cr.GetName() + "-ingestor"},
	); err != nil {
		return reconcile.Result{}, fmt.Errorf("list ingestor pods: %w", err)
	}
	pods := podList.Items
	if len(pods) == 0 {
		log.InfoContext(ctx, "no pods found, deferring restart checks")
		return reconcile.Result{RequeueAfter: restartRetryInterval}, nil
	}

	// Defer eviction if a spec template rollout is in progress: any running pod whose
	// controller-revision-hash label doesn't match UpdateRevision is being recycled by
	// the operator's OnDelete handler. Unlike a PDB eviction, a template rollout changes
	// UpdateRevision and leaves stale-hash pods behind until they are recycled one-by-one.
	// This is the same per-pod check the operator uses in splkcontroller/statefulset.go.
	if sts.Status.UpdateRevision != "" {
		for i := range pods {
			if pods[i].Labels["controller-revision-hash"] != sts.Status.UpdateRevision {
				log.InfoContext(ctx, "StatefulSet rollout in progress, deferring restart checks",
					"pod", pods[i].Name,
					"podRevision", pods[i].Labels["controller-revision-hash"],
					"updateRevision", sts.Status.UpdateRevision)
				return reconcile.Result{RequeueAfter: restartRetryInterval}, nil
			}
		}
	}

	// Keep eviction order deterministic by StatefulSet ordinal.
	sort.Slice(pods, func(i, j int) bool {
		return ingestorPodOrdinal(pods[i].Name) < ingestorPodOrdinal(pods[j].Name)
	})

	results := checkRestartRequiredConcurrently(ctx, c, cr, pods)
	if ctx.Err() != nil {
		return reconcile.Result{}, ctx.Err()
	}

	var candidates []corev1.Pod
	failedChecks := 0
	for _, r := range results {
		if r.err != nil {
			failedChecks++
			log.WarnContext(ctx, "failed to check restart_required", "pod", r.pod.Name, "error", r.err)
			continue
		}
		if r.restartRequired {
			candidates = append(candidates, r.pod)
		}
	}

	if len(candidates) == 0 {
		if failedChecks > 0 {
			setIngestorRestartCondition(cr, metav1.ConditionUnknown,
				string(enterpriseApi.ReasonRestartCheckIncomplete),
				fmt.Sprintf("Restart checks failed for %d ingestor pod(s)", failedChecks))
			return reconcile.Result{RequeueAfter: restartRetryInterval}, nil
		}
		markIngestorRestartCompleteIfActive(cr)
		return reconcile.Result{RequeueAfter: restartPollInterval}, nil
	}

	// Evict candidates in ordinal order, stopping when the PDB blocks.
	for i := range candidates {
		candidate := &candidates[i]
		err := evictIngestorPod(ctx, c, candidate)

		switch {
		case k8serrors.IsTooManyRequests(err):
			setIngestorRestartCondition(cr, metav1.ConditionTrue,
				string(enterpriseApi.ReasonRestartBlockedByPDB),
				fmt.Sprintf("Restart of pod %s is temporarily blocked by a PodDisruptionBudget", candidate.Name))
			log.InfoContext(ctx, "eviction temporarily blocked by PDB", "pod", candidate.Name)
			return reconcile.Result{RequeueAfter: pdbRetryInterval}, nil

		case k8serrors.IsNotFound(err) || k8serrors.IsConflict(err):
			// Pod is already terminating or gone — desired outcome achieved, continue to next candidate.
			log.InfoContext(ctx, "pod already terminating, skipping eviction", "pod", candidate.Name)

		case err != nil:
			log.ErrorContext(ctx, "eviction failed", "pod", candidate.Name, "error", err)
			return reconcile.Result{RequeueAfter: restartRetryInterval}, fmt.Errorf("evict pod %s: %w", candidate.Name, err)

		default:
			setIngestorRestartCondition(cr, metav1.ConditionTrue,
				string(enterpriseApi.ReasonRollingRestartInProgress),
				fmt.Sprintf("Eviction accepted for ingestor pod %s", candidate.Name))
			log.InfoContext(ctx, "eviction accepted", "pod", candidate.Name)
		}
	}
	// All candidates evicted; pod and StatefulSet events will trigger the next reconcile.
	return reconcile.Result{RequeueAfter: restartRetryInterval}, nil
}

func checkRestartRequiredConcurrently(
	ctx context.Context,
	c splcommon.ControllerClient,
	cr *enterpriseApi.IngestorCluster,
	pods []corev1.Pod,
) []restartCheckResult {
	results := make([]restartCheckResult, len(pods))
	checker := MakeRestartRequiredChecker(c, cr)
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(restartCheckConcurrency)

	for i := range pods {
		i := i
		group.Go(func() error {
			pod := pods[i]
			ordinal := ingestorPodOrdinal(pod.Name)
			checkCtx, cancel := context.WithTimeout(groupCtx, restartCheckTimeout)
			defer cancel()
			required, err := checker(checkCtx, int32(ordinal))
			// Each goroutine owns a unique index — no mutex needed.
			results[i] = restartCheckResult{pod: pod, restartRequired: required, err: err}
			return nil // preserve per-pod errors; don't cancel siblings
		})
	}
	_ = group.Wait()
	return results
}

func evictIngestorPod(ctx context.Context, c splcommon.ControllerClient, pod *corev1.Pod) error {
	eviction := &policyv1.Eviction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
	}
	return c.SubResource("eviction").Create(ctx, pod, eviction)
}

// ingestorPodOrdinal parses the trailing integer from a StatefulSet pod name (e.g. "splunk-foo-0" → 0).
func ingestorPodOrdinal(name string) int {
	idx := strings.LastIndex(name, "-")
	if idx < 0 || idx == len(name)-1 {
		return int(^uint(0) >> 1)
	}
	n, err := strconv.Atoi(name[idx+1:])
	if err != nil {
		return int(^uint(0) >> 1)
	}
	return n
}

func setIngestorRestartCondition(cr *enterpriseApi.IngestorCluster, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{
		Type:               string(enterpriseApi.ConditionRestarting),
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cr.GetGeneration(),
	})
}

func markIngestorRestartCompleteIfActive(cr *enterpriseApi.IngestorCluster) {
	existing := meta.FindStatusCondition(cr.Status.Conditions, string(enterpriseApi.ConditionRestarting))
	// Skip if condition is absent or already Complete — nothing to do.
	if existing == nil || existing.Status == metav1.ConditionFalse {
		return
	}
	setIngestorRestartCondition(cr, metav1.ConditionFalse,
		string(enterpriseApi.ReasonRollingRestartComplete),
		"All ingestor pods are ready and no restart is required")
}
