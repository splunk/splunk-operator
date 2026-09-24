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

// Package recoverytypes holds the engine-agnostic value objects (DTOs) exchanged
// across the recovery port. Like backuptypes it is a leaf package that depends on
// neither core nor any use-case package, so both the core models and the recovery
// adapter can import these types without risking an import cycle.
package recoverytypes

// SourceKind identifies the base-backup source a recovery bootstraps from. It is
// the operator's own vocabulary: the adapter maps it onto concrete provider
// bootstrap shapes so callers never depend on a provisioner's types.
//
// The zero value is explicitly invalid: a well-formed plan must name one of the
// real sources.
type SourceKind int

const (
	_ SourceKind = iota // zero value — never valid
	// SourceVolumeSnapshot restores from a volume snapshot only; recovery stops
	// at the snapshot point (no WAL replay from an object store).
	SourceVolumeSnapshot
	// SourceVolumeSnapshotWithWAL restores from a volume-snapshot base and replays
	// WAL from an object-store archive on top of it (snapshot-based PITR).
	SourceVolumeSnapshotWithWAL
	// SourceObjectStorage restores entirely from an object-store base backup plus
	// its archived WAL, with no volume snapshot involved.
	SourceObjectStorage
)

// ReadsObjectStore reports whether recovering from this source requires reading
// WAL (and, for an object-store source, the base backup) from an object store.
func (s SourceKind) ReadsObjectStore() bool {
	return s == SourceVolumeSnapshotWithWAL || s == SourceObjectStorage
}

// TargetKind is the operator's vocabulary for a point-in-time recovery target
// kind. Its string values match the CRD enum so status/spec echoes are stable,
// but the adapter — not the caller — owns the mapping to provider fields.
type TargetKind string

const (
	// TargetTime recovers up to a timestamp.
	TargetTime TargetKind = "time"
	// TargetLSN recovers up to a WAL log sequence number.
	TargetLSN TargetKind = "lsn"
	// TargetXID recovers up to a transaction ID.
	TargetXID TargetKind = "xid"
	// TargetName recovers to a named restore point.
	TargetName TargetKind = "name"
	// TargetImmediate ends recovery at the first consistent state.
	TargetImmediate TargetKind = "immediate"
)

// RecoveryPlan is the provider-neutral description of a restore request. Core
// derives it from the immutable bootstrapFrom spec and the referenced class, then
// asks a RecoveryBackend whether the target provisioner can execute it. A
// different provisioner may accept source/target combinations CNPG rejects.
type RecoveryPlan struct {
	// Source is the base-backup source the recovery bootstraps from.
	Source SourceKind
	// TargetKind is the kind of PITR target requested; empty (with HasTarget
	// false) means recover to the latest available WAL.
	TargetKind TargetKind
	// HasTarget reports whether a PITR target was requested at all.
	HasTarget bool
	// ClassProvidesObjectStore reports whether the referenced class defines an
	// object store the backend can resolve WAL/base-backup access from.
	ClassProvidesObjectStore bool
}

// CapabilityViolation reports, in the operator's own vocabulary, one way the
// target provisioner cannot execute a RecoveryPlan. Field is the spec path the
// violation concerns so callers can attach it to an admission error.
type CapabilityViolation struct {
	Field   string
	Message string
}
