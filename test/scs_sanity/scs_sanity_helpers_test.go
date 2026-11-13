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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

func newFakeScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to register client-go scheme: %v", err)
	}
	if err := enterpriseApi.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to register enterpriseApi scheme: %v", err)
	}
	return scheme
}

func pod(name string, restarts int32, deleting bool) corev1.Pod {
	p := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "splunk-operator"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "manager"}}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{Name: "manager", RestartCount: restarts},
		}},
	}
	if deleting {
		now := metav1.Now()
		p.DeletionTimestamp = &now
	}
	return p
}

func TestEvaluateOperatorPodRestarts(t *testing.T) {
	crashed := pod("op-b", 3, false)
	crashed.Status.ContainerStatuses[0].LastTerminationState.Terminated = &corev1.ContainerStateTerminated{
		Reason: "Error", ExitCode: 2, FinishedAt: metav1.NewTime(time.Date(2026, 10, 28, 20, 0, 0, 0, time.UTC)),
	}
	backoff := pod("op-c", 1, false)
	backoff.Status.ContainerStatuses[0].State.Waiting = &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}

	tests := []struct {
		name    string
		pods    []corev1.Pod
		wantErr string
	}{
		{"clean", []corev1.Pod{pod("op-a", 0, false)}, ""},
		{"restarted", []corev1.Pod{crashed}, "op-b/manager restarted 3 time(s) (last exit: reason=Error exitCode=2 finishedAt=2026-10-28T20:00:00Z)"},
		{"restarted and now backing off", []corev1.Pod{backoff}, "op-c/manager restarted 1 time(s) (now waiting: CrashLoopBackOff)"},
		{"terminating old pod with restarts is ignored", []corev1.Pod{pod("old", 5, true), pod("op-a", 0, false)}, ""},
		{"no pods", nil, "no live operator pods"},
		{"only terminating pods", []corev1.Pod{pod("old", 0, true)}, "no live operator pods"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := evaluateOperatorPodRestarts(tt.pods, nil)
			assertErrContains(t, err, tt.wantErr)
		})
	}
}

func TestFindPanicLine(t *testing.T) {
	if _, found := findPanicLine("level=info msg=started\nreconcile ok\n"); found {
		t.Error("clean logs must not report a panic")
	}
	line, found := findPanicLine("info\n  panic: runtime error: index out of range [0] with length 0\ngoroutine 1")
	if !found || !strings.HasPrefix(line, "panic: runtime error") {
		t.Errorf("expected a trimmed panic line, got %q (found=%v)", line, found)
	}
	if _, found := findPanicLine(`{"msg":"Observed a panic in reconciler"}`); !found {
		t.Error("a recovered controller-runtime panic must be detected")
	}
	long, _ := findPanicLine("panic: " + strings.Repeat("x", 500))
	if len(long) > logLineMaxLen+3 {
		t.Errorf("long panic line was not truncated: %d chars", len(long))
	}
}

func TestScanOperatorLogsForPanics(t *testing.T) {
	pods := []corev1.Pod{pod("op-a", 0, false), pod("old", 0, true)}
	ctx := context.Background()
	noCutoff := time.Time{}

	clean := func(context.Context, string, string, string, *metav1.Time) (string, error) { return "all good", nil }
	if err := scanOperatorLogsForPanics(ctx, pods, clean, noCutoff); err != nil {
		t.Errorf("clean logs: unexpected error %v", err)
	}

	calls := 0
	panicky := func(_ context.Context, _, name, _ string, _ *metav1.Time) (string, error) {
		calls++
		return "panic: boom", nil
	}
	err := scanOperatorLogsForPanics(ctx, pods, panicky, noCutoff)
	assertErrContains(t, err, "op-a/manager: panic: boom")
	if calls != 1 {
		t.Errorf("terminating pod must not be read, fetcher called %d times", calls)
	}

	gone := func(context.Context, string, string, string, *metav1.Time) (string, error) {
		return "", apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "op-a")
	}
	if err := scanOperatorLogsForPanics(ctx, pods, gone, noCutoff); err != nil {
		t.Errorf("a pod that rolled away must be skipped, got %v", err)
	}

	denied := func(context.Context, string, string, string, *metav1.Time) (string, error) {
		return "", apierrors.NewForbidden(schema.GroupResource{Resource: "pods/log"}, "op-a", errors.New("no"))
	}
	err = scanOperatorLogsForPanics(ctx, pods, denied, noCutoff)
	if !apierrors.IsForbidden(err) {
		t.Errorf("Forbidden must stay detectable by callers, got %v", err)
	}
}

func TestParseUpgradeStart(t *testing.T) {
	if got, err := parseUpgradeStart(""); err != nil || !got.IsZero() {
		t.Errorf("empty must mean no cutoff, got %v, %v", got, err)
	}
	if got, err := parseUpgradeStart("  \n"); err != nil || !got.IsZero() {
		t.Errorf("blank must mean no cutoff, got %v, %v", got, err)
	}
	got, err := parseUpgradeStart("2026-10-29T01:02:03Z\n")
	if err != nil || !got.Equal(time.Date(2026, 10, 29, 1, 2, 3, 0, time.UTC)) {
		t.Errorf("valid RFC3339 UTC must parse, got %v, %v", got, err)
	}
	offset, err := parseUpgradeStart("2026-10-29T03:02:03+02:00")
	if err != nil || offset.Location() != time.UTC || !offset.Equal(got) {
		t.Errorf("an offset must normalise to UTC, got %v, %v", offset, err)
	}
	for _, bad := range []string{"yesterday", "2026-10-29", "1793000000", "2026-10-29 01:02:03"} {
		if _, err := parseUpgradeStart(bad); err == nil {
			t.Errorf("%q must be rejected loudly", bad)
		}
	}
}

func TestPanicScanSince(t *testing.T) {
	cutoff := time.Date(2026, 10, 29, 1, 0, 0, 0, time.UTC)
	made := func(created time.Time) corev1.Pod {
		p := pod("op", 0, false)
		p.CreationTimestamp = metav1.NewTime(created)
		return p
	}
	if got := panicScanSince(made(cutoff.Add(-time.Hour)), time.Time{}); got != nil {
		t.Errorf("no cutoff keeps the unbounded scan, got %v", got)
	}
	survivor := panicScanSince(made(cutoff.Add(-time.Hour)), cutoff)
	if survivor == nil || !survivor.Time.Equal(cutoff) {
		t.Errorf("a pod that predates the cutoff must only be scanned after it, got %v", survivor)
	}
	if got := panicScanSince(made(cutoff.Add(time.Second)), cutoff); got != nil {
		t.Errorf("a replacement pod created after the cutoff keeps its startup logs, got %v", got)
	}
	if got := panicScanSince(made(cutoff), cutoff); got != nil {
		t.Errorf("a pod created exactly at the cutoff is a replacement, got %v", got)
	}
}

func TestScanOperatorLogsForPanicsUsesPerPodWindow(t *testing.T) {
	cutoff := time.Date(2026, 10, 29, 1, 0, 0, 0, time.UTC)
	survivor, replacement := pod("survivor", 0, false), pod("replacement", 0, false)
	survivor.CreationTimestamp = metav1.NewTime(cutoff.Add(-24 * time.Hour))
	replacement.CreationTimestamp = metav1.NewTime(cutoff.Add(time.Minute))

	seen := map[string]*metav1.Time{}
	fetch := func(_ context.Context, _, name, _ string, since *metav1.Time) (string, error) {
		seen[name] = since
		return "all good", nil
	}
	if err := scanOperatorLogsForPanics(context.Background(), []corev1.Pod{survivor, replacement}, fetch, cutoff); err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if seen["survivor"] == nil || !seen["survivor"].Time.Equal(cutoff) {
		t.Errorf("surviving pod must be read since the cutoff, got %v", seen["survivor"])
	}
	if since, ok := seen["replacement"]; !ok || since != nil {
		t.Errorf("replacement pod must be read in full, got %v (read: %v)", since, ok)
	}
}

func crd(name, group string, established corev1.ConditionStatus, withCondition bool) apiextensionsv1.CustomResourceDefinition {
	c := apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       apiextensionsv1.CustomResourceDefinitionSpec{Group: group},
	}
	if withCondition {
		c.Status.Conditions = []apiextensionsv1.CustomResourceDefinitionCondition{
			{Type: apiextensionsv1.Established, Status: apiextensionsv1.ConditionStatus(established)},
		}
	}
	return c
}

func TestEvaluateCRDsEstablished(t *testing.T) {
	ok := crd("standalones.enterprise.splunk.com", "enterprise.splunk.com", corev1.ConditionTrue, true)
	other := crd("certificates.cert-manager.io", "cert-manager.io", corev1.ConditionFalse, true)

	tests := []struct {
		name    string
		crds    []apiextensionsv1.CustomResourceDefinition
		wantErr string
	}{
		{"all established, foreign CRDs ignored", []apiextensionsv1.CustomResourceDefinition{ok, other}, ""},
		{"platform group checked when present", []apiextensionsv1.CustomResourceDefinition{ok, crd("postgresclusters.platform.splunk.com", "platform.splunk.com", corev1.ConditionTrue, true)}, ""},
		{"enterprise CRD not established", []apiextensionsv1.CustomResourceDefinition{ok, crd("indexerclusters.enterprise.splunk.com", "enterprise.splunk.com", corev1.ConditionFalse, true)}, "indexerclusters.enterprise.splunk.com: Established=False"},
		{"platform CRD not established", []apiextensionsv1.CustomResourceDefinition{ok, crd("postgresclusters.platform.splunk.com", "platform.splunk.com", corev1.ConditionFalse, true)}, "postgresclusters.platform.splunk.com"},
		{"no condition reported", []apiextensionsv1.CustomResourceDefinition{ok, crd("clustermanagers.enterprise.splunk.com", "enterprise.splunk.com", "", false)}, "no Established condition"},
		{"no enterprise CRDs at all", []apiextensionsv1.CustomResourceDefinition{other}, "no CustomResourceDefinitions found in group enterprise.splunk.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertErrContains(t, evaluateCRDsEstablished(tt.crds), tt.wantErr)
		})
	}
}

const fleetNS = "sok-fleet"

func oldCR(now time.Time) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: fleetNS, UID: types.UID("uid"), CreationTimestamp: metav1.NewTime(now.Add(-time.Hour))}
}

func goodPDB(cr sokCR) *policyv1.PodDisruptionBudget {
	maxUnavailable := intstr.FromInt32(1)
	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Instance, Namespace: cr.Namespace,
			OwnerReferences: []metav1.OwnerReference{{Kind: cr.Kind, UID: cr.UID, Name: cr.Name}},
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MaxUnavailable: &maxUnavailable,
			Selector:       &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/instance": cr.Instance}},
		},
	}
}

func stsFor(cr sokCR) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: cr.Instance, Namespace: cr.Namespace},
		Spec: appsv1.StatefulSetSpec{Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app.kubernetes.io/instance": cr.Instance}},
		}},
	}
}

func TestPDBProblems(t *testing.T) {
	cr := sokCR{Kind: "IndexerCluster", Namespace: fleetNS, Name: "idx", UID: "uid", Instance: "splunk-idx-indexer"}

	if got := pdbProblems(cr, goodPDB(cr), stsFor(cr)); len(got) != 0 {
		t.Errorf("healthy PDB reported problems: %v", got)
	}
	if got := pdbProblems(cr, nil, stsFor(cr)); len(got) != 1 || !strings.Contains(got[0], "is missing") {
		t.Errorf("missing PDB not reported: %v", got)
	}

	notOwned := goodPDB(cr)
	notOwned.OwnerReferences = []metav1.OwnerReference{{Kind: cr.Kind, UID: "someone-else"}}
	if got := pdbProblems(cr, notOwned, stsFor(cr)); len(got) != 1 || !strings.Contains(got[0], "not owned") {
		t.Errorf("foreign owner not reported: %v", got)
	}

	two := goodPDB(cr)
	v := intstr.FromInt32(2)
	two.Spec.MaxUnavailable = &v
	if got := pdbProblems(cr, two, stsFor(cr)); len(got) != 1 || !strings.Contains(got[0], "maxUnavailable is 2") {
		t.Errorf("wrong maxUnavailable not reported: %v", got)
	}

	unset := goodPDB(cr)
	unset.Spec.MaxUnavailable = nil
	if got := pdbProblems(cr, unset, stsFor(cr)); len(got) != 1 || !strings.Contains(got[0], "maxUnavailable is unset") {
		t.Errorf("unset maxUnavailable not reported: %v", got)
	}

	wrongSelector := goodPDB(cr)
	wrongSelector.Spec.Selector.MatchLabels = map[string]string{"app.kubernetes.io/instance": "other"}
	if got := pdbProblems(cr, wrongSelector, stsFor(cr)); len(got) != 1 || !strings.Contains(got[0], "does not match") {
		t.Errorf("selector mismatch not reported: %v", got)
	}

	emptySelector := goodPDB(cr)
	emptySelector.Spec.Selector = &metav1.LabelSelector{}
	if got := pdbProblems(cr, emptySelector, stsFor(cr)); len(got) != 1 || !strings.Contains(got[0], "no usable selector") {
		t.Errorf("empty selector (matches every pod) not reported: %v", got)
	}

	if got := pdbProblems(cr, goodPDB(cr), nil); len(got) != 1 || !strings.Contains(got[0], "StatefulSet splunk-idx-indexer is missing") {
		t.Errorf("missing StatefulSet not reported: %v", got)
	}
}

type fleetFixture struct {
	now    time.Time
	scheme *runtime.Scheme
	idx    *enterpriseApi.IndexerCluster
	ing    *enterpriseApi.IngestorCluster
	idxCR  sokCR
	ingCR  sokCR
}

func newFleetFixture(t *testing.T) fleetFixture {
	t.Helper()
	now := time.Now()
	idxMeta := oldCR(now)
	idxMeta.Name = "idx"
	ingMeta := oldCR(now)
	ingMeta.Name = "ing"
	ingMeta.UID = "uid-ing"
	return fleetFixture{
		now:    now,
		scheme: newFakeScheme(t),
		idx:    &enterpriseApi.IndexerCluster{ObjectMeta: idxMeta},
		ing:    &enterpriseApi.IngestorCluster{ObjectMeta: ingMeta},
		idxCR:  sokCR{Kind: "IndexerCluster", Namespace: fleetNS, Name: "idx", UID: "uid", Instance: "splunk-idx-indexer"},
		ingCR:  sokCR{Kind: "IngestorCluster", Namespace: fleetNS, Name: "ing", UID: "uid-ing", Instance: "splunk-ing-ingestor"},
	}
}

func (f fleetFixture) client(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(f.scheme).WithObjects(objs...).Build()
}

func (f fleetFixture) clientWith(funcs interceptor.Funcs, objs ...client.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(f.scheme).WithObjects(objs...).WithInterceptorFuncs(funcs).Build()
}

func TestCheckPDBFleetHealthyFleet(t *testing.T) {
	f := newFleetFixture(t)
	res, err := checkPDBFleet(context.Background(), f.client(f.idx, f.ing, goodPDB(f.idxCR), stsFor(f.idxCR), goodPDB(f.ingCR), stsFor(f.ingCR)), f.now)
	if err != nil || res.SkipReason != "" || len(res.Problems) != 0 || res.Checked != 2 {
		t.Errorf("unexpected result: %+v err=%v", res, err)
	}
}

func TestCheckPDBFleetEmptyClusterPasses(t *testing.T) {
	f := newFleetFixture(t)
	res, err := checkPDBFleet(context.Background(), f.client(), f.now)
	if err != nil || res.Checked != 0 || len(res.Problems) != 0 {
		t.Errorf("unexpected result: %+v err=%v", res, err)
	}
}

func TestCheckPDBFleetMissingPDBIsReported(t *testing.T) {
	f := newFleetFixture(t)
	res, err := checkPDBFleet(context.Background(), f.client(f.idx, stsFor(f.idxCR)), f.now)
	if err != nil || len(res.Problems) != 1 || !strings.Contains(res.Problems[0], "is missing") {
		t.Errorf("unexpected result: %+v err=%v", res, err)
	}
}

func TestCheckPDBFleetYoungAndDeletingCRsAreSkipped(t *testing.T) {
	f := newFleetFixture(t)
	young := f.idx.DeepCopy()
	young.CreationTimestamp = metav1.NewTime(f.now.Add(-time.Minute))
	deleting := f.ing.DeepCopy()
	ts := metav1.NewTime(f.now)
	deleting.DeletionTimestamp = &ts
	deleting.Finalizers = []string{"splunk.com/test"}
	res, err := checkPDBFleet(context.Background(), f.client(young, deleting), f.now)
	if err != nil || res.Checked != 0 || len(res.Problems) != 0 {
		t.Errorf("unexpected result: %+v err=%v", res, err)
	}
}

func TestCheckPDBFleetPausedCRsAreSkipped(t *testing.T) {
	f := newFleetFixture(t)
	pausedIdx := f.idx.DeepCopy()
	pausedIdx.Annotations = map[string]string{enterpriseApi.IndexerClusterPausedAnnotation: "true"}
	pausedIng := f.ing.DeepCopy()
	pausedIng.Annotations = map[string]string{enterpriseApi.IngestorClusterPausedAnnotation: "true"}
	res, err := checkPDBFleet(context.Background(), f.client(pausedIdx, pausedIng), f.now)
	if err != nil || res.Checked != 0 || len(res.Problems) != 0 {
		t.Errorf("unexpected result: %+v err=%v", res, err)
	}
	notPaused := f.idx.DeepCopy()
	notPaused.Annotations = map[string]string{enterpriseApi.IndexerClusterPausedAnnotation: "false"}
	res, err = checkPDBFleet(context.Background(), f.client(notPaused, stsFor(f.idxCR)), f.now)
	if err != nil || res.Checked != 1 || len(res.Problems) != 1 {
		t.Errorf("a CR with paused=false must be checked: %+v err=%v", res, err)
	}
}

func TestCheckPDBFleetForbiddenListSkipsWithAReason(t *testing.T) {
	f := newFleetFixture(t)
	c := f.clientWith(interceptor.Funcs{
		List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
			return apierrors.NewForbidden(schema.GroupResource{Resource: "indexerclusters"}, "", errors.New("no"))
		},
	})
	res, err := checkPDBFleet(context.Background(), c, f.now)
	if err != nil || !strings.Contains(res.SkipReason, "cannot list IndexerClusters") {
		t.Errorf("expected a skip, got %+v err=%v", res, err)
	}
}

func TestCheckPDBFleetOtherListErrorsFail(t *testing.T) {
	f := newFleetFixture(t)
	c := f.clientWith(interceptor.Funcs{
		List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
			return errors.New("connection refused")
		},
	})
	if _, err := checkPDBFleet(context.Background(), c, f.now); err == nil {
		t.Error("expected an error for a non-access failure")
	}
}

func TestCheckPDBFleetForbiddenPDBGetSkips(t *testing.T) {
	f := newFleetFixture(t)
	c := f.clientWith(interceptor.Funcs{
		Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
			if _, ok := obj.(*policyv1.PodDisruptionBudget); ok {
				return apierrors.NewForbidden(schema.GroupResource{Resource: "poddisruptionbudgets"}, "x", errors.New("no"))
			}
			return nil
		},
	}, f.idx)
	res, err := checkPDBFleet(context.Background(), c, f.now)
	if err != nil || !strings.Contains(res.SkipReason, "cannot read PodDisruptionBudgets") {
		t.Errorf("expected a skip, got %+v err=%v", res, err)
	}
}

func assertErrContains(t *testing.T, err error, want string) {
	t.Helper()
	switch {
	case want == "" && err != nil:
		t.Errorf("unexpected error: %v", err)
	case want != "" && err == nil:
		t.Errorf("expected an error containing %q, got nil", want)
	case want != "" && !strings.Contains(err.Error(), want):
		t.Errorf("expected an error containing %q, got %q", want, err.Error())
	}
}

func TestAdviseOnPDBFleet(t *testing.T) {
	scheme := newFakeScheme(t)
	now := time.Now()
	idxMeta := oldCR(now)
	idxMeta.Name = "idx"
	idx := &enterpriseApi.IndexerCluster{ObjectMeta: idxMeta}
	idxCR := sokCR{Kind: "IndexerCluster", Namespace: fleetNS, Name: "idx", UID: "uid", Instance: "splunk-idx-indexer"}

	t.Run("healthy fleet has no warnings", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(idx, goodPDB(idxCR), stsFor(idxCR)).Build()
		got := adviseOnPDBFleet(context.Background(), c, 50*time.Millisecond, 5*time.Millisecond)
		if got.Skip != "" || len(got.Warnings) != 0 || got.Checked != 1 {
			t.Errorf("unexpected advice: %+v", got)
		}
	})

	t.Run("a persistent problem becomes a warning, never an error", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(idx, stsFor(idxCR)).Build()
		got := adviseOnPDBFleet(context.Background(), c, 40*time.Millisecond, 5*time.Millisecond)
		if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "is missing") || got.Checked != 1 {
			t.Errorf("unexpected advice: %+v", got)
		}
	})

	t.Run("a problem that clears during the retry window is not reported", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(idx, stsFor(idxCR)).Build()
		go func() {
			time.Sleep(30 * time.Millisecond)
			_ = c.Create(context.Background(), goodPDB(idxCR))
		}()
		got := adviseOnPDBFleet(context.Background(), c, 2*time.Second, 5*time.Millisecond)
		if len(got.Warnings) != 0 || got.Skip != "" {
			t.Errorf("a recovered PDB must not be reported: %+v", got)
		}
	})

	t.Run("no access skips", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
				return apierrors.NewForbidden(schema.GroupResource{Resource: "indexerclusters"}, "", errors.New("no"))
			},
		}).Build()
		got := adviseOnPDBFleet(context.Background(), c, 40*time.Millisecond, 5*time.Millisecond)
		if !strings.Contains(got.Skip, "cannot list IndexerClusters") || len(got.Warnings) != 0 {
			t.Errorf("unexpected advice: %+v", got)
		}
	})

	t.Run("an unexpected API error is a warning, not a failure", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
				return errors.New("connection refused")
			},
		}).Build()
		got := adviseOnPDBFleet(context.Background(), c, 40*time.Millisecond, 5*time.Millisecond)
		if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "connection refused") {
			t.Errorf("unexpected advice: %+v", got)
		}
	})
}

func uidPod(name, uid string, restarts int32) corev1.Pod {
	p := pod(name, restarts, false)
	p.UID = types.UID(uid)
	return p
}

func TestEvaluateOperatorPodRestartsAgainstBaseline(t *testing.T) {
	baseline := podRestartBaseline{
		"uid-old":   {"manager": 3},
		"uid-quiet": {"manager": 0},
	}
	tests := []struct {
		name     string
		pods     []corev1.Pod
		baseline podRestartBaseline
		wantErr  string
	}{
		{"historical restarts on a surviving pod are ignored", []corev1.Pod{uidPod("op-a", "uid-old", 3)}, baseline, ""},
		{"new restarts on a surviving pod fail", []corev1.Pod{uidPod("op-a", "uid-old", 5)}, baseline,
			"op-a/manager restarted 2 more time(s) since the pre-upgrade baseline (now 5, was 3)"},
		{"one restart on a previously quiet pod fails", []corev1.Pod{uidPod("op-b", "uid-quiet", 1)}, baseline,
			"op-b/manager restarted 1 more time(s) since the pre-upgrade baseline (now 1, was 0)"},
		{"a replacement pod with any restart fails", []corev1.Pod{uidPod("op-c", "uid-new", 1)}, baseline, "op-c/manager restarted 1 time(s)"},
		{"a clean replacement pod passes", []corev1.Pod{uidPod("op-c", "uid-new", 0)}, baseline, ""},
		{"a container missing from the baseline starts at zero", []corev1.Pod{uidPod("op-d", "uid-empty", 2)}, podRestartBaseline{"uid-empty": {}},
			"op-d/manager restarted 2 more time(s)"},
		{"no baseline means every restart counts", []corev1.Pod{uidPod("op-a", "uid-old", 3)}, nil, "op-a/manager restarted 3 time(s)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertErrContains(t, evaluateOperatorPodRestarts(tt.pods, tt.baseline), tt.wantErr)
		})
	}
}

func TestLoadOperatorPodBaseline(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if b, err := loadOperatorPodBaseline(""); b != nil || err != nil {
		t.Errorf("an empty path must mean no baseline, got %v, %v", b, err)
	}
	good := write("good.json", `[{"name":"op-a","uid":"uid-1","containers":[{"name":"manager","restartCount":4},{"name":"proxy","restartCount":0}]}]`)
	b, err := loadOperatorPodBaseline(good)
	if err != nil || b["uid-1"]["manager"] != 4 || len(b["uid-1"]) != 2 {
		t.Errorf("unexpected baseline %v, %v", b, err)
	}
	empty := write("empty.json", `[]`)
	if b, err := loadOperatorPodBaseline(empty); err != nil || b == nil || len(b) != 0 {
		t.Errorf("an empty list is a valid first-install baseline, got %v, %v", b, err)
	}
	if _, err := loadOperatorPodBaseline(write("bad.json", `{not json`)); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("invalid JSON must fail loudly, got %v", err)
	}
	if _, err := loadOperatorPodBaseline(filepath.Join(dir, "missing.json")); err == nil || !strings.Contains(err.Error(), "failed to read") {
		t.Errorf("a named but missing file must fail loudly, got %v", err)
	}
}
