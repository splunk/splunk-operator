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
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splunkutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

const (
	panicLogTailLines = 5000
	logLineMaxLen     = 200
	// A CR this young may legitimately not have its PodDisruptionBudget or StatefulSet yet.
	youngCRGrace = 10 * time.Minute
	// The operator may not have reconciled older CRs yet right after the rollout, so the advisory
	// PDB check retries before it reports.
	fleetAdvisoryTimeout  = 60 * time.Second
	fleetAdvisoryInterval = 10 * time.Second
	// Both IndexerCluster and IngestorCluster controllers apply a PDB with this value.
	pdbMaxUnavailable = 1
)

var panicLogMarkers = []string{"panic:", "Observed a panic"}

// podRestartBaseline holds the restart count of every container of every operator pod that existed
// before the Helm upgrade, keyed by pod UID. nil means there was no earlier release (first install).
type podRestartBaseline map[types.UID]map[string]int32

type baselinePodFile struct {
	UID        string `json:"uid"`
	Containers []struct {
		Name         string `json:"name"`
		RestartCount int32  `json:"restartCount"`
	} `json:"containers"`
}

// loadOperatorPodBaseline reads the pre-upgrade pod list the deploy script wrote. An empty path
// means no baseline, so every restart counts.
func loadOperatorPodBaseline(path string) (podRestartBaseline, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read the operator pod baseline %s: %w", path, err)
	}
	var pods []baselinePodFile
	if err := json.Unmarshal(raw, &pods); err != nil {
		return nil, fmt.Errorf("operator pod baseline %s is not valid JSON: %w", path, err)
	}
	baseline := podRestartBaseline{}
	for _, p := range pods {
		counts := map[string]int32{}
		for _, c := range p.Containers {
			counts[c.Name] = c.RestartCount
		}
		baseline[types.UID(p.UID)] = counts
	}
	return baseline, nil
}

// evaluateOperatorPodRestarts fails on restarts that happened during this rollout. A pod that
// existed before the Helm upgrade may carry old restarts when the upgrade left its template alone,
// so only the growth over the baseline counts; a pod missing from the baseline is a replacement
// and every restart counts.
func evaluateOperatorPodRestarts(pods []corev1.Pod, baseline podRestartBaseline) error {
	live := 0
	var restarted []string
	for _, pod := range pods {
		if pod.DeletionTimestamp != nil {
			continue
		}
		live++
		before, existed := baseline[pod.UID]
		for _, cs := range pod.Status.ContainerStatuses {
			if detail, ok := containerRestartDetail(pod.Name, cs, before, existed); ok {
				restarted = append(restarted, detail)
			}
		}
	}
	if live == 0 {
		return fmt.Errorf("no live operator pods found to check for restarts; check that SCS_OPERATOR_NAMESPACE is the release namespace and that the operator Deployment has running pods")
	}
	if len(restarted) > 0 {
		return fmt.Errorf("operator container restarts since the rollout: %s", strings.Join(restarted, "; "))
	}
	return nil
}

// containerRestartDetail describes a container's restarts that count against this rollout, or
// reports false when it has none. before and existed come from the pre-upgrade baseline.
func containerRestartDetail(podName string, cs corev1.ContainerStatus, before map[string]int32, existed bool) (string, bool) {
	delta := cs.RestartCount
	detail := fmt.Sprintf("%s/%s restarted %d time(s)", podName, cs.Name, cs.RestartCount)
	if existed {
		delta = cs.RestartCount - before[cs.Name]
		detail = fmt.Sprintf("%s/%s restarted %d more time(s) since the pre-upgrade baseline (now %d, was %d)", podName, cs.Name, delta, cs.RestartCount, before[cs.Name])
	}
	if delta <= 0 {
		return "", false
	}
	if t := cs.LastTerminationState.Terminated; t != nil {
		detail += fmt.Sprintf(" (last exit: reason=%s exitCode=%d finishedAt=%s)", t.Reason, t.ExitCode, t.FinishedAt.UTC().Format(time.RFC3339))
	}
	if w := cs.State.Waiting; w != nil {
		detail += fmt.Sprintf(" (now waiting: %s)", w.Reason)
	}
	return detail, true
}

// findPanicLine returns the first log line that looks like a Go panic or a controller-runtime
// recovered panic, trimmed to a readable length.
func findPanicLine(logs string) (string, bool) {
	for _, line := range strings.Split(logs, "\n") {
		for _, marker := range panicLogMarkers {
			if strings.Contains(line, marker) {
				line = strings.TrimSpace(line)
				if len(line) > logLineMaxLen {
					line = line[:logLineMaxLen] + "..."
				}
				return line, true
			}
		}
	}
	return "", false
}

// podLogFetcher reads a container's recent logs; a non-nil since limits them to lines written after it.
type podLogFetcher func(ctx context.Context, namespace, pod, container string, since *metav1.Time) (string, error)

// parseUpgradeStart parses SCS_SANITY_UPGRADE_START_TIME. Empty means no cutoff (first install or a
// local run), which keeps the unbounded tail scan.
func parseUpgradeStart(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("SCS_SANITY_UPGRADE_START_TIME %q is not RFC3339 (the gate reads it from upgrade-start-time.txt in the evidence dir; fix that file or unset the variable): %w", raw, err)
	}
	return t.UTC(), nil
}

// panicScanSince picks the log window for one pod. A pod that survived the upgrade may carry panics
// recovered before this deployment, so only lines after the cutoff count; a pod created after the cutoff
// is a replacement whose whole log, startup included, belongs to this deployment.
func panicScanSince(pod corev1.Pod, cutoff time.Time) *metav1.Time {
	if cutoff.IsZero() || !pod.CreationTimestamp.Time.Before(cutoff) {
		return nil
	}
	since := metav1.NewTime(cutoff)
	return &since
}

// scanOperatorLogsForPanics reads recent logs for every container of every live operator pod.
// A pod that rolled away between listing and reading is skipped; any other read error is
// returned unchanged so callers can tell Forbidden apart.
func scanOperatorLogsForPanics(ctx context.Context, pods []corev1.Pod, fetch podLogFetcher, cutoff time.Time) error {
	var panics []string
	for _, pod := range pods {
		if pod.DeletionTimestamp != nil {
			continue
		}
		since := panicScanSince(pod, cutoff)
		for _, c := range pod.Spec.Containers {
			logs, err := fetch(ctx, pod.Namespace, pod.Name, c.Name, since)
			if apierrors.IsNotFound(err) {
				continue
			}
			if err != nil {
				return fmt.Errorf("failed to read logs of %s/%s container %s: %w", pod.Namespace, pod.Name, c.Name, err)
			}
			if line, found := findPanicLine(logs); found {
				panics = append(panics, fmt.Sprintf("%s/%s: %s", pod.Name, c.Name, line))
			}
		}
	}
	if len(panics) > 0 {
		return fmt.Errorf("panic found in operator logs: %s", strings.Join(panics, "; "))
	}
	return nil
}

func newPodLogFetcher() (podLogFetcher, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig for pod log reads: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build clientset for pod log reads: %w", err)
	}
	return func(ctx context.Context, namespace, pod, container string, since *metav1.Time) (string, error) {
		tail := int64(panicLogTailLines)
		raw, err := clientset.CoreV1().Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{Container: container, TailLines: &tail, SinceTime: since}).DoRaw(ctx)
		return string(raw), err
	}, nil
}

// listOperatorPods returns the pods selected by the operator Deployment.
func listOperatorPods(ctx context.Context, reader client.Reader, namespace, name string) ([]corev1.Pod, error) {
	dep := &appsv1.Deployment{}
	if err := reader.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, dep); err != nil {
		return nil, fmt.Errorf("failed to get operator Deployment %s/%s: %w", namespace, name, err)
	}
	selector, err := metav1.LabelSelectorAsSelector(dep.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("operator Deployment %s/%s has an invalid selector: %w", namespace, name, err)
	}
	var list corev1.PodList
	if err := reader.List(ctx, &list, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, fmt.Errorf("failed to list operator pods in %s: %w", namespace, err)
	}
	return list.Items, nil
}

// listCRDs reads CRDs as unstructured so the suite needs no apiextensions scheme registration.
func listCRDs(ctx context.Context, reader client.Reader) ([]apiextensionsv1.CustomResourceDefinition, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinitionList"})
	if err := reader.List(ctx, list); err != nil {
		return nil, fmt.Errorf("failed to list CustomResourceDefinitions: %w", err)
	}
	crds := make([]apiextensionsv1.CustomResourceDefinition, 0, len(list.Items))
	for _, item := range list.Items {
		var crd apiextensionsv1.CustomResourceDefinition
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &crd); err != nil {
			return nil, fmt.Errorf("failed to convert CRD %s: %w", item.GetName(), err)
		}
		crds = append(crds, crd)
	}
	return crds, nil
}

// evaluateCRDsEstablished requires at least one enterprise.splunk.com CRD and an Established
// condition of True on every CRD in the enterprise and platform groups. The platform group is
// checked only when its CRDs are installed.
func evaluateCRDsEstablished(crds []apiextensionsv1.CustomResourceDefinition) error {
	sokGroups := map[string]bool{
		enterpriseApi.GroupVersion.Group: true,
		platformApi.GroupVersion.Group:   true,
	}
	enterpriseFound := 0
	var problems []string
	for _, crd := range crds {
		if !sokGroups[crd.Spec.Group] {
			continue
		}
		if crd.Spec.Group == enterpriseApi.GroupVersion.Group {
			enterpriseFound++
		}
		established := false
		message := "no Established condition reported"
		for _, cond := range crd.Status.Conditions {
			if cond.Type != apiextensionsv1.Established {
				continue
			}
			established = cond.Status == apiextensionsv1.ConditionTrue
			message = fmt.Sprintf("Established=%s reason=%s", cond.Status, cond.Reason)
		}
		if !established {
			problems = append(problems, fmt.Sprintf("%s: %s", crd.Name, message))
		}
	}
	if enterpriseFound == 0 {
		return fmt.Errorf("no CustomResourceDefinitions found in group %s; check that the apply target CRDs step succeeded in the job log", enterpriseApi.GroupVersion.Group)
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("SOK CRDs not Established: %s", strings.Join(problems, "; "))
	}
	return nil
}

// sokCR is the slice of an IndexerCluster or IngestorCluster the PDB check needs.
type sokCR struct {
	Kind      string
	Namespace string
	Name      string
	UID       types.UID
	Instance  string
}

func (c sokCR) String() string { return fmt.Sprintf("%s %s/%s", c.Kind, c.Namespace, c.Name) }

// pdbProblems checks one CR's PodDisruptionBudget against what the controllers create: named
// after the StatefulSet, owned by the CR, maxUnavailable of 1, and selecting the StatefulSet's pods.
func pdbProblems(cr sokCR, pdb *policyv1.PodDisruptionBudget, sts *appsv1.StatefulSet) []string {
	if pdb == nil {
		return []string{fmt.Sprintf("%s: PodDisruptionBudget %s is missing", cr, cr.Instance)}
	}
	var problems []string
	owned := false
	for _, ref := range pdb.OwnerReferences {
		if ref.UID == cr.UID && ref.Kind == cr.Kind {
			owned = true
		}
	}
	if !owned {
		problems = append(problems, fmt.Sprintf("%s: PodDisruptionBudget %s is not owned by the CR", cr, pdb.Name))
	}
	if mu := pdb.Spec.MaxUnavailable; mu == nil || mu.Type != intstr.Int || mu.IntVal != pdbMaxUnavailable {
		got := "unset"
		if mu != nil {
			got = mu.String()
		}
		problems = append(problems, fmt.Sprintf("%s: PodDisruptionBudget %s maxUnavailable is %s, expected %d", cr, pdb.Name, got, pdbMaxUnavailable))
	}
	selector, err := metav1.LabelSelectorAsSelector(pdb.Spec.Selector)
	switch {
	case err != nil || pdb.Spec.Selector == nil || (len(pdb.Spec.Selector.MatchLabels) == 0 && len(pdb.Spec.Selector.MatchExpressions) == 0):
		problems = append(problems, fmt.Sprintf("%s: PodDisruptionBudget %s has no usable selector", cr, pdb.Name))
	case sts == nil:
		problems = append(problems, fmt.Sprintf("%s: StatefulSet %s is missing, so the PodDisruptionBudget selector cannot be confirmed", cr, cr.Instance))
	case !selector.Matches(labels.Set(sts.Spec.Template.Labels)):
		problems = append(problems, fmt.Sprintf("%s: PodDisruptionBudget %s selector does not match the pods of StatefulSet %s", cr, pdb.Name, sts.Name))
	}
	return problems
}

type pdbFleetResult struct {
	Checked    int
	Problems   []string
	SkipReason string
}

func skippableAccessError(err error) bool {
	return apierrors.IsForbidden(err) || meta.IsNoMatchError(err)
}

// checkPDBFleet verifies the PodDisruptionBudget of every IndexerCluster and IngestorCluster the
// caller can see, skipping deleting, paused and very young CRs. Missing access or an uninstalled
// CRD yields a SkipReason, not an error.
func checkPDBFleet(ctx context.Context, reader client.Reader, now time.Time) (pdbFleetResult, error) {
	var indexers enterpriseApi.IndexerClusterList
	if err := reader.List(ctx, &indexers); err != nil {
		return skipOrFail(err, "IndexerClusters")
	}
	var ingestors enterpriseApi.IngestorClusterList
	if err := reader.List(ctx, &ingestors); err != nil {
		return skipOrFail(err, "IngestorClusters")
	}

	var crs []sokCR
	add := func(kind string, obj metav1.Object, instanceType splcommon.InstanceType, pausedAnnotation string) {
		if obj.GetDeletionTimestamp() != nil || now.Sub(obj.GetCreationTimestamp().Time) < youngCRGrace {
			return
		}
		// A paused IndexerCluster never reaches the PDB step, so a missing PDB is not a regression.
		if obj.GetAnnotations()[pausedAnnotation] == "true" {
			return
		}
		crs = append(crs, sokCR{
			Kind: kind, Namespace: obj.GetNamespace(), Name: obj.GetName(), UID: obj.GetUID(),
			Instance: splunkutil.GetSplunkStatefulsetName(instanceType, obj.GetName()),
		})
	}
	for i := range indexers.Items {
		add("IndexerCluster", &indexers.Items[i], splcommon.SplunkIndexer, enterpriseApi.IndexerClusterPausedAnnotation)
	}
	for i := range ingestors.Items {
		add("IngestorCluster", &ingestors.Items[i], splcommon.SplunkIngestor, enterpriseApi.IngestorClusterPausedAnnotation)
	}

	result := pdbFleetResult{}
	for _, cr := range crs {
		key := client.ObjectKey{Namespace: cr.Namespace, Name: cr.Instance}
		var pdb *policyv1.PodDisruptionBudget
		var sts *appsv1.StatefulSet
		got := &policyv1.PodDisruptionBudget{}
		switch err := reader.Get(ctx, key, got); {
		case err == nil:
			pdb = got
		case apierrors.IsNotFound(err):
		case skippableAccessError(err):
			return pdbFleetResult{SkipReason: fmt.Sprintf("cannot read PodDisruptionBudgets: %v", err)}, nil
		default:
			return result, fmt.Errorf("failed to get PodDisruptionBudget %s: %w", key, err)
		}
		gotSts := &appsv1.StatefulSet{}
		switch err := reader.Get(ctx, key, gotSts); {
		case err == nil:
			sts = gotSts
		case apierrors.IsNotFound(err):
		case skippableAccessError(err):
			return pdbFleetResult{SkipReason: fmt.Sprintf("cannot read StatefulSets: %v", err)}, nil
		default:
			return result, fmt.Errorf("failed to get StatefulSet %s: %w", key, err)
		}
		result.Checked++
		result.Problems = append(result.Problems, pdbProblems(cr, pdb, sts)...)
	}
	return result, nil
}

// pdbAdvisory is the outcome of the advisory fleet PDB check, which never fails the gate.
type pdbAdvisory struct {
	Checked  int
	Skip     string
	Warnings []string
}

// adviseOnPDBFleet retries checkPDBFleet while PDB problems persist and returns whatever remains as
// warnings, so a slow reconcile of an old CR after the upgrade is not reported as a regression.
func adviseOnPDBFleet(ctx context.Context, reader client.Reader, timeout, interval time.Duration) pdbAdvisory {
	var result pdbFleetResult
	err := retryUntil(ctx, timeout, interval, func(ctx context.Context) error {
		var checkErr error
		result, checkErr = checkPDBFleet(ctx, reader, time.Now())
		if checkErr != nil {
			return &terminalError{err: checkErr}
		}
		if result.SkipReason == "" && len(result.Problems) > 0 {
			return fmt.Errorf("%s", strings.Join(result.Problems, "; "))
		}
		return nil
	})
	switch {
	case result.SkipReason != "":
		return pdbAdvisory{Skip: result.SkipReason}
	case err != nil && len(result.Problems) > 0:
		return pdbAdvisory{Checked: result.Checked, Warnings: result.Problems}
	case err != nil:
		return pdbAdvisory{Warnings: []string{err.Error()}}
	}
	return pdbAdvisory{Checked: result.Checked}
}

func skipOrFail(err error, what string) (pdbFleetResult, error) {
	if skippableAccessError(err) {
		return pdbFleetResult{SkipReason: fmt.Sprintf("cannot list %s: %v", what, err)}, nil
	}
	return pdbFleetResult{}, fmt.Errorf("failed to list %s: %w", what, err)
}
