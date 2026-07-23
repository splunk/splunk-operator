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
package core

import (
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/recoverytypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// recoveryTestCfg builds a MergedConfig with the given bootstrapFrom and (optionally) a class
// barman object store, enough to exercise the recovery translation helpers.
func recoveryTestCfg(b *enterprisev4.BootstrapFrom, withObjectStore bool) *MergedConfig {
	version := "17"
	instances := int32(1)
	cfg := &MergedConfig{
		Spec: &enterprisev4.PostgresClusterSpec{
			PostgresVersion: &version,
			Instances:       &instances,
			BootstrapFrom:   b,
			Resources:       &corev1.ResourceRequirements{},
		},
		CNPG: &enterprisev4.CNPGConfig{},
	}
	if withObjectStore {
		cfg.CNPG.Backup = &enterprisev4.CNPGBackupConfig{
			BarmanObjectStore: &enterprisev4.CNPGBarmanObjectStoreConfig{
				DestinationPath: "s3://bucket/pg",
				S3Credentials: enterprisev4.CNPGBarmanS3Credentials{
					AccessKeyId:     corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "creds"}, Key: "id"},
					SecretAccessKey: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "creds"}, Key: "secret"},
				},
			},
		}
	}
	return cfg
}

func TestBuildBootstrapRecovery(t *testing.T) {
	t.Parallel()

	const snapAPIGroup = "snapshot.storage.k8s.io"

	tests := []struct {
		name        string
		bootstrap   *enterprisev4.BootstrapFrom
		wantStorage string  // "" => no volumeSnapshots
		wantWalSnap string  // "" => no walStorage in the snapshot data source
		wantSource  string  // "" => recovery.Source unset
		wantTarget  *string // expected recovery target time (only targetTime asserted here)
	}{
		{
			name: "volume snapshot only",
			bootstrap: &enterprisev4.BootstrapFrom{
				VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "snap-1"},
			},
			wantStorage: "snap-1",
		},
		{
			name: "volume snapshot with separate WAL volume snapshot",
			bootstrap: &enterprisev4.BootstrapFrom{
				VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "snap-1", WalStorage: ptr.To("wal-snap-1")},
			},
			wantStorage: "snap-1",
			wantWalSnap: "wal-snap-1",
		},
		{
			name: "volume snapshot + walArchive + PITR sets recovery.source",
			bootstrap: &enterprisev4.BootstrapFrom{
				VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{
					Storage:    "snap-1",
					WalArchive: &enterprisev4.ObjectStorageSource{ServerName: "src"},
				},
				RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetTime, Value: "2026-05-01T13:30:00Z"},
			},
			wantStorage: "snap-1",
			wantSource:  recoveryExternalClusterName,
			// UTC "Z" is normalized to a numeric offset PostgreSQL's recovery_target_time GUC accepts.
			wantTarget: ptr.To("2026-05-01 13:30:00+00:00"),
		},
		{
			name: "object storage source sets recovery.source, no volumeSnapshots",
			bootstrap: &enterprisev4.BootstrapFrom{
				ObjectStorage:  &enterprisev4.ObjectStorageSource{ServerName: "src"},
				RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetTime, Value: "2026-05-01T13:30:00Z"},
			},
			wantSource: recoveryExternalClusterName,
			wantTarget: ptr.To("2026-05-01 13:30:00+00:00"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := buildBootstrapRecovery(tt.bootstrap)
			require.NotNil(t, rec)

			if tt.wantStorage == "" {
				assert.Nil(t, rec.VolumeSnapshots, "expected no volumeSnapshots")
			} else {
				require.NotNil(t, rec.VolumeSnapshots)
				assert.Equal(t, tt.wantStorage, rec.VolumeSnapshots.Storage.Name)
				assert.Equal(t, "VolumeSnapshot", rec.VolumeSnapshots.Storage.Kind)
				require.NotNil(t, rec.VolumeSnapshots.Storage.APIGroup)
				assert.Equal(t, snapAPIGroup, *rec.VolumeSnapshots.Storage.APIGroup)
			}

			if tt.wantWalSnap == "" {
				if rec.VolumeSnapshots != nil {
					assert.Nil(t, rec.VolumeSnapshots.WalStorage)
				}
			} else {
				require.NotNil(t, rec.VolumeSnapshots.WalStorage)
				assert.Equal(t, tt.wantWalSnap, rec.VolumeSnapshots.WalStorage.Name)
			}

			assert.Equal(t, tt.wantSource, rec.Source)

			if tt.wantTarget == nil {
				assert.Nil(t, rec.RecoveryTarget)
			} else {
				require.NotNil(t, rec.RecoveryTarget)
				assert.Equal(t, *tt.wantTarget, rec.RecoveryTarget.TargetTime)
			}
		})
	}
}

// TestBuildRecoveryTargetMapsAllFields asserts every provider-agnostic RecoveryTarget field maps
// onto the CNPG type. targetTime/LSN/XID/Name are exercised individually because at most one may be
// set; Exclusive is orthogonal and combined with targetTime.
func TestBuildRecoveryTargetMapsAllFields(t *testing.T) {
	t.Parallel()

	assert.Nil(t, buildRecoveryTarget(nil), "nil target => nil (recover to latest)")

	tt := buildRecoveryTarget(&enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetTime, Value: "2026-05-01T13:30:00Z", Exclusive: ptr.To(true)})
	require.NotNil(t, tt)
	assert.Equal(t, "2026-05-01 13:30:00+00:00", tt.TargetTime)
	require.NotNil(t, tt.Exclusive)
	assert.True(t, *tt.Exclusive)

	assert.Equal(t, "0/16D68D0", buildRecoveryTarget(&enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetLSN, Value: "0/16D68D0"}).TargetLSN)
	assert.Equal(t, "1234567", buildRecoveryTarget(&enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetXID, Value: "1234567"}).TargetXID)
	assert.Equal(t, "before-migration", buildRecoveryTarget(&enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetName, Value: "before-migration"}).TargetName)

	imm := buildRecoveryTarget(&enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetImmediate})
	require.NotNil(t, imm.TargetImmediate)
	assert.True(t, *imm.TargetImmediate)
}

// TestBuildRecoveryTargetNormalizesTime asserts a type=time value is re-rendered into a layout
// PostgreSQL's recovery_target_time GUC accepts. PG rejects the RFC 3339 "Z" zone designator, so an
// admitted UTC "...Z" value must be emitted with a numeric offset or the restore fails with
// "invalid value for parameter recovery_target_time". Regression guard for the e2e-found bug.
func TestBuildRecoveryTargetNormalizesTime(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"utc Z is rewritten to numeric offset", "2026-05-01T13:30:00Z", "2026-05-01 13:30:00+00:00"},
		{"fractional utc Z keeps sub-second precision", "2026-07-14T08:08:11.954622Z", "2026-07-14 08:08:11.954622+00:00"},
		{"non-zero offset is preserved as the same instant", "2026-07-14T10:08:11+02:00", "2026-07-14 10:08:11+02:00"},
		{"unparseable value passes through unchanged", "not-a-time", "not-a-time"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildRecoveryTarget(&enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetTime, Value: tc.value})
			require.NotNil(t, got)
			assert.Equal(t, tc.want, got.TargetTime)
			assert.NotContains(t, got.TargetTime, "Z", "recovery_target_time must not carry the Z designator PG rejects")
		})
	}
}

func TestBuildRecoveryExternalClusters(t *testing.T) {
	t.Parallel()

	t.Run("nil when no object store source", func(t *testing.T) {
		t.Parallel()
		cfg := recoveryTestCfg(&enterprisev4.BootstrapFrom{
			VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "snap-1"},
		}, true)
		assert.Nil(t, buildRecoveryExternalClusters(cfg, "c1"))
	})

	t.Run("built for walArchive source", func(t *testing.T) {
		t.Parallel()
		cfg := recoveryTestCfg(&enterprisev4.BootstrapFrom{
			VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{
				Storage:    "snap-1",
				WalArchive: &enterprisev4.ObjectStorageSource{ServerName: "src"},
			},
		}, true)
		ext := buildRecoveryExternalClusters(cfg, "c1")
		require.Len(t, ext, 1)
		assert.Equal(t, recoveryExternalClusterName, ext[0].Name)
		require.NotNil(t, ext[0].PluginConfiguration)
		assert.Equal(t, barmanCloudPluginName, ext[0].PluginConfiguration.Name)
		assert.Equal(t, objectStoreName("c1"), ext[0].PluginConfiguration.Parameters["barmanObjectName"])
		assert.Equal(t, "src", ext[0].PluginConfiguration.Parameters["serverName"])
	})

	t.Run("built for objectStorage source", func(t *testing.T) {
		t.Parallel()
		cfg := recoveryTestCfg(&enterprisev4.BootstrapFrom{
			ObjectStorage: &enterprisev4.ObjectStorageSource{ServerName: "src"},
		}, true)
		ext := buildRecoveryExternalClusters(cfg, "c1")
		require.Len(t, ext, 1)
		assert.Equal(t, "src", ext[0].PluginConfiguration.Parameters["serverName"])
	})
}

// TestBuildCNPGClusterSpec_RecoveryWiring asserts the recovery source/externalClusters are attached
// to the full ClusterSpec, and that a foreign externalClusters entry survives the reconcile.
func TestBuildCNPGClusterSpec_RecoveryWiring(t *testing.T) {
	t.Parallel()

	cfg := recoveryTestCfg(&enterprisev4.BootstrapFrom{
		ObjectStorage:  &enterprisev4.ObjectStorageSource{ServerName: "src"},
		RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetTime, Value: "2026-05-01T13:30:00Z"},
	}, true)
	storage := resource.MustParse("1Gi")
	cfg.Spec.Storage = &storage

	live := cnpgv1.ClusterSpec{
		ExternalClusters: []cnpgv1.ExternalCluster{{Name: "foreign"}},
	}
	spec := buildCNPGClusterSpec(live, cfg, "c1", "my-secret", false)

	require.NotNil(t, spec.Bootstrap.Recovery)
	assert.Nil(t, spec.Bootstrap.InitDB, "recovery bootstrap must not also set initdb")
	assert.Equal(t, recoveryExternalClusterName, spec.Bootstrap.Recovery.Source)

	var names []string
	for _, e := range spec.ExternalClusters {
		names = append(names, e.Name)
	}
	assert.Contains(t, names, "foreign", "foreign externalClusters entry must be preserved")
	assert.Contains(t, names, recoveryExternalClusterName, "operator recovery entry must be present")
}

// TestBuildCNPGClusterSpec_ReservedOriginNameCollision documents that recoveryExternalClusterName
// ("origin") is reserved: a live externalCluster using that name is treated as operator-owned and
// replaced with the synthesized recovery entry, never preserved as a foreign entry.
func TestBuildCNPGClusterSpec_ReservedOriginNameCollision(t *testing.T) {
	t.Parallel()

	cfg := recoveryTestCfg(&enterprisev4.BootstrapFrom{
		ObjectStorage: &enterprisev4.ObjectStorageSource{ServerName: "src"},
	}, true)
	storage := resource.MustParse("1Gi")
	cfg.Spec.Storage = &storage

	// A pre-existing entry named "origin" with foreign connection parameters.
	live := cnpgv1.ClusterSpec{
		ExternalClusters: []cnpgv1.ExternalCluster{
			{Name: recoveryExternalClusterName, ConnectionParameters: map[string]string{"host": "foreign-host"}},
		},
	}
	spec := buildCNPGClusterSpec(live, cfg, "c1", "my-secret", false)

	var origins []cnpgv1.ExternalCluster
	for _, e := range spec.ExternalClusters {
		if e.Name == recoveryExternalClusterName {
			origins = append(origins, e)
		}
	}
	require.Len(t, origins, 1, "exactly one origin entry expected (the foreign one is replaced, not duplicated)")
	assert.Nil(t, origins[0].ConnectionParameters, "foreign origin entry must be replaced by the operator's plugin-based entry")
	require.NotNil(t, origins[0].PluginConfiguration)
	assert.Equal(t, "src", origins[0].PluginConfiguration.Parameters["serverName"])
}

func TestManagedObjectStoreCfg_RestoreWithBackupDisabled(t *testing.T) {
	t.Parallel()

	// backup disabled, but an objectStorage restore source is set => ObjectStore CR still required.
	cfg := recoveryTestCfg(&enterprisev4.BootstrapFrom{
		ObjectStorage: &enterprisev4.ObjectStorageSource{ServerName: "src"},
	}, true)
	assert.Nil(t, activeBarmanObjectStoreCfg(cfg), "precondition: backup not enabled")
	assert.NotNil(t, managedObjectStoreCfg(cfg), "restore source must require the ObjectStore CR even with backup disabled")

	// walArchive on a snapshot source also requires it.
	cfgWal := recoveryTestCfg(&enterprisev4.BootstrapFrom{
		VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{
			Storage:    "snap-1",
			WalArchive: &enterprisev4.ObjectStorageSource{ServerName: "src"},
		},
	}, true)
	assert.NotNil(t, managedObjectStoreCfg(cfgWal))

	// plain snapshot restore, no object store use => not required.
	cfgPlain := recoveryTestCfg(&enterprisev4.BootstrapFrom{
		VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "snap-1"},
	}, true)
	assert.Nil(t, managedObjectStoreCfg(cfgPlain))
}

func TestValidateBootstrapFrom(t *testing.T) {
	t.Parallel()

	classWithStore := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "c"},
		Spec: enterprisev4.PostgresClusterClassSpec{
			CNPG: &enterprisev4.CNPGConfig{
				Backup: &enterprisev4.CNPGBackupConfig{
					BarmanObjectStore: &enterprisev4.CNPGBarmanObjectStoreConfig{DestinationPath: "s3://b/p"},
				},
			},
		},
	}
	classNoStore := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "c"},
		Spec:       enterprisev4.PostgresClusterClassSpec{CNPG: &enterprisev4.CNPGConfig{}},
	}

	clusterWith := func(b *enterprisev4.BootstrapFrom) *enterprisev4.PostgresCluster {
		return &enterprisev4.PostgresCluster{Spec: enterprisev4.PostgresClusterSpec{BootstrapFrom: b}}
	}

	tests := []struct {
		name      string
		class     *enterprisev4.PostgresClusterClass
		bootstrap *enterprisev4.BootstrapFrom
		wantField string // "" => expect no error
	}{
		{
			name:      "no bootstrapFrom is valid",
			class:     classWithStore,
			bootstrap: nil,
		},
		{
			name:      "plain snapshot valid without object store",
			class:     classNoStore,
			bootstrap: &enterprisev4.BootstrapFrom{VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "s"}},
		},
		{
			name:      "both sources set rejected",
			class:     classWithStore,
			bootstrap: &enterprisev4.BootstrapFrom{VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "s"}, ObjectStorage: &enterprisev4.ObjectStorageSource{ServerName: "src"}},
			wantField: "spec.bootstrapFrom",
		},
		{
			name:      "neither source set rejected",
			class:     classWithStore,
			bootstrap: &enterprisev4.BootstrapFrom{},
			wantField: "spec.bootstrapFrom",
		},
		{
			name:      "snapshot PITR without walArchive rejected",
			class:     classWithStore,
			bootstrap: &enterprisev4.BootstrapFrom{VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "s"}, RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetTime, Value: "2026-05-01T13:30:00Z"}},
			wantField: "spec.bootstrapFrom.volumeSnapshot.walArchive",
		},
		{
			name:      "snapshot PITR with walArchive accepted",
			class:     classWithStore,
			bootstrap: &enterprisev4.BootstrapFrom{VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "s", WalArchive: &enterprisev4.ObjectStorageSource{ServerName: "src"}}, RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetTime, Value: "2026-05-01T13:30:00Z"}},
		},
		// Note: the class-object-store requirement and the object-store target-kind restriction are
		// provider capability rules and no longer live in validateBootstrapFrom — they are exercised
		// via the RecoveryBackend port in TestValidateRecoveryCapabilities and the CNPG adapter test.
		{
			name:      "objectStorage with class object store accepted",
			class:     classWithStore,
			bootstrap: &enterprisev4.BootstrapFrom{ObjectStorage: &enterprisev4.ObjectStorageSource{ServerName: "src"}},
		},
		{
			name:      "objectStorage with type time accepted",
			class:     classWithStore,
			bootstrap: &enterprisev4.BootstrapFrom{ObjectStorage: &enterprisev4.ObjectStorageSource{ServerName: "src"}, RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetTime, Value: "2026-05-01T13:30:00Z"}},
		},
		{
			name:      "objectStorage with type lsn accepted",
			class:     classWithStore,
			bootstrap: &enterprisev4.BootstrapFrom{ObjectStorage: &enterprisev4.ObjectStorageSource{ServerName: "src"}, RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetLSN, Value: "0/16D68D0"}},
		},
		{
			name:      "volumeSnapshot with type xid accepted (format valid; capability checked via backend)",
			class:     classWithStore,
			bootstrap: &enterprisev4.BootstrapFrom{VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "s", WalArchive: &enterprisev4.ObjectStorageSource{ServerName: "src"}}, RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetXID, Value: "1234567"}},
		},
		{
			name:      "malformed type time value rejected",
			class:     classWithStore,
			bootstrap: &enterprisev4.BootstrapFrom{ObjectStorage: &enterprisev4.ObjectStorageSource{ServerName: "src"}, RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetTime, Value: "2026-05-01 13:30"}},
			wantField: "spec.bootstrapFrom.recoveryTarget.value",
		},
		{
			name:      "malformed type lsn value rejected",
			class:     classWithStore,
			bootstrap: &enterprisev4.BootstrapFrom{ObjectStorage: &enterprisev4.ObjectStorageSource{ServerName: "src"}, RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetLSN, Value: "not-an-lsn"}},
			wantField: "spec.bootstrapFrom.recoveryTarget.value",
		},
		{
			name:      "non-numeric type xid value rejected",
			class:     classWithStore,
			bootstrap: &enterprisev4.BootstrapFrom{VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "s", WalArchive: &enterprisev4.ObjectStorageSource{ServerName: "src"}}, RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetXID, Value: "12ab"}},
			wantField: "spec.bootstrapFrom.recoveryTarget.value",
		},
		{
			name:      "type name value with control character rejected",
			class:     classWithStore,
			bootstrap: &enterprisev4.BootstrapFrom{VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "s", WalArchive: &enterprisev4.ObjectStorageSource{ServerName: "src"}}, RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetName, Value: "bad\x00name"}},
			wantField: "spec.bootstrapFrom.recoveryTarget.value",
		},
		// Empty values are normally caught by the CRD CEL rule (self.value != ''); these assert the
		// value-format validators reject them too, so admission fails safe if that rule is weakened.
		{
			name:      "empty type time value rejected",
			class:     classWithStore,
			bootstrap: &enterprisev4.BootstrapFrom{ObjectStorage: &enterprisev4.ObjectStorageSource{ServerName: "src"}, RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetTime, Value: ""}},
			wantField: "spec.bootstrapFrom.recoveryTarget.value",
		},
		{
			name:      "empty type lsn value rejected",
			class:     classWithStore,
			bootstrap: &enterprisev4.BootstrapFrom{ObjectStorage: &enterprisev4.ObjectStorageSource{ServerName: "src"}, RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetLSN, Value: ""}},
			wantField: "spec.bootstrapFrom.recoveryTarget.value",
		},
		{
			name:      "empty type name value rejected",
			class:     classWithStore,
			bootstrap: &enterprisev4.BootstrapFrom{VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "s", WalArchive: &enterprisev4.ObjectStorageSource{ServerName: "src"}}, RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetName, Value: ""}},
			wantField: "spec.bootstrapFrom.recoveryTarget.value",
		},
		{
			// immediate carries no value and must be accepted format-wise (the empty value is expected).
			name:      "type immediate with no value accepted",
			class:     classWithStore,
			bootstrap: &enterprisev4.BootstrapFrom{VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "s", WalArchive: &enterprisev4.ObjectStorageSource{ServerName: "src"}}, RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetImmediate}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			errs := validateBootstrapFrom(tt.class, clusterWith(tt.bootstrap))
			if tt.wantField == "" {
				assert.Empty(t, errs, "expected no errors, got %v", errs)
				return
			}
			var found bool
			for _, e := range errs {
				if e.Field == tt.wantField {
					found = true
				}
			}
			assert.True(t, found, "expected error on %s, got %v", tt.wantField, errs)
		})
	}
}

func TestRestoreSourceStatus(t *testing.T) {
	t.Parallel()

	assert.Equal(t, enterprisev4.RestoreSourceStatus{}, restoreSourceStatus(nil))

	snap := restoreSourceStatus(&enterprisev4.BootstrapFrom{
		VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "snap-1"},
	})
	require.NotNil(t, snap.VolumeSnapshot)
	assert.Equal(t, "snap-1", *snap.VolumeSnapshot)
	assert.Nil(t, snap.ObjectStorage)
	assert.Nil(t, snap.RequestedRecoveryTarget)

	obj := restoreSourceStatus(&enterprisev4.BootstrapFrom{
		ObjectStorage:  &enterprisev4.ObjectStorageSource{ServerName: "src"},
		RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetTime, Value: "2026-05-01T13:30:00Z"},
	})
	require.NotNil(t, obj.ObjectStorage)
	assert.Equal(t, "src", *obj.ObjectStorage)
	require.NotNil(t, obj.RequestedRecoveryTarget)
	assert.Equal(t, enterprisev4.RecoveryTargetTime, obj.RequestedRecoveryTarget.Type)
	assert.Equal(t, "2026-05-01T13:30:00Z", obj.RequestedRecoveryTarget.Value)
	assert.Nil(t, obj.RequestedRecoveryTarget.Exclusive)

	// exclusive is echoed structurally so inclusive and exclusive restores to the same target are
	// distinguishable in status.
	excl := restoreSourceStatus(&enterprisev4.BootstrapFrom{
		ObjectStorage:  &enterprisev4.ObjectStorageSource{ServerName: "src"},
		RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetTime, Value: "2026-05-01T13:30:00Z", Exclusive: ptr.To(true)},
	})
	require.NotNil(t, excl.RequestedRecoveryTarget)
	require.NotNil(t, excl.RequestedRecoveryTarget.Exclusive)
	assert.True(t, *excl.RequestedRecoveryTarget.Exclusive)

	// exclusive:false is echoed as-is (false, not dropped) so status faithfully mirrors the spec.
	incl := restoreSourceStatus(&enterprisev4.BootstrapFrom{
		ObjectStorage:  &enterprisev4.ObjectStorageSource{ServerName: "src"},
		RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetLSN, Value: "0/16D68D0", Exclusive: ptr.To(false)},
	})
	require.NotNil(t, incl.RequestedRecoveryTarget)
	assert.Equal(t, enterprisev4.RecoveryTargetLSN, incl.RequestedRecoveryTarget.Type)
	assert.Equal(t, "0/16D68D0", incl.RequestedRecoveryTarget.Value)
	require.NotNil(t, incl.RequestedRecoveryTarget.Exclusive)
	assert.False(t, *incl.RequestedRecoveryTarget.Exclusive)
}

// fakeRecoveryBackend is a test double for the RecoveryBackend port. It records the plan it was
// asked to validate and returns a fixed set of violations, so core's plan derivation and error
// mapping can be tested without depending on any concrete provisioner adapter.
type fakeRecoveryBackend struct {
	gotPlan    recoverytypes.RecoveryPlan
	called     bool
	violations []recoverytypes.CapabilityViolation
}

func (f *fakeRecoveryBackend) ValidatePlan(plan recoverytypes.RecoveryPlan) []recoverytypes.CapabilityViolation {
	f.called = true
	f.gotPlan = plan
	return f.violations
}

func TestDeriveRecoveryPlan(t *testing.T) {
	t.Parallel()

	classWithStore := &enterprisev4.PostgresClusterClass{
		Spec: enterprisev4.PostgresClusterClassSpec{
			CNPG: &enterprisev4.CNPGConfig{
				Backup: &enterprisev4.CNPGBackupConfig{
					BarmanObjectStore: &enterprisev4.CNPGBarmanObjectStoreConfig{DestinationPath: "s3://b/p"},
				},
			},
		},
	}
	classNoStore := &enterprisev4.PostgresClusterClass{Spec: enterprisev4.PostgresClusterClassSpec{CNPG: &enterprisev4.CNPGConfig{}}}
	clusterWith := func(b *enterprisev4.BootstrapFrom) *enterprisev4.PostgresCluster {
		return &enterprisev4.PostgresCluster{Spec: enterprisev4.PostgresClusterSpec{BootstrapFrom: b}}
	}

	tests := []struct {
		name      string
		class     *enterprisev4.PostgresClusterClass
		bootstrap *enterprisev4.BootstrapFrom
		wantOK    bool
		wantPlan  recoverytypes.RecoveryPlan
	}{
		{name: "no bootstrapFrom => not ok", class: classWithStore, bootstrap: nil, wantOK: false},
		{
			name:      "both sources => not ok (structural error owns it)",
			class:     classWithStore,
			bootstrap: &enterprisev4.BootstrapFrom{VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "s"}, ObjectStorage: &enterprisev4.ObjectStorageSource{ServerName: "src"}},
			wantOK:    false,
		},
		{
			name:      "plain snapshot",
			class:     classNoStore,
			bootstrap: &enterprisev4.BootstrapFrom{VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "s"}},
			wantOK:    true,
			wantPlan:  recoverytypes.RecoveryPlan{Source: recoverytypes.SourceVolumeSnapshot, ClassProvidesObjectStore: false},
		},
		{
			name:      "snapshot + walArchive + target",
			class:     classWithStore,
			bootstrap: &enterprisev4.BootstrapFrom{VolumeSnapshot: &enterprisev4.VolumeSnapshotSource{Storage: "s", WalArchive: &enterprisev4.ObjectStorageSource{ServerName: "src"}}, RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetXID, Value: "1234567"}},
			wantOK:    true,
			wantPlan:  recoverytypes.RecoveryPlan{Source: recoverytypes.SourceVolumeSnapshotWithWAL, HasTarget: true, TargetKind: recoverytypes.TargetXID, ClassProvidesObjectStore: true},
		},
		{
			name:      "objectStorage, no target",
			class:     classWithStore,
			bootstrap: &enterprisev4.BootstrapFrom{ObjectStorage: &enterprisev4.ObjectStorageSource{ServerName: "src"}},
			wantOK:    true,
			wantPlan:  recoverytypes.RecoveryPlan{Source: recoverytypes.SourceObjectStorage, ClassProvidesObjectStore: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan, ok := deriveRecoveryPlan(tt.class, clusterWith(tt.bootstrap))
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantPlan, plan)
			}
		})
	}
}

// TestValidateRecoveryCapabilities asserts core derives the plan, delegates to the port, and maps
// violations back to ConfigValidationErrors — without knowing any provisioner-specific rule itself.
func TestValidateRecoveryCapabilities(t *testing.T) {
	t.Parallel()

	classWithStore := &enterprisev4.PostgresClusterClass{
		Spec: enterprisev4.PostgresClusterClassSpec{
			CNPG: &enterprisev4.CNPGConfig{
				Backup: &enterprisev4.CNPGBackupConfig{
					BarmanObjectStore: &enterprisev4.CNPGBarmanObjectStoreConfig{DestinationPath: "s3://b/p"},
				},
			},
		},
	}
	cluster := &enterprisev4.PostgresCluster{Spec: enterprisev4.PostgresClusterSpec{
		BootstrapFrom: &enterprisev4.BootstrapFrom{
			ObjectStorage:  &enterprisev4.ObjectStorageSource{ServerName: "src"},
			RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetXID, Value: "1234567"},
		},
	}}

	t.Run("nil backend is a no-op", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, ValidateRecoveryCapabilities(nil, classWithStore, cluster))
	})

	t.Run("no bootstrapFrom does not call the backend", func(t *testing.T) {
		t.Parallel()
		backend := &fakeRecoveryBackend{}
		errs := ValidateRecoveryCapabilities(backend, classWithStore, &enterprisev4.PostgresCluster{})
		assert.Empty(t, errs)
		assert.False(t, backend.called, "backend must not be consulted without a restore request")
	})

	t.Run("passes derived plan and maps violations", func(t *testing.T) {
		t.Parallel()
		backend := &fakeRecoveryBackend{violations: []recoverytypes.CapabilityViolation{
			{Field: "spec.bootstrapFrom.recoveryTarget", Message: "nope"},
		}}
		errs := ValidateRecoveryCapabilities(backend, classWithStore, cluster)
		require.True(t, backend.called)
		assert.Equal(t, recoverytypes.SourceObjectStorage, backend.gotPlan.Source)
		assert.True(t, backend.gotPlan.HasTarget)
		assert.Equal(t, recoverytypes.TargetXID, backend.gotPlan.TargetKind)
		assert.True(t, backend.gotPlan.ClassProvidesObjectStore)
		require.Len(t, errs, 1)
		assert.Equal(t, "spec.bootstrapFrom.recoveryTarget", errs[0].Field)
		assert.Equal(t, "nope", errs[0].Message)
	})

	t.Run("no violations yields no errors", func(t *testing.T) {
		t.Parallel()
		backend := &fakeRecoveryBackend{}
		assert.Empty(t, ValidateRecoveryCapabilities(backend, classWithStore, cluster))
	})
}

// TestNormalizeRecoveryDriftDetection asserts the operator-owned recovery wiring participates in
// drift detection: a live CNPG spec with the "origin" externalCluster removed (or the recovery
// source/target cleared) is seen as drift against the rebuilt desired spec, so an out-of-band edit
// before bootstrap completes is healed rather than silently accepted.
func TestNormalizeRecoveryDriftDetection(t *testing.T) {
	t.Parallel()

	cfg := recoveryTestCfg(&enterprisev4.BootstrapFrom{
		ObjectStorage:  &enterprisev4.ObjectStorageSource{ServerName: "src"},
		RecoveryTarget: &enterprisev4.RecoveryTarget{Type: enterprisev4.RecoveryTargetTime, Value: "2026-05-01T13:30:00Z", Exclusive: ptr.To(true)},
	}, true)
	storage := resource.MustParse("1Gi")
	cfg.Spec.Storage = &storage

	desired := buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, "c1", "my-secret", false)
	desiredNorm := normalizeCNPGClusterSpec(desired)
	require.NotNil(t, desiredNorm.Recovery, "recovery wiring must be captured for drift detection")
	require.NotNil(t, desiredNorm.Recovery.ExternalCluster)
	require.NotNil(t, desiredNorm.Recovery.Target)
	assert.Equal(t, recoveryExternalClusterName, desiredNorm.Recovery.Source)

	// No drift against itself.
	assert.False(t, isClusterDrift(desiredNorm, normalizeCNPGClusterSpec(desired)))

	// Origin externalCluster deleted out-of-band => drift.
	tampered := *desired.DeepCopy()
	tampered.ExternalClusters = nil
	assert.True(t, isClusterDrift(desiredNorm, normalizeCNPGClusterSpec(tampered)), "removing the origin externalCluster must be detected as drift")

	// Recovery source cleared out-of-band => drift.
	tampered2 := *desired.DeepCopy()
	tampered2.Bootstrap.Recovery.Source = ""
	assert.True(t, isClusterDrift(desiredNorm, normalizeCNPGClusterSpec(tampered2)), "clearing recovery.source must be detected as drift")

	// Recovery target's exclusive flipped out-of-band => drift.
	tampered3 := *desired.DeepCopy()
	tampered3.Bootstrap.Recovery.RecoveryTarget.Exclusive = ptr.To(false)
	assert.True(t, isClusterDrift(desiredNorm, normalizeCNPGClusterSpec(tampered3)), "changing the recovery target must be detected as drift")
}
