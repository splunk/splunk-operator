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
package credentials

import (
	"context"
	"errors"

	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
)

const (
	ConditionSecretsReady = "SecretsReady"

	ReasonSecretsCreated                 = "SecretsCreated"
	ReasonSecretsCreationFailed          = "SecretsCreationFailed"
	ReasonSecretOperationsNotConfigured  = "SecretOperationsNotConfigured"
	ReasonInvalidCredentialIntent        = "InvalidCredentialIntent"
	ReasonManagedSecretMissing           = "ManagedSecretMissing"
	ReasonManagedSecretOwnershipConflict = "ManagedSecretOwnershipConflict"
	ReasonExternalSecretMissing          = "ExternalSecretMissing"
	ReasonExternalSecretInvalid          = "ExternalSecretInvalid"
	ReasonExternalSecretMissingData      = "ExternalSecretMissingData"
	ReasonExternalSecretMissingKeys      = "ExternalSecretMissingKeys"
	ReasonExternalSecretMissingLabel     = "ExternalSecretMissingReloadLabel"

	PhaseProvisioning = "Provisioning"
	PhaseFailed       = "Failed"
)

var (
	// ErrSecretNotFound lets the policy distinguish an absent Secret while the
	// adapter retains the Kubernetes NotFound error as its wrapped cause.
	ErrSecretNotFound = errors.New("credential Secret not found")
	// ErrSecretAlreadyExists marks a create race. The reconciler always reads
	// again and classifies the live Secret instead of assuming it is safe.
	ErrSecretAlreadyExists = errors.New("credential Secret already exists")
	// ErrSecretConflict marks a read-modify-write race. The caller must requeue
	// without publishing a credential failure because no desired-state decision
	// was applied.
	ErrSecretConflict = errors.New("credential Secret changed during adoption")
)

// Source identifies who supplies a credential's data.
type Source string

const (
	SourceGenerated Source = "Generated"
	SourceExternal  Source = "External"
)

// Continuity tells the component whether this database has already published
// credentials. Only a new generated credential may be created when absent.
type Continuity string

const (
	ContinuityNew       Continuity = "New"
	ContinuityPublished Continuity = "Published"
)

// SecretRef is the only Kubernetes identity the policy needs to address a
// credential Secret.
type SecretRef struct {
	Namespace string
	Name      string
}

// OwnerIdentity contains the fields used to identify a Secret controller. Kind
// is retained for unambiguous ownership-conflict diagnostics; Name and UID
// determine whether the Secret belongs to this PostgresDatabase.
type OwnerIdentity struct {
	Name string
	UID  string
	Kind string
}

// Intent is one desired role credential. Generated Secret ownership comes from
// the owner-bound SecretOperations port rather than being repeated per intent.
type Intent struct {
	Ref        SecretRef
	Role       string
	Source     Source
	Continuity Continuity
}

// ObservedSecret is the non-sensitive Secret view consumed by policy. Password
// contents never leave the adapter/infrastructure boundary.
type ObservedSecret struct {
	DataDefined     bool
	Username        string
	UsernamePresent bool
	PasswordPresent bool
	ReloadEnabled   bool
	RetainedFrom    string
	ResourceVersion string
	Controller      *OwnerIdentity
}

// SecretReader is the read-only port used by external credential policy.
// Keeping it separate makes external Secret reconciliation incapable of
// creating or adopting a Secret through its declared dependency.
type SecretReader interface {
	Read(context.Context, SecretRef) (ObservedSecret, error)
}

// SecretOperations is the consumer-owned, owner-bound port for generated
// credentials. OwnerIdentity returns the same PostgresDatabase used by
// generated Secret mutations, so policy ownership checks and Kubernetes
// ownership updates cannot diverge.
type SecretOperations interface {
	SecretReader
	OwnerIdentity() OwnerIdentity
	CreateGenerated(context.Context, SecretRef, string) error
	Adopt(context.Context, SecretRef, string) error
}

// Result is the component decision. Outcome is the single source of lifecycle,
// status, and error information. RetainedReadopted records a metadata-only
// adoption for the future facade to log when it wires this component into the
// production lifecycle.
type Result struct {
	Outcome           reconciliationTypes.Outcome
	RetainedReadopted bool
}
