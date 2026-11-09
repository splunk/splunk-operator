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
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationtypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	bluegreen "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/blue_green"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const candidateCredentialCheckMessage = "Checking candidate credential identities."

// Recovery continues after the availability target expires, but each attempt
// must release its worker even if an established SQL connection stalls.
const cutoverRecoveryTimeout = 30 * time.Second

type cutoverEndpoints interface {
	ObserveCommit(context.Context, types.NamespacedName, string) (bool, error)
	PublishPreparationIntent(context.Context, types.NamespacedName, string) error
	PreparationComplete(context.Context, types.NamespacedName, string) (bool, error)
	CommitCanonical(context.Context, types.NamespacedName, string) error
	RecoverCanonical(context.Context, types.NamespacedName, string) error
	RestoreUncommittedCanonical(context.Context, types.NamespacedName, string) error
	CancelPreparation(context.Context, types.NamespacedName, string, int64) error
	PublishPublicationIntent(context.Context, types.NamespacedName, string) error
	ReconcilePublicationStatus(context.Context, types.NamespacedName, string) (bool, error)
}

type cutoverAccess interface {
	Snapshot(context.Context, *cnpgv1.Cluster, *cnpgv1.Cluster, []declaredDatabase, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error
	PrepareControl(context.Context, *cnpgv1.Cluster, *cnpgv1.Cluster, []declaredDatabase, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error
	Credentials(context.Context, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (cutoverIdentity, error)
	ApplyFence(context.Context, *cnpgv1.Cluster, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error
	VerifyFences(context.Context, *cnpgv1.Cluster, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error)
	FenceBlue(context.Context, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error)
	RestoreBlue(context.Context, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error)
	OpenGreen(context.Context, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error)
}

type cutoverSQL interface {
	Drain(context.Context, *cnpgv1.Cluster, cutoverIdentity, bool) (bool, error)
	DrainRetainedSource(context.Context, *cnpgv1.Cluster, cutoverIdentity, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error)
	SourceLSN(context.Context, *cnpgv1.Cluster, cutoverIdentity) (string, error)
	VerifyFinalParity(context.Context, *cnpgv1.Cluster, *cnpgv1.Cluster, []declaredDatabase, *platformv1alpha1.PostgresBlueGreenUpgradeStatus, cutoverIdentity) (bool, error)
	SynchronizeSequences(context.Context, *cnpgv1.Cluster, *cnpgv1.Cluster, []declaredDatabase, cutoverIdentity) error
	RemoveGuards(context.Context, *cnpgv1.Cluster, []declaredDatabase, cutoverIdentity) error
	VerifyTargetDurability(context.Context, *cnpgv1.Cluster, cutoverIdentity) (bool, error)
}

// BlueGreenSwitchover advances exactly one durable cutover step per reconcile.
// Every rollback decision rereads the canonical commit record. After commit,
// the only available path repairs projections and opens green.
type BlueGreenSwitchover struct {
	client    client.Client
	reader    client.Reader
	key       types.NamespacedName
	owner     *platformv1alpha1.PostgresCluster
	plan      BlueGreenCandidatePlan
	access    cutoverAccess
	sql       cutoverSQL
	endpoints cutoverEndpoints
	now       func() time.Time
}

func NewBlueGreenSwitchover(c client.Client, reader client.Reader, key types.NamespacedName, owner *platformv1alpha1.PostgresCluster, plan BlueGreenCandidatePlan) (*BlueGreenSwitchover, error) {
	if owner == nil || key.Name == "" || key.Namespace == "" || key.Name != owner.Name || key.Namespace != owner.Namespace {
		return nil, errors.New("blue/green switchover requires clients and a persisted parent identity")
	}
	endpoints, err := NewEndpointCoordinator(c, reader)
	if err != nil {
		return nil, fmt.Errorf("construct switchover endpoint coordinator: %w", err)
	}
	live := liveReadClient{Client: c, reader: reader}
	sql, err := newPostgresSwitchoverSQL(live, key.Name, owner)
	if err != nil {
		return nil, fmt.Errorf("construct switchover SQL: %w", err)
	}
	access, err := newBlueGreenAccess(c, reader, key, owner, sql)
	if err != nil {
		return nil, fmt.Errorf("construct switchover access: %w", err)
	}
	return &BlueGreenSwitchover{client: c, reader: reader, key: key, owner: owner, plan: plan,
		access: access, sql: sql, endpoints: endpoints, now: time.Now}, nil
}

// liveReadClient keeps credential, catalog-inventory and configuration safety
// reads outside the informer cache while retaining explicit writer operations.
type liveReadClient struct {
	client.Client
	reader client.Reader
}

func (c liveReadClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return c.reader.Get(ctx, key, obj, opts...)
}
func (c liveReadClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return c.reader.List(ctx, list, opts...)
}

func (s *BlueGreenSwitchover) AdvanceSwitchover(ctx context.Context, intent mvutypes.Intent, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bluegreen.Outcome, error) {
	parent, status, err := s.readAttempt(ctx, intent.AttemptID)
	if err != nil {
		return bluegreen.Outcome{}, err
	}
	if status.Switchover == nil ||
		status.Gates == nil || status.Gates.Switchover == nil || status.Gates.Switchover.ConsumedAt == nil || status.Gates.Switchover.Generation <= 0 {
		return s.pending(status, "Waiting for a durable switchover authorization."), errors.Join(mvutypes.ErrUpgradeFlowFailed, errors.New("switchover authorization or timeout receipt is incomplete"))
	}
	if w := status.Switchover; w.StartedAt != nil && (w.DeadlineAt == nil || w.LatchedTimeout == nil || w.LatchedTimeout.Duration < platformv1alpha1.MinimumBlueGreenSwitchoverTimeout || w.LatchedTimeout.Duration > platformv1alpha1.MaximumBlueGreenSwitchoverTimeout) {
		return s.pending(status, "Waiting for a valid latched interruption deadline."), errors.New("switchover deadline receipt is incomplete")
	}
	committed, err := s.endpoints.ObserveCommit(ctx, s.key, intent.AttemptID)
	if err != nil {
		return s.pending(status, "Waiting for an unambiguous canonical commit record."), err
	}
	if committed {
		return s.afterCommit(ctx, intent, status)
	}
	if status.Switchover.DeadlineAt != nil && !s.now().Before(status.Switchover.DeadlineAt.Time) && status.Switchover.TimedOutAt == nil {
		now := metav1.NewTime(s.now())
		status.Switchover.TimedOutAt = &now
		status.Switchover.Failure = new("Switchover exceeded its deadline before endpoint commit.")
		return s.pending(status, "The switchover timed out; restoring blue access."), nil
	}
	if config := parent.Spec.PostgresMajorUpgradeConfig; config != nil && config.BlueGreen != nil && config.BlueGreen.Cancel != nil && *config.BlueGreen.Cancel && status.Switchover.Failure == nil {
		status.Switchover.Failure = new("Switchover was cancelled before endpoint commit.")
		return s.pending(status, "Restoring blue access before cancellation can finish."), nil
	}
	if status.Switchover.Failure != nil {
		return s.restoreSource(ctx, intent, status)
	}
	blue, green, databases, err := s.inputs(ctx, status)
	if err != nil {
		return s.precommitError(ctx, intent, status, err)
	}
	bounded := ctx
	if status.Switchover.DeadlineAt != nil {
		var cancel context.CancelFunc
		bounded, cancel = context.WithDeadline(ctx, status.Switchover.DeadlineAt.Time)
		defer cancel()
	}
	outcome, err := s.beforeCommit(bounded, intent, status, blue, green, databases)
	if err != nil {
		return s.precommitError(ctx, intent, outcome.BlueGreen, err)
	}
	return outcome, nil
}

func (s *BlueGreenSwitchover) beforeCommit(ctx context.Context, intent mvutypes.Intent, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, blue, green *cnpgv1.Cluster, databases []declaredDatabase) (bluegreen.Outcome, error) {
	w := status.Switchover
	now := metav1.NewTime(s.now())
	if w.ControlIdentity == nil || len(w.GreenCredentialFingerprints) == 0 {
		if w.ControlIdentity != nil && w.ControlIdentity.PreparedAt != nil {
			return s.pending(status, "Checking the access recovery baseline."), errors.New("prepared controls have no frozen candidate credentials")
		}
		if err := s.access.Snapshot(ctx, blue, green, databases, status); err != nil {
			return s.pending(status, "Capturing the access recovery baseline."), err
		}
		credentials, err := s.credentialBaseline().credentialFingerprints(ctx, green, databases, replicationSecretName(s.key.Name, status.AttemptID))
		if err != nil {
			return s.pending(status, "Capturing candidate credential identities."), err
		}
		w.GreenCredentialFingerprints = credentials
		return s.pending(status, "The access recovery baseline is recorded."), nil
	}
	if w.ControlIdentity.PreparedAt == nil {
		if err := s.access.PrepareControl(ctx, blue, green, databases, status); err != nil {
			return s.pending(status, "Preparing operator-only control access."), err
		}
		return s.pending(status, "Operator control access is verified."), nil
	}
	if err := s.verifyFrozenConfiguration(ctx, blue, databases, status); err != nil {
		return s.pending(status, "Checking the frozen configuration and credentials."), err
	}
	normalizedGreen, err := NormalizeFence(green, status, sideGreen)
	if err != nil {
		return s.pending(status, "Checking the frozen green configuration."), err
	}
	greenConfiguration, err := candidateConfigurationFingerprint(normalizedGreen)
	if err != nil || w.GreenConfigurationFingerprint == nil || greenConfiguration != *w.GreenConfigurationFingerprint {
		return s.pending(status, "Checking the frozen green configuration."), errors.Join(err, errors.New("frozen green configuration changed during switchover"))
	}
	greenCredentials, err := s.credentialBaseline().credentialFingerprints(ctx, normalizedGreen, databases, replicationSecretName(s.key.Name, status.AttemptID))
	if err != nil {
		return s.pending(status, candidateCredentialCheckMessage), err
	}
	if err := verifyCredentialBaseline(w.GreenCredentialFingerprints, greenCredentials); err != nil {
		return s.pending(status, candidateCredentialCheckMessage), err
	}
	if w.StartedAt == nil {
		timeout := intent.SwitchoverTimeout
		if timeout == 0 {
			timeout = platformv1alpha1.DefaultBlueGreenSwitchoverTimeout
		}
		if timeout < platformv1alpha1.MinimumBlueGreenSwitchoverTimeout || timeout > platformv1alpha1.MaximumBlueGreenSwitchoverTimeout {
			return s.pending(status, "Validating the switchover interruption policy."), errors.New("switchover timeout must be between 30s and 3600s")
		}
		w.StartedAt, w.LatchedTimeout = &now, &metav1.Duration{Duration: timeout}
		w.DeadlineAt = new(metav1.NewTime(now.Add(timeout)))
		return s.pending(status, "The interruption deadline is latched; applying the access fence next."), nil
	}
	if w.FenceAppliedAt == nil {
		if err := s.access.ApplyFence(ctx, blue, green, status); err != nil {
			return s.pending(status, "Applying the declarative access fence."), err
		}
		w.FenceAppliedAt = &now
		return s.pending(status, "The access fence is declared on blue and green."), nil
	}
	fenced, err := s.access.VerifyFences(ctx, blue, green, status)
	if err != nil || !fenced {
		return s.pending(status, "Waiting for the access fence to be effective on every instance."), err
	}
	if w.FenceVerifiedAt == nil {
		w.FenceVerifiedAt = &now
		return s.pending(status, "The access fence is verified on every instance."), nil
	}
	id, err := s.access.Credentials(ctx, status)
	if err != nil {
		return s.pending(status, "Reading the operator control identity."), err
	}
	if w.DrainCompletedAt == nil {
		greenDrained, err := s.sql.Drain(ctx, green, id, true)
		if err != nil || !greenDrained {
			return s.pending(status, "Terminating candidate sessions that predate the green fence."), err
		}
		drain := min(5*time.Second, w.LatchedTimeout.Duration/4)
		terminate := !s.now().Before(w.FenceVerifiedAt.Add(drain))
		drained, err := s.sql.Drain(ctx, blue, id, terminate)
		if err != nil || !drained {
			return s.pending(status, "Draining blue application transactions within the switchover deadline."), err
		}
		w.DrainCompletedAt = &now
		return s.pending(status, "Blue application sessions have drained."), nil
	}
	if w.FinalBlueLSN == nil {
		drained, err := s.sql.Drain(ctx, blue, id, true)
		if err != nil || !drained {
			return s.pending(status, "Verifying blue has no remaining ordinary sessions."), err
		}
		lsn, err := s.sql.SourceLSN(ctx, blue, id)
		if err != nil {
			return s.pending(status, "Capturing the final blue WAL position."), err
		}
		w.FinalBlueLSN = &lsn
		return s.pending(status, "The final blue WAL position is recorded."), nil
	}
	parity, err := s.sql.VerifyFinalParity(ctx, blue, green, databases, status, id)
	if err != nil || !parity {
		return s.pending(status, "Waiting for final logical replication parity and frozen catalog proof."), err
	}
	if w.SequenceSynchronization == nil || w.SequenceSynchronization.VerifiedAt == nil {
		if err := s.sql.SynchronizeSequences(ctx, blue, green, databases, id); err != nil {
			return s.pending(status, "Synchronizing managed sequence values."), err
		}
		w.SequenceSynchronization = &platformv1alpha1.BlueGreenSequenceSynchronizationStatus{CompletedAt: &now, VerifiedAt: &now}
		return s.pending(status, "Managed sequence values are synchronized and verified."), nil
	}
	if w.CandidateGuardsRemovedAt == nil {
		if w.CandidateGuardsRemovalStartedAt == nil {
			w.CandidateGuardsRemovalStartedAt = &now
			return s.pending(status, "Guard removal is authorized behind the verified green fence."), nil
		}
		drained, err := s.sql.Drain(ctx, green, id, true)
		if err != nil || !drained {
			return s.pending(status, "Verifying green has no ordinary sessions before removing its write guards."), err
		}
		if err := s.sql.RemoveGuards(ctx, green, databases, id); err != nil {
			return s.pending(status, "Removing candidate write guards behind the green access fence."), err
		}
		w.CandidateGuardsRemovedAt = &now
		return s.pending(status, "Candidate write guards are removed; green remains fenced."), nil
	}
	if w.FinalParityVerifiedAt == nil {
		w.FinalParityVerifiedAt = &now
		return s.pending(status, "Final cutover parity is verified."), nil
	}
	if status.EndpointPreparation == nil {
		if err := s.endpoints.PublishPreparationIntent(ctx, s.key, intent.AttemptID); err != nil {
			return s.pending(status, "Preparing database endpoint projections."), err
		}
		return s.refreshed(ctx, intent.AttemptID, "Waiting for database endpoint preparation acknowledgements.")
	}
	complete, err := s.endpoints.PreparationComplete(ctx, s.key, intent.AttemptID)
	if err != nil || !complete {
		return s.pending(status, "Waiting for every database endpoint preparation acknowledgement."), err
	}
	for _, environment := range []*cnpgv1.Cluster{blue, green} {
		drained, err := s.sql.Drain(ctx, environment, id, true)
		if err != nil || !drained {
			return s.pending(status, "Verifying both environments are drained immediately before endpoint commit."), err
		}
	}
	durable, err := s.sql.VerifyTargetDurability(ctx, green, id)
	if err != nil || !durable {
		return s.pending(status, "Waiting for final green WAL to be durable on every failover candidate."), err
	}
	// All safety facts above are fresh in this pass. Check the hard deadline
	// immediately before handing the irreversible write to the coordinator.
	if !s.now().Before(w.DeadlineAt.Time) {
		return s.pending(status, "The endpoint commit deadline expired."), context.DeadlineExceeded
	}
	if err := s.endpoints.CommitCanonical(ctx, s.key, intent.AttemptID); err != nil {
		return s.pending(status, "Recording the canonical green endpoint commit."), err
	}
	return s.refreshed(ctx, intent.AttemptID, "Green endpoints committed; publishing database projections.")
}

func (s *BlueGreenSwitchover) afterCommit(ctx context.Context, intent mvutypes.Intent, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bluegreen.Outcome, error) {
	ctx, cancel := context.WithTimeout(ctx, cutoverRecoveryTimeout)
	defer cancel()

	if status.CommitReceipt == nil {
		if err := s.endpoints.CommitCanonical(ctx, s.key, intent.AttemptID); err != nil {
			return s.pending(status, "Repairing the durable endpoint commit receipt."), err
		}
		return s.refreshed(ctx, intent.AttemptID, "The endpoint commit receipt is repaired.")
	}
	if err := s.endpoints.RecoverCanonical(ctx, s.key, intent.AttemptID); err != nil {
		return s.pending(status, "Repairing the canonical green endpoint projection."), err
	}
	_, status, err := s.readAttempt(ctx, intent.AttemptID)
	if err != nil {
		return bluegreen.Outcome{}, err
	}
	w := status.Switchover
	if w == nil || w.ControlIdentity == nil || w.FenceVerifiedAt == nil || w.DeadlineAt == nil {
		return s.pending(status, "Waiting for the committed attempt's durable access-fence proof."), errors.New("committed attempt has incomplete cutover access receipts")
	}
	if w.TargetAvailableAt != nil {
		return s.finishAvailability(ctx, status)
	}
	if w.TimedOutAt == nil && !s.now().Before(w.DeadlineAt.Time) {
		now := metav1.NewTime(s.now())
		w.TimedOutAt = &now
		return s.pending(status, "The availability target was exceeded after commit; continuing toward green."), nil
	}
	green, err := s.environment(ctx, status.Green)
	if err != nil {
		return s.pending(status, "Waiting for the committed green environment identity."), err
	}
	blue, err := s.retiredSource(ctx, status)
	if err != nil {
		return s.pending(status, "Waiting for the retired source to be absent or safely fenced."), err
	}
	if blue != nil {
		fenced, err := s.access.FenceBlue(ctx, blue, status)
		if err != nil || !fenced {
			return s.pending(status, "Keeping blue fenced while green publication recovers."), err
		}
	}
	if status.EndpointPublication == nil {
		if err := s.endpoints.PublishPublicationIntent(ctx, s.key, intent.AttemptID); err != nil {
			return s.pending(status, "Publishing the committed database endpoint intent."), err
		}
		return s.refreshed(ctx, intent.AttemptID, "Waiting for database ConfigMaps to publish green.")
	}
	published, err := s.endpoints.ReconcilePublicationStatus(ctx, s.key, intent.AttemptID)
	if err != nil || !published {
		return s.pending(status, "Repairing and verifying every database ConfigMap before opening green."), err
	}
	id, err := s.access.Credentials(ctx, status)
	if err != nil {
		return s.pending(status, "Reading the committed attempt's control identity."), err
	}
	if blue != nil {
		drained, err := s.sql.DrainRetainedSource(ctx, blue, id, status)
		if err != nil || !drained {
			return s.pending(status, "Terminating any remaining blue application sessions."), err
		}
	}
	if w.BlueSessionsTerminatedAt == nil {
		now := metav1.NewTime(s.now())
		w.BlueSessionsTerminatedAt = &now
		return s.pending(status, "All remaining blue application sessions are terminated."), nil
	}
	open, err := s.access.OpenGreen(ctx, green, status)
	if err != nil || !open {
		return s.pending(status, "Restoring and verifying ordinary green application access."), err
	}
	now := metav1.NewTime(s.now())
	if w.TimedOutAt == nil && !now.Before(w.DeadlineAt) {
		w.TimedOutAt = now.DeepCopy()
	}
	w.GreenFenceRemovedAt, w.TargetAvailableAt = &now, &now
	return s.pending(status, "Green application access is verified; recording its publication Event."), nil
}

// A missing retired Cluster is safe only with the exact durable commit/fence
// proof and no surviving source instances. Do not fabricate provider state or
// infer isolation from readiness, deletion timestamps, or failed SQL dials.
func (s *BlueGreenSwitchover) retiredSource(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (*cnpgv1.Cluster, error) {
	blue, err := s.environment(ctx, status.Blue)
	if !apierrors.IsNotFound(err) {
		return blue, err
	}
	if status.Switchover.ControlIdentity == nil || !mvutypes.HasRetainedSourceFenceProof(status, status.AttemptID, status.Switchover.ControlIdentity.RoleName) ||
		(status.Blue.Ref.Namespace != "" && status.Blue.Ref.Namespace != s.key.Namespace) {
		return nil, fmt.Errorf("%w: missing retired source lacks its committed fence and drain proof", mvutypes.ErrUpgradeFlowFailed)
	}
	pods := new(corev1.PodList)
	if err := s.reader.List(ctx, pods, client.InNamespace(s.key.Namespace)); err != nil {
		return nil, fmt.Errorf("listing surviving retired source instances: %w", err)
	}
	for _, pod := range pods.Items {
		owner := metav1.GetControllerOf(&pod)
		if (owner != nil && owner.UID == status.Blue.Ref.UID) || pod.Labels["cnpg.io/cluster"] == status.Blue.Ref.Name {
			return nil, fmt.Errorf("%w: retired source Cluster is missing but instance %q still exists", mvutypes.ErrUpgradeFlowPending, pod.Name)
		}
	}
	// A same-name replacement observed during inventory must not be adopted.
	if _, err := s.environment(ctx, status.Blue); !apierrors.IsNotFound(err) {
		if err == nil {
			err = errors.New("retired source reappeared during instance discovery")
		}
		return nil, err
	}
	return nil, nil
}

func (s *BlueGreenSwitchover) finishAvailability(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bluegreen.Outcome, error) {
	w := status.Switchover
	if w.EndpointPublicationEventRef == nil {
		ref, err := s.publicationEvent(ctx, status)
		if err != nil {
			return s.pending(status, "Recording the endpoint publication Event."), err
		}
		w.EndpointPublicationEventRef = ref
	}
	outcome := s.pending(status, "Green is available; target-major recoverability is the next workflow stage.")
	outcome.Report.Phase = string(mvutypes.PostUpgradeBackup)
	return outcome, nil
}

func (s *BlueGreenSwitchover) precommitError(ctx context.Context, intent mvutypes.Intent, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, cause error) (bluegreen.Outcome, error) {
	if status == nil {
		_, latest, err := s.readAttempt(ctx, intent.AttemptID)
		if err != nil {
			return bluegreen.Outcome{}, errors.Join(cause, err)
		}
		status = latest
	}
	committed, err := s.endpoints.ObserveCommit(ctx, s.key, intent.AttemptID)
	if err != nil {
		return s.pending(status, "Rechecking the commit boundary before any source restoration."), errors.Join(cause, err)
	}
	if committed {
		return s.refreshed(ctx, intent.AttemptID, "The canonical write committed; recovery will continue toward green.")
	}
	status.Switchover.Failure = new(cause.Error())
	if status.Switchover.DeadlineAt != nil && !s.now().Before(status.Switchover.DeadlineAt.Time) && status.Switchover.TimedOutAt == nil {
		status.Switchover.TimedOutAt = new(metav1.NewTime(s.now()))
	}
	return s.pending(status, "The pre-commit handoff failed; restoring verified blue access."), nil
}

func (s *BlueGreenSwitchover) restoreSource(ctx context.Context, intent mvutypes.Intent, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bluegreen.Outcome, error) {
	if err := s.endpoints.RestoreUncommittedCanonical(ctx, s.key, intent.AttemptID); err != nil {
		return s.pending(status, "Restoring the uncommitted blue endpoint record."), err
	}
	if status.EndpointPreparation != nil {
		if err := s.endpoints.CancelPreparation(ctx, s.key, intent.AttemptID, status.EndpointPreparation.Generation); err != nil {
			return s.pending(status, "Clearing the uncommitted endpoint preparation."), err
		}
		return s.refreshed(ctx, intent.AttemptID, "Endpoint preparation is cleared; restoring blue access.")
	}
	w := status.Switchover
	if w.ControlIdentity != nil && w.ControlIdentity.PreparedAt != nil {
		blue, err := s.environment(ctx, status.Blue)
		if err != nil {
			return s.pending(status, "Waiting for blue's recorded environment before restoration."), err
		}
		open, err := s.access.RestoreBlue(ctx, blue, status)
		if err != nil || !open {
			return s.pending(status, "Verifying source access is restored before ending this window."), err
		}
	}
	// Without a durable PreparedAt receipt no fence operation was authorized;
	// the control bootstrap never modifies ordinary LOGIN flags or HBA rules.
	if w.SourceFenceRestoredAt == nil {
		w.SourceFenceRestoredAt = new(metav1.NewTime(s.now()))
	}
	reason := "SwitchoverFailed"
	if w.TimedOutAt != nil {
		reason = "SwitchoverTimedOut"
	}
	return bluegreen.Outcome{BlueGreen: status, Report: reconciliationtypes.Report{Name: mvutypes.UseCaseName, Phase: string(mvutypes.Failed), Reason: reason,
		Message: "Blue remains authoritative and its access is restored. A newer retry timestamp is required for another interruption."}}, nil
}

func (s *BlueGreenSwitchover) verifyFrozenConfiguration(ctx context.Context, source *cnpgv1.Cluster, databases []declaredDatabase, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	normalized, err := NormalizeFence(source, status, sideBlue)
	if err != nil {
		return err
	}
	baseline := s.credentialBaseline()
	fingerprint, err := baseline.configurationFingerprint(normalized, databases)
	if err != nil {
		return err
	}
	if status.SourceConfigurationFingerprint == nil || fingerprint != *status.SourceConfigurationFingerprint {
		return fmt.Errorf("%w: frozen source configuration changed during switchover", mvutypes.ErrUpgradeFlowFailed)
	}
	credentials, err := baseline.credentialFingerprints(ctx, normalized, databases, replicationSecretName(s.key.Name, status.AttemptID))
	if err != nil {
		return err
	}
	return verifyCredentialBaseline(status.CredentialFingerprints, credentials)
}

func (s *BlueGreenSwitchover) credentialBaseline() sourceBaseline {
	return sourceBaseline{client: liveReadClient{Client: s.client, reader: s.reader}, key: s.key, cluster: s.owner,
		targetConfiguration: s.plan.TargetConfiguration, targetImage: s.plan.TargetImage}
}

func (s *BlueGreenSwitchover) readAttempt(ctx context.Context, attemptID string) (*platformv1alpha1.PostgresCluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus, error) {
	parent := &platformv1alpha1.PostgresCluster{}
	if err := s.reader.Get(ctx, s.key, parent); err != nil {
		return nil, nil, err
	}
	if parent.UID == "" || parent.UID != s.owner.UID || parent.DeletionTimestamp != nil || len(parent.Status.PostgresMajorUpgradeStatus) == 0 {
		return nil, nil, errors.New("switchover parent identity is stale or deleting")
	}
	latest := parent.Status.PostgresMajorUpgradeStatus[len(parent.Status.PostgresMajorUpgradeStatus)-1].BlueGreen
	if latest == nil || attemptID == "" || latest.AttemptID != attemptID {
		return nil, nil, errors.New("switchover attempt is stale")
	}
	return parent, latest.DeepCopy(), nil
}

func (s *BlueGreenSwitchover) environment(ctx context.Context, environment *platformv1alpha1.BlueGreenEnvironmentStatus) (*cnpgv1.Cluster, error) {
	if environment == nil || environment.Ref.Name == "" || environment.Ref.UID == "" {
		return nil, errors.New("switchover environment identity is missing")
	}
	cluster := &cnpgv1.Cluster{}
	if err := s.reader.Get(ctx, types.NamespacedName{Namespace: s.key.Namespace, Name: environment.Ref.Name}, cluster); err != nil {
		return nil, err
	}
	if cluster.UID != environment.Ref.UID || cluster.DeletionTimestamp != nil || !metav1.IsControlledBy(cluster, s.owner) {
		return nil, errors.New("switchover CNPG environment identity changed")
	}
	return cluster, nil
}

func (s *BlueGreenSwitchover) inputs(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (*cnpgv1.Cluster, *cnpgv1.Cluster, []declaredDatabase, error) {
	blue, err := s.environment(ctx, status.Blue)
	if err != nil {
		return nil, nil, nil, err
	}
	green, err := s.environment(ctx, status.Green)
	if err != nil {
		return nil, nil, nil, err
	}
	databases, err := listManagedDatabases(ctx, liveReadClient{Client: s.client, reader: s.reader}, s.key)
	return blue, green, databases, err
}

func (s *BlueGreenSwitchover) refreshed(ctx context.Context, attemptID, message string) (bluegreen.Outcome, error) {
	_, status, err := s.readAttempt(ctx, attemptID)
	if err != nil {
		return bluegreen.Outcome{}, err
	}
	return s.pending(status, message), nil
}

// A fixed requeue delay keeps successful durable steps out of the controller
// failure backoff, which could otherwise consume the interruption deadline.
func (s *BlueGreenSwitchover) pending(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, message string) bluegreen.Outcome {
	return bluegreen.Outcome{BlueGreen: status, Report: reconciliationtypes.Report{Name: mvutypes.UseCaseName,
		Phase: string(mvutypes.SwitchingOver), Reason: mvutypes.ReasonUpgradeFlowPending, Message: message, Retry: true, Sleep: new(1)}}
}

func (s *BlueGreenSwitchover) publicationEvent(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (*corev1.ObjectReference, error) {
	digest := sha256.Sum256([]byte(status.AttemptID))
	name := fmt.Sprintf("%.30s-bg-endpoints-%x", s.key.Name, digest[:8])
	message := fmt.Sprintf("Application endpoints published for blue/green attempt %s; environment %s is available.", status.AttemptID, status.Green.Ref.Name)
	event := &corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.key.Namespace,
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(s.owner, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}},
		InvolvedObject: corev1.ObjectReference{APIVersion: platformv1alpha1.GroupVersion.String(), Kind: "PostgresCluster", Namespace: s.key.Namespace, Name: s.key.Name, UID: s.owner.UID},
		Reason:         "PostgresEndpointsPublished", Message: message, Type: corev1.EventTypeNormal, Count: 1,
		FirstTimestamp: *status.Switchover.TargetAvailableAt, LastTimestamp: *status.Switchover.TargetAvailableAt,
		Source: corev1.EventSource{Component: "postgrescluster-controller"}}
	if err := s.client.Create(ctx, event); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, err
	}
	if err := s.reader.Get(ctx, client.ObjectKeyFromObject(event), event); err != nil {
		return nil, err
	}
	if event.UID == "" || event.InvolvedObject.UID != s.owner.UID || event.Reason != "PostgresEndpointsPublished" || event.Message != message || event.Type != corev1.EventTypeNormal || !metav1.IsControlledBy(event, s.owner) {
		return nil, errors.New("endpoint publication Event identity differs from this attempt")
	}
	return &corev1.ObjectReference{APIVersion: "v1", Kind: "Event", Namespace: event.Namespace, Name: event.Name, UID: event.UID}, nil
}
