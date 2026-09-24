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

const (
	EventMajorUpgradeScheduled    = "MajorUpgradeScheduled"
	EventMajorUpgradeStarted      = "MajorUpgradeStarted"
	EventMajorUpgradeCompleted    = "MajorUpgradeCompleted"
	EventMajorUpgradeFailed       = "MajorUpgradeFailed"
	EventMajorUpgradeRetryPending = "MajorUpgradeRetryPending"
	EventPreUpgradeBackupStarted  = "PreUpgradeBackupStarted"
	EventPostUpgradeBackupStarted = "PostUpgradeBackupStarted"
)

const (
	MessageMajorUpgradeScheduled    = "Major version upgrade scheduled"
	MessageMajorUpgradeStarted      = "Major version upgrade started"
	MessageMajorUpgradeCompleted    = "Major version upgrade completed"
	MessageMajorUpgradeFailed       = "Major version upgrade failed: %s"
	MessagePreUpgradeBackupStarted  = "Pre-upgrade backup started"
	MessagePostUpgradeBackupStarted = "Post-upgrade backup started"
)
