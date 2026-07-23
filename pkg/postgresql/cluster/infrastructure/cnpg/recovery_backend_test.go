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
package cnpg

import (
	"testing"

	"github.com/splunk/splunk-operator/pkg/postgresql/shared/recoverytypes"
	"github.com/stretchr/testify/assert"
)

// TestRecoveryBackendValidatePlan pins CNPG's recovery capability rules to the adapter: this is the
// single place those provider-specific constraints live now that core delegates them through the
// RecoveryBackend port.
func TestRecoveryBackendValidatePlan(t *testing.T) {
	t.Parallel()

	backend := NewRecoveryBackend()

	tests := []struct {
		name       string
		plan       recoverytypes.RecoveryPlan
		wantFields []string
	}{
		{
			name: "plain volume snapshot, no target => accepted",
			plan: recoverytypes.RecoveryPlan{Source: recoverytypes.SourceVolumeSnapshot},
		},
		{
			name: "volume snapshot with time target and class store => accepted",
			plan: recoverytypes.RecoveryPlan{
				Source:                   recoverytypes.SourceVolumeSnapshotWithWAL,
				HasTarget:                true,
				TargetKind:               recoverytypes.TargetTime,
				ClassProvidesObjectStore: true,
			},
		},
		{
			name: "objectStorage with lsn target and class store => accepted",
			plan: recoverytypes.RecoveryPlan{
				Source:                   recoverytypes.SourceObjectStorage,
				HasTarget:                true,
				TargetKind:               recoverytypes.TargetLSN,
				ClassProvidesObjectStore: true,
			},
		},
		{
			name: "objectStorage with xid target => target-kind violation",
			plan: recoverytypes.RecoveryPlan{
				Source:                   recoverytypes.SourceObjectStorage,
				HasTarget:                true,
				TargetKind:               recoverytypes.TargetXID,
				ClassProvidesObjectStore: true,
			},
			wantFields: []string{"spec.bootstrapFrom.recoveryTarget"},
		},
		{
			name: "objectStorage with name target => target-kind violation",
			plan: recoverytypes.RecoveryPlan{
				Source:                   recoverytypes.SourceObjectStorage,
				HasTarget:                true,
				TargetKind:               recoverytypes.TargetName,
				ClassProvidesObjectStore: true,
			},
			wantFields: []string{"spec.bootstrapFrom.recoveryTarget"},
		},
		{
			name: "objectStorage with immediate target => target-kind violation",
			plan: recoverytypes.RecoveryPlan{
				Source:                   recoverytypes.SourceObjectStorage,
				HasTarget:                true,
				TargetKind:               recoverytypes.TargetImmediate,
				ClassProvidesObjectStore: true,
			},
			wantFields: []string{"spec.bootstrapFrom.recoveryTarget"},
		},
		{
			name: "volume snapshot with WAL archive but class lacks store => walArchive violation",
			plan: recoverytypes.RecoveryPlan{
				Source:                   recoverytypes.SourceVolumeSnapshotWithWAL,
				HasTarget:                true,
				TargetKind:               recoverytypes.TargetTime,
				ClassProvidesObjectStore: false,
			},
			wantFields: []string{"spec.bootstrapFrom.volumeSnapshot.walArchive"},
		},
		{
			name: "objectStorage source but class lacks store => objectStorage violation",
			plan: recoverytypes.RecoveryPlan{
				Source:                   recoverytypes.SourceObjectStorage,
				HasTarget:                true,
				TargetKind:               recoverytypes.TargetTime,
				ClassProvidesObjectStore: false,
			},
			wantFields: []string{"spec.bootstrapFrom.objectStorage"},
		},
		{
			name: "objectStorage, xid target, class lacks store => both violations",
			plan: recoverytypes.RecoveryPlan{
				Source:                   recoverytypes.SourceObjectStorage,
				HasTarget:                true,
				TargetKind:               recoverytypes.TargetXID,
				ClassProvidesObjectStore: false,
			},
			wantFields: []string{"spec.bootstrapFrom.recoveryTarget", "spec.bootstrapFrom.objectStorage"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := backend.ValidatePlan(tt.plan)
			gotFields := make([]string, 0, len(got))
			for _, v := range got {
				gotFields = append(gotFields, v.Field)
				assert.NotEmpty(t, v.Message, "every violation must carry an actionable message")
			}
			assert.ElementsMatch(t, tt.wantFields, gotFields)
		})
	}
}
