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

// Package backuptypes holds the engine-agnostic value objects (DTOs) exchanged
// across the backup port. It is a leaf package that depends on neither core nor
// any use-case package, so both the core models and future use-cases can import
// these types without risking an import cycle between them.
package backuptypes

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BackupMethod is the operator's vocabulary for how a backup is produced. It is
// translated to the underlying CNPG method inside the adapter so that callers
// (the backup component, future use-cases) never depend on cnpg types directly.
//
// The zero value (methodInvalid) is explicitly invalid: callers must set
// a named constant. Any unset or unknown value is rejected by the adapter.
type BackupMethod int

const (
	methodInvalid              BackupMethod = iota // zero value — never valid
	BackupMethodVolumeSnapshot                     // CSI VolumeSnapshot backups
	BackupMethodPlugin                             // CNPG plugin (barman-cloud object store)
)

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

// ScheduleResult is the observed state of a scheduled backup.
type ScheduleResult struct {
	// Exists reports whether the ScheduledBackup object is present.
	Exists bool
	// LastScheduleTime is when the backend last triggered a backup, if known.
	LastScheduleTime *metav1.Time
	// NextScheduleTime is when the backend will next trigger a backup, if known.
	NextScheduleTime *metav1.Time
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

// BackupResult is the observed state of a single backup run. It exposes the
// coordinates downstream consumers need to record, translated out of the
// CNPG Backup object's spec and status.
//
// Fields intentionally omitted: Namespace (callers already know it), Labels /
// Annotations (internal CNPG implementation detail), and raw CNPG object
// references (would re-couple callers to cnpg types).
type BackupResult struct {
	// Name is the Backup object name.
	Name string
	// CNPGClusterName is the cluster the backup targets.
	CNPGClusterName string
	// Method is the backup mechanism used.
	Method BackupMethod
	// Phase is the raw CNPG backup phase (pending/started/running/completed/failed/...).
	Phase string
	// Done is true once the backup has completed successfully.
	Done bool
	// Failed is true once the backup has terminally failed.
	Failed bool
	// Error carries the CNPG-reported failure detail when Failed is true.
	Error string
	// BackupID identifies the completed backup (CNPG status.backupId).
	BackupID string
	// StartedAt is when the backup started, if known.
	StartedAt *metav1.Time
	// StoppedAt is when the backup stopped, if known.
	StoppedAt *metav1.Time
	// SnapshotNames lists the VolumeSnapshot resource names produced by a
	// volumeSnapshot-method backup; empty for plugin/object-store backups.
	SnapshotNames []string
}
