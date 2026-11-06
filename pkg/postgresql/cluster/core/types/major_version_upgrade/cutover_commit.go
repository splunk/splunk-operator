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

package majorversionupgradetypes

import (
	"context"
	"fmt"
	"time"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
)

// AuthorizeCutoverCommit is the switchover policy. The endpoint protocol receives
// it from its caller and does not interpret interruption milestones itself.
func AuthorizeCutoverCommit(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, now time.Time) error {
	if bg == nil || bg.Gates == nil || bg.Gates.Switchover == nil || bg.Gates.Switchover.Generation <= 0 || bg.Gates.Switchover.ConsumedAt == nil {
		return fmt.Errorf("canonical commit requires a consumed switchover gate")
	}
	w := bg.Switchover
	if w == nil ||
		w.FinalBlueLSN == nil || *w.FinalBlueLSN == "" || w.SequenceSynchronization == nil || w.SequenceSynchronization.VerifiedAt == nil ||
		w.SourceFenceRestoredAt != nil || w.Failure != nil || w.TimedOutAt != nil {
		return fmt.Errorf("canonical commit requires uninterrupted fence, drain and final parity receipts")
	}
	for _, milestone := range SwitchoverMilestones() {
		if milestone.RequiredBeforeCommit && milestone.Time(w) == nil {
			return fmt.Errorf("canonical commit requires switchover %s", milestone.Name)
		}
	}
	if !now.Before(w.DeadlineAt.Time) {
		return context.DeadlineExceeded
	}
	return nil
}
