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

package types

import "errors"

var (
	// ErrManagedRoleIntentConflict identifies a conflicting status write so the
	// pipeline can retry immediately without publishing a failure condition.
	ErrManagedRoleIntentConflict = errors.New("managed-role intent write conflict")
	// ErrManagedRoleAcknowledgementRead identifies a transient failure before
	// the cluster acknowledgement could be evaluated.
	ErrManagedRoleAcknowledgementRead = errors.New("managed-role acknowledgement read failed")
)

// ManagedRoleParticipant identifies one PostgresDatabase participant. Both
// fields are required when correlating an acknowledgement.
type ManagedRoleParticipant struct {
	Name string
	UID  string
}

// ManagedRolePublicationTarget identifies the exact status object and
// generation to which managed-role intent belongs.
type ManagedRolePublicationTarget struct {
	Name       string
	Namespace  string
	UID        string
	Generation int64
}

// ManagedRoleIntent is one committed role claim or removal signal.
type ManagedRoleIntent struct {
	Name       string
	SecretName string
	Exists     bool
}

// DatabaseRoleIntent groups role intent by logical database.
type DatabaseRoleIntent struct {
	Database string
	Roles    []ManagedRoleIntent
}

// ManagedRolePublication is the complete database-owned role projection for
// one reconciliation pass.
type ManagedRolePublication struct {
	Target    ManagedRolePublicationTarget
	Databases []DatabaseRoleIntent
}

// ManagedRoleAcknowledgementTarget identifies the cluster status carrying
// the acknowledgement.
type ManagedRoleAcknowledgementTarget struct {
	Namespace   string
	ClusterName string
}

// ManagedRoleConflict records a role claim withheld from one participant.
type ManagedRoleConflict struct {
	Role        string
	AttemptedBy ManagedRoleParticipant
}

// ManagedRoleAcknowledgement contains the cluster-owned facts consumed by the
// database acknowledgement gate. Published is false when the status has not
// been produced yet.
type ManagedRoleAcknowledgement struct {
	Published  bool
	Reconciled []string
	Failed     map[string]string
	Owners     map[string]ManagedRoleParticipant
	Conflicts  []ManagedRoleConflict
}
