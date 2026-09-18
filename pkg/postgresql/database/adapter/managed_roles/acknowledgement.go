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

package managedroles

// This file receives the cluster-owned managed-role acknowledgement from
// PostgresCluster status and translates it for the PostgresDatabase gate.

import (
	"context"
	"fmt"

	dbk8s "github.com/splunk/splunk-operator/pkg/postgresql/database/infrastructure/k8s"
	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AcknowledgementReader reads cluster-owned managed-role acknowledgement.
type AcknowledgementReader struct {
	reader dbk8s.ClusterReader
}

// NewAcknowledgementReader returns a Kubernetes-backed acknowledgement
// reader.
func NewAcknowledgementReader(reader client.Reader) *AcknowledgementReader {
	return &AcknowledgementReader{reader: dbk8s.NewClusterReader(reader)}
}

func (r *AcknowledgementReader) Read(
	ctx context.Context,
	target dbtypes.ManagedRoleAcknowledgementTarget,
) (dbtypes.ManagedRoleAcknowledgement, error) {
	snapshot, err := r.reader.Read(ctx, target.Namespace, target.ClusterName)
	if err != nil {
		return dbtypes.ManagedRoleAcknowledgement{}, fmt.Errorf(
			"%w: reading PostgresCluster %s/%s: %w",
			dbtypes.ErrManagedRoleAcknowledgementRead, target.Namespace, target.ClusterName, err,
		)
	}
	if snapshot.ManagedRolesStatus == nil {
		return dbtypes.ManagedRoleAcknowledgement{}, nil
	}

	status := snapshot.ManagedRolesStatus
	acknowledgement := dbtypes.ManagedRoleAcknowledgement{
		Published:  true,
		Reconciled: append([]string(nil), status.Reconciled...),
		Failed:     make(map[string]string, len(status.Failed)),
		Owners:     make(map[string]dbtypes.ManagedRoleParticipant, len(status.RoleOwners)),
		Conflicts:  make([]dbtypes.ManagedRoleConflict, 0, len(status.Conflicts)),
	}
	for role, reason := range status.Failed {
		acknowledgement.Failed[role] = reason
	}
	for role, owner := range status.RoleOwners {
		acknowledgement.Owners[role] = dbtypes.ManagedRoleParticipant{Name: owner.Name, UID: owner.UID}
	}
	for _, conflict := range status.Conflicts {
		acknowledgement.Conflicts = append(acknowledgement.Conflicts, dbtypes.ManagedRoleConflict{
			Role: conflict.Role,
			AttemptedBy: dbtypes.ManagedRoleParticipant{
				Name: conflict.AttemptedBy.Name,
				UID:  conflict.AttemptedBy.UID,
			},
		})
	}
	return acknowledgement, nil
}
