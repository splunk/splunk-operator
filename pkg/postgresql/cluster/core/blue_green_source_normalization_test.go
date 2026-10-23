/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package core

import (
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
)

func TestNormalizeBlueGreenSourceCNPGSpecUsesCNPGDefaults(t *testing.T) {
	normalized := NormalizeBlueGreenSourceCNPGSpec(cnpgv1.ClusterSpec{})
	assert.False(t, normalized.EnableSuperuserAccess)
	require.NotNil(t, normalized.ReplicationSlots.HighAvailability)
	assert.True(t, ptr.Deref(normalized.ReplicationSlots.HighAvailability.Enabled, false))
	assert.Equal(t, cnpgv1.DefaultReplicationSlotsHASlotPrefix, normalized.ReplicationSlots.HighAvailability.SlotPrefix)
	assert.Equal(t, cnpgv1.DefaultReplicationSlotsUpdateInterval, normalized.ReplicationSlots.UpdateInterval)
}

func TestNormalizeBlueGreenSourceCNPGSpecCapturesAndCopiesLiveSettings(t *testing.T) {
	storageClass := "fast"
	spec := cnpgv1.ClusterSpec{
		EnableSuperuserAccess: new(true),
		SuperuserSecret:       &cnpgv1.LocalObjectReference{Name: "source-superuser"},
		StorageConfiguration:  cnpgv1.StorageConfiguration{StorageClass: &storageClass},
		ReplicationSlots: &cnpgv1.ReplicationSlotsConfiguration{
			HighAvailability:    &cnpgv1.ReplicationSlotsHAConfiguration{SlotPrefix: "custom"},
			SynchronizeReplicas: &cnpgv1.SynchronizeReplicasConfiguration{},
		},
	}
	spec.PostgresConfiguration.Parameters = map[string]string{"wal_level": "logical"}
	normalized := NormalizeBlueGreenSourceCNPGSpec(spec)
	assert.True(t, normalized.EnableSuperuserAccess)
	assert.Equal(t, "source-superuser", normalized.SuperuserSecretName)
	assert.Equal(t, storageClass, normalized.StorageClass)
	assert.Equal(t, "logical", normalized.PostgreSQLParameters["wal_level"])
	assert.Equal(t, "custom", normalized.ReplicationSlots.HighAvailability.SlotPrefix)
	require.NotNil(t, normalized.ReplicationSlots.SynchronizeReplicas)
	assert.True(t, ptr.Deref(normalized.ReplicationSlots.SynchronizeReplicas.Enabled, false))

	spec.PostgresConfiguration.Parameters["wal_level"] = "replica"
	spec.ReplicationSlots.HighAvailability.SlotPrefix = "changed"
	assert.Equal(t, "logical", normalized.PostgreSQLParameters["wal_level"])
	assert.Equal(t, "custom", normalized.ReplicationSlots.HighAvailability.SlotPrefix)
}
