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

package custom_metrics

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	mtypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
)

const (
	// Reserve space below Condition.Message's 32 KiB limit.
	maxDiagnosticDetailBytes = 24 * 1024
	diagnosticsOmitted       = "; additional diagnostics omitted"
)

type Model struct {
	sources     DataRepository
	parser      Parser
	collider    Collider
	aggregator  Aggregator
	provisioner Provisioner
}

func NewModel(
	sources DataRepository,
	parser Parser,
	collider Collider,
	aggregator Aggregator,
	provisioner Provisioner,
) *Model {
	return &Model{
		sources:     sources,
		parser:      parser,
		collider:    collider,
		aggregator:  aggregator,
		provisioner: provisioner,
	}
}

type run struct {
	m                   *Model
	events              []Event
	invalid             InvalidKind
	details             []string
	invalidContributors map[string]acknowledgementFailure
}

// Desired-state failures return an Outcome; infrastructure failures return an error.
func (m *Model) Reconcile(
	ctx context.Context,
	cluster *enterprisev4.PostgresCluster,
	previous []mtypes.DatabaseAcknowledgement,
) (Outcome, error) {
	r := &run{m: m, invalidContributors: map[string]acknowledgementFailure{}}

	refs, snapshot, err := m.gatherSourceRefs(ctx, cluster)
	if err != nil {
		return Outcome{DatabaseContributions: append([]mtypes.DatabaseAcknowledgement(nil), previous...)}, err
	}
	out := Outcome{
		Disabled:              len(refs) == 0,
		DatabaseContributions: pendingAcknowledgements(snapshot.Contributions, previous, "CustomMetricsPending", "Waiting for custom-metrics aggregation"),
	}
	if len(snapshot.Unpublished) > 0 {
		out.Pending = true
		out.InvalidDetail = fmt.Sprintf("waiting for %d PostgresDatabase contribution(s) to be published", len(snapshot.Unpublished))
		if err := m.rollback(ctx, &out); err != nil {
			return out, err
		}
		return out, nil
	}

	var clusterSets []mtypes.ClusterQuerySet
	var dbSets []mtypes.DatabaseQuerySet

	for _, ref := range refs {
		resolved, valid, err := r.resolveSource(ctx, ref)
		if err != nil {
			out.DatabaseContributions = preservedAcknowledgements(snapshot.Contributions, previous)
			return out, err
		}
		if !valid {
			continue
		}
		if len(resolved) == 0 {
			continue
		}
		if ref.targetDB == nil {
			clusterSets = append(clusterSets, mtypes.ClusterQuerySet{Queries: resolved})
		} else {
			dbSets = append(dbSets, mtypes.DatabaseQuerySet{
				DatabaseName:      *ref.targetDB,
				CreationTimestamp: ref.creationTime,
				Contributor:       ref.contributor,
				Queries:           resolved,
			})
		}
	}

	if r.invalid != InvalidNone {
		return m.finishInvalid(
			ctx,
			out,
			r,
			snapshot.Contributions,
			previous,
			r.invalidContributors,
			newAcknowledgementFailure(invalidReason(r.invalid), r.detail()),
		)
	}

	// Older PostgresDatabase objects win same-scope collisions.
	sortDatabaseQuerySets(dbSets)
	acceptedCluster, acceptedDB, collisions := m.collider.DetectCollisions(clusterSets, dbSets)
	if len(collisions) > 0 {
		msgs := make([]string, 0, len(collisions))
		for i := range collisions {
			msgs = append(msgs, collisions[i].Error())
		}
		joined := strings.Join(msgs, "; ")
		r.markInvalid(InvalidCollision, joined)
		r.events = append(r.events, Event{Kind: EventCollision, Message: joined})
		// The collision is reported but accepted queries are still applied.
	}

	cfg := m.aggregator.Aggregate(acceptedCluster, acceptedDB)
	applied, err := m.provisioner.Apply(ctx, cfg)
	if err != nil {
		if errors.Is(err, mtypes.ErrGeneratedResourceOwnershipConflict) {
			detail := strings.TrimPrefix(err.Error(), mtypes.ErrGeneratedResourceOwnershipConflict.Error()+": ")
			if detail == "" {
				detail = err.Error()
			}
			r.invalid = InvalidOwnershipConflict
			r.details = []string{detail}
			r.events = append(r.events, Event{Kind: EventOwnershipConflict, Message: detail})
			out.Invalid = r.invalid
			out.InvalidDetail = r.detail()
			out.Events = r.events
			failures := make(map[string]acknowledgementFailure, len(snapshot.Contributions))
			for _, contribution := range snapshot.Contributions {
				failures[acknowledgementKey(contribution.Identity)] =
					newAcknowledgementFailure("GeneratedResourceOwnershipConflict", detail)
			}
			out.DatabaseContributions = invalidAcknowledgements(
				snapshot.Contributions,
				previous,
				failures,
				newAcknowledgementFailure("GeneratedResourceOwnershipConflict", detail),
			)
			return out, nil
		}
		if errors.Is(err, mtypes.ErrGeneratedConfigTooLarge) {
			detail := strings.TrimPrefix(err.Error(), mtypes.ErrGeneratedConfigTooLarge.Error()+": ")
			if detail == "" {
				detail = err.Error()
			}
			// Size blocks publication and takes condition priority over collisions.
			r.invalid = InvalidConfigTooLarge
			r.details = []string{detail}
			r.events = append(r.events, Event{
				Kind:    EventConfigTooLarge,
				Message: fmt.Sprintf("custom metrics configuration is too large: %s; reduce the number or size of custom queries; previous complete configuration remains active", detail),
			})
			return m.finishInvalid(
				ctx,
				out,
				r,
				snapshot.Contributions,
				previous,
				nil,
				newAcknowledgementFailure("CustomMetricsConfigTooLarge", detail),
			)
		}
		out.DatabaseContributions = pendingAcknowledgements(
			snapshot.Contributions,
			previous,
			"CustomMetricsApplyFailed",
			err.Error(),
		)
		return out, err
	}

	observation, err := m.provisioner.Observe(ctx, applied)
	if err != nil {
		out.DatabaseContributions = pendingAcknowledgements(
			snapshot.Contributions,
			previous,
			"CustomMetricsConfiguring",
			err.Error(),
		)
		return out, fmt.Errorf("observing expected custom-metrics revision %q: %w",
			applied.Revision, err)
	}
	switch observation.State {
	case mtypes.ObservationPending:
		out.Configuring = true
		out.Requeue = true
		out.InvalidDetail = observation.Message
		out.DatabaseContributions = pendingAcknowledgements(
			snapshot.Contributions,
			previous,
			"CustomMetricsConfiguring",
			observation.Message,
		)
		return out, nil
	}
	if !confirmedMatchesExpected(observation.Confirmed, applied) {
		return out, fmt.Errorf("provisioner reported ready without confirming expected custom-metrics revision %q", applied.Revision)
	}
	saved, err := m.provisioner.Save(ctx, *observation.Confirmed)
	if err != nil {
		out.DatabaseContributions = unknownAcknowledgements(
			snapshot.Contributions,
			previous,
			"CustomMetricsSafetySaveFailed",
			err.Error(),
		)
		return out, fmt.Errorf("saving confirmed custom-metrics revision %q: %w",
			observation.Confirmed.Revision, err)
	}

	count := observation.Confirmed.QueryCount
	if saved.Changed && count > 0 && r.invalid == InvalidNone {
		noun := "definitions"
		if count == 1 {
			noun = "definition"
		}
		r.events = append(r.events, Event{
			Kind:    EventQueryApplied,
			Message: fmt.Sprintf("Applied %d custom metric query %s", count, noun),
		})
	}

	out.Invalid = r.invalid
	out.InvalidDetail = r.detail()
	out.Events = r.events
	out.DatabaseContributions = appliedAcknowledgements(snapshot.Contributions, previous, collisions)
	return out, nil
}

func (m *Model) finishInvalid(
	ctx context.Context,
	out Outcome,
	r *run,
	contributions []mtypes.DatabaseContribution,
	previous []mtypes.DatabaseAcknowledgement,
	failures map[string]acknowledgementFailure,
	fallback acknowledgementFailure,
) (Outcome, error) {
	out.Invalid = r.invalid
	out.InvalidDetail = r.detail()
	out.Events = r.events
	out.DatabaseContributions = invalidAcknowledgements(contributions, previous, failures, fallback)
	if err := m.rollback(ctx, &out); err != nil {
		out.DatabaseContributions = unknownAcknowledgements(
			contributions,
			previous,
			"CustomMetricsRollbackFailed",
			err.Error(),
		)
		return out, err
	}
	return out, nil
}

func (m *Model) rollback(ctx context.Context, out *Outcome) error {
	restored, err := m.provisioner.Rollback(ctx)
	if err != nil {
		if errors.Is(err, mtypes.ErrGeneratedResourceOwnershipConflict) {
			detail := strings.TrimPrefix(err.Error(), mtypes.ErrGeneratedResourceOwnershipConflict.Error()+": ")
			if detail == "" {
				detail = err.Error()
			}
			out.Invalid = InvalidOwnershipConflict
			out.InvalidDetail = appendDiagnostic(out.InvalidDetail, detail)
			out.Events = append(out.Events, Event{Kind: EventOwnershipConflict, Message: detail})
			for i := range out.DatabaseContributions {
				out.DatabaseContributions[i].Status = mtypes.AcknowledgementFalse
				out.DatabaseContributions[i].Reason = "GeneratedResourceOwnershipConflict"
				out.DatabaseContributions[i].Message = detail
			}
			return nil
		}
		return fmt.Errorf("restoring last-known-good custom metrics: %w", err)
	}
	if !restored.Available {
		if restored.Message != "" {
			out.InvalidDetail = appendDiagnostic(out.InvalidDetail, restored.Message)
		}
		return nil
	}
	if restored.Changed {
		out.Events = append(out.Events, Event{
			Kind:    EventQueryRepaired,
			Message: fmt.Sprintf("Restored last-known-good custom metrics revision %s", restored.Expected.Revision),
		})
	}
	observation, err := m.provisioner.Observe(ctx, restored.Expected)
	if err != nil {
		return fmt.Errorf("observing restored custom metrics revision %q: %w", restored.Expected.Revision, err)
	}
	switch observation.State {
	case mtypes.ObservationPending:
		out.Configuring = true
		out.Requeue = true
		out.InvalidDetail = appendDiagnostic(out.InvalidDetail, observation.Message)
	case mtypes.ObservationReady:
		if !confirmedMatchesExpected(observation.Confirmed, restored.Expected) {
			return fmt.Errorf("provisioner reported ready without confirming restored custom-metrics revision %q",
				restored.Expected.Revision)
		}
	}
	return nil
}

func confirmedMatchesExpected(confirmed *mtypes.ConfirmedState, expected mtypes.ExpectedState) bool {
	return confirmed != nil &&
		confirmed.Revision == expected.Revision &&
		confirmed.Enabled == expected.Enabled &&
		confirmed.QueryCount == expected.QueryCount
}

func appendDiagnostic(current, additional string) string {
	if current == "" {
		return joinDiagnostics([]string{additional})
	}
	if additional == "" {
		return current
	}
	return joinDiagnostics([]string{current, additional})
}

func (r *run) markInvalid(kind InvalidKind, detail string) {
	if r.invalid == InvalidNone {
		r.invalid = kind
	}
	if r.invalid != kind {
		return // One condition reason wins; other failure kinds still emit Events.
	}
	for _, existing := range r.details {
		if existing == detail {
			return
		}
	}
	r.details = append(r.details, detail)
}

func (r *run) detail() string {
	return joinDiagnostics(r.details)
}

func (r *run) markContributorInvalid(contributor *mtypes.ContributorIdentity, reason, message string) {
	if contributor == nil {
		return
	}
	key := acknowledgementKey(*contributor)
	if current, found := r.invalidContributors[key]; found {
		detail := message
		if current.reason != reason {
			detail = fmt.Sprintf("%s: %s", reason, message)
		}
		current.addDetail(detail)
		r.invalidContributors[key] = current
		return
	}
	r.invalidContributors[key] = newAcknowledgementFailure(reason, message)
}

func joinDiagnostics(details []string) string {
	joined := strings.Join(details, "; ")
	if len(joined) <= maxDiagnosticDetailBytes {
		return joined
	}
	return validUTF8Prefix(joined, maxDiagnosticDetailBytes-len(diagnosticsOmitted)) + diagnosticsOmitted
}

func validUTF8Prefix(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	for maxBytes > 0 && !utf8.RuneStart(value[maxBytes]) {
		maxBytes--
	}
	return value[:maxBytes]
}
