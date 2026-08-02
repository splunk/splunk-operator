package operatorreadiness

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
)

type accessReviewResult struct {
	allowed bool
	reason  string
	err     error
}

type cacheSynchronizerFunc func(context.Context) error

func (f cacheSynchronizerFunc) Synchronize(ctx context.Context) error {
	return f(ctx)
}

var synchronizedCache = cacheSynchronizerFunc(func(context.Context) error { return nil })

type scriptedAccessReviewer struct {
	mu      sync.Mutex
	results []accessReviewResult
	reviews []*authorizationv1.SelfSubjectAccessReview
}

func (r *scriptedAccessReviewer) Create(
	_ context.Context,
	review *authorizationv1.SelfSubjectAccessReview,
	_ metav1.CreateOptions,
) (*authorizationv1.SelfSubjectAccessReview, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reviews = append(r.reviews, review.DeepCopy())
	if len(r.results) == 0 {
		return nil, errors.New("unexpected review")
	}
	result := r.results[0]
	if len(r.results) > 1 {
		r.results = r.results[1:]
	}
	if result.err != nil {
		return nil, result.err
	}
	return &authorizationv1.SelfSubjectAccessReview{
		Status: authorizationv1.SubjectAccessReviewStatus{
			Allowed: result.allowed,
			Reason:  result.reason,
		},
	}, nil
}

func (r *scriptedAccessReviewer) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.reviews)
}

func (r *scriptedAccessReviewer) requestedAttributes() []authorizationv1.ResourceAttributes {
	r.mu.Lock()
	defer r.mu.Unlock()
	attributes := make([]authorizationv1.ResourceAttributes, 0, len(r.reviews))
	for _, review := range r.reviews {
		if review.Spec.ResourceAttributes != nil {
			attributes = append(attributes, *review.Spec.ResourceAttributes.DeepCopy())
		}
	}
	return attributes
}

type telemetryTransition struct {
	ready  bool
	reason string
}

type fakeTelemetry struct {
	mu          sync.Mutex
	checks      map[string]bool
	transitions []telemetryTransition
}

func newFakeTelemetry() *fakeTelemetry {
	return &fakeTelemetry{checks: map[string]bool{}}
}

func (m *fakeTelemetry) SetCheck(name string, ready bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checks[name] = ready
}

func (m *fakeTelemetry) RecordTransition(ready bool, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transitions = append(m.transitions, telemetryTransition{ready: ready, reason: reason})
}

func (m *fakeTelemetry) snapshot() (map[string]bool, []telemetryTransition) {
	m.mu.Lock()
	defer m.mu.Unlock()
	checks := make(map[string]bool, len(m.checks))
	for key, value := range m.checks {
		checks[key] = value
	}
	transitions := append([]telemetryTransition(nil), m.transitions...)
	return checks, transitions
}

func allowedReviews(count int) []accessReviewResult {
	results := make([]accessReviewResult, count)
	for index := range results {
		results[index].allowed = true
	}
	return results
}

func newTestMonitor(
	t *testing.T,
	reviewer AccessReviewer,
	leaderElection bool,
	refreshInterval time.Duration,
) (*Monitor, *record.FakeRecorder, *fakeTelemetry) {
	return newTestMonitorWithCache(t, reviewer, synchronizedCache, leaderElection, refreshInterval)
}

func newTestMonitorWithCache(
	t *testing.T,
	reviewer AccessReviewer,
	cacheSynchronizer CacheSynchronizer,
	leaderElection bool,
	refreshInterval time.Duration,
) (*Monitor, *record.FakeRecorder, *fakeTelemetry) {
	t.Helper()
	events := record.NewFakeRecorder(20)
	telemetry := newFakeTelemetry()
	monitor, err := newMonitor(reviewer, cacheSynchronizer, logr.Discard(), events, Options{
		LeaderElectionEnabled: leaderElection,
		LeaseNamespace:        "operator-system",
		LeaseName:             "270bec8c.splunk.com",
		PodNamespace:          "operator-system",
		PodName:               "operator-0",
		PodUID:                "operator-uid",
		RefreshInterval:       refreshInterval,
		RequestTimeout:        50 * time.Millisecond,
	}, telemetry)
	if err != nil {
		t.Fatalf("newMonitor() error = %v", err)
	}
	return monitor, events, telemetry
}

func warmupMonitor(t *testing.T, ctx context.Context, monitor *Monitor) {
	t.Helper()
	if err := monitor.Warmup(ctx); err != nil {
		t.Fatalf("Warmup() error = %v", err)
	}
}

func checkError(monitor *Monitor) error {
	return monitor.Check(&http.Request{})
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true before timeout")
}

func TestMonitorStartsNotReady(t *testing.T) {
	monitor, _, telemetry := newTestMonitor(t, &scriptedAccessReviewer{
		results: allowedReviews(3),
	}, true, time.Hour)

	if err := checkError(monitor); err == nil || !strings.Contains(err.Error(), ReasonCacheStarting) {
		t.Fatalf("Check() before Start error = %v, want %q", err, ReasonCacheStarting)
	}
	checks, transitions := telemetry.snapshot()
	if checks[CheckCacheSynchronized] || checks[CheckLeaderElectionAccess] || checks[CheckReconciliationParticipation] {
		t.Fatalf("initial checks = %#v, want all false", checks)
	}
	if len(transitions) != 0 {
		t.Fatalf("initial transitions = %#v, want none", transitions)
	}
}

func TestMonitorBecomesReadyAfterCacheAndExactLeaseReviews(t *testing.T) {
	reviewer := &scriptedAccessReviewer{results: allowedReviews(3)}
	monitor, events, telemetry := newTestMonitor(t, reviewer, true, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- monitor.Start(ctx) }()
	warmupMonitor(t, ctx, monitor)

	waitFor(t, time.Second, func() bool { return checkError(monitor) == nil })
	if reviewer.callCount() != 3 {
		t.Fatalf("review calls = %d, want 3", reviewer.callCount())
	}
	attributes := reviewer.requestedAttributes()
	for index, action := range requiredLeaseActions {
		got := attributes[index]
		wantName := ""
		if action.usesName {
			wantName = "270bec8c.splunk.com"
		}
		if got.Namespace != "operator-system" || got.Verb != action.verb ||
			got.Group != coordinationAPIGroup || got.Resource != leaseResource ||
			got.Name != wantName {
			t.Fatalf("review %d attributes = %#v, want exact %q Lease action with name %q", index, got, action.verb, wantName)
		}
	}
	checks, transitions := telemetry.snapshot()
	if !checks[CheckCacheSynchronized] || !checks[CheckLeaderElectionAccess] || !checks[CheckReconciliationParticipation] {
		t.Fatalf("ready checks = %#v, want all true", checks)
	}
	if len(transitions) != 1 || !transitions[0].ready || transitions[0].reason != ReasonLeaseAccessAllowed {
		t.Fatalf("transitions = %#v, want one ready transition", transitions)
	}
	select {
	case event := <-events.Events:
		if !strings.Contains(event, EventReasonReady) {
			t.Fatalf("event = %q, want reason %q", event, EventReasonReady)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ready Event")
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestMonitorSkipsLeaseReviewWhenLeaderElectionIsDisabled(t *testing.T) {
	reviewer := &scriptedAccessReviewer{}
	monitor, _, telemetry := newTestMonitor(t, reviewer, false, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- monitor.Start(ctx) }()
	warmupMonitor(t, ctx, monitor)

	waitFor(t, time.Second, func() bool { return checkError(monitor) == nil })
	if reviewer.callCount() != 0 {
		t.Fatalf("review calls = %d, want 0", reviewer.callCount())
	}
	checks, transitions := telemetry.snapshot()
	if !checks[CheckCacheSynchronized] || !checks[CheckLeaderElectionAccess] || !checks[CheckReconciliationParticipation] {
		t.Fatalf("ready checks = %#v, want all true", checks)
	}
	if len(transitions) != 1 || transitions[0].reason != ReasonLeaderElectionDisabled {
		t.Fatalf("transitions = %#v, want disabled ready transition", transitions)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestMonitorDeniedVerbThenRecoveryDoesNotRestartRunnable(t *testing.T) {
	results := []accessReviewResult{
		{allowed: true},
		{allowed: true},
		{allowed: false, reason: "RBAC denied update"},
	}
	results = append(results, allowedReviews(3)...)
	reviewer := &scriptedAccessReviewer{results: results}
	monitor, events, telemetry := newTestMonitor(t, reviewer, true, 5*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- monitor.Start(ctx) }()
	warmupMonitor(t, ctx, monitor)

	waitFor(t, time.Second, func() bool {
		_, transitions := telemetry.snapshot()
		return len(transitions) >= 1 && !transitions[0].ready
	})
	waitFor(t, time.Second, func() bool { return checkError(monitor) == nil })
	if reviewer.callCount() < 6 {
		t.Fatalf("review calls = %d, want at least 6", reviewer.callCount())
	}
	_, transitions := telemetry.snapshot()
	if len(transitions) != 2 || transitions[0].ready || !transitions[1].ready {
		t.Fatalf("transitions = %#v, want not-ready then ready", transitions)
	}

	first := <-events.Events
	second := <-events.Events
	if !strings.Contains(first, EventReasonNotReady) || !strings.Contains(second, EventReasonRecovered) {
		t.Fatalf("events = [%q, %q], want not-ready then recovered", first, second)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestMonitorRepeatedAPIFailureIsTransitionBounded(t *testing.T) {
	reviewer := &scriptedAccessReviewer{results: []accessReviewResult{{err: errors.New("api unavailable")}}}
	monitor, events, telemetry := newTestMonitor(t, reviewer, true, 5*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- monitor.Start(ctx) }()
	warmupMonitor(t, ctx, monitor)

	waitFor(t, time.Second, func() bool { return reviewer.callCount() >= 3 })
	if err := checkError(monitor); err == nil || !strings.Contains(err.Error(), ReasonAuthorizationReviewError) {
		t.Fatalf("Check() error = %v, want %q", err, ReasonAuthorizationReviewError)
	}
	_, transitions := telemetry.snapshot()
	if len(transitions) != 1 || transitions[0].ready {
		t.Fatalf("transitions = %#v, want one not-ready transition", transitions)
	}
	select {
	case event := <-events.Events:
		if !strings.Contains(event, EventReasonNotReady) {
			t.Fatalf("event = %q, want reason %q", event, EventReasonNotReady)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for failure Event")
	}
	select {
	case event := <-events.Events:
		t.Fatalf("unexpected repeated Event %q", event)
	case <-time.After(20 * time.Millisecond):
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestMonitorStopsReviewCycleAtFirstDeniedVerb(t *testing.T) {
	reviewer := &scriptedAccessReviewer{results: []accessReviewResult{
		{allowed: true},
		{allowed: false},
	}}
	monitor, _, _ := newTestMonitor(t, reviewer, true, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- monitor.Start(ctx) }()
	warmupMonitor(t, ctx, monitor)

	waitFor(t, time.Second, func() bool { return reviewer.callCount() == 2 })
	if err := checkError(monitor); err == nil || !strings.Contains(err.Error(), ReasonLeaseAccessDenied) {
		t.Fatalf("Check() error = %v, want %q", err, ReasonLeaseAccessDenied)
	}
	attributes := reviewer.requestedAttributes()
	if len(attributes) != 2 || attributes[0].Verb != "get" || attributes[1].Verb != "create" {
		t.Fatalf("requested attributes = %#v, want get then create only", attributes)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestMonitorCheckIsConcurrentWithRefresh(t *testing.T) {
	reviewer := &scriptedAccessReviewer{results: allowedReviews(3)}
	monitor, _, _ := newTestMonitor(t, reviewer, true, time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- monitor.Start(ctx) }()
	warmupMonitor(t, ctx, monitor)
	waitFor(t, time.Second, func() bool { return checkError(monitor) == nil })

	var workers sync.WaitGroup
	for range 20 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 100 {
				_ = checkError(monitor)
			}
		}()
	}
	workers.Wait()

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestNewMonitorValidatesLeaderElectionInputs(t *testing.T) {
	_, err := newMonitor(nil, synchronizedCache, logr.Discard(), nil, Options{
		LeaderElectionEnabled: true,
		RefreshInterval:       time.Second,
		RequestTimeout:        time.Second,
	}, newFakeTelemetry())
	if err == nil {
		t.Fatal("newMonitor() error = nil, want invalid configuration")
	}
}

func TestMonitorWaitsForExplicitInformerSynchronization(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	synchronizer := cacheSynchronizerFunc(func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	monitor, _, telemetry := newTestMonitorWithCache(t,
		&scriptedAccessReviewer{results: allowedReviews(3)}, synchronizer, true, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- monitor.Start(ctx) }()
	warmDone := make(chan error, 1)
	go func() { warmDone <- monitor.Warmup(ctx) }()

	<-started
	waitFor(t, time.Second, func() bool {
		checks, _ := telemetry.snapshot()
		return checks[CheckLeaderElectionAccess]
	})
	if err := checkError(monitor); err == nil || !strings.Contains(err.Error(), ReasonCacheStarting) {
		t.Fatalf("Check() before informer sync error = %v, want %q", err, ReasonCacheStarting)
	}
	checks, transitions := telemetry.snapshot()
	if checks[CheckCacheSynchronized] || checks[CheckReconciliationParticipation] || len(transitions) != 0 {
		t.Fatalf("pre-sync telemetry = (%#v, %#v), want cache and aggregate false with no transition", checks, transitions)
	}

	close(release)
	if err := <-warmDone; err != nil {
		t.Fatalf("Warmup() error = %v", err)
	}
	waitFor(t, time.Second, func() bool { return checkError(monitor) == nil })

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}
