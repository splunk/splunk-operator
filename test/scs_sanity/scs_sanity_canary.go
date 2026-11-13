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
	"regexp"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splunkutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

const (
	canaryLabelKey   = "scs-sanity.splunk.com/canary"
	canaryNamePrefix = "scs-sanity-canary-"
	instanceLabelKey = "app.kubernetes.io/instance"
	// About twice the p90 of 27 develop S1 smoke runs on EKS (172s with a cold image pull); the scale
	// waits are estimates because no Standalone scale timing was measured.
	defaultCanaryReadyTimeout     = 6 * time.Minute
	defaultCanaryScaleUpTimeout   = 5 * time.Minute
	defaultCanaryScaleDownTimeout = 3 * time.Minute
	// Cleanup in total is canaryCleanupTimeout plus canaryPVCSweepTimeout (3m); the PVC sweep waits for pods to go.
	canaryCleanupTimeout  = 2 * time.Minute
	canaryPVCSweepTimeout = 1 * time.Minute
	canaryPollInterval    = 5 * time.Second
	// A live run never outlasts the gate timeout, so anything older than this was abandoned.
	minStaleCanaryAge = 3 * time.Hour
	maxWarningEvents  = 10
	// A pod can sit Unschedulable briefly while the autoscaler adds a node.
	unschedulableGrace = 3 * time.Minute
	environmentPrefix  = "ENVIRONMENT problem, not an operator regression: "
)

var canaryRunIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]{2,7}$`)

var terminalWaitingReasons = map[string]bool{
	"ImagePullBackOff": true,
	"InvalidImageName": true,
}

// These show up briefly during a registry blip or while a Secret is still being created.
var gracedWaitingReasons = map[string]bool{
	"ErrImagePull":               true,
	"CreateContainerConfigError": true,
}

const gracedWaitingPeriod = time.Minute

// waitTracker is the state of one wait step: when it began, and since when each graced waiting
// reason has been observed without interruption.
type waitTracker struct {
	start     time.Time
	firstSeen map[string]time.Time
}

func newWaitTracker(start time.Time) *waitTracker {
	return &waitTracker{start: start, firstSeen: map[string]time.Time{}}
}

type canaryConfig struct {
	Enabled          bool
	Namespace        string
	ReadyTimeout     time.Duration
	ScaleUpTimeout   time.Duration
	ScaleDownTimeout time.Duration
}

// canaryCleanupBudget is the total time one cleanup may take.
const canaryCleanupBudget = canaryCleanupTimeout + canaryPVCSweepTimeout

func parseCanaryEnabled(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	}
	return false, fmt.Errorf("SCS_SANITY_CANARY_ENABLED must be true or false, got %q; fix the CI/CD variable or unset it", raw)
}

func parseCanaryDuration(name, raw string, fallback time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive Go duration such as 6m, got %q", name, raw)
	}
	return d, nil
}

// loadCanaryConfig reads the canary settings; nothing else is validated while the canary is off.
func loadCanaryConfig(getenv func(string) string, defaultNamespace string) (canaryConfig, error) {
	enabled, err := parseCanaryEnabled(getenv("SCS_SANITY_CANARY_ENABLED"))
	if err != nil || !enabled {
		return canaryConfig{}, err
	}
	ready, err := parseCanaryDuration("SCS_SANITY_CANARY_TIMEOUT", getenv("SCS_SANITY_CANARY_TIMEOUT"), defaultCanaryReadyTimeout)
	if err != nil {
		return canaryConfig{}, err
	}
	up, err := parseCanaryDuration("SCS_SANITY_CANARY_SCALE_UP_TIMEOUT", getenv("SCS_SANITY_CANARY_SCALE_UP_TIMEOUT"), defaultCanaryScaleUpTimeout)
	if err != nil {
		return canaryConfig{}, err
	}
	down, err := parseCanaryDuration("SCS_SANITY_CANARY_SCALE_DOWN_TIMEOUT", getenv("SCS_SANITY_CANARY_SCALE_DOWN_TIMEOUT"), defaultCanaryScaleDownTimeout)
	if err != nil {
		return canaryConfig{}, err
	}
	namespace := strings.TrimSpace(getenv("SCS_SANITY_CANARY_NAMESPACE"))
	if namespace == "" {
		namespace = defaultNamespace
	}
	if namespace == "" {
		return canaryConfig{}, errors.New("SCS_SANITY_CANARY_NAMESPACE is empty and there is no default namespace; set it to an existing namespace the deployer can create the canary CRs in")
	}
	return canaryConfig{Enabled: true, Namespace: namespace, ReadyTimeout: ready, ScaleUpTimeout: up, ScaleDownTimeout: down}, nil
}

type canaryNames struct {
	RunID      string
	Standalone string
}

func newCanaryNames(runID string) (canaryNames, error) {
	if !canaryRunIDPattern.MatchString(runID) {
		return canaryNames{}, fmt.Errorf("canary run id %q must match %s; the gate generates the run id itself, so this is a bug in the gate: file an issue against test/scs_sanity", runID, canaryRunIDPattern)
	}
	return canaryNames{
		RunID:      runID,
		Standalone: canaryNamePrefix + runID + "-sa",
	}, nil
}

func (n canaryNames) Labels() map[string]string {
	return map[string]string{canaryLabelKey: n.RunID}
}

func (n canaryNames) StandaloneInstance() string {
	return splunkutil.GetSplunkStatefulsetName(splcommon.SplunkStandalone, n.Standalone)
}

// Explicit resources keep the pod inside a namespace LimitRange; the operator defaults (limit 40x the
// request on CPU, 16x on memory) were rejected on a live cluster whose max limit/request ratio is 16 and 8.
func canaryResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:              resource.MustParse("500m"),
			corev1.ResourceMemory:           resource.MustParse("2Gi"),
			corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:              resource.MustParse("2"),
			corev1.ResourceMemory:           resource.MustParse("4Gi"),
			corev1.ResourceEphemeralStorage: resource.MustParse("4Gi"),
		},
	}
}

// The default Splunk image is used on purpose: the operator treats a failed image pull as terminal.
// No delete-pvc finalizer: a spec-validation failure returns before the operator's deletion handling and would wedge the CR.
func newCanaryStandalone(namespace string, n canaryNames, replicas int32) *enterpriseApi.Standalone {
	return &enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{Name: n.Standalone, Namespace: namespace, Labels: n.Labels()},
		Spec: enterpriseApi.StandaloneSpec{
			Replicas:         replicas,
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{Resources: canaryResources()},
		},
	}
}

func canaryOwned(obj metav1.Object, runID string) bool {
	return obj.GetLabels()[canaryLabelKey] == runID && strings.HasPrefix(obj.GetName(), canaryNamePrefix)
}

// staleCanary matches leftovers of an earlier run whose cleanup never ran, such as a cancelled job.
func staleCanary(obj metav1.Object, now time.Time, maxAge time.Duration) bool {
	runID, labelled := obj.GetLabels()[canaryLabelKey]
	if !labelled || !canaryRunIDPattern.MatchString(runID) || !strings.HasPrefix(obj.GetName(), canaryNamePrefix+runID+"-") {
		return false
	}
	return now.Sub(obj.GetCreationTimestamp().Time) >= maxAge
}

// staleCanaryMaxAge keeps the stale threshold above the suite timeout so a retried job never
// deletes the live canary of a run that is still going.
func staleCanaryMaxAge(getenv func(string) string) time.Duration {
	suite, err := parseSuiteTimeout(getenv("SCS_SANITY_SUITE_TIMEOUT"), 0)
	if err != nil || suite+time.Hour < minStaleCanaryAge {
		return minStaleCanaryAge
	}
	return suite + time.Hour
}

func pvcBelongsToInstance(pvcName, instance string) bool {
	return regexp.MustCompile(`^pvc-[a-z]+-` + regexp.QuoteMeta(instance) + `-[0-9]+$`).MatchString(pvcName)
}

var canaryInstanceLabelPattern = regexp.MustCompile(`^splunk-` + regexp.QuoteMeta(canaryNamePrefix) + `([a-z][a-z0-9]{2,7})-(sa-standalone)$`)

// staleCanaryOrphan matches a pod or PVC of an earlier run that outlived its CR. These carry only
// the operator's instance label, not the canary run label, so the label value, the object name and
// the age must all agree before anything is deleted.
func staleCanaryOrphan(obj metav1.Object, now time.Time, maxAge time.Duration) bool {
	instance := obj.GetLabels()[instanceLabelKey]
	if !canaryInstanceLabelPattern.MatchString(instance) {
		return false
	}
	if !pvcBelongsToInstance(obj.GetName(), instance) && !podBelongsToInstance(obj.GetName(), instance) {
		return false
	}
	return now.Sub(obj.GetCreationTimestamp().Time) >= maxAge
}

func podBelongsToInstance(podName, instance string) bool {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(instance) + `-[0-9]+$`).MatchString(podName)
}

func describeCreateError(err error, resource, namespace string) error {
	if apierrors.IsForbidden(err) {
		return fmt.Errorf("deploy identity is missing RBAC verb 'create' on %s in namespace %s (the platform team must extend the deployer role grants in cd.splunkdev.com/kub/namespaces): %w", resource, namespace, err)
	}
	return fmt.Errorf("failed to create %s in namespace %s: %w", resource, namespace, err)
}

// standaloneReplicaProblem is nil only when the operator reports the Standalone Ready at want replicas.
func standaloneReplicaProblem(sa *enterpriseApi.Standalone, sts *appsv1.StatefulSet, want int32) error {
	if sa.Status.Phase == enterpriseApi.PhaseError && meta.IsStatusConditionTrue(sa.Status.Conditions, string(enterpriseApi.ConditionStalled)) {
		return &terminalError{err: fmt.Errorf("canary Standalone %s is stalled in phase %s and will not recover on its own", sa.Name, sa.Status.Phase)}
	}
	switch {
	case sts == nil:
		return fmt.Errorf("StatefulSet for canary Standalone %s not created yet (phase %q)", sa.Name, sa.Status.Phase)
	case sts.Spec.Replicas == nil || *sts.Spec.Replicas != want:
		return fmt.Errorf("StatefulSet %s spec.replicas is not %d yet", sts.Name, want)
	case sts.Status.ReadyReplicas != want:
		return fmt.Errorf("StatefulSet %s has %d of %d replicas ready", sts.Name, sts.Status.ReadyReplicas, want)
	case sa.Status.ObservedGeneration < sa.Generation:
		return fmt.Errorf("operator has not observed generation %d of Standalone %s yet (observed %d)", sa.Generation, sa.Name, sa.Status.ObservedGeneration)
	case sa.Status.Phase != enterpriseApi.PhaseReady:
		return fmt.Errorf("canary Standalone %s is in phase %q, want %s", sa.Name, sa.Status.Phase, enterpriseApi.PhaseReady)
	case sa.Status.ReadyReplicas != want:
		return fmt.Errorf("canary Standalone %s reports %d ready replicas, want %d", sa.Name, sa.Status.ReadyReplicas, want)
	}
	return nil
}

// canaryEnvironmentProblem reports pod and event conditions that no amount of waiting fixes and
// that point at the cluster, not the operator: an unpullable image, an unschedulable pod, an
// exhausted quota or a pod the cluster refuses to admit. The result is a terminalError so the wait stops.
func canaryEnvironmentProblem(pods []corev1.Pod, events []corev1.Event, now time.Time, tracker *waitTracker) error {
	var problems []string
	observed := map[string]bool{}
	for _, pod := range pods {
		problems = append(problems, containerWaitingProblems(pod, now, tracker, observed)...)
		problems = append(problems, unschedulableProblems(pod, now)...)
	}
	problems = append(problems, admissionRejectionProblems(pods, events, now, tracker, observed)...)
	forgetUnobserved(tracker, observed)
	problems = append(problems, quotaEventProblems(events, tracker.start)...)
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return &terminalError{err: errors.New(environmentPrefix + strings.Join(problems, "; "))}
}

// containerWaitingProblems flags containers stuck in a terminal waiting reason at once, and in a
// transient one only after it has been observed continuously for gracedWaitingPeriod.
func containerWaitingProblems(pod corev1.Pod, now time.Time, tracker *waitTracker, observed map[string]bool) []string {
	var problems []string
	statuses := append(append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...)
	for _, cs := range statuses {
		w := cs.State.Waiting
		if w == nil {
			continue
		}
		if terminalWaitingReasons[w.Reason] {
			problems = append(problems, fmt.Sprintf("pod %s container %s is %s: %s", pod.Name, cs.Name, w.Reason, w.Message))
			continue
		}
		if !gracedWaitingReasons[w.Reason] {
			continue
		}
		key := pod.Name + "/" + cs.Name + "/" + w.Reason
		observed[key] = true
		first, seen := tracker.firstSeen[key]
		if !seen {
			tracker.firstSeen[key], first = now, now
		}
		if now.Sub(first) >= gracedWaitingPeriod {
			problems = append(problems, fmt.Sprintf("pod %s container %s has been %s for %s: %s", pod.Name, cs.Name, w.Reason, now.Sub(first).Round(time.Second), w.Message))
		}
	}
	return problems
}

func unschedulableProblems(pod corev1.Pod, now time.Time) []string {
	var problems []string
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse && cond.Reason == corev1.PodReasonUnschedulable &&
			now.Sub(pod.CreationTimestamp.Time) >= unschedulableGrace {
			problems = append(problems, fmt.Sprintf("pod %s is Unschedulable: %s", pod.Name, cond.Message))
		}
	}
	return problems
}

// forgetUnobserved drops grace timers for conditions that cleared, so a recurrence starts a new period.
func forgetUnobserved(tracker *waitTracker, observed map[string]bool) {
	for key := range tracker.firstSeen {
		if !observed[key] {
			delete(tracker.firstSeen, key)
		}
	}
}

// admissionRejectionProblems flags a StatefulSet whose pod creation keeps being refused (LimitRange,
// admission policy) while no pod exists yet. It reports only after the refusal has been seen for
// gracedWaitingPeriod, since a webhook can fail once and succeed on the controller's retry.
func admissionRejectionProblems(pods []corev1.Pod, events []corev1.Event, now time.Time, tracker *waitTracker, observed map[string]bool) []string {
	var problems []string
	for _, ev := range events {
		if ev.Type != corev1.EventTypeWarning || ev.Reason != "FailedCreate" || !strings.Contains(ev.Message, "is forbidden") ||
			strings.Contains(ev.Message, "exceeded quota") || !eventTime(ev).After(tracker.start) {
			continue
		}
		if podExistsFor(pods, ev.InvolvedObject.Name) {
			continue
		}
		key := "admission/" + ev.InvolvedObject.Name
		observed[key] = true
		first, seen := tracker.firstSeen[key]
		if !seen {
			tracker.firstSeen[key], first = now, now
		}
		if now.Sub(first) >= gracedWaitingPeriod {
			problems = append(problems, fmt.Sprintf("the cluster rejected the canary pod for %s for %s (likely a namespace LimitRange ratio or max, a ResourceQuota or an admission policy): %s",
				ev.InvolvedObject.Name, now.Sub(first).Round(time.Second), ev.Message))
		}
	}
	return problems
}

func podExistsFor(pods []corev1.Pod, statefulSet string) bool {
	for _, pod := range pods {
		if strings.HasPrefix(pod.Name, statefulSet+"-") {
			return true
		}
	}
	return false
}

func quotaEventProblems(events []corev1.Event, since time.Time) []string {
	var problems []string
	for _, ev := range events {
		if ev.Type == corev1.EventTypeWarning && ev.Reason == "FailedCreate" && strings.Contains(ev.Message, "exceeded quota") && eventTime(ev).After(since) {
			problems = append(problems, fmt.Sprintf("%s hit a resource quota: %s", ev.InvolvedObject.Name, ev.Message))
		}
	}
	return problems
}

func eventTime(ev corev1.Event) time.Time {
	switch {
	case !ev.LastTimestamp.IsZero():
		return ev.LastTimestamp.Time
	case !ev.EventTime.IsZero():
		return ev.EventTime.Time
	}
	return ev.CreationTimestamp.Time
}

// recentWarningEvents lists the newest Warning events whose object name starts with one of prefixes.
func recentWarningEvents(events []corev1.Event, prefixes []string, limit int) []string {
	var matched []corev1.Event
	for _, ev := range events {
		if ev.Type != corev1.EventTypeWarning {
			continue
		}
		for _, prefix := range prefixes {
			if strings.HasPrefix(ev.InvolvedObject.Name, prefix) {
				matched = append(matched, ev)
				break
			}
		}
	}
	sort.SliceStable(matched, func(i, j int) bool { return eventTime(matched[i]).After(eventTime(matched[j])) })
	if len(matched) > limit {
		matched = matched[:limit]
	}
	lines := make([]string, 0, len(matched))
	for _, ev := range matched {
		lines = append(lines, fmt.Sprintf("%s %s: %s (x%d)", ev.InvolvedObject.Name, ev.Reason, ev.Message, ev.Count))
	}
	return lines
}

func describeStandalone(sa *enterpriseApi.Standalone, sts *appsv1.StatefulSet, warnings []string) string {
	var b strings.Builder
	if sa == nil {
		b.WriteString("Standalone: not found\n")
	} else {
		fmt.Fprintf(&b, "Standalone %s: phase=%q replicas=%d readyReplicas=%d generation=%d observed=%d\n",
			sa.Name, sa.Status.Phase, sa.Status.Replicas, sa.Status.ReadyReplicas, sa.Generation, sa.Status.ObservedGeneration)
		for _, c := range sa.Status.Conditions {
			fmt.Fprintf(&b, "  condition %s=%s reason=%s: %s\n", c.Type, c.Status, c.Reason, c.Message)
		}
	}
	if sts == nil {
		b.WriteString("StatefulSet: not found\n")
	} else {
		desired := int32(0)
		if sts.Spec.Replicas != nil {
			desired = *sts.Spec.Replicas
		}
		fmt.Fprintf(&b, "StatefulSet %s: spec.replicas=%d ready=%d current=%d\n", sts.Name, desired, sts.Status.ReadyReplicas, sts.Status.CurrentReplicas)
	}
	if len(warnings) == 0 {
		b.WriteString("Warning events: none\n")
	}
	for _, w := range warnings {
		fmt.Fprintf(&b, "Warning event: %s\n", w)
	}
	return b.String()
}

// canaryEnv writes through the live client and reads uncached, because the shared cache would list
// and watch cluster-wide, which a namespace-scoped deploy identity is forbidden from.
type canaryEnv struct {
	Writer    client.Client
	Reader    client.Reader
	Namespace string
	// Poll overrides canaryPollInterval for cleanup waits; unit tests shorten it.
	Poll time.Duration
}

func (e canaryEnv) poll() time.Duration {
	if e.Poll > 0 {
		return e.Poll
	}
	return canaryPollInterval
}

func (e canaryEnv) create(ctx context.Context, obj client.Object, resource string) error {
	if err := e.Writer.Create(ctx, obj); err != nil {
		return describeCreateError(err, resource, e.Namespace)
	}
	return nil
}

func (e canaryEnv) getStandalone(ctx context.Context, n canaryNames) (*enterpriseApi.Standalone, *appsv1.StatefulSet, error) {
	sa := &enterpriseApi.Standalone{}
	if err := e.Reader.Get(ctx, client.ObjectKey{Namespace: e.Namespace, Name: n.Standalone}, sa); err != nil {
		return nil, nil, fmt.Errorf("failed to get canary Standalone %s/%s: %w", e.Namespace, n.Standalone, err)
	}
	sts := &appsv1.StatefulSet{}
	err := e.Reader.Get(ctx, client.ObjectKey{Namespace: e.Namespace, Name: n.StandaloneInstance()}, sts)
	switch {
	case apierrors.IsNotFound(err):
		return sa, nil, nil
	case err != nil:
		return sa, nil, fmt.Errorf("failed to get StatefulSet %s/%s: %w", e.Namespace, n.StandaloneInstance(), err)
	}
	return sa, sts, nil
}

func (e canaryEnv) standaloneAt(ctx context.Context, n canaryNames, want int32, tracker *waitTracker) error {
	sa, sts, err := e.getStandalone(ctx, n)
	if err != nil {
		return err
	}
	problem := standaloneReplicaProblem(sa, sts, want)
	if problem == nil {
		return nil
	}
	if envProblem := e.environmentProblem(ctx, tracker, n.StandaloneInstance()); envProblem != nil {
		return envProblem
	}
	return problem
}

func instanceSelector(instances ...string) (labels.Selector, error) {
	requirement, err := labels.NewRequirement(instanceLabelKey, selection.In, instances)
	if err != nil {
		return nil, err
	}
	return labels.NewSelector().Add(*requirement), nil
}

// environmentProblem inspects the pods and Warning events of the given instances. It never fails the
// wait on its own read errors, since this is only a better explanation for a wait that is failing.
func (e canaryEnv) environmentProblem(ctx context.Context, tracker *waitTracker, instances ...string) error {
	selector, err := instanceSelector(instances...)
	if err != nil {
		return nil
	}
	var pods corev1.PodList
	if err := e.Reader.List(ctx, &pods, client.InNamespace(e.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil
	}
	var events corev1.EventList
	if err := e.Reader.List(ctx, &events, client.InNamespace(e.Namespace)); err != nil {
		events = corev1.EventList{}
	}
	var relevant []corev1.Event
	for _, ev := range events.Items {
		for _, instance := range instances {
			if strings.HasPrefix(ev.InvolvedObject.Name, instance) {
				relevant = append(relevant, ev)
				break
			}
		}
	}
	return canaryEnvironmentProblem(pods.Items, relevant, time.Now(), tracker)
}

func (e canaryEnv) scaleStandalone(ctx context.Context, n canaryNames, replicas int32) error {
	sa := &enterpriseApi.Standalone{}
	if err := e.Reader.Get(ctx, client.ObjectKey{Namespace: e.Namespace, Name: n.Standalone}, sa); err != nil {
		return fmt.Errorf("failed to get canary Standalone %s/%s: %w", e.Namespace, n.Standalone, err)
	}
	patch := client.MergeFrom(sa.DeepCopy())
	sa.Spec.Replicas = replicas
	if err := e.Writer.Patch(ctx, sa, patch); err != nil {
		return fmt.Errorf("failed to scale canary Standalone %s/%s to %d: %w", e.Namespace, n.Standalone, replicas, err)
	}
	return nil
}

func (e canaryEnv) diagnoseStandalone(ctx context.Context, n canaryNames) string {
	sa, sts, err := e.getStandalone(ctx, n)
	var warnings []string
	var events corev1.EventList
	if listErr := e.Reader.List(ctx, &events, client.InNamespace(e.Namespace)); listErr == nil {
		warnings = recentWarningEvents(events.Items, []string{n.Standalone, n.StandaloneInstance()}, maxWarningEvents)
	} else {
		warnings = []string{fmt.Sprintf("could not list events: %v", listErr)}
	}
	text := describeStandalone(sa, sts, warnings)
	if err != nil {
		text += fmt.Sprintf("lookup error: %v\n", err)
	}
	return text
}

func ignoreNotFound(err error) error {
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// sweepStale deletes canary CRs left behind by an earlier run, then their orphaned pods and PVCs. A
// forbidden or uninstalled kind is skipped, and callers treat the returned error as a warning.
func (e canaryEnv) sweepStale(ctx context.Context, now time.Time, maxAge time.Duration) error {
	requirement, err := labels.NewRequirement(canaryLabelKey, selection.Exists, nil)
	if err != nil {
		return err
	}
	opts := []client.ListOption{client.InNamespace(e.Namespace), client.MatchingLabelsSelector{Selector: labels.NewSelector().Add(*requirement)}}
	var errs []error

	var standalones enterpriseApi.StandaloneList
	if err := e.Reader.List(ctx, &standalones, opts...); err != nil && !skippableAccessError(err) {
		errs = append(errs, fmt.Errorf("failed to list canary Standalones: %w", err))
	}
	for i := range standalones.Items {
		if staleCanary(&standalones.Items[i], now, maxAge) {
			errs = append(errs, ignoreNotFound(e.Writer.Delete(ctx, &standalones.Items[i])))
		}
	}

	return errors.Join(append(errs, e.sweepStaleOrphans(ctx, now, maxAge))...)
}

// sweepStaleOrphans deletes pods and PVCs an abandoned run left behind, including ones whose CR is
// already gone, because the operator's PVC finalizer can no longer reclaim those.
func (e canaryEnv) sweepStaleOrphans(ctx context.Context, now time.Time, maxAge time.Duration) error {
	requirement, err := labels.NewRequirement(instanceLabelKey, selection.Exists, nil)
	if err != nil {
		return err
	}
	opts := []client.ListOption{client.InNamespace(e.Namespace), client.MatchingLabelsSelector{Selector: labels.NewSelector().Add(*requirement)}}
	var errs []error

	var pvcs corev1.PersistentVolumeClaimList
	if err := e.Reader.List(ctx, &pvcs, opts...); err != nil && !skippableAccessError(err) {
		errs = append(errs, fmt.Errorf("failed to list PVCs of abandoned canary runs: %w", err))
	}
	for i := range pvcs.Items {
		if staleCanaryOrphan(&pvcs.Items[i], now, maxAge) {
			errs = append(errs, ignoreNotFound(e.Writer.Delete(ctx, &pvcs.Items[i])))
		}
	}

	var pods corev1.PodList
	if err := e.Reader.List(ctx, &pods, opts...); err != nil && !skippableAccessError(err) {
		errs = append(errs, fmt.Errorf("failed to list pods of abandoned canary runs: %w", err))
	}
	for i := range pods.Items {
		if staleCanaryOrphan(&pods.Items[i], now, maxAge) {
			errs = append(errs, ignoreNotFound(e.Writer.Delete(ctx, &pods.Items[i])))
		}
	}
	return errors.Join(errs...)
}

func (e canaryEnv) deleteIfOwned(ctx context.Context, obj client.Object, n canaryNames) error {
	if err := e.Reader.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
		return ignoreNotFound(err)
	}
	if !canaryOwned(obj, n.RunID) {
		return fmt.Errorf("refusing to delete %s/%s: it is not labelled as canary run %s", obj.GetNamespace(), obj.GetName(), n.RunID)
	}
	return ignoreNotFound(e.Writer.Delete(ctx, obj))
}

func (e canaryEnv) allGone(ctx context.Context, n canaryNames) error {
	checks := []struct {
		kind string
		obj  client.Object
		name string
	}{
		{"Standalone", &enterpriseApi.Standalone{}, n.Standalone},
		{"StatefulSet", &appsv1.StatefulSet{}, n.StandaloneInstance()},
	}
	var remaining []string
	if selector, err := instanceSelector(n.StandaloneInstance()); err == nil {
		var pods corev1.PodList
		if err := e.Reader.List(ctx, &pods, client.InNamespace(e.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return fmt.Errorf("failed to confirm the canary pods are gone: %w", err)
		}
		for _, pod := range pods.Items {
			remaining = append(remaining, "Pod/"+pod.Name)
		}
	}
	for _, c := range checks {
		err := e.Reader.Get(ctx, client.ObjectKey{Namespace: e.Namespace, Name: c.name}, c.obj)
		switch {
		case apierrors.IsNotFound(err):
		case err != nil:
			return fmt.Errorf("failed to confirm %s %s is gone: %w", c.kind, c.name, err)
		default:
			remaining = append(remaining, c.kind+"/"+c.name)
		}
	}
	if len(remaining) > 0 {
		return fmt.Errorf("canary resources still present: %s", strings.Join(remaining, ", "))
	}
	return nil
}

// cleanup deletes only objects labelled with this run, waits for them and their pods to disappear,
// then sweeps leftover PVCs. PVC problems come back as warnings because the operator's finalizer
// normally owns them.
func (e canaryEnv) cleanup(ctx context.Context, n canaryNames) (warnings []string, err error) {
	err = e.deleteIfOwned(ctx, &enterpriseApi.Standalone{ObjectMeta: metav1.ObjectMeta{Namespace: e.Namespace, Name: n.Standalone}}, n)
	if err != nil {
		return nil, err
	}
	if err := retryUntil(ctx, canaryCleanupTimeout, e.poll(), func(ctx context.Context) error { return e.allGone(ctx, n) }); err != nil {
		return nil, err
	}
	if err := retryUntil(ctx, canaryPVCSweepTimeout, e.poll(), func(ctx context.Context) error { return e.sweepPVCs(ctx, n) }); err != nil {
		warnings = append(warnings, fmt.Sprintf("canary PVC sweep did not finish: %v", err))
	}
	return warnings, nil
}

// sweepPVCs deletes the canary's PVCs and fails while any still exist, so the caller retries
// until they have finished terminating.
func (e canaryEnv) sweepPVCs(ctx context.Context, n canaryNames) error {
	var pvcs corev1.PersistentVolumeClaimList
	if err := e.Reader.List(ctx, &pvcs, client.InNamespace(e.Namespace)); err != nil {
		return fmt.Errorf("could not list PVCs to sweep leftovers: %w", err)
	}
	var remaining []string
	for i := range pvcs.Items {
		name := pvcs.Items[i].Name
		if !pvcBelongsToInstance(name, n.StandaloneInstance()) {
			continue
		}
		remaining = append(remaining, name)
		if err := ignoreNotFound(e.Writer.Delete(ctx, &pvcs.Items[i])); err != nil {
			return fmt.Errorf("could not delete leftover PVC %s: %w", name, err)
		}
	}
	if len(remaining) > 0 {
		return fmt.Errorf("canary PVCs still present: %s", strings.Join(remaining, ", "))
	}
	return nil
}

// canaryRun is the state shared by the canary specs of one invocation.
type canaryRun struct {
	cfg             canaryConfig
	env             canaryEnv
	names           canaryNames
	standaloneReady bool
}

// newCanaryRun reads the canary settings and builds the run. It returns a non-empty skipReason, and no
// run, when the gate did not enable the canary for this cluster. Leftovers of a cancelled earlier run
// are removed first; failing to remove them comes back as a warning, never as an error.
func newCanaryRun(ctx context.Context, getenv func(string) string, defaultNamespace string, writer client.Client, reader client.Reader, randomID func(int) string) (run *canaryRun, skipReason string, warning error, err error) {
	cfg, err := loadCanaryConfig(getenv, defaultNamespace)
	if err != nil {
		return nil, "", nil, err
	}
	if !cfg.Enabled {
		return nil, "SCS_SANITY_CANARY_ENABLED is not true for this cluster, so the canary specs do not run", nil, nil
	}
	names, err := newCanaryNames(strings.ToLower(randomID(5)))
	if err != nil {
		return nil, "", nil, err
	}
	run = &canaryRun{cfg: cfg, names: names, env: canaryEnv{Writer: writer, Reader: reader, Namespace: cfg.Namespace}}
	if sweepErr := run.env.sweepStale(ctx, time.Now(), staleCanaryMaxAge(getenv)); sweepErr != nil {
		warning = fmt.Errorf("could not remove stale canary resources from namespace %s: %w", cfg.Namespace, sweepErr)
	}
	return run, "", warning, nil
}

// diagnose describes the canary Standalone for a failure message.
func (r *canaryRun) diagnose(what string) func() string {
	return func() string {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return fmt.Sprintf("%s\n%s", what, r.env.diagnoseStandalone(ctx, r.names))
	}
}

// cleanupAtEnd deletes only objects carrying this run's label, within the cleanup budget, and returns
// the PVC warnings along with any error.
func (r *canaryRun) cleanupAtEnd() (warnings []string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), canaryCleanupBudget)
	defer cancel()
	return r.env.cleanup(ctx, r.names)
}
