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

package majorversionupgradetypes

import (
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgendpointprotocol "github.com/splunk/splunk-operator/pkg/postgresql/shared/endpointprotocol"
)

// CutoverStage describes durable execution ownership, not permission to perform
// a canonical commit. Only fresh canonical observation proves that boundary.
type CutoverStage uint8

const (
	CutoverNone CutoverStage = iota
	CutoverAuthorized
	CutoverInterrupting
	CutoverRestored
	CutoverCommitted
	CutoverTargetAvailable
	CutoverHandedOff
)

func CutoverStageOf(entry platformv1alpha1.PostgresMajorUpgradeStatus) CutoverStage {
	if IsBlueGreenAttemptCleaned(entry) || entry.Phase != nil && (*entry.Phase == string(Completed) || *entry.Phase == string(Cancelled)) {
		return CutoverNone
	}
	return CutoverStageOfStatus(entry.BlueGreen)
}

// CutoverStageOfStatus deliberately prefers recovery for partial durable evidence.
func CutoverStageOfStatus(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) CutoverStage {
	if bg == nil || bg.HistoryCompacted {
		return CutoverNone
	}
	w := bg.Switchover
	if bg.CommitReceipt != nil {
		if pgendpointprotocol.CommitReceiptTargetsGreen(bg) && bg.CommitReceipt.CommittedAt != nil && w != nil && w.TargetAvailableAt != nil {
			if w.EndpointPublicationEventRef != nil {
				return CutoverHandedOff
			}
			return CutoverTargetAvailable
		}
		return CutoverCommitted
	}
	if w != nil {
		if w.TargetAvailableAt != nil {
			return CutoverInterrupting
		}
		if w.SourceFenceRestoredAt != nil {
			return CutoverRestored
		}
		if w.StartedAt != nil {
			return CutoverInterrupting
		}
	}
	if w != nil && w.Sequence > 0 || bg.Gates != nil && bg.Gates.Switchover != nil && bg.Gates.Switchover.ConsumedAt != nil {
		return CutoverAuthorized
	}
	return CutoverNone
}

func (stage CutoverStage) OwnsRecovery() bool {
	switch stage {
	case CutoverAuthorized, CutoverInterrupting, CutoverCommitted, CutoverTargetAvailable, CutoverHandedOff:
		return true
	}
	return false
}

// NormalizeCutoverProgress is the one phase-safety policy used by flow and by
// persistence after merging any newer coordinator evidence inside its CAS.
func NormalizeCutoverProgress(progress Progress) Progress {
	stage := CutoverStageOfStatus(progress.BlueGreen)
	if !stage.OwnsRecovery() {
		return progress
	}
	phase := SwitchingOver
	if stage == CutoverHandedOff {
		if progress.Report.Phase == string(Completed) || progress.Report.Phase == string(PostUpgradeBackup) {
			return progress
		}
		phase = PostUpgradeBackup
	}
	progress.Report.Name = UseCaseName
	progress.Report.Phase = string(phase)
	progress.Report.Retry = true
	progress.Report.Sleep = new(1)
	return progress
}

// CutoverStageForIntent classifies only the matching durable attempt.
func CutoverStageForIntent(intent Intent) CutoverStage {
	for i := len(intent.State) - 1; i >= 0; i-- {
		if MatchesIntent(intent.State[i], intent) {
			return CutoverStageOf(intent.State[i])
		}
	}
	return CutoverNone
}
