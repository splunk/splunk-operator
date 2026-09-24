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

package backuptypes

// ScheduleSpec is the backend-agnostic description of a scheduled backup. It
// carries only the operator's own vocabulary — the adapter is responsible for
// translating it into a concrete CNPG ScheduledBackup object.
type ScheduleSpec struct {
	// Name is the ScheduledBackup object name.
	Name string
	// Namespace is the namespace the ScheduledBackup lives in.
	Namespace string
	// CNPGClusterName is the name of the CNPG Cluster the backup targets.
	CNPGClusterName string
	// Schedule is a standard 5-field cron expression; the adapter converts it
	// to the 6-field form CNPG expects.
	Schedule string
	// Target selects which instance performs the backup ("primary"/"prefer-standby").
	Target string
	// Method selects the backup mechanism (volume snapshot or plugin).
	Method BackupMethod
	// PluginName is the CNPG plugin name when Method is BackupMethodPlugin; empty otherwise.
	PluginName string
}

// BackupRequest is the backend-agnostic description of a single one-shot backup.
// As with ScheduleSpec it carries only the operator's own vocabulary; the
// adapter translates it into a concrete CNPG Backup object.
type BackupRequest struct {
	// Name is the Backup object name. Callers derive a deterministic name so that
	// repeated reconciles of the same request are idempotent.
	Name string
	// Namespace is the namespace the Backup lives in.
	Namespace string
	// CNPGClusterName is the name of the CNPG Cluster the backup targets.
	CNPGClusterName string
	// Target selects which instance performs the backup ("primary"/"prefer-standby").
	Target string
	// Method selects the backup mechanism (volume snapshot or plugin).
	Method BackupMethod
	// PluginName is the CNPG plugin name when Method is BackupMethodPlugin; empty otherwise.
	PluginName string
}
