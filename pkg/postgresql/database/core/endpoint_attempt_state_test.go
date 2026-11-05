package core

import (
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestInspectEndpointAttemptUsesExplicitHistoryStateAndHead(t *testing.T) {
	completed := "Completed"
	failed := string(mvutypes.Failed)
	blueGreen := mvutypes.MajorUpgradeFlowBlueGreen
	pgUpgrade := mvutypes.MajorUpgradeFlowPgUpgrade
	cleaned := &platformv1alpha1.BlueGreenCleanupStatus{State: platformv1alpha1.BlueGreenCleanupStateCleaned}
	unavailable := []metav1.Condition{{Type: mvutypes.ConditionMajorUpgradeTerminalFailure, Status: metav1.ConditionTrue, Reason: mvutypes.ReasonBlueGreenStrategyUnavailable}}
	for _, tc := range []struct {
		name          string
		entries       []platformv1alpha1.PostgresMajorUpgradeStatus
		want          endpointAttemptState
		head          bool
		wantAttemptID string
	}{
		{name: "no attempt", want: endpointAttemptAbsent},
		{name: "active head", entries: []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "a"}}}, want: endpointAttemptActive, head: true},
		{name: "cleaned with details", entries: []platformv1alpha1.PostgresMajorUpgradeStatus{{Phase: &completed, BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "a", Cleanup: cleaned, CommitReceipt: &platformv1alpha1.BlueGreenCommitReceiptStatus{Sequence: 1}}}}, want: endpointAttemptCleanedDetailed, head: true},
		{name: "explicit compacted summary", entries: []platformv1alpha1.PostgresMajorUpgradeStatus{{Phase: &completed, BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "a", Cleanup: cleaned, HistoryCompacted: true}}}, want: endpointAttemptCleanedCompacted, head: true},
		{name: "legacy compacted summary without marker", entries: []platformv1alpha1.PostgresMajorUpgradeStatus{{Phase: &completed, BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "a", Cleanup: cleaned}}}, want: endpointAttemptCleanedCompacted, head: true},
		{name: "marker before cleanup is invalid", entries: []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "a", HistoryCompacted: true}}}, want: endpointAttemptInvalid, head: true},
		{name: "missing attempt ID is invalid", entries: []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{Cleanup: cleaned, HistoryCompacted: true}}}, want: endpointAttemptInvalid, head: true},
		{name: "older blue green is historical", entries: []platformv1alpha1.PostgresMajorUpgradeStatus{{BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "a", Cleanup: cleaned}}, {}}, want: endpointAttemptHistorical},
		{name: "unavailable placeholder before pgUpgrade owns no endpoints", entries: []platformv1alpha1.PostgresMajorUpgradeStatus{
			{Phase: &failed, Strategy: &blueGreen, Conditions: unavailable, BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "unavailable"}},
			{Phase: &completed, Strategy: &pgUpgrade},
		}, want: endpointAttemptHistoricalNonOwning},
		{name: "unavailable placeholder does not conceal older retained resources", entries: []platformv1alpha1.PostgresMajorUpgradeStatus{
			{Phase: &completed, Strategy: &blueGreen, BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "owner", Cleanup: &platformv1alpha1.BlueGreenCleanupStatus{State: platformv1alpha1.BlueGreenCleanupStateRetained}}},
			{Phase: &failed, Strategy: &blueGreen, Conditions: unavailable, BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "unavailable"}},
			{Phase: &completed, Strategy: &pgUpgrade},
		}, want: endpointAttemptHistorical, wantAttemptID: "owner"},
		{name: "unavailable history with endpoint intent stays fenced", entries: []platformv1alpha1.PostgresMajorUpgradeStatus{
			{Phase: &failed, Strategy: &blueGreen, Conditions: unavailable, BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "a", EndpointIntentRevision: 1}},
			{Phase: &completed, Strategy: &pgUpgrade},
		}, want: endpointAttemptHistorical},
		{name: "unavailable history with retained cleanup stays fenced", entries: []platformv1alpha1.PostgresMajorUpgradeStatus{
			{Phase: &failed, Strategy: &blueGreen, Conditions: unavailable, BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "a", Cleanup: &platformv1alpha1.BlueGreenCleanupStatus{State: platformv1alpha1.BlueGreenCleanupStateRetained}}},
			{Phase: &completed, Strategy: &pgUpgrade},
		}, want: endpointAttemptHistorical},
		{name: "unavailable history with a candidate resource stays fenced", entries: []platformv1alpha1.PostgresMajorUpgradeStatus{
			{Phase: &failed, Strategy: &blueGreen, Conditions: unavailable, BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "a", CandidateEndpoints: &platformv1alpha1.BlueGreenCandidateEndpointsStatus{}}},
			{Phase: &completed, Strategy: &pgUpgrade},
		}, want: endpointAttemptHistorical},
		{name: "historical placeholder without explicit unavailable failure stays fenced", entries: []platformv1alpha1.PostgresMajorUpgradeStatus{
			{Phase: &failed, Strategy: &blueGreen, BlueGreen: &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "a"}},
			{Phase: &completed, Strategy: &pgUpgrade},
		}, want: endpointAttemptHistorical},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := &platformv1alpha1.PostgresCluster{Status: platformv1alpha1.PostgresClusterStatus{PostgresMajorUpgradeStatus: tc.entries}}
			view := inspectEndpointAttempt(parent)
			assert.Equal(t, tc.want, view.state)
			assert.Equal(t, tc.head, view.head)
			if tc.wantAttemptID != "" {
				assert.Equal(t, tc.wantAttemptID, view.status.AttemptID)
			}
			if tc.want == endpointAttemptCleanedCompacted || tc.wantAttemptID == "owner" {
				assert.True(t, view.productionCompleted(view.status))
			} else if !tc.head {
				assert.False(t, view.productionCompleted(view.status))
			}
		})
	}
}
