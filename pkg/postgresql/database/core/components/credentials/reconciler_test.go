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
	"testing"

	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type readResult struct {
	facts ObservedSecret
	err   error
}

type fakeOperations struct {
	owner     *OwnerIdentity
	reads     []readResult
	readCalls int
	create    error
	adopt     error
	created   []SecretRef
	adopted   []SecretRef
}

type readOnlySecretReader struct {
	reads []readResult
}

func (r *readOnlySecretReader) Read(context.Context, SecretRef) (ObservedSecret, error) {
	if len(r.reads) == 0 {
		return ObservedSecret{}, errors.New("unexpected Secret read")
	}
	result := r.reads[0]
	r.reads = r.reads[1:]
	return result.facts, result.err
}

func (f *fakeOperations) OwnerIdentity() OwnerIdentity {
	if f.owner == nil {
		return OwnerIdentity{Name: "orders", UID: "orders-uid", Kind: "PostgresDatabase"}
	}
	return *f.owner
}

func (f *fakeOperations) Read(context.Context, SecretRef) (ObservedSecret, error) {
	f.readCalls++
	if len(f.reads) == 0 {
		return ObservedSecret{}, errors.New("unexpected Secret read")
	}
	result := f.reads[0]
	f.reads = f.reads[1:]
	return result.facts, result.err
}

func (f *fakeOperations) CreateGenerated(_ context.Context, ref SecretRef, _ string) error {
	f.created = append(f.created, ref)
	return f.create
}

func (f *fakeOperations) Adopt(_ context.Context, ref SecretRef, _ string) error {
	f.adopted = append(f.adopted, ref)
	return f.adopt
}

func generatedIntent(continuity Continuity) Intent {
	return Intent{
		Ref:        SecretRef{Namespace: "dbs", Name: "orders-admin"},
		Role:       "orders_admin",
		Source:     SourceGenerated,
		Continuity: continuity,
	}
}

func externalIntent() Intent {
	intent := generatedIntent(ContinuityNew)
	intent.Ref.Name = "external-orders-admin"
	intent.Source = SourceExternal
	return intent
}

func newReconciler(t *testing.T, operations SecretOperations) Reconciler {
	t.Helper()
	reconciler, err := New(operations)
	require.NoError(t, err)
	return reconciler
}

func TestReconcilerGeneratedCredentialPolicy(t *testing.T) {
	owner := OwnerIdentity{Name: "orders", UID: "orders-uid"}
	foreign := OwnerIdentity{Name: "other", UID: "other-uid", Kind: "PostgresCluster"}
	tests := []struct {
		name          string
		intent        Intent
		operations    *fakeOperations
		wantMode      reconciliationTypes.Mode
		wantReason    string
		wantCreates   int
		wantAdoptions int
		wantReAdopted bool
		wantMessage   string
	}{
		{
			name:        "creates a new generated credential once",
			intent:      generatedIntent(ContinuityNew),
			operations:  &fakeOperations{reads: []readResult{{err: ErrSecretNotFound}}},
			wantMode:    reconciliationTypes.ModeConverged,
			wantCreates: 1,
		},
		{
			name:       "reports a missing published credential as drift",
			intent:     generatedIntent(ContinuityPublished),
			operations: &fakeOperations{reads: []readResult{{err: ErrSecretNotFound}}},
			wantMode:   reconciliationTypes.ModeWaiting,
			wantReason: ReasonManagedSecretMissing,
		},
		{
			name:       "keeps a self-owned credential unchanged",
			intent:     generatedIntent(ContinuityPublished),
			operations: &fakeOperations{reads: []readResult{{facts: ObservedSecret{Controller: &owner}}}},
			wantMode:   reconciliationTypes.ModeConverged,
		},
		{
			name:          "re-adopts retained credential before accepting its existing owner",
			intent:        generatedIntent(ContinuityPublished),
			operations:    &fakeOperations{reads: []readResult{{facts: ObservedSecret{Controller: &owner, RetainedFrom: owner.Name}}}},
			wantMode:      reconciliationTypes.ModeConverged,
			wantAdoptions: 1,
			wantReAdopted: true,
		},
		{
			name:          "re-adopts a retained credential without changing data",
			intent:        generatedIntent(ContinuityPublished),
			operations:    &fakeOperations{reads: []readResult{{facts: ObservedSecret{RetainedFrom: owner.Name}}}},
			wantMode:      reconciliationTypes.ModeConverged,
			wantAdoptions: 1,
			wantReAdopted: true,
		},
		{
			name:          "adopts an unowned credential",
			intent:        generatedIntent(ContinuityNew),
			operations:    &fakeOperations{reads: []readResult{{facts: ObservedSecret{}}}},
			wantMode:      reconciliationTypes.ModeConverged,
			wantAdoptions: 1,
		},
		{
			name:        "reports a foreign-owned credential as drift",
			intent:      generatedIntent(ContinuityPublished),
			operations:  &fakeOperations{reads: []readResult{{facts: ObservedSecret{Controller: &foreign}}}},
			wantMode:    reconciliationTypes.ModeWaiting,
			wantReason:  ReasonManagedSecretOwnershipConflict,
			wantMessage: "PostgresCluster other",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := newReconciler(t, tt.operations).Reconcile(t.Context(), []Intent{tt.intent})
			require.NoError(t, result.Outcome.Validate("credentials"))
			assert.Equal(t, tt.wantMode, result.Outcome.Mode())
			assert.Equal(t, tt.wantReason, result.Outcome.Reason())
			assert.Len(t, tt.operations.created, tt.wantCreates)
			assert.Len(t, tt.operations.adopted, tt.wantAdoptions)
			assert.Equal(t, tt.wantReAdopted, result.RetainedReadopted)
			if tt.wantMessage != "" {
				assert.Contains(t, result.Outcome.Message(), tt.wantMessage)
			}
		})
	}
}

func TestReconcilerExternalCredentialsAreReadOnlyAndClassifyInvalidStateAsDrift(t *testing.T) {
	tests := []struct {
		name       string
		operations *fakeOperations
		wantMode   reconciliationTypes.Mode
		wantReason string
	}{
		{
			name:       "missing",
			operations: &fakeOperations{reads: []readResult{{err: ErrSecretNotFound}}},
			wantMode:   reconciliationTypes.ModeTerminalError,
			wantReason: ReasonExternalSecretMissing,
		},
		{
			name:       "missing data",
			operations: &fakeOperations{reads: []readResult{{facts: ObservedSecret{}}}},
			wantMode:   reconciliationTypes.ModeWaiting,
			wantReason: ReasonExternalSecretMissingData,
		},
		{
			name:       "missing required keys",
			operations: &fakeOperations{reads: []readResult{{facts: ObservedSecret{DataDefined: true, UsernamePresent: true}}}},
			wantMode:   reconciliationTypes.ModeWaiting,
			wantReason: ReasonExternalSecretMissingKeys,
		},
		{
			name:       "wrong username",
			operations: &fakeOperations{reads: []readResult{{facts: ObservedSecret{DataDefined: true, Username: "wrong", UsernamePresent: true, PasswordPresent: true, ReloadEnabled: true}}}},
			wantMode:   reconciliationTypes.ModeWaiting,
			wantReason: ReasonExternalSecretInvalid,
		},
		{
			name:       "missing reload label",
			operations: &fakeOperations{reads: []readResult{{facts: ObservedSecret{DataDefined: true, Username: "orders_admin", UsernamePresent: true, PasswordPresent: true}}}},
			wantMode:   reconciliationTypes.ModeWaiting,
			wantReason: ReasonExternalSecretMissingLabel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := newReconciler(t, tt.operations).Reconcile(t.Context(), []Intent{externalIntent()})
			require.NoError(t, result.Outcome.Validate("credentials"))
			assert.Equal(t, tt.wantMode, result.Outcome.Mode())
			assert.Equal(t, tt.wantReason, result.Outcome.Reason())
			if tt.wantMode == reconciliationTypes.ModeTerminalError {
				assert.ErrorIs(t, result.Outcome.Err(), reconcile.TerminalError(nil))
			} else {
				assert.NoError(t, result.Outcome.Err())
				assert.Equal(t, reconciliationTypes.ReadinessRetryDelay, result.Outcome.Result().RequeueAfter)
			}
			assert.Empty(t, tt.operations.created)
			assert.Empty(t, tt.operations.adopted)
		})
	}
}

func TestExternalCredentialStrategyDependsOnlyOnSecretReader(t *testing.T) {
	strategy := externalCredentialStrategy{reader: &readOnlySecretReader{
		reads: []readResult{{facts: ObservedSecret{
			DataDefined:     true,
			Username:        "orders_admin",
			UsernamePresent: true,
			PasswordPresent: true,
			ReloadEnabled:   true,
		}}},
	}}

	result := strategy.Reconcile(t.Context(), externalIntent())
	require.NoError(t, result.Outcome.Validate("credentials"))
	assert.Equal(t, reconciliationTypes.ModeConverged, result.Outcome.Mode())
}

func TestReconcilerReReadsAfterCreateRace(t *testing.T) {
	owner := OwnerIdentity{Name: "orders", UID: "orders-uid"}
	operations := &fakeOperations{
		reads: []readResult{
			{err: ErrSecretNotFound},
			{facts: ObservedSecret{Controller: &owner}},
		},
		create: ErrSecretAlreadyExists,
	}

	result := newReconciler(t, operations).Reconcile(t.Context(), []Intent{generatedIntent(ContinuityNew)})
	require.NoError(t, result.Outcome.Validate("credentials"))
	assert.Equal(t, reconciliationTypes.ModeConverged, result.Outcome.Mode())
	assert.Len(t, operations.created, 1)
	assert.Empty(t, operations.adopted)
}

func TestNewRejectsNilOperations(t *testing.T) {
	_, err := New(nil)
	assert.EqualError(t, err, "credential Secret operations are not configured")
}

func TestNewRejectsIncompleteOwnerIdentity(t *testing.T) {
	_, err := New(&fakeOperations{owner: &OwnerIdentity{Name: "orders"}})
	assert.EqualError(t, err, "credential Secret owner name and UID are not configured")
}

func TestReconcilerRejectsInvalidIntentsBeforeSecretOperations(t *testing.T) {
	tests := []struct {
		name    string
		intents []Intent
		want    string
	}{
		{
			name: "empty intent list",
			want: "at least one credential intent is required",
		},
		{
			name: "unknown source",
			intents: []Intent{{
				Ref: SecretRef{Name: "orders-admin"}, Role: "orders_admin", Source: "unknown", Continuity: ContinuityNew,
			}},
			want: "unknown source",
		},
		{
			name: "unknown continuity",
			intents: []Intent{{
				Ref: SecretRef{Name: "orders-admin"}, Role: "orders_admin", Source: SourceGenerated, Continuity: "unknown",
			}},
			want: "unknown continuity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operations := &fakeOperations{}
			result := newReconciler(t, operations).Reconcile(t.Context(), tt.intents)
			require.NoError(t, result.Outcome.Validate("credentials"))
			assert.Equal(t, reconciliationTypes.ModeTerminalError, result.Outcome.Mode())
			assert.Contains(t, result.Outcome.Message(), tt.want)
			assert.Zero(t, operations.readCalls)
			assert.Empty(t, operations.created)
			assert.Empty(t, operations.adopted)
		})
	}
}

func TestReconcilerDefersAdoptionConflictWithoutStatus(t *testing.T) {
	operations := &fakeOperations{
		reads: []readResult{{facts: ObservedSecret{}}},
		adopt: ErrSecretConflict,
	}
	result := newReconciler(t, operations).Reconcile(t.Context(), []Intent{generatedIntent(ContinuityNew)})

	require.NoError(t, result.Outcome.Validate("credentials"))
	assert.Equal(t, reconciliationTypes.ModeDeferred, result.Outcome.Mode())
	assert.Equal(t, reconciliationTypes.StatusNone, result.Outcome.StatusAction())
	assert.Equal(t, reconciliationTypes.ReadinessRetryDelay, result.Outcome.Result().RequeueAfter)
	assert.NoError(t, result.Outcome.Err())
}

func TestReconcilerPrefersExternalMissingOverManagedDrift(t *testing.T) {
	operations := &fakeOperations{reads: []readResult{{err: ErrSecretNotFound}, {err: ErrSecretNotFound}}}
	result := newReconciler(t, operations).Reconcile(t.Context(), []Intent{generatedIntent(ContinuityPublished), externalIntent()})
	require.NoError(t, result.Outcome.Validate("credentials"))
	assert.Equal(t, ReasonExternalSecretMissing, result.Outcome.Reason())
	assert.Contains(t, result.Outcome.Message(), "Managed Secret orders-admin is missing")
	assert.Contains(t, result.Outcome.Message(), "external secret \"external-orders-admin\" is missing")
}

func TestReconcilerPrefersCredentialPolicyAndPreservesSiblingMessages(t *testing.T) {
	readFailure := errors.New("temporary Kubernetes API failure")
	result := newReconciler(t, &fakeOperations{reads: []readResult{
		{err: readFailure},
		{facts: ObservedSecret{DataDefined: true, Username: "wrong", UsernamePresent: true, PasswordPresent: true, ReloadEnabled: true}},
	}}).Reconcile(t.Context(), []Intent{generatedIntent(ContinuityNew), externalIntent()})

	require.NoError(t, result.Outcome.Validate("credentials"))
	assert.Equal(t, reconciliationTypes.ModeWaiting, result.Outcome.Mode())
	assert.Equal(t, ReasonExternalSecretInvalid, result.Outcome.Reason())
	assert.Contains(t, result.Outcome.Message(), "failed to read Secret orders-admin")
	assert.Contains(t, result.Outcome.Message(), "username does not match PostgreSQL role")
	assert.NoError(t, result.Outcome.Err())
}

func TestReconcilerKeepsReAdoptionFactWhenSiblingFails(t *testing.T) {
	owner := OwnerIdentity{Name: "orders", UID: "orders-uid"}
	result := newReconciler(t, &fakeOperations{reads: []readResult{
		{facts: ObservedSecret{Controller: &owner, RetainedFrom: owner.Name}},
		{err: ErrSecretNotFound},
	}}).Reconcile(t.Context(), []Intent{generatedIntent(ContinuityPublished), externalIntent()})

	require.NoError(t, result.Outcome.Validate("credentials"))
	assert.Equal(t, ReasonExternalSecretMissing, result.Outcome.Reason())
	assert.True(t, result.RetainedReadopted)
}

func TestSelectFailurePrefersTerminalOverWaitingWithSameReason(t *testing.T) {
	result := selectFailure([]Result{
		drift(ReasonExternalSecretInvalid, "external Secret data is invalid"),
		terminal(ReasonExternalSecretInvalid, "external Secret reference is empty", errors.New("empty external reference")),
	})

	require.NoError(t, result.Outcome.Validate("credentials"))
	assert.Equal(t, reconciliationTypes.ModeTerminalError, result.Outcome.Mode())
	assert.Equal(t, ReasonExternalSecretInvalid, result.Outcome.Reason())
}

func TestReadyDocumentsFutureFacadeSuccessContract(t *testing.T) {
	result := Ready(2)
	require.NoError(t, result.Outcome.Validate("credentials"))
	assert.Equal(t, reconciliationTypes.ModeConverged, result.Outcome.Mode())
	assert.Equal(t, reconciliationTypes.StatusPersistAndContinue, result.Outcome.StatusAction())
	assert.Equal(t, ReasonSecretsCreated, result.Outcome.Reason())
}

func TestReconcilerCombinesTerminalCredentialMessages(t *testing.T) {
	admin := externalIntent()
	rw := externalIntent()
	rw.Ref.Name = "external-orders-rw"
	rw.Role = "orders_rw"

	result := newReconciler(t, &fakeOperations{reads: []readResult{{err: ErrSecretNotFound}, {err: ErrSecretNotFound}}}).
		Reconcile(t.Context(), []Intent{admin, rw})

	require.NoError(t, result.Outcome.Validate("credentials"))
	assert.Equal(t, reconciliationTypes.ModeTerminalError, result.Outcome.Mode())
	assert.Contains(t, result.Outcome.Message(), admin.Ref.Name)
	assert.Contains(t, result.Outcome.Message(), rw.Ref.Name)
	assert.Contains(t, result.Outcome.Err().Error(), admin.Ref.Name)
	assert.Contains(t, result.Outcome.Err().Error(), rw.Ref.Name)
}
