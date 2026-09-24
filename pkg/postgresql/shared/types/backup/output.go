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

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type BackupMethod int

const (
	methodInvalid              BackupMethod = iota // zero value — never valid
	BackupMethodVolumeSnapshot                     // CSI VolumeSnapshot backups
	BackupMethodPlugin                             // CNPG plugin (barman-cloud object store)
)

// ScheduleResult is the observed state of a scheduled backup.
type ScheduleResult struct {
	// Exists reports whether the ScheduledBackup object is present.
	Exists bool
	// LastScheduleTime is when the backend last triggered a backup, if known.
	LastScheduleTime *metav1.Time
	// NextScheduleTime is when the backend will next trigger a backup, if known.
	NextScheduleTime *metav1.Time
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
