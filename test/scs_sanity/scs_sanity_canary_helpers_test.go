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
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

const canaryNS = "splunk-operator"

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestParseCanaryEnabled(t *testing.T) {
	for raw, want := range map[string]bool{"": false, "false": false, "FALSE": false, " true ": true, "true": true} {
		got, err := parseCanaryEnabled(raw)
		if err != nil || got != want {
			t.Errorf("parseCanaryEnabled(%q) = %v, %v; want %v", raw, got, err, want)
		}
	}
	for _, raw := range []string{"yes", "1", "enabled"} {
		if _, err := parseCanaryEnabled(raw); err == nil {
			t.Errorf("parseCanaryEnabled(%q) must reject a value that is not true or false", raw)
		}
	}
}

func TestParseCanaryDuration(t *testing.T) {
	if got, err := parseCanaryDuration("X", "", defaultCanaryReadyTimeout); err != nil || got != defaultCanaryReadyTimeout {
		t.Errorf("empty value = %v, %v; want the %v default", got, err, defaultCanaryReadyTimeout)
	}
	if got, err := parseCanaryDuration("X", "20m", time.Minute); err != nil || got != 20*time.Minute {
		t.Errorf("20m = %v, %v", got, err)
	}
	for _, raw := range []string{"soon", "0s", "-5m", "15"} {
		if _, err := parseCanaryDuration("SCS_SANITY_CANARY_TIMEOUT", raw, time.Minute); err == nil {
			t.Errorf("parseCanaryDuration(%q) must be rejected", raw)
		} else if !strings.Contains(err.Error(), "SCS_SANITY_CANARY_TIMEOUT") {
			t.Errorf("the error for %q must name the variable, got %v", raw, err)
		}
	}
}

func TestLoadCanaryConfig(t *testing.T) {
	cfg, err := loadCanaryConfig(envFrom(map[string]string{"SCS_SANITY_CANARY_TIMEOUT": "garbage"}), canaryNS)
	if err != nil || cfg.Enabled {
		t.Errorf("a disabled canary must ignore other settings, got %+v err=%v", cfg, err)
	}

	cfg, err = loadCanaryConfig(envFrom(map[string]string{"SCS_SANITY_CANARY_ENABLED": "true"}), canaryNS)
	if err != nil || !cfg.Enabled || cfg.Namespace != canaryNS || cfg.ReadyTimeout != defaultCanaryReadyTimeout || cfg.ScaleUpTimeout != defaultCanaryScaleUpTimeout || cfg.ScaleDownTimeout != defaultCanaryScaleDownTimeout {
		t.Errorf("defaults not applied: %+v err=%v", cfg, err)
	}

	cfg, err = loadCanaryConfig(envFrom(map[string]string{
		"SCS_SANITY_CANARY_ENABLED": "true", "SCS_SANITY_CANARY_NAMESPACE": " canary-ns ", "SCS_SANITY_CANARY_TIMEOUT": "7m",
		"SCS_SANITY_CANARY_SCALE_UP_TIMEOUT": "8m", "SCS_SANITY_CANARY_SCALE_DOWN_TIMEOUT": "4m",
	}), canaryNS)
	if err != nil || cfg.Namespace != "canary-ns" || cfg.ReadyTimeout != 7*time.Minute || cfg.ScaleUpTimeout != 8*time.Minute || cfg.ScaleDownTimeout != 4*time.Minute {
		t.Errorf("explicit settings not honored: %+v err=%v", cfg, err)
	}

	if _, err = loadCanaryConfig(envFrom(map[string]string{"SCS_SANITY_CANARY_ENABLED": "true"}), ""); err == nil {
		t.Error("an enabled canary with no namespace must be rejected")
	}
	if _, err = loadCanaryConfig(envFrom(map[string]string{"SCS_SANITY_CANARY_ENABLED": "maybe"}), canaryNS); err == nil {
		t.Error("an invalid enabled flag must be rejected")
	}
	for _, name := range []string{"SCS_SANITY_CANARY_TIMEOUT", "SCS_SANITY_CANARY_SCALE_UP_TIMEOUT", "SCS_SANITY_CANARY_SCALE_DOWN_TIMEOUT"} {
		if _, err = loadCanaryConfig(envFrom(map[string]string{"SCS_SANITY_CANARY_ENABLED": "true", name: "0s"}), canaryNS); err == nil {
			t.Errorf("an enabled canary with a bad %s must be rejected", name)
		}
	}
}

func TestNewCanaryNames(t *testing.T) {
	n, err := newCanaryNames("abc12")
	if err != nil {
		t.Fatal(err)
	}
	if n.Standalone != "scs-sanity-canary-abc12-sa" {
		t.Errorf("unexpected names: %+v", n)
	}
	if n.StandaloneInstance() != "splunk-scs-sanity-canary-abc12-sa-standalone" {
		t.Errorf("unexpected instance name: %s", n.StandaloneInstance())
	}
	for _, name := range []string{n.Standalone, n.StandaloneInstance()} {
		if len(name) > 63 {
			t.Errorf("%s is longer than a DNS label", name)
		}
	}
	if n.Labels()[canaryLabelKey] != "abc12" {
		t.Errorf("canary label missing: %v", n.Labels())
	}
	for _, bad := range []string{"", "ab", "1abc", "ABC12", "abc-12", "abcdefghi"} {
		if _, err := newCanaryNames(bad); err == nil {
			t.Errorf("newCanaryNames(%q) must be rejected", bad)
		}
	}
}

func TestCanaryBuilders(t *testing.T) {
	n, _ := newCanaryNames("abc12")
	sa := newCanaryStandalone(canaryNS, n, 1)
	if sa.Spec.Image != "" || sa.Spec.Replicas != 1 || sa.Labels[canaryLabelKey] != "abc12" || sa.Namespace != canaryNS {
		t.Errorf("canary Standalone must use the default image, 1 replica and the run label: %+v", sa)
	}
	for name, res := range map[string]corev1.ResourceRequirements{"Standalone": sa.Spec.Resources} {
		for _, r := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory, corev1.ResourceEphemeralStorage} {
			req, lim := res.Requests[r], res.Limits[r]
			if req.IsZero() || lim.IsZero() {
				t.Fatalf("canary %s must set both a request and a limit for %s", name, r)
			}
			if ratio := float64(lim.MilliValue()) / float64(req.MilliValue()); ratio >= 8 {
				t.Errorf("canary %s %s limit/request ratio %.1f must stay under the smallest LimitRange ratio observed (8)", name, r, ratio)
			}
		}
	}
}

func TestCanaryOwnedAndStale(t *testing.T) {
	now := time.Now()
	obj := func(name string, lbls map[string]string, age time.Duration) *enterpriseApi.Standalone {
		return &enterpriseApi.Standalone{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: lbls, CreationTimestamp: metav1.NewTime(now.Add(-age))}}
	}
	mine := obj("scs-sanity-canary-abc12-sa", map[string]string{canaryLabelKey: "abc12"}, time.Minute)
	if !canaryOwned(mine, "abc12") || canaryOwned(mine, "zzz99") {
		t.Error("ownership must require the exact run label")
	}
	if canaryOwned(obj("production-standalone", map[string]string{canaryLabelKey: "abc12"}, 0), "abc12") {
		t.Error("an object outside the canary name prefix must never be owned")
	}
	if canaryOwned(obj("scs-sanity-canary-abc12-sa", nil, 0), "abc12") {
		t.Error("an unlabelled object must never be owned")
	}

	old := obj("scs-sanity-canary-old11-sa", map[string]string{canaryLabelKey: "old11"}, 2*time.Hour)
	fresh := obj("scs-sanity-canary-new11-sa", map[string]string{canaryLabelKey: "new11"}, time.Minute)
	foreign := obj("customer-standalone", map[string]string{canaryLabelKey: "old11"}, 5*time.Hour)
	unlabelled := obj("scs-sanity-canary-old22-sa", nil, 5*time.Hour)
	mismatched := obj("scs-sanity-canary-old11-sa", map[string]string{canaryLabelKey: "zzzzz"}, 5*time.Hour)
	badValue := obj("scs-sanity-canary-BAD-sa", map[string]string{canaryLabelKey: "BAD"}, 5*time.Hour)
	for name, tc := range map[string]struct {
		obj  *enterpriseApi.Standalone
		want bool
	}{
		"old": {old, true}, "fresh": {fresh, false}, "foreign": {foreign, false}, "unlabelled": {unlabelled, false},
		"label value does not match the name": {mismatched, false}, "label value is not a run id": {badValue, false},
	} {
		if got := staleCanary(tc.obj, now, time.Hour); got != tc.want {
			t.Errorf("staleCanary(%s) = %v, want %v", name, got, tc.want)
		}
	}
}

func TestPVCBelongsToInstance(t *testing.T) {
	instance := "splunk-scs-sanity-canary-abc12-sa-standalone"
	for pvc, want := range map[string]bool{
		"pvc-etc-" + instance + "-0":                  true,
		"pvc-var-" + instance + "-1":                  true,
		"pvc-etc-" + instance + "-0-extra":            false,
		"pvc-etc-splunk-other-standalone-0":           false,
		"pvc-etc-" + strings.ToUpper(instance) + "-0": false,
		instance + "-0":                               false,
	} {
		if got := pvcBelongsToInstance(pvc, instance); got != want {
			t.Errorf("pvcBelongsToInstance(%q) = %v, want %v", pvc, got, want)
		}
	}
}

func TestDescribeCreateError(t *testing.T) {
	forbidden := apierrors.NewForbidden(schema.GroupResource{Group: "enterprise.splunk.com", Resource: "standalones"}, "x", errors.New("denied"))
	err := describeCreateError(forbidden, "standalones.enterprise.splunk.com", "splunk-operator")
	for _, want := range []string{"missing RBAC verb 'create'", "standalones.enterprise.splunk.com", "splunk-operator"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("forbidden error %q must mention %q", err, want)
		}
	}
	if !apierrors.IsForbidden(err) {
		t.Error("the Forbidden cause must stay unwrappable")
	}
	other := describeCreateError(errors.New("boom"), "standalones.enterprise.splunk.com", "ns")
	if strings.Contains(other.Error(), "RBAC") || !strings.Contains(other.Error(), "boom") {
		t.Errorf("a non-forbidden error must not claim missing RBAC: %v", other)
	}
}

func readyStandalone(replicas int32) (*enterpriseApi.Standalone, *appsv1.StatefulSet) {
	sa := &enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{Name: "sa", Generation: 3},
		Status: enterpriseApi.StandaloneStatus{
			Phase: enterpriseApi.PhaseReady, ObservedGeneration: 3, Replicas: replicas, ReadyReplicas: replicas,
		},
	}
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-sa-standalone"},
		Spec:       appsv1.StatefulSetSpec{Replicas: &replicas},
		Status:     appsv1.StatefulSetStatus{ReadyReplicas: replicas},
	}
	return sa, sts
}

func TestStandaloneReplicaProblem(t *testing.T) {
	sa, sts := readyStandalone(2)
	if err := standaloneReplicaProblem(sa, sts, 2); err != nil {
		t.Errorf("a Ready Standalone at the wanted size reported %v", err)
	}

	cases := map[string]struct {
		mutate func(*enterpriseApi.Standalone, **appsv1.StatefulSet)
		want   string
	}{
		"no statefulset":    {func(_ *enterpriseApi.Standalone, s **appsv1.StatefulSet) { *s = nil }, "not created yet"},
		"spec not updated":  {func(_ *enterpriseApi.Standalone, s **appsv1.StatefulSet) { one := int32(1); (*s).Spec.Replicas = &one }, "spec.replicas is not 2"},
		"nil replicas":      {func(_ *enterpriseApi.Standalone, s **appsv1.StatefulSet) { (*s).Spec.Replicas = nil }, "spec.replicas is not 2"},
		"pods not ready":    {func(_ *enterpriseApi.Standalone, s **appsv1.StatefulSet) { (*s).Status.ReadyReplicas = 1 }, "1 of 2 replicas ready"},
		"generation behind": {func(a *enterpriseApi.Standalone, _ **appsv1.StatefulSet) { a.Status.ObservedGeneration = 2 }, "has not observed generation 3"},
		"not ready phase": {func(a *enterpriseApi.Standalone, _ **appsv1.StatefulSet) {
			a.Status.Phase = enterpriseApi.PhaseScalingUp
		}, `phase "ScalingUp"`},
		"status lags": {func(a *enterpriseApi.Standalone, _ **appsv1.StatefulSet) { a.Status.ReadyReplicas = 1 }, "reports 1 ready replicas"},
	}
	for name, tc := range cases {
		sa, sts := readyStandalone(2)
		tc.mutate(sa, &sts)
		err := standaloneReplicaProblem(sa, sts, 2)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error containing %q", name, err, tc.want)
			continue
		}
		var terminal *terminalError
		if errors.As(err, &terminal) {
			t.Errorf("%s: a transient state must not be terminal", name)
		}
	}

	stalled, sts := readyStandalone(1)
	stalled.Status.Phase = enterpriseApi.PhaseError
	stalled.Status.Conditions = []metav1.Condition{{Type: string(enterpriseApi.ConditionStalled), Status: metav1.ConditionTrue}}
	err := standaloneReplicaProblem(stalled, sts, 1)
	var terminal *terminalError
	if !errors.As(err, &terminal) {
		t.Errorf("a stalled Error phase must be terminal, got %v", err)
	}
	transient, sts := readyStandalone(1)
	transient.Status.Phase = enterpriseApi.PhaseError
	if err := standaloneReplicaProblem(transient, sts, 1); err == nil || errors.As(err, &terminal) {
		t.Errorf("an Error phase without Stalled must keep waiting, got %v", err)
	}
}

func TestRetryUntilStopsOnTerminalError(t *testing.T) {
	calls := 0
	err := retryUntil(context.Background(), time.Minute, time.Millisecond, func(context.Context) error {
		calls++
		return &terminalError{err: errors.New("stalled")}
	})
	if calls != 1 || err == nil || !strings.Contains(err.Error(), "stalled") {
		t.Errorf("a terminal error must end the wait after one call, got calls=%d err=%v", calls, err)
	}

	calls = 0
	err = retryUntil(context.Background(), time.Minute, time.Millisecond, func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("not yet")
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Errorf("a transient error must be retried until success, got calls=%d err=%v", calls, err)
	}
}

func TestRecentWarningEvents(t *testing.T) {
	now := time.Now()
	ev := func(typ, object, reason string, age time.Duration) corev1.Event {
		return corev1.Event{
			Type: typ, Reason: reason, Message: reason + " happened", Count: 2,
			InvolvedObject: corev1.ObjectReference{Name: object},
			LastTimestamp:  metav1.NewTime(now.Add(-age)),
		}
	}
	events := []corev1.Event{
		ev("Warning", "splunk-sa-standalone-0", "Failed", 3*time.Minute),
		ev("Normal", "splunk-sa-standalone-0", "Pulled", time.Minute),
		ev("Warning", "unrelated-pod", "Failed", time.Minute),
		ev("Warning", "sa", "ReconcileFailed", time.Minute),
		ev("Warning", "splunk-sa-standalone", "FailedCreate", 2*time.Minute),
	}
	got := recentWarningEvents(events, []string{"sa", "splunk-sa-standalone"}, 2)
	if len(got) != 2 || !strings.Contains(got[0], "ReconcileFailed") || !strings.Contains(got[1], "FailedCreate") {
		t.Errorf("expected the two newest matching Warning events in order, got %v", got)
	}
	if all := recentWarningEvents(events, []string{"sa", "splunk-sa-standalone"}, 10); len(all) != 3 {
		t.Errorf("expected 3 matching Warning events, got %v", all)
	}
}

func TestDescribeStandalone(t *testing.T) {
	sa, sts := readyStandalone(1)
	sa.Status.Phase = enterpriseApi.PhaseError
	sa.Status.Conditions = []metav1.Condition{{Type: "Stalled", Status: metav1.ConditionTrue, Reason: "ImagePullBackOff", Message: "manual fix required"}}
	got := describeStandalone(sa, sts, []string{"pod Failed: pull denied"})
	for _, want := range []string{`phase="Error"`, "Stalled=True reason=ImagePullBackOff: manual fix required", "spec.replicas=1", "Warning event: pod Failed: pull denied"} {
		if !strings.Contains(got, want) {
			t.Errorf("description must contain %q, got:\n%s", want, got)
		}
	}
	if none := describeStandalone(nil, nil, nil); !strings.Contains(none, "Standalone: not found") || !strings.Contains(none, "StatefulSet: not found") || !strings.Contains(none, "Warning events: none") {
		t.Errorf("missing objects must be reported, got:\n%s", none)
	}
}

func canaryFixtures(t *testing.T, objs ...client.Object) (canaryEnv, canaryNames, client.Client) {
	t.Helper()
	n, err := newCanaryNames("abc12")
	if err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(newFakeScheme(t)).WithObjects(objs...).Build()
	return canaryEnv{Writer: c, Reader: c, Namespace: canaryNS, Poll: 5 * time.Millisecond}, n, c
}

func exists(t *testing.T, c client.Client, obj client.Object, name string) bool {
	t.Helper()
	err := c.Get(context.Background(), client.ObjectKey{Namespace: canaryNS, Name: name}, obj)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("unexpected error getting %s: %v", name, err)
	}
	return err == nil
}

func TestCanarySweepStale(t *testing.T) {
	now := time.Now()
	mk := func(name string, lbls map[string]string, age time.Duration) *enterpriseApi.Standalone {
		return &enterpriseApi.Standalone{ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: canaryNS, Labels: lbls, CreationTimestamp: metav1.NewTime(now.Add(-age)),
		}}
	}
	env, _, c := canaryFixtures(t,
		mk("scs-sanity-canary-old11-sa", map[string]string{canaryLabelKey: "old11"}, 3*time.Hour),
		mk("scs-sanity-canary-new11-sa", map[string]string{canaryLabelKey: "new11"}, time.Minute),
		mk("customer-standalone", nil, 10*time.Hour),
	)
	if err := env.sweepStale(context.Background(), now, time.Hour); err != nil {
		t.Fatal(err)
	}
	if exists(t, c, &enterpriseApi.Standalone{}, "scs-sanity-canary-old11-sa") {
		t.Error("stale canary CRs must be deleted")
	}
	if !exists(t, c, &enterpriseApi.Standalone{}, "scs-sanity-canary-new11-sa") || !exists(t, c, &enterpriseApi.Standalone{}, "customer-standalone") {
		t.Error("fresh canaries and unrelated CRs must be left alone")
	}
}

func TestCanaryCleanup(t *testing.T) {
	n, _ := newCanaryNames("abc12")
	mine := map[string]string{canaryLabelKey: "abc12"}
	pvc := func(name string) *corev1.PersistentVolumeClaim {
		return &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: canaryNS}}
	}
	env, _, c := canaryFixtures(t,
		&enterpriseApi.Standalone{ObjectMeta: metav1.ObjectMeta{Name: n.Standalone, Namespace: canaryNS, Labels: mine}},
		pvc("pvc-etc-"+n.StandaloneInstance()+"-0"),
		pvc("pvc-etc-splunk-customer-standalone-0"),
	)
	warnings, err := env.cleanup(context.Background(), n)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("cleanup failed: warnings=%v err=%v", warnings, err)
	}
	if exists(t, c, &enterpriseApi.Standalone{}, n.Standalone) {
		t.Error("cleanup must delete the canary CR")
	}
	if exists(t, c, &corev1.PersistentVolumeClaim{}, "pvc-etc-"+n.StandaloneInstance()+"-0") {
		t.Error("cleanup must sweep the canary's leftover PVC")
	}
	if !exists(t, c, &corev1.PersistentVolumeClaim{}, "pvc-etc-splunk-customer-standalone-0") {
		t.Error("cleanup must not touch an unrelated PVC")
	}
	if _, err := env.cleanup(context.Background(), n); err != nil {
		t.Errorf("cleanup must be idempotent, got %v", err)
	}
}

func TestCanaryCleanupRefusesForeignObjects(t *testing.T) {
	n, _ := newCanaryNames("abc12")
	env, _, c := canaryFixtures(t,
		&enterpriseApi.Standalone{ObjectMeta: metav1.ObjectMeta{Name: n.Standalone, Namespace: canaryNS, Labels: map[string]string{canaryLabelKey: "someone-else"}}},
	)
	if _, err := env.cleanup(context.Background(), n); err == nil || !strings.Contains(err.Error(), "refusing to delete") {
		t.Errorf("cleanup must refuse an object that is not labelled for this run, got %v", err)
	}
	if !exists(t, c, &enterpriseApi.Standalone{}, n.Standalone) {
		t.Error("a foreign object must survive cleanup")
	}
}

func TestCanaryStandaloneAtAndScale(t *testing.T) {
	n, _ := newCanaryNames("abc12")
	sa, sts := readyStandalone(1)
	sa.Name, sa.Namespace = n.Standalone, canaryNS
	sts.Name, sts.Namespace = n.StandaloneInstance(), canaryNS
	env, _, c := canaryFixtures(t, sa, sts)

	if err := env.standaloneAt(context.Background(), n, 1, newWaitTracker(time.Now())); err != nil {
		t.Errorf("a Ready 1-replica canary must pass, got %v", err)
	}
	if err := env.standaloneAt(context.Background(), n, 2, newWaitTracker(time.Now())); err == nil {
		t.Error("a 1-replica canary must not pass as 2 replicas")
	}
	if err := env.scaleStandalone(context.Background(), n, 2); err != nil {
		t.Fatal(err)
	}
	got := &enterpriseApi.Standalone{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: canaryNS, Name: n.Standalone}, got); err != nil || got.Spec.Replicas != 2 {
		t.Errorf("scaleStandalone must patch spec.replicas to 2, got %d err=%v", got.Spec.Replicas, err)
	}
	if text := env.diagnoseStandalone(context.Background(), n); !strings.Contains(text, "Standalone "+n.Standalone) {
		t.Errorf("diagnostics must describe the Standalone, got:\n%s", text)
	}
}

func TestStaleCanaryMaxAge(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want time.Duration
	}{
		{"unset uses the floor", nil, 3 * time.Hour},
		{"short suite uses the floor", map[string]string{"SCS_SANITY_SUITE_TIMEOUT": "28m"}, 3 * time.Hour},
		{"long suite stays above the timeout", map[string]string{"SCS_SANITY_SUITE_TIMEOUT": "4h"}, 5 * time.Hour},
		{"invalid falls back to the floor", map[string]string{"SCS_SANITY_SUITE_TIMEOUT": "soon"}, 3 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := staleCanaryMaxAge(envFrom(tt.env)); got != tt.want {
				t.Errorf("staleCanaryMaxAge = %s, want %s", got, tt.want)
			}
		})
	}
}

func envPod(name, instance string, age time.Duration) corev1.Pod {
	return corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: canaryNS, Labels: map[string]string{instanceLabelKey: instance}, CreationTimestamp: metav1.NewTime(time.Now().Add(-age)),
	}}
}

func TestCanaryEnvironmentProblem(t *testing.T) {
	now := time.Now()
	pullFail := envPod("p-0", "i", time.Minute)
	pullFail.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "splunk", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "pull access denied"}}}}
	initFail := envPod("p-1", "i", time.Minute)
	initFail.Status.InitContainerStatuses = []corev1.ContainerStatus{{Name: "init", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "InvalidImageName"}}}}
	crashLoop := envPod("p-2", "i", time.Minute)
	crashLoop.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "splunk", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}}}
	unschedulable := func(age time.Duration) corev1.Pod {
		p := envPod("p-3", "i", age)
		p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable, Message: "0/5 nodes are available"}}
		return p
	}
	quota := corev1.Event{Type: corev1.EventTypeWarning, Reason: "FailedCreate", Message: "pods are forbidden: exceeded quota: compute", InvolvedObject: corev1.ObjectReference{Name: "splunk-x-standalone"}, LastTimestamp: metav1.NewTime(now)}
	staleQuota := quota
	staleQuota.LastTimestamp = metav1.NewTime(now.Add(-time.Hour))
	noTimeQuota := quota
	noTimeQuota.LastTimestamp = metav1.Time{}

	tests := []struct {
		name    string
		pods    []corev1.Pod
		events  []corev1.Event
		wantErr string
	}{
		{"healthy", []corev1.Pod{envPod("ok", "i", time.Hour)}, nil, ""},
		{"image pull backoff", []corev1.Pod{pullFail}, nil, "pod p-0 container splunk is ImagePullBackOff: pull access denied"},
		{"init container pull failure", []corev1.Pod{initFail}, nil, "container init is InvalidImageName"},
		{"a crash loop is the operator's business, not the environment's", []corev1.Pod{crashLoop}, nil, ""},
		{"unschedulable inside the grace period", []corev1.Pod{unschedulable(time.Minute)}, nil, ""},
		{"unschedulable past the grace period", []corev1.Pod{unschedulable(10 * time.Minute)}, nil, "p-3 is Unschedulable: 0/5 nodes are available"},
		{"resource quota", nil, []corev1.Event{quota}, "hit a resource quota"},
		{"a quota event from before this wait step is ignored", nil, []corev1.Event{staleQuota}, ""},
		{"a quota event without a timestamp is ignored", nil, []corev1.Event{noTimeQuota}, ""},
		{"other failed create is ignored", nil, []corev1.Event{{Type: corev1.EventTypeWarning, Reason: "FailedCreate", Message: "other"}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := canaryEnvironmentProblem(tt.pods, tt.events, now, newWaitTracker(now.Add(-time.Minute)))
			assertErrContains(t, err, tt.wantErr)
			if tt.wantErr == "" {
				return
			}
			var terminal *terminalError
			if !errors.As(err, &terminal) || !strings.Contains(err.Error(), "not an operator regression") {
				t.Errorf("an environment problem must be terminal and say so, got %v", err)
			}
		})
	}
}

func waitingPod(name, reason string) corev1.Pod {
	p := envPod(name, "i", time.Minute)
	p.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "splunk", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}}}}
	return p
}

func TestCanaryTransientWaitingReasonsNeedAGracePeriod(t *testing.T) {
	start := time.Now()
	for _, reason := range []string{"ErrImagePull", "CreateContainerConfigError"} {
		t.Run(reason, func(t *testing.T) {
			tracker := newWaitTracker(start)
			pod := waitingPod("p-0", reason)
			if err := canaryEnvironmentProblem([]corev1.Pod{pod}, nil, start, tracker); err != nil {
				t.Fatalf("first sighting of %s must not be terminal, got %v", reason, err)
			}
			if err := canaryEnvironmentProblem([]corev1.Pod{pod}, nil, start.Add(59*time.Second), tracker); err != nil {
				t.Fatalf("%s just inside the grace period must not be terminal, got %v", reason, err)
			}
			err := canaryEnvironmentProblem([]corev1.Pod{pod}, nil, start.Add(61*time.Second), tracker)
			var terminal *terminalError
			if !errors.As(err, &terminal) || !strings.Contains(err.Error(), reason) || !strings.Contains(err.Error(), "not an operator regression") {
				t.Fatalf("%s persisting past the grace period must be terminal, got %v", reason, err)
			}
		})
	}
}

func TestCanaryGraceClockRestartsAfterAnInterruption(t *testing.T) {
	start := time.Now()
	tracker := newWaitTracker(start)
	blip := waitingPod("p-0", "ErrImagePull")
	healthy := envPod("p-0", "i", time.Minute)
	_ = canaryEnvironmentProblem([]corev1.Pod{blip}, nil, start, tracker)
	_ = canaryEnvironmentProblem([]corev1.Pod{healthy}, nil, start.Add(30*time.Second), tracker)
	if err := canaryEnvironmentProblem([]corev1.Pod{blip}, nil, start.Add(70*time.Second), tracker); err != nil {
		t.Errorf("a reason that cleared and came back must start a new grace period, got %v", err)
	}
	if err := canaryEnvironmentProblem([]corev1.Pod{blip}, nil, start.Add(131*time.Second), tracker); err == nil {
		t.Error("the new grace period must still expire")
	}
}

func TestCanaryGraceIsTrackedPerPod(t *testing.T) {
	start := time.Now()
	tracker := newWaitTracker(start)
	_ = canaryEnvironmentProblem([]corev1.Pod{waitingPod("p-0", "ErrImagePull")}, nil, start, tracker)
	both := []corev1.Pod{waitingPod("p-0", "ErrImagePull"), waitingPod("p-1", "ErrImagePull")}
	err := canaryEnvironmentProblem(both, nil, start.Add(65*time.Second), tracker)
	if err == nil || !strings.Contains(err.Error(), "p-0") || strings.Contains(err.Error(), "p-1") {
		t.Errorf("only the pod past its own grace period may be reported, got %v", err)
	}
}

func TestCanaryImagePullBackOffAndInvalidImageNameStayImmediatelyTerminal(t *testing.T) {
	start := time.Now()
	for _, reason := range []string{"ImagePullBackOff", "InvalidImageName"} {
		if err := canaryEnvironmentProblem([]corev1.Pod{waitingPod("p-0", reason)}, nil, start, newWaitTracker(start)); err == nil {
			t.Errorf("%s must stop the wait at once", reason)
		}
	}
}

func TestCanaryStandaloneAtReportsEnvironmentProblem(t *testing.T) {
	n, _ := newCanaryNames("abc12")
	sa, sts := readyStandalone(1)
	sa.Name, sa.Namespace = n.Standalone, canaryNS
	sa.Status.Phase = enterpriseApi.PhasePending
	sts.Name, sts.Namespace = n.StandaloneInstance(), canaryNS
	sts.Status.ReadyReplicas = 0
	stuck := envPod(n.StandaloneInstance()+"-0", n.StandaloneInstance(), time.Minute)
	stuck.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "splunk", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}}}
	env, _, _ := canaryFixtures(t, sa, sts, &stuck)

	err := env.standaloneAt(context.Background(), n, 1, newWaitTracker(time.Now()))
	var terminal *terminalError
	if !errors.As(err, &terminal) || !strings.Contains(err.Error(), "ImagePullBackOff") {
		t.Errorf("an unpullable image must stop the wait with an environment message, got %v", err)
	}

	envWithoutPod, _, _ := canaryFixtures(t, sa, sts)
	err = envWithoutPod.standaloneAt(context.Background(), n, 1, newWaitTracker(time.Now()))
	if err == nil || errors.As(err, &terminal) {
		t.Errorf("a pending pod with no known cause must keep waiting, got %v", err)
	}
}

func TestCanaryCleanupWaitsForPods(t *testing.T) {
	n, _ := newCanaryNames("abc12")
	mine := map[string]string{canaryLabelKey: "abc12"}
	lingering := envPod("pod-0", n.StandaloneInstance(), time.Minute)
	env, _, c := canaryFixtures(t,
		&enterpriseApi.Standalone{ObjectMeta: metav1.ObjectMeta{Name: n.Standalone, Namespace: canaryNS, Labels: mine}},
		&lingering,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := env.cleanup(ctx, n); err == nil || !strings.Contains(err.Error(), "Pod/pod-0") {
		t.Errorf("cleanup must not report success while a canary pod exists, got %v", err)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = c.Delete(context.Background(), &lingering)
	}()
	if _, err := env.cleanup(context.Background(), n); err != nil {
		t.Errorf("cleanup must succeed once the pod is gone, got %v", err)
	}
}

func TestCanaryCleanupPVCSweepIsWarningOnly(t *testing.T) {
	n, _ := newCanaryNames("abc12")
	c := fake.NewClientBuilder().WithScheme(newFakeScheme(t)).WithInterceptorFuncs(interceptor.Funcs{
		List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*corev1.PersistentVolumeClaimList); ok {
				return apierrors.NewForbidden(schema.GroupResource{Resource: "persistentvolumeclaims"}, "", errors.New("no"))
			}
			return cl.List(ctx, list, opts...)
		},
	}).Build()
	env := canaryEnv{Writer: c, Reader: c, Namespace: canaryNS, Poll: 5 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	warnings, err := env.cleanup(ctx, n)
	if err != nil || len(warnings) != 1 || !strings.Contains(warnings[0], "PVC sweep did not finish") {
		t.Errorf("a PVC sweep problem must be a warning, got warnings=%v err=%v", warnings, err)
	}
}

func TestRetryPhase(t *testing.T) {
	err := retryPhase(context.Background(), "wait A", time.Second, time.Millisecond, func(context.Context) error { return nil })
	if err != nil {
		t.Errorf("a passing check must pass, got %v", err)
	}

	err = retryPhase(context.Background(), "wait A", 30*time.Millisecond, 5*time.Millisecond, func(context.Context) error { return errors.New("not yet") })
	if err == nil || !strings.Contains(err.Error(), "wait A did not finish within 30ms: not yet") {
		t.Errorf("a timeout must name the phase and the last error, got %v", err)
	}

	err = retryPhase(context.Background(), "wait B", time.Second, time.Millisecond, func(context.Context) error { return &terminalError{err: errors.New("boom")} })
	if err == nil || !strings.Contains(err.Error(), "wait B stopped early: boom") {
		t.Errorf("a terminal error must say the wait stopped early, got %v", err)
	}
}

func TestParseSuiteTimeout(t *testing.T) {
	fallback := 120 * time.Minute
	for raw, want := range map[string]time.Duration{"": fallback, " ": fallback, "28m": 28 * time.Minute, "1h30m": 90 * time.Minute} {
		if got, err := parseSuiteTimeout(raw, fallback); err != nil || got != want {
			t.Errorf("parseSuiteTimeout(%q) = %s, %v, want %s", raw, got, err, want)
		}
	}
	for _, raw := range []string{"soon", "0", "-5m"} {
		if _, err := parseSuiteTimeout(raw, fallback); err == nil {
			t.Errorf("parseSuiteTimeout(%q) must fail", raw)
		}
	}
}

func orphanObjects(now time.Time) (pvcs []*corev1.PersistentVolumeClaim, pods []*corev1.Pod) {
	mkPVC := func(name, instance string, age time.Duration) *corev1.PersistentVolumeClaim {
		lbls := map[string]string{}
		if instance != "" {
			lbls[instanceLabelKey] = instance
		}
		return &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: canaryNS, Labels: lbls, CreationTimestamp: metav1.NewTime(now.Add(-age)),
		}}
	}
	mkPod := func(name, instance string, age time.Duration) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: canaryNS, Labels: map[string]string{instanceLabelKey: instance}, CreationTimestamp: metav1.NewTime(now.Add(-age)),
		}}
	}
	const oldSA = "splunk-scs-sanity-canary-old11-sa-standalone"
	return []*corev1.PersistentVolumeClaim{
		mkPVC("pvc-etc-"+oldSA+"-0", oldSA, 4*time.Hour),
		mkPVC("pvc-var-"+oldSA+"-0", oldSA, 4*time.Hour),
		mkPVC("pvc-etc-splunk-scs-sanity-canary-new11-sa-standalone-0", "splunk-scs-sanity-canary-new11-sa-standalone", time.Minute),
		mkPVC("pvc-etc-splunk-customer-standalone-0", "splunk-customer-standalone", 10*time.Hour),
		mkPVC("pvc-etc-"+oldSA+"-0-extra", oldSA, 10*time.Hour),
		mkPVC("pvc-etc-"+oldSA+"-1", "", 10*time.Hour),
		mkPVC("pvc-etc-splunk-scs-sanity-canary-old11-sa-indexer-0", "splunk-scs-sanity-canary-old11-sa-indexer", 10*time.Hour),
	}, []*corev1.Pod{
		mkPod(oldSA+"-0", oldSA, 4*time.Hour),
		mkPod("splunk-scs-sanity-canary-new11-sa-standalone-0", "splunk-scs-sanity-canary-new11-sa-standalone", time.Minute),
		mkPod("splunk-customer-standalone-0", "splunk-customer-standalone", 10*time.Hour),
		mkPod(oldSA+"-notanordinal", oldSA, 10*time.Hour),
	}
}

func TestStaleCanaryOrphan(t *testing.T) {
	now := time.Now()
	pvcs, pods := orphanObjects(now)
	want := map[string]bool{
		"pvc-etc-splunk-scs-sanity-canary-old11-sa-standalone-0":       true,
		"pvc-var-splunk-scs-sanity-canary-old11-sa-standalone-0":       true,
		"splunk-scs-sanity-canary-old11-sa-standalone-0":               true,
		"pvc-etc-splunk-scs-sanity-canary-new11-sa-standalone-0":       false,
		"splunk-scs-sanity-canary-new11-sa-standalone-0":               false,
		"pvc-etc-splunk-customer-standalone-0":                         false,
		"splunk-customer-standalone-0":                                 false,
		"pvc-etc-splunk-scs-sanity-canary-old11-sa-standalone-0-extra": false,
		"pvc-etc-splunk-scs-sanity-canary-old11-sa-standalone-1":       false,
		"pvc-etc-splunk-scs-sanity-canary-old11-sa-indexer-0":          false,
		"splunk-scs-sanity-canary-old11-sa-standalone-notanordinal":    false,
	}
	check := func(obj metav1.Object) {
		got := staleCanaryOrphan(obj, now, 3*time.Hour)
		if expected, ok := want[obj.GetName()]; ok && got != expected {
			t.Errorf("staleCanaryOrphan(%s) = %v, want %v", obj.GetName(), got, expected)
		}
	}
	for _, p := range pvcs {
		check(p)
	}
	for _, p := range pods {
		check(p)
	}
}

func TestCanarySweepStaleReclaimsOrphansWithoutCRs(t *testing.T) {
	now := time.Now()
	pvcs, pods := orphanObjects(now)
	var objs []client.Object
	for _, p := range pvcs {
		objs = append(objs, p)
	}
	for _, p := range pods {
		objs = append(objs, p)
	}
	env, _, c := canaryFixtures(t, objs...)
	if err := env.sweepStale(context.Background(), now, 3*time.Hour); err != nil {
		t.Fatal(err)
	}
	gone := []string{
		"pvc-etc-splunk-scs-sanity-canary-old11-sa-standalone-0",
		"pvc-var-splunk-scs-sanity-canary-old11-sa-standalone-0",
	}
	for _, name := range gone {
		if exists(t, c, &corev1.PersistentVolumeClaim{}, name) {
			t.Errorf("stale canary PVC %s must be deleted even though its CR is gone", name)
		}
	}
	if exists(t, c, &corev1.Pod{}, "splunk-scs-sanity-canary-old11-sa-standalone-0") {
		t.Error("stale canary pod must be deleted")
	}
	kept := []string{
		"pvc-etc-splunk-scs-sanity-canary-new11-sa-standalone-0",
		"pvc-etc-splunk-customer-standalone-0",
		"pvc-etc-splunk-scs-sanity-canary-old11-sa-standalone-0-extra",
		"pvc-etc-splunk-scs-sanity-canary-old11-sa-standalone-1",
		"pvc-etc-splunk-scs-sanity-canary-old11-sa-indexer-0",
	}
	for _, name := range kept {
		if !exists(t, c, &corev1.PersistentVolumeClaim{}, name) {
			t.Errorf("PVC %s does not match both the canary naming and the stale age, so it must be kept", name)
		}
	}
	for _, name := range []string{"splunk-scs-sanity-canary-new11-sa-standalone-0", "splunk-customer-standalone-0", "splunk-scs-sanity-canary-old11-sa-standalone-notanordinal"} {
		if !exists(t, c, &corev1.Pod{}, name) {
			t.Errorf("pod %s must be kept", name)
		}
	}
}

func TestCanarySweepStaleOrphanListForbiddenIsSkipped(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newFakeScheme(t)).WithInterceptorFuncs(interceptor.Funcs{
		List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			switch list.(type) {
			case *corev1.PersistentVolumeClaimList, *corev1.PodList:
				return apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("no"))
			}
			return cl.List(ctx, list, opts...)
		},
	}).Build()
	env := canaryEnv{Writer: c, Reader: c, Namespace: canaryNS, Poll: time.Millisecond}
	if err := env.sweepStale(context.Background(), time.Now(), 3*time.Hour); err != nil {
		t.Fatalf("a forbidden orphan listing must not fail the sweep: %v", err)
	}
}

func TestCanaryAdmissionRejectionFailsFastAfterGrace(t *testing.T) {
	start := time.Now()
	forbidden := corev1.Event{
		Type: corev1.EventTypeWarning, Reason: "FailedCreate", InvolvedObject: corev1.ObjectReference{Name: "splunk-x-standalone"},
		Message:       `create Pod splunk-x-standalone-0 failed error: pods "splunk-x-standalone-0" is forbidden: [cpu max limit to request ratio per Container is 16, but provided ratio is 40.000000]`,
		LastTimestamp: metav1.NewTime(start.Add(time.Second)),
	}
	tracker := newWaitTracker(start)
	if err := canaryEnvironmentProblem(nil, []corev1.Event{forbidden}, start.Add(2*time.Second), tracker); err != nil {
		t.Fatalf("first sighting of a refusal must not be terminal, got %v", err)
	}
	if err := canaryEnvironmentProblem(nil, []corev1.Event{forbidden}, start.Add(60*time.Second), tracker); err != nil {
		t.Fatalf("a refusal just inside the grace period must not be terminal, got %v", err)
	}
	err := canaryEnvironmentProblem(nil, []corev1.Event{forbidden}, start.Add(63*time.Second), tracker)
	var terminal *terminalError
	if !errors.As(err, &terminal) {
		t.Fatalf("a persistent refusal must stop the wait, got %v", err)
	}
	for _, want := range []string{"not an operator regression", "rejected the canary pod", "LimitRange", "ResourceQuota", "admission policy", "limit to request ratio"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message must contain %q, got %v", want, err)
		}
	}
}

func TestCanaryAdmissionRejectionIsIgnoredWhenThePodExistsOrTheEventIsOld(t *testing.T) {
	start := time.Now()
	forbidden := corev1.Event{
		Type: corev1.EventTypeWarning, Reason: "FailedCreate", InvolvedObject: corev1.ObjectReference{Name: "splunk-x-standalone"},
		Message: `pods "splunk-x-standalone-0" is forbidden: nope`, LastTimestamp: metav1.NewTime(start.Add(time.Second)),
	}
	pod := envPod("splunk-x-standalone-0", "splunk-x-standalone", time.Minute)
	tracker := newWaitTracker(start)
	_ = canaryEnvironmentProblem([]corev1.Pod{pod}, []corev1.Event{forbidden}, start.Add(time.Second), tracker)
	if err := canaryEnvironmentProblem([]corev1.Pod{pod}, []corev1.Event{forbidden}, start.Add(5*time.Minute), tracker); err != nil {
		t.Errorf("a refusal that a later retry got past (the pod exists) must not fail the wait, got %v", err)
	}

	old := forbidden
	old.LastTimestamp = metav1.NewTime(start.Add(-time.Hour))
	tracker = newWaitTracker(start)
	_ = canaryEnvironmentProblem(nil, []corev1.Event{old}, start.Add(time.Second), tracker)
	if err := canaryEnvironmentProblem(nil, []corev1.Event{old}, start.Add(5*time.Minute), tracker); err != nil {
		t.Errorf("a refusal from before this wait step must be ignored, got %v", err)
	}

	cleared := newWaitTracker(start)
	_ = canaryEnvironmentProblem(nil, []corev1.Event{forbidden}, start.Add(time.Second), cleared)
	_ = canaryEnvironmentProblem(nil, nil, start.Add(30*time.Second), cleared)
	if err := canaryEnvironmentProblem(nil, []corev1.Event{forbidden}, start.Add(70*time.Second), cleared); err != nil {
		t.Errorf("a refusal that cleared and returned must start a new grace period, got %v", err)
	}
}

func TestRetryUntilKeepsTheLastObservationWhenTheDeadlineCutsACheckShort(t *testing.T) {
	calls := 0
	err := retryUntil(context.Background(), 150*time.Millisecond, 20*time.Millisecond, func(ctx context.Context) error {
		calls++
		if calls == 1 {
			return errors.New("phase=Pending ready=0/1")
		}
		<-ctx.Done()
		return fmt.Errorf("failed to get canary Standalone: %w", ctx.Err())
	})
	if err == nil || !strings.Contains(err.Error(), "phase=Pending ready=0/1") || strings.Contains(err.Error(), "failed to get") {
		t.Errorf("the timeout must report the last real observation, got %v", err)
	}

	err = retryUntil(context.Background(), 100*time.Millisecond, 20*time.Millisecond, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if err == nil {
		t.Error("a wait that never made an observation must still fail")
	}
}
