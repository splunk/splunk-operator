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

package majorupgradeadapter

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestPrecommitIntegrityFailureRestoresBlueAndRequiresRebuild(t *testing.T) {
	f := newCutoverFixture(t)
	f.sql.parityErr = fmt.Errorf("%w: frozen catalog changed during cutover", mvutypes.ErrCandidateInvalid)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.SourceFenceRestoredAt != nil
	})

	status := f.status()
	require.NotNil(t, status.RebuildRequired)
	assert.Contains(t, *status.RebuildRequired, "frozen catalog changed during cutover")
	assert.Nil(t, status.CommitReceipt)
	assert.Contains(t, f.calls, "restore-blue", "classification must not skip pre-commit blue restoration")
	assert.NotContains(t, f.calls, "open-green")
}

func TestPrecommitControlDriftRequiresRebuild(t *testing.T) {
	for name, drifted := range map[string]func(string) bool{
		"role capabilities": func(sql string) bool { return sql == controlRoleSQL },
		"helper definition": func(sql string) bool { return sql == controlHelperProofSQL },
	} {
		t.Run(name, func(t *testing.T) {
			conn := &cutoverSQLTestConn{query: func(sql string, _ []any) ([][]any, error) {
				return [][]any{{!drifted(sql)}}, nil
			}}
			drift := verifyControlDatabase(t.Context(), conn, cutoverIdentity{Role: "control", AttemptID: "attempt"}, "postgres")
			require.ErrorIs(t, drift, mvutypes.ErrCandidateInvalid)

			f := newCutoverFixture(t)
			f.sql.parityErr = drift
			f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
				return bg.Switchover.SourceFenceRestoredAt != nil
			})
			require.NotNil(t, f.status().RebuildRequired)
			outcome, err := f.step()
			require.NoError(t, err)
			assert.Equal(t, mvutypes.ReasonCandidateRebuildRequired, outcome.Report.Reason)
		})
	}
}

func TestPrecommitReplacedOrReconfiguredGreenRequiresRebuild(t *testing.T) {
	for name, drift := range map[string]func(*cutoverFixture){
		"configuration": func(f *cutoverFixture) {
			green := &cnpgv1.Cluster{}
			require.NoError(t, f.runtime.reader.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "green"}, green))
			green.Spec.Instances = 5
			require.NoError(t, f.runtime.client.Update(t.Context(), green))
		},
		"replaced identity": func(f *cutoverFixture) {
			f.mutate(func(_ *platformv1alpha1.PostgresCluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
				bg.Green.Ref.UID = "previous-green"
			})
		},
		"changed owner": func(f *cutoverFixture) {
			green := &cnpgv1.Cluster{}
			require.NoError(t, f.runtime.reader.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "green"}, green))
			green.OwnerReferences = nil
			require.NoError(t, f.runtime.client.Update(t.Context(), green))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newCutoverFixture(t)
			f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
				return bg.Switchover.ControlIdentity != nil && bg.Switchover.ControlIdentity.PreparedAt != nil
			})
			drift(f)
			f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
				return bg.Switchover.SourceFenceRestoredAt != nil
			})
			require.NotNil(t, f.status().RebuildRequired)
			outcome, err := f.step()
			require.NoError(t, err)
			assert.Equal(t, mvutypes.ReasonCandidateRebuildRequired, outcome.Report.Reason)
		})
	}
}

func TestPrecommitSupportedObjectDriftRequiresRebuild(t *testing.T) {
	unloggedRows := func(sql string, _ []any) ([][]any, error) {
		switch {
		case sql == unsupportedDatabaseObjectsSQL:
			return [][]any{{false}}, nil
		case strings.Contains(sql, "relpersistence = 'u'"):
			return [][]any{{"public.scratch"}}, nil
		}
		return [][]any{{true}}, nil
	}
	transient := func(sql string, _ []any) ([][]any, error) {
		if strings.Contains(sql, "relpersistence = 'u'") {
			return nil, errors.New("connection reset")
		}
		return [][]any{{false}}, nil
	}
	check := func(query func(string, []any) ([][]any, error)) error {
		return (&postgresSwitchoverSQL{}).verifySupportedObjects(t.Context(), &cutoverSQLTestConn{query: query}, nil, declaredDatabase{name: "orders"}, cutoverIdentity{AttemptID: "attempt"}, sideGreen)
	}
	drift := check(unloggedRows)
	require.ErrorIs(t, drift, mvutypes.ErrCandidateInvalid, "a nonempty unlogged table appeared after preflight")
	require.False(t, mvutypes.IsTerminalFlowFailure(check(transient)), "query errors stay retryable")

	for name, cause := range map[string]error{"drift": drift, "transient": check(transient)} {
		t.Run(name, func(t *testing.T) {
			f := newCutoverFixture(t)
			f.sql.parityErr = cause
			f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
				return bg.Switchover.SourceFenceRestoredAt != nil
			})
			assert.Equal(t, name == "drift", f.status().RebuildRequired != nil)
		})
	}
}

func TestPrecommitOrdinaryFailureRemainsReusable(t *testing.T) {
	f := newCutoverFixture(t)
	f.sql.parityErr = errors.New("transient parity read failure")
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.SourceFenceRestoredAt != nil
	})
	assert.Nil(t, f.status().RebuildRequired)
}

func TestIntegrityFailureRacingCanonicalCommitIsNeverClassified(t *testing.T) {
	f := newCutoverFixture(t)
	// The canonical write lands, but its call reports an integrity failure
	// before status records a receipt. Only the canonical record knows.
	f.endpoints.commitErr = fmt.Errorf("%w: frozen catalog changed during cutover", mvutypes.ErrCandidateInvalid)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.EndpointPublicationEventRef != nil
	})
	status := f.status()
	assert.Nil(t, status.RebuildRequired, "a committed attempt fails forward and is never classified for rebuild")
	assert.NotContains(t, f.calls, "restore-blue")
}

func TestRestoredWindowReportMatchesRebuildClassification(t *testing.T) {
	now := metav1.Now()
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{Switchover: &platformv1alpha1.BlueGreenSwitchoverStatus{Sequence: 1, SourceFenceRestoredAt: &now}}
	report := restoredWindowReport(status)
	assert.Equal(t, "SwitchoverFailed", report.Reason)
	assert.Contains(t, report.Message, "newer retry timestamp")

	status.Switchover.TimedOutAt = &now
	assert.Equal(t, "SwitchoverTimedOut", restoredWindowReport(status).Reason)

	status.RebuildRequired = new("frozen catalog changed during cutover")
	report = restoredWindowReport(status)
	assert.Equal(t, string(mvutypes.Failed), report.Phase)
	assert.Equal(t, mvutypes.ReasonCandidateRebuildRequired, report.Reason, "a rebuild must not be advertised as retryable")
	assert.NotContains(t, report.Message, "retry timestamp")
}
