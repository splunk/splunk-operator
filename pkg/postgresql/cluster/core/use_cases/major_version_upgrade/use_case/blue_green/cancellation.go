/*
Copyright 2026.

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

package bluegreen

import (
	"context"
	"errors"
	"fmt"
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Canceller performs the external steps of a pre-commit cancellation. Each
// step is idempotent and reports completion only from observed state.
type Canceller interface {
	// CommitObserved reports whether the canonical record publishes green.
	CommitObserved(ctx context.Context, attemptID string) (bool, error)
	// RestoreSource restores and verifies blue endpoints and access.
	RestoreSource(ctx context.Context, intent mvutypes.Intent, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error)
	RemoveReplication(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error)
	RemoveCredentials(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error)
	// FenceCandidate applies and verifies the one-way green fence.
	FenceCandidate(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error)
}

const cancellationStepTimeout = 30 * time.Second

// WithCanceller returns a flow that can cancel attempts before commit.
func (f Flow) WithCanceller(canceller Canceller) Flow {
	f.canceller = canceller
	return f
}

// advanceCancellation latches the request, then completes one step per pass:
// restore blue, remove replication, remove credentials, fence green.
func (f Flow) advanceCancellation(ctx context.Context, intent mvutypes.Intent, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (Outcome, error) {
	if f.canceller == nil {
		return Outcome{}, errors.Join(mvutypes.ErrUpgradeFlowFailed, errors.New("blue/green cancellation port is not configured"))
	}
	if status == nil {
		status = &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: intent.AttemptID}
	}
	// An earlier canonical write can finish after cancellation was latched.
	// Recheck before every step; only fail-forward is safe once it lands.
	observation, cancel := context.WithTimeout(ctx, cancellationStepTimeout)
	committed, err := f.canceller.CommitObserved(observation, intent.AttemptID)
	cancel()
	if err != nil {
		return cancellingOutcome(status, "Checking the endpoint commit before cancelling.", err), err
	}
	if committed {
		return f.advanceSwitchover(ctx, intent, status)
	}
	if !mvutypes.BlueGreenCancelling(status) {
		if status.Gates == nil {
			status.Gates = &platformv1alpha1.BlueGreenGateStatus{}
		}
		status.Gates.Cancellation = &platformv1alpha1.BlueGreenCancellationStatus{RequestedAt: new(metav1.Now())}
		return cancellingOutcome(status, mvutypes.MessageCancellationInProgress, nil), nil
	}
	cancellation := status.Gates.Cancellation
	for _, step := range []struct {
		receipt **metav1.Time
		run     func(context.Context) (bool, error)
		message string
	}{
		{&cancellation.SourceAccessRestoredAt, func(ctx context.Context) (bool, error) { return f.canceller.RestoreSource(ctx, intent, status) }, "Restoring and verifying blue access."},
		{&cancellation.ReplicationRemovedAt, func(ctx context.Context) (bool, error) { return f.canceller.RemoveReplication(ctx, status) }, "Removing logical replication."},
		{&cancellation.CredentialsRemovedAt, func(ctx context.Context) (bool, error) { return f.canceller.RemoveCredentials(ctx, status) }, "Removing temporary credentials."},
		{&cancellation.GreenFencedAt, func(ctx context.Context) (bool, error) { return f.canceller.FenceCandidate(ctx, status) }, "Fencing the retained green candidate."},
	} {
		if *step.receipt != nil {
			continue
		}
		bounded, cancel := context.WithTimeout(ctx, cancellationStepTimeout)
		done, err := step.run(bounded)
		cancel()
		if err != nil || !done {
			return cancellingOutcome(status, step.message, err), err
		}
		*step.receipt = new(metav1.Now())
		return cancellingOutcome(status, step.message, nil), nil
	}
	if cancellation.CompletedAt == nil {
		cancellation.CompletedAt = new(metav1.Now())
	}
	return Outcome{BlueGreen: status, Report: reconciliationTypes.Report{
		Name: mvutypes.UseCaseName, Phase: string(mvutypes.Cancelled),
		Reason: mvutypes.ReasonCancelled, Message: mvutypes.MessageCancelled,
	}}, nil
}

// Cancellation never fails the attempt: blocked steps stay Cancelling and
// retry, because no other path may restore or abandon the attempt.
func cancellingOutcome(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, message string, err error) Outcome {
	reason := mvutypes.ReasonCancellationInProgress
	if err != nil {
		reason = mvutypes.ReasonCancellationBlocked
		message = fmt.Sprintf("%s Cancellation is blocked and will be retried: %v", message, err)
	}
	return Outcome{BlueGreen: status, Report: reconciliationTypes.Report{
		Name: mvutypes.UseCaseName, Phase: string(mvutypes.Cancelling),
		Reason: reason, Message: message, Retry: true,
	}}
}
