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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Intent struct {
	Strategy        string
	SourcePgVersion string
	TargetPgVersion string

	Policy           UpgradePolicy
	State            []platformv1alpha1.PostgresMajorUpgradeStatus
	RetryRequestedAt *metav1.Time
}

type BackupInfo struct {
	BackupStatus *platformv1alpha1.BackupStatus
	BackupName   string
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
