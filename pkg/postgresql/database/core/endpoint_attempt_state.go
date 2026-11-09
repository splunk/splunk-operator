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
	"reflect"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// These are persisted values from the unavailable blue/green upgrade status.
// Keep this database protocol independent of the cluster upgrade core package.
const (
	unavailableHistoryStrategy  = "blueGreen"
	unavailableHistoryPhase     = "Failed"
	unavailableHistoryCondition = "MajorUpgradeTerminalFailure"
	unavailableHistoryReason    = "BlueGreenStrategyUnavailable"
)

type endpointAttemptState uint8

const (
	endpointAttemptAbsent endpointAttemptState = iota
	endpointAttemptActive
	endpointAttemptCleanedDetailed
	endpointAttemptCleanedCompacted
	endpointAttemptHistorical
	endpointAttemptHistoricalNonOwning
	endpointAttemptInvalid
)

// endpointAttemptView makes the latest endpoint-owning blue/green entry and its
// durable form a single decision. An older entry never becomes the current
// endpoint writer.
type endpointAttemptView struct {
	status    *platformv1alpha1.PostgresBlueGreenUpgradeStatus
	state     endpointAttemptState
	phase     string
	head      bool
	compacted bool
}

func inspectEndpointAttempt(parent *platformv1alpha1.PostgresCluster) endpointAttemptView {
	if parent == nil {
		return endpointAttemptView{state: endpointAttemptAbsent}
	}
	entries := parent.Status.PostgresMajorUpgradeStatus
	var nonOwning endpointAttemptView
	for i := len(entries) - 1; i >= 0; i-- {
		bg := entries[i].BlueGreen
		if bg == nil {
			continue
		}
		view := endpointAttemptView{status: bg, head: i == len(entries)-1, compacted: bg.HistoryCompacted || legacyCompactedEndpointHistory(bg)}
		if entries[i].Phase != nil {
			view.phase = *entries[i].Phase
		}
		view.state = classifyEndpointAttempt(view, entries[i])
		if view.state == endpointAttemptHistoricalNonOwning {
			// Keep scanning: a placeholder must not conceal an older owner.
			if nonOwning.status == nil {
				nonOwning = view
			}
			continue
		}

		return view
	}
	if nonOwning.status != nil {
		return nonOwning
	}
	return endpointAttemptView{state: endpointAttemptAbsent}
}

// The unavailable-strategy placeholder predates the blue/green runtime. It
// never owned endpoints or created resources, so a later pgUpgrade does not
// need to wait for Cleanup=Cleaned. Require the exact empty payload so any
// evidence of intent or retained artifacts keeps the historical fence.
func inactiveEndpointAttemptHistory(entry platformv1alpha1.PostgresMajorUpgradeStatus) bool {
	bg := entry.BlueGreen
	if bg == nil || entry.Strategy == nil || *entry.Strategy != unavailableHistoryStrategy ||
		entry.Phase == nil || *entry.Phase != unavailableHistoryPhase ||
		!reflect.DeepEqual(*bg, platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: bg.AttemptID}) {
		return false
	}
	for _, condition := range entry.Conditions {
		if condition.Type == unavailableHistoryCondition &&
			condition.Status == metav1.ConditionTrue && condition.Reason == unavailableHistoryReason {
			return true
		}
	}
	return false
}

// Releases before HistoryCompacted existed persisted only these summary fields.
// Keep their exact old shape readable until all saved status has been rewritten
// with the explicit marker. New summaries are classified by HistoryCompacted.
func legacyCompactedEndpointHistory(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
	return bg != nil && bg.Cleanup != nil && bg.Cleanup.State == platformv1alpha1.BlueGreenCleanupStateCleaned &&
		bg.Blue == nil && bg.Green == nil && bg.CandidateEndpoints == nil &&
		len(bg.DatabaseReplication) == 0 && len(bg.CredentialFingerprints) == 0 &&
		bg.Gates == nil && bg.EndpointPreparation == nil && bg.EndpointIntentRevision == 0 &&
		bg.EndpointPublication == nil && bg.CommitReceipt == nil && bg.Switchover == nil &&
		bg.Recoverability == nil
}

func (view endpointAttemptView) isLatest(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
	return view.head && view.status != nil && bg != nil && bg.AttemptID != "" && view.status.AttemptID == bg.AttemptID
}

func (view endpointAttemptView) productionCompleted(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
	return view.status != nil && bg != nil && bg.AttemptID != "" && view.status.AttemptID == bg.AttemptID && view.phase == "Completed"
}

func (view endpointAttemptView) hasPublicationProof(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
	return view.isLatest(bg) || view.state == endpointAttemptHistorical &&
		view.status != nil && bg != nil && view.status.AttemptID == bg.AttemptID &&
		bg.Cleanup != nil && bg.Cleanup.State == platformv1alpha1.BlueGreenCleanupStateCleaned
}

// A detailed historical entry without a receipt can use the current-provider
// convergence check only when it positively proves a pre-commit cancellation.
func (view endpointAttemptView) precommitCancellation(parent *platformv1alpha1.PostgresCluster) bool {
	bg := view.status
	if view.state != endpointAttemptHistorical || view.compacted || bg == nil ||
		bg.Cleanup == nil || bg.Cleanup.State != platformv1alpha1.BlueGreenCleanupStateCleaned ||
		bg.Blue == nil || bg.Blue.Ref.Name == "" || bg.Blue.Ref.UID == "" ||
		bg.CommitReceipt != nil || bg.EndpointPublication != nil ||
		parent == nil || parent.Status.ProvisionerRef == nil ||
		parent.Status.ProvisionerRef.Name != bg.Blue.Ref.Name || parent.Status.ProvisionerRef.UID != bg.Blue.Ref.UID {
		return false
	}
	return bg.Switchover == nil || bg.Switchover.TargetAvailableAt == nil
}

func classifyEndpointAttempt(view endpointAttemptView, entry platformv1alpha1.PostgresMajorUpgradeStatus) endpointAttemptState {
	bg := view.status
	switch {
	case bg.AttemptID == "":
		return endpointAttemptInvalid
	case bg.HistoryCompacted && (bg.Cleanup == nil || bg.Cleanup.State != platformv1alpha1.BlueGreenCleanupStateCleaned):
		return endpointAttemptInvalid
	case !view.head && inactiveEndpointAttemptHistory(entry):
		return endpointAttemptHistoricalNonOwning
	case !view.head:
		return endpointAttemptHistorical
	case view.compacted:
		return endpointAttemptCleanedCompacted
	case bg.Cleanup != nil && bg.Cleanup.State == platformv1alpha1.BlueGreenCleanupStateCleaned:
		return endpointAttemptCleanedDetailed
	default:
		return endpointAttemptActive
	}
}
