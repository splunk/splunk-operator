package operatorreadiness

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-logr/logr"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
)

const (
	// CheckCacheSynchronized reports whether controller-runtime has crossed its
	// cache synchronization boundary and started this non-leader runnable.
	CheckCacheSynchronized = "cache_synchronized"
	// CheckLeaderElectionAccess reports whether the service account has every
	// Lease verb required by the client-go LeaseLock.
	CheckLeaderElectionAccess = "leader_election_access"
	// CheckReconciliationParticipation is the aggregate Pod-readiness result.
	CheckReconciliationParticipation = "reconciliation_participation"

	ReasonCacheStarting            = "cache_starting"
	ReasonLeaseAccessNotChecked    = "lease_access_not_checked"
	ReasonLeaseAccessAllowed       = "lease_access_allowed"
	ReasonLeaseAccessDenied        = "lease_access_denied"
	ReasonAuthorizationReviewError = "authorization_review_error"
	ReasonLeaderElectionDisabled   = "leader_election_disabled"
	EventReasonReady               = "OperatorReconciliationReady"
	EventReasonNotReady            = "OperatorReconciliationNotReady"
	EventReasonRecovered           = "OperatorReconciliationRecovered"
	defaultRefreshInterval         = 10 * time.Second
	defaultRequestTimeout          = 3 * time.Second
	coordinationAPIGroup           = "coordination.k8s.io"
	leaseResource                  = "leases"
)

type leaseAction struct {
	verb     string
	usesName bool
}

var requiredLeaseActions = []leaseAction{
	{verb: "get", usesName: true},
	{verb: "create", usesName: false},
	{verb: "update", usesName: true},
}

// AccessReviewer is the narrow authorization client used by the monitor.
type AccessReviewer interface {
	Create(
		ctx context.Context,
		review *authorizationv1.SelfSubjectAccessReview,
		opts metav1.CreateOptions,
	) (*authorizationv1.SelfSubjectAccessReview, error)
}

// Options configures one manager readiness monitor.
type Options struct {
	LeaderElectionEnabled bool
	LeaseNamespace        string
	LeaseName             string
	PodNamespace          string
	PodName               string
	PodUID                types.UID
	RefreshInterval       time.Duration
	RequestTimeout        time.Duration
}

type readinessState struct {
	cacheSynchronized bool
	leaseAccess       bool
	reason            string
	reviewObserved    bool
	everFailed        bool
}

// Monitor starts after controller-runtime's cache synchronization boundary,
// periodically checks the current service account's leader-Lease capability,
// and serves a non-blocking healthz.Checker snapshot.
type Monitor struct {
	reviewer  AccessReviewer
	logger    logr.Logger
	recorder  record.EventRecorder
	options   Options
	telemetry telemetryRecorder

	mu    sync.RWMutex
	state readinessState
}

// New constructs a monitor backed by the controller-runtime Prometheus
// registry.
func New(
	reviewer AccessReviewer,
	logger logr.Logger,
	recorder record.EventRecorder,
	options Options,
) (*Monitor, error) {
	return newMonitor(reviewer, logger, recorder, options, prometheusTelemetry{})
}

func newMonitor(
	reviewer AccessReviewer,
	logger logr.Logger,
	recorder record.EventRecorder,
	options Options,
	telemetry telemetryRecorder,
) (*Monitor, error) {
	if options.RefreshInterval <= 0 {
		options.RefreshInterval = defaultRefreshInterval
	}
	if options.RequestTimeout <= 0 {
		options.RequestTimeout = defaultRequestTimeout
	}
	if options.LeaderElectionEnabled {
		switch {
		case reviewer == nil:
			return nil, errors.New("leader election readiness requires an authorization reviewer")
		case options.LeaseNamespace == "":
			return nil, errors.New("leader election readiness requires the Lease namespace")
		case options.LeaseName == "":
			return nil, errors.New("leader election readiness requires the Lease name")
		}
	}
	if telemetry == nil {
		return nil, errors.New("operator readiness requires a telemetry recorder")
	}

	monitor := &Monitor{
		reviewer:  reviewer,
		logger:    logger,
		recorder:  recorder,
		options:   options,
		telemetry: telemetry,
		state: readinessState{
			reason: ReasonCacheStarting,
		},
	}
	monitor.telemetry.SetCheck(CheckCacheSynchronized, false)
	monitor.telemetry.SetCheck(CheckLeaderElectionAccess, false)
	monitor.telemetry.SetCheck(CheckReconciliationParticipation, false)
	return monitor, nil
}

// NeedLeaderElection keeps the monitor running on both the leader and every
// non-leading contender. Controller-runtime starts non-leader runnables after
// its caches have synchronized.
func (*Monitor) NeedLeaderElection() bool {
	return false
}

// Start marks the controller-runtime cache boundary, performs an immediate
// authorization review, and refreshes it without blocking kubelet probes.
func (m *Monitor) Start(ctx context.Context) error {
	m.mu.Lock()
	m.state.cacheSynchronized = true
	m.state.reason = ReasonLeaseAccessNotChecked
	m.mu.Unlock()
	m.telemetry.SetCheck(CheckCacheSynchronized, true)

	if !m.options.LeaderElectionEnabled {
		m.applyLeaseAccess(true, ReasonLeaderElectionDisabled, nil)
		<-ctx.Done()
		return nil
	}

	m.refresh(ctx)
	ticker := time.NewTicker(m.options.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			m.refresh(ctx)
		}
	}
}

// Check implements healthz.Checker. It reads only in-memory state and never
// makes a Kubernetes API call on the kubelet request path.
func (m *Monitor) Check(_ *http.Request) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.state.cacheSynchronized {
		return fmt.Errorf("operator reconciliation participation is not ready: %s", ReasonCacheStarting)
	}
	if !m.state.leaseAccess {
		reason := m.state.reason
		if reason == "" {
			reason = ReasonLeaseAccessNotChecked
		}
		return fmt.Errorf("operator reconciliation participation is not ready: %s", reason)
	}
	return nil
}

func (m *Monitor) refresh(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, m.options.RequestTimeout)
	defer cancel()

	for _, action := range requiredLeaseActions {
		resourceName := ""
		if action.usesName {
			resourceName = m.options.LeaseName
		}
		review, err := m.reviewer.Create(ctx, &authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: m.options.LeaseNamespace,
					Verb:      action.verb,
					Group:     coordinationAPIGroup,
					Resource:  leaseResource,
					Name:      resourceName,
				},
			},
		}, metav1.CreateOptions{})
		if err != nil {
			if parent.Err() != nil {
				return
			}
			m.applyLeaseAccess(false, ReasonAuthorizationReviewError, err)
			return
		}
		if review == nil {
			m.applyLeaseAccess(false, ReasonAuthorizationReviewError, errors.New("authorization review returned no response"))
			return
		}
		if !review.Status.Allowed {
			cause := fmt.Errorf("authorization review denied Lease verb %q", action.verb)
			if review.Status.Reason != "" {
				cause = fmt.Errorf("authorization review denied Lease verb %q: %s", action.verb, review.Status.Reason)
			}
			m.applyLeaseAccess(false, ReasonLeaseAccessDenied, cause)
			return
		}
	}
	m.applyLeaseAccess(true, ReasonLeaseAccessAllowed, nil)
}

func (m *Monitor) applyLeaseAccess(allowed bool, reason string, cause error) {
	m.mu.Lock()
	previousObserved := m.state.reviewObserved
	previousReady := m.state.cacheSynchronized && m.state.leaseAccess
	previousReason := m.state.reason
	m.state.leaseAccess = allowed
	m.state.reason = reason
	m.state.reviewObserved = true
	ready := m.state.cacheSynchronized && m.state.leaseAccess
	changed := !previousObserved || previousReady != ready || previousReason != reason
	if !ready {
		m.state.everFailed = true
	}
	everFailed := m.state.everFailed
	m.mu.Unlock()

	m.telemetry.SetCheck(CheckLeaderElectionAccess, allowed)
	m.telemetry.SetCheck(CheckReconciliationParticipation, ready)
	if !changed {
		return
	}
	m.telemetry.RecordTransition(ready, reason)

	log := m.logger.WithValues(
		"ready", ready,
		"reason", reason,
		"lease_namespace", m.options.LeaseNamespace,
		"lease_name", m.options.LeaseName,
	)
	if ready {
		log.Info("Operator reconciliation participation is ready")
	} else if cause != nil {
		log.Error(cause, "Operator reconciliation participation is not ready")
	} else {
		log.Info("Operator reconciliation participation is not ready")
	}

	if m.recorder == nil || m.options.PodNamespace == "" || m.options.PodName == "" {
		return
	}
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: m.options.PodNamespace,
			Name:      m.options.PodName,
			UID:       m.options.PodUID,
		},
	}
	if ready {
		eventReason := EventReasonReady
		message := "Operator cache is synchronized and leader-election Lease access is available"
		if everFailed && previousObserved {
			eventReason = EventReasonRecovered
			message = "Operator reconciliation participation recovered"
		}
		m.recorder.Event(pod, corev1.EventTypeNormal, eventReason, message)
		return
	}
	m.recorder.Eventf(
		pod,
		corev1.EventTypeWarning,
		EventReasonNotReady,
		"Operator reconciliation participation is unavailable: %s",
		reason,
	)
}
