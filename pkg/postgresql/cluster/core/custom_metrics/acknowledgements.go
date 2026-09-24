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
	"sort"

	mtypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
)

func acknowledgementKey(identity mtypes.ContributorIdentity) string {
	return identity.PostgresDatabaseName + "\x00" + identity.PostgresDatabaseUID + "\x00" + identity.DatabaseName
}

func previousAcknowledgements(previous []mtypes.DatabaseAcknowledgement) map[string]mtypes.DatabaseAcknowledgement {
	result := make(map[string]mtypes.DatabaseAcknowledgement, len(previous))
	for _, acknowledgement := range previous {
		result[acknowledgementKey(acknowledgement.Identity)] = acknowledgement
	}
	return result
}

func pendingAcknowledgements(
	contributions []mtypes.DatabaseContribution,
	previous []mtypes.DatabaseAcknowledgement,
	reason, message string,
) []mtypes.DatabaseAcknowledgement {
	prior := previousAcknowledgements(previous)
	result := make([]mtypes.DatabaseAcknowledgement, 0, len(contributions))
	for _, contribution := range contributions {
		ack := mtypes.DatabaseAcknowledgement{
			Identity:        contribution.Identity,
			DesiredRevision: contribution.Revision,
			Status:          mtypes.AcknowledgementUnknown,
			Reason:          reason,
			Message:         message,
		}
		if old, ok := prior[acknowledgementKey(contribution.Identity)]; ok {
			ack.AppliedRevision = old.AppliedRevision
		}
		result = append(result, ack)
	}
	return result
}

func preservedAcknowledgements(
	contributions []mtypes.DatabaseContribution,
	previous []mtypes.DatabaseAcknowledgement,
) []mtypes.DatabaseAcknowledgement {
	prior := previousAcknowledgements(previous)
	result := make([]mtypes.DatabaseAcknowledgement, 0, len(contributions))
	for _, contribution := range contributions {
		if old, found := prior[acknowledgementKey(contribution.Identity)]; found && old.DesiredRevision == contribution.Revision {
			old.Identity = contribution.Identity
			result = append(result, old)
			continue
		}
		result = append(result, mtypes.DatabaseAcknowledgement{
			Identity:        contribution.Identity,
			DesiredRevision: contribution.Revision,
			Status:          mtypes.AcknowledgementUnknown,
			Reason:          "CustomMetricsPending",
			Message:         "Waiting for custom-metrics reconciliation",
		})
	}
	return result
}

func unknownAcknowledgements(
	contributions []mtypes.DatabaseContribution,
	previous []mtypes.DatabaseAcknowledgement,
	reason, message string,
) []mtypes.DatabaseAcknowledgement {
	prior := previousAcknowledgements(previous)
	result := make([]mtypes.DatabaseAcknowledgement, 0, len(contributions))
	for _, contribution := range contributions {
		ack := mtypes.DatabaseAcknowledgement{
			Identity:        contribution.Identity,
			DesiredRevision: contribution.Revision,
			Status:          mtypes.AcknowledgementUnknown,
			Reason:          reason,
			Message:         message,
		}
		if old, found := prior[acknowledgementKey(contribution.Identity)]; found {
			ack.AppliedRevision = old.AppliedRevision
		}
		result = append(result, ack)
	}
	return result
}

type acknowledgementFailure struct {
	reason  string
	details []string
}

func newAcknowledgementFailure(reason string, details ...string) acknowledgementFailure {
	return acknowledgementFailure{reason: reason, details: details}
}

func (f *acknowledgementFailure) addDetail(detail string) {
	for _, existing := range f.details {
		if existing == detail {
			return
		}
	}
	f.details = append(f.details, detail)
}

func (f acknowledgementFailure) message() string {
	details := append([]string(nil), f.details...)
	sort.Strings(details)
	return joinDiagnostics(details)
}

func invalidAcknowledgements(
	contributions []mtypes.DatabaseContribution,
	previous []mtypes.DatabaseAcknowledgement,
	failures map[string]acknowledgementFailure,
	fallback acknowledgementFailure,
) []mtypes.DatabaseAcknowledgement {
	result := pendingAcknowledgements(contributions, previous, "CustomMetricsPending", "Blocked by an invalid custom-metrics contribution")
	for i := range result {
		if failure, found := failures[acknowledgementKey(result[i].Identity)]; found {
			result[i].Status = mtypes.AcknowledgementFalse
			result[i].Reason = failure.reason
			result[i].Message = failure.message()
		}
	}
	if len(failures) == 0 {
		for i := range result {
			if result[i].Status != mtypes.AcknowledgementTrue {
				result[i].Status = mtypes.AcknowledgementFalse
				result[i].Reason = fallback.reason
				result[i].Message = fallback.message()
			}
		}
	}
	return result
}

func appliedAcknowledgements(
	contributions []mtypes.DatabaseContribution,
	previous []mtypes.DatabaseAcknowledgement,
	collisions []mtypes.CollisionError,
) []mtypes.DatabaseAcknowledgement {
	prior := previousAcknowledgements(previous)
	rejected := make(map[string]acknowledgementFailure)
	for _, collision := range collisions {
		if collision.Second.Contributor != nil {
			key := acknowledgementKey(*collision.Second.Contributor)
			failure := rejected[key]
			failure.reason = "MetricNameCollision"
			failure.addDetail(collision.Error())
			rejected[key] = failure
		}
	}
	result := make([]mtypes.DatabaseAcknowledgement, 0, len(contributions))
	for _, contribution := range contributions {
		ack := mtypes.DatabaseAcknowledgement{
			Identity:        contribution.Identity,
			DesiredRevision: contribution.Revision,
			AppliedRevision: contribution.Revision,
			Status:          mtypes.AcknowledgementTrue,
			Reason:          "CustomMetricsReady",
			Message:         "Database custom metrics are applied",
		}
		if !contribution.Exists {
			ack.Reason = "CustomMetricsDisabled"
			ack.Message = "Database custom metrics are disabled"
		}
		if failure, found := rejected[acknowledgementKey(contribution.Identity)]; found {
			ack.Status = mtypes.AcknowledgementFalse
			ack.Reason = failure.reason
			ack.Message = failure.message()
			ack.AppliedRevision = prior[acknowledgementKey(contribution.Identity)].AppliedRevision
		}
		result = append(result, ack)
	}
	return result
}

func invalidReason(kind InvalidKind) string {
	switch kind {
	case InvalidConfigMapNotFound:
		return "CustomMetricsConfigMapNotFound"
	case InvalidQuery:
		return "InvalidQueryDefinition"
	case InvalidCollision:
		return "MetricNameCollision"
	case InvalidConfigTooLarge:
		return "CustomMetricsConfigTooLarge"
	case InvalidOwnershipConflict:
		return "GeneratedResourceOwnershipConflict"
	default:
		return "CustomMetricsPending"
	}
}
