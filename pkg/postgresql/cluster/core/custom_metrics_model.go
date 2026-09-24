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

package core

import (
	"context"
	"errors"
	"fmt"
	"sort"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	mon "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/custom_metrics"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	mtypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type customMetricsModel struct {
	model        *mon.Model
	events       eventEmitter
	updateStatus healthStatusUpdater
	contracts    *reconcileContracts
	cluster      *enterprisev4.PostgresCluster

	// Observe diffs against the pre-reconcile status.
	statusBefore *enterprisev4.PostgresClusterStatus
	outcome      mon.Outcome
}

func newCustomMetricsModel(model *mon.Model, events eventEmitter, updateStatus healthStatusUpdater, cluster *enterprisev4.PostgresCluster, contracts *reconcileContracts) *customMetricsModel {
	return &customMetricsModel{
		model:        model,
		events:       events,
		updateStatus: updateStatus,
		contracts:    contracts,
		cluster:      cluster,
	}
}

func (m *customMetricsModel) Name() string            { return pgcConstants.ComponentCustomMetrics }
func (m *customMetricsModel) Requires() []contractKey { return []contractKey{contractCNPGCluster} }
func (m *customMetricsModel) Provides() []contractKey { return nil }

func (m *customMetricsModel) CheckContracts() error {
	if !checkContractsFromRequirements(m.Requires(), m.contracts) {
		return errContractsNotReady
	}
	return nil
}

func (m *customMetricsModel) Reconcile(ctx context.Context) error {
	m.statusBefore = m.cluster.Status.DeepCopy()
	m.outcome = mon.Outcome{}

	out, err := m.model.Reconcile(
		ctx,
		m.cluster,
		databaseAcknowledgementsFromStatus(m.cluster.Status.CustomMetricsStatus),
	)
	m.outcome = out
	if err != nil {
		return newReconcileFailure(reasonCustomMetricsApplyFailed, err)
	}
	m.emitEvents(out.Events)
	return nil
}

func (m *customMetricsModel) emitEvents(events []mon.Event) {
	for _, e := range events {
		switch e.Kind {
		case mon.EventQueryApplied:
			m.events.emitNormal(m.cluster, EventCustomMetricsQueryApplied, e.Message)
		case mon.EventQueryRepaired:
			m.events.emitNormal(m.cluster, EventCustomMetricsQueryRepaired, e.Message)
		case mon.EventInvalidQuery:
			m.events.emitWarning(m.cluster, EventCustomMetricsInvalidQuery, e.Message)
		case mon.EventCollision:
			m.events.emitWarning(m.cluster, EventCustomMetricsCollision, e.Message)
		case mon.EventConfigMapNotFound:
			m.events.emitWarning(m.cluster, EventCustomMetricsConfigMapNotFound, e.Message)
		case mon.EventConfigTooLarge:
			m.events.emitWarning(m.cluster, EventCustomMetricsConfigTooLarge, e.Message)
		case mon.EventOwnershipConflict:
			m.events.emitWarning(m.cluster, EventCustomMetricsOwnershipConflict, e.Message)
		}
	}
}

func (m *customMetricsModel) Observe(ctx context.Context, reconcileErr error) (componentHealth, error) {
	before := m.statusBefore
	if before == nil {
		before = m.cluster.Status.DeepCopy()
	}
	health, err := m.computeHealth(reconcileErr)
	m.cluster.Status.CustomMetricsStatus = customMetricsStatusFromAcknowledgements(m.outcome.DatabaseContributions)
	statusErr := writeComponentStatus(m.updateStatus, before, health)
	return health, errors.Join(err, statusErr)
}

func (m *customMetricsModel) computeHealth(reconcileErr error) (componentHealth, error) {
	if retryErr, ok := retryableCustomMetricsError(reconcileErr); ok {
		return newCustomMetricsConfiguringHealth(
			reasonCustomMetricsApplyRetrying,
			fmt.Sprintf(msgFmtCustomMetricsApplyFailed, retryErr),
		), retryErr
	}
	if h, err, ok := classifyReconcileErr(reconcileErr, customMetricsReady, m.events, m.cluster, EventCustomMetricsReconcileFailed, "custom metrics"); ok {
		return h, err
	}
	var health componentHealth
	switch m.outcome.Invalid {
	case mon.InvalidConfigMapNotFound:
		health = newDegradedHealth(customMetricsReady, reasonCustomMetricsConfigMapNotFound,
			fmt.Sprintf(msgFmtCustomMetricsConfigMapMiss, m.outcome.InvalidDetail))
	case mon.InvalidQuery:
		health = newDegradedHealth(customMetricsReady, reasonCustomMetricsInvalidQuery,
			fmt.Sprintf(msgFmtCustomMetricsInvalidQuery, m.outcome.InvalidDetail))
	case mon.InvalidCollision:
		health = newDegradedHealth(customMetricsReady, reasonCustomMetricsMetricNameCollision,
			fmt.Sprintf(msgFmtCustomMetricsCollision, m.outcome.InvalidDetail))
	case mon.InvalidConfigTooLarge:
		health = newDegradedHealth(customMetricsReady, reasonCustomMetricsConfigTooLarge,
			fmt.Sprintf(msgFmtCustomMetricsConfigTooLarge, m.outcome.InvalidDetail))
	case mon.InvalidOwnershipConflict:
		health = newDegradedHealth(customMetricsReady, reasonCustomMetricsOwnershipConflict,
			fmt.Sprintf(msgFmtCustomMetricsOwnershipConflict, m.outcome.InvalidDetail))
	}
	if health.Condition != "" {
		return withCustomMetricsRepairRequeue(health, m.outcome.Requeue), nil
	}
	if m.outcome.Pending {
		health = newDegradedHealth(customMetricsReady, reasonCustomMetricsPending, m.outcome.InvalidDetail)
		return withCustomMetricsRepairRequeue(health, m.outcome.Requeue), nil
	}
	if m.outcome.Configuring {
		return newCustomMetricsConfiguringHealth(
			reasonCustomMetricsConfiguring,
			m.outcome.InvalidDetail,
		), nil
	}
	if m.outcome.Disabled {
		return newReadyHealth(customMetricsReady, reasonCustomMetricsDisabled, msgCustomMetricsDisabled), nil
	}
	return newReadyHealth(customMetricsReady, reasonCustomMetricsReady, msgCustomMetricsReady), nil
}

func withCustomMetricsRepairRequeue(health componentHealth, requeue bool) componentHealth {
	if requeue {
		health.State = pgcConstants.Configuring
		health.Phase = configuringClusterPhase
		health.Result.RequeueAfter = retryDelay
	}
	return health
}

func newCustomMetricsConfiguringHealth(reason conditionReasons, message string) componentHealth {
	health := newConfiguringHealth(customMetricsReady, reason, message)
	status := metav1.ConditionFalse
	health.ConditionStatus = &status
	return health
}

func retryableCustomMetricsError(err error) (error, bool) {
	if err == nil {
		return nil, false
	}
	if failure, ok := errors.AsType[*reconcileFailure](err); ok {
		err = failure.err
	}
	if apierrors.IsConflict(err) ||
		apierrors.IsNotFound(err) ||
		apierrors.IsTimeout(err) ||
		apierrors.IsServerTimeout(err) ||
		apierrors.IsTooManyRequests(err) ||
		apierrors.IsServiceUnavailable(err) ||
		errors.Is(err, mtypes.ErrConfirmedResourceUnavailable) ||
		errors.Is(err, context.DeadlineExceeded) {
		return err, true
	}
	return nil, false
}

func databaseAcknowledgementsFromStatus(status *enterprisev4.CustomMetricsStatus) []mtypes.DatabaseAcknowledgement {
	if status == nil {
		return nil
	}
	result := make([]mtypes.DatabaseAcknowledgement, 0, len(status.DatabaseContributions))
	for _, current := range status.DatabaseContributions {
		result = append(result, mtypes.DatabaseAcknowledgement{
			Identity: mtypes.ContributorIdentity{
				PostgresDatabaseName: current.PostgresDatabaseName,
				PostgresDatabaseUID:  current.PostgresDatabaseUID,
				DatabaseName:         current.DatabaseName,
			},
			DesiredRevision: current.DesiredRevision,
			AppliedRevision: current.AppliedRevision,
			Status:          mtypes.AcknowledgementStatus(current.Status),
			Reason:          current.Reason,
			Message:         current.Message,
		})
	}
	return result
}

func customMetricsStatusFromAcknowledgements(acknowledgements []mtypes.DatabaseAcknowledgement) *enterprisev4.CustomMetricsStatus {
	if len(acknowledgements) == 0 {
		return nil
	}
	sort.Slice(acknowledgements, func(i, j int) bool {
		if acknowledgements[i].Identity.PostgresDatabaseName != acknowledgements[j].Identity.PostgresDatabaseName {
			return acknowledgements[i].Identity.PostgresDatabaseName < acknowledgements[j].Identity.PostgresDatabaseName
		}
		if acknowledgements[i].Identity.PostgresDatabaseUID != acknowledgements[j].Identity.PostgresDatabaseUID {
			return acknowledgements[i].Identity.PostgresDatabaseUID < acknowledgements[j].Identity.PostgresDatabaseUID
		}
		return acknowledgements[i].Identity.DatabaseName < acknowledgements[j].Identity.DatabaseName
	})
	status := &enterprisev4.CustomMetricsStatus{
		DatabaseContributions: make([]enterprisev4.DatabaseCustomMetricsStatus, 0, len(acknowledgements)),
	}
	for _, acknowledgement := range acknowledgements {
		status.DatabaseContributions = append(status.DatabaseContributions, enterprisev4.DatabaseCustomMetricsStatus{
			PostgresDatabaseName: acknowledgement.Identity.PostgresDatabaseName,
			PostgresDatabaseUID:  acknowledgement.Identity.PostgresDatabaseUID,
			DatabaseName:         acknowledgement.Identity.DatabaseName,
			DesiredRevision:      acknowledgement.DesiredRevision,
			AppliedRevision:      acknowledgement.AppliedRevision,
			Status:               metav1.ConditionStatus(acknowledgement.Status),
			Reason:               acknowledgement.Reason,
			Message:              acknowledgement.Message,
		})
	}
	return status
}
