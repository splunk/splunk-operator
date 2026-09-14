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
	"fmt"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Intent struct {
	Strategy        string
	SourcePgVersion string
	TargetPgVersion string
	// AttemptID identifies the active blue/green attempt. It is empty for the
	// shipped pgUpgrade strategy, whose existing version/strategy identity is
	// retained for backward compatibility.
	AttemptID string
	// RequiresBlueGreenRearm identifies the one-step, controller-owned action
	// that records a false allow gate after a cleaned blue/green attempt. It is
	// an execution directive only: Schedule may discover it but must not
	// persist it. Act performs the durable status write before a later
	// allow=true can create a new attempt for the same upgrade family.
	RequiresBlueGreenRearm bool

	Policy           UpgradePolicy
	State            []platformv1alpha1.PostgresMajorUpgradeStatus
	RetryRequestedAt *metav1.Time
}

type BackupInfo struct {
	BackupStatus *platformv1alpha1.BackupStatus
	BackupName   string
}

// Progress is one durable major-upgrade status write. BlueGreen is optional so
// the existing pgUpgrade flow continues to preserve its current status shape.
// A non-nil BlueGreen value replaces the current attempt's nested durable
// record together with the generic report and backup evidence.
type Progress struct {
	Report    reconciliationTypes.Report
	Baseline  *BackupInfo
	BlueGreen *platformv1alpha1.PostgresBlueGreenUpgradeStatus
}

type UpgradePolicy struct {
	AllowDirectMultiMajorJump bool
}

func DefaultUpgradePolicy() UpgradePolicy {
	return UpgradePolicy{
		AllowDirectMultiMajorJump: false,
	}
}

func PreUpgradeBackupName(intent Intent) string {
	return fmt.Sprintf("pre-upgrade-%s-%s", intent.SourcePgVersion, intent.TargetPgVersion)
}

func PostUpgradeBackupName(intent Intent) string {
	return fmt.Sprintf("post-upgrade-%s-%s", intent.SourcePgVersion, intent.TargetPgVersion)
}
