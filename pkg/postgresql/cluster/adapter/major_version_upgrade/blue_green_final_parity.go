/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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
	"context"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
)

func (s *postgresSwitchoverSQL) SourceLSN(ctx context.Context, source *cnpgv1.Cluster, id cutoverIdentity) (string, error) {
	var lsn string
	err := s.withConn(ctx, source, "", "postgres", id, func(conn pginfra.Conn) error {
		// Include asynchronously acknowledged commits whose WAL is still buffered.
		return queryOne(ctx, conn, &lsn, cutoverSourceFinalWALPositionSQL)
	})
	if err != nil {
		return "", err
	}
	return lsn, nil
}

func (s *postgresSwitchoverSQL) verifier(id cutoverIdentity) *postgresCandidateVerifier {
	v := newPostgresCandidateVerifier(s.client, s.logicalName, s.owner)
	v.connectEnvironment = func(ctx context.Context, cluster *cnpgv1.Cluster, host, database string) (pginfra.Conn, error) {
		return s.connectCluster(ctx, cluster, host, database, id)
	}
	return v
}

func (s *postgresSwitchoverSQL) VerifyFinalParity(ctx context.Context, source, target *cnpgv1.Cluster, databases []declaredDatabase, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, id cutoverIdentity) (bool, error) {
	if status == nil || status.Switchover == nil || status.Switchover.FinalBlueLSN == nil || *status.Switchover.FinalBlueLSN == "" {
		return false, fmt.Errorf("%w: final blue LSN is missing", mvutypes.ErrUpgradeFlowFailed)
	}
	if !sourceReadyForFailover(source) || !sourceReadyForFailover(target) {
		return false, nil
	}
	if len(databases) != len(status.DatabaseReplication) {
		return false, fmt.Errorf("%w: final parity database roster changed", mvutypes.ErrUpgradeFlowFailed)
	}
	v := s.verifier(id)
	instances, pods, err := v.healthySourcePods(ctx, source)
	if err != nil {
		return false, err
	}
	for _, environment := range []struct {
		cluster *cnpgv1.Cluster
		side    environmentSide
	}{{source, sideBlue}, {target, sideGreen}} {
		fenced, err := s.VerifyFence(ctx, environment.cluster, id, roleNames(accessOriginalRoles(status, environment.side)))
		if err != nil || !fenced {
			return false, err
		}
		if err := s.verifyFrozenCatalogs(ctx, environment.cluster, databases, status, id, environment.side); err != nil {
			return false, err
		}
	}

	ready := true
	for i := range status.DatabaseReplication {
		r := &status.DatabaseReplication[i]
		subscription, copyComplete, err := v.subscriptionState(ctx, target, r.DatabaseName, *status.Switchover.FinalBlueLSN)
		if err != nil {
			return false, err
		}
		slots, slotReady, err := v.slotState(ctx, source, r.DatabaseName, *status.Switchover.FinalBlueLSN, instances, pods)
		if err != nil {
			return false, err
		}
		r.SubscriptionHealthy = new(subscription)
		r.InitialCopyComplete = new(copyComplete)
		r.SlotHealthy = new(slotReady)
		r.ReplicaSlots = slots
		ready = ready && subscription && copyComplete && slotReady
	}
	inventory, err := v.verifySourceSlotInventory(ctx, source, databases)
	if err != nil || !ready || !inventory {
		return false, err
	}
	if receipt := status.Switchover.SequenceSynchronization; receipt != nil && receipt.VerifiedAt != nil {
		return s.VerifySequenceParity(ctx, source, target, databases, id)
	}
	return true, nil
}

func (s *postgresSwitchoverSQL) verifyFrozenCatalogs(ctx context.Context, cluster *cnpgv1.Cluster, databases []declaredDatabase, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, id cutoverIdentity, side environmentSide) error {
	if err := s.withConn(ctx, cluster, "", "postgres", id, func(conn pginfra.Conn) error {
		return s.verifyMaintenanceCatalog(ctx, conn, databases, id, side)
	}); err != nil {
		return err
	}
	loginJSON, err := json.Marshal(append([]platformv1alpha1.BlueGreenRoleLoginStatus{}, accessOriginalRoles(status, side)...))
	if err != nil {
		return err
	}
	for _, db := range databases {
		index := slices.IndexFunc(status.DatabaseReplication, func(r platformv1alpha1.BlueGreenDatabaseReplicationStatus) bool { return r.DatabaseName == db.name })
		if index < 0 {
			return fmt.Errorf("%w: database has no frozen receipt", mvutypes.ErrUpgradeFlowFailed)
		}
		receipt := status.DatabaseReplication[index]
		schemaExpected, accessExpected := receipt.SourceSchemaFingerprint, receipt.SourceAccessPolicyFingerprint
		if side == sideGreen {
			schemaExpected, accessExpected = receipt.TargetSchemaFingerprint, receipt.TargetAccessPolicyFingerprint
		}
		if schemaExpected == nil || accessExpected == nil {
			return fmt.Errorf("%w: incomplete frozen catalog receipt", mvutypes.ErrUpgradeFlowFailed)
		}
		var schema, access string
		if err := s.withConn(ctx, cluster, "", db.name, id, func(conn pginfra.Conn) error {
			if err := verifyControlDatabase(ctx, conn, id); err != nil {
				return err
			}
			if err := verifyControlSequenceGrants(ctx, conn, id, side == sideGreen); err != nil {
				return err
			}
			if err := verifyCutoverGuardState(ctx, conn, status.Switchover, side); err != nil {
				return err
			}
			if err := s.verifySupportedObjects(ctx, conn, cluster, db, id, side); err != nil {
				return err
			}
			if err := queryOne(ctx, conn, &schema, schemaFingerprintSQL, candidateGuardTrigger, candidateGuardSchema, candidateGuardFunction); err != nil {
				return err
			}
			return queryOne(ctx, conn, &access, cutoverAccessFingerprintSQL(side), replicationRoleName(id.AttemptID), id.Role, string(loginJSON))
		}); err != nil {
			return err
		}
		if schema != *schemaExpected || access != *accessExpected {
			return fmt.Errorf("%w: frozen catalog for database %q changed during cutover", mvutypes.ErrUpgradeFlowFailed, db.name)
		}
	}
	return nil
}

func (s *postgresSwitchoverSQL) verifyMaintenanceCatalog(ctx context.Context, conn pginfra.Conn, databases []declaredDatabase, id cutoverIdentity, side environmentSide) error {
	roles, err := queryStrings(ctx, conn, sourceRolesSQL)
	if err != nil {
		return err
	}
	allowed := declaredRoleNames(databases, id.AttemptID)
	allowed[id.Role] = struct{}{}
	if err := validateSourceRoles(roles, allowed); err != nil {
		return err
	}
	actual, err := queryStrings(ctx, conn, sourceDatabasesSQL)
	if err != nil {
		return err
	}
	expected := make([]string, 0, len(databases))
	for _, db := range databases {
		expected = append(expected, db.name)
	}
	slices.Sort(expected)
	if !slices.Equal(expected, actual) {
		return fmt.Errorf("%w: database inventory changed during cutover", mvutypes.ErrUpgradeFlowFailed)
	}
	if err := verifyCutoverSubscriptionRoster(ctx, conn, databases, s.logicalName, side); err != nil {
		return err
	}
	unsupported, err := queryStrings(ctx, conn, cutoverUnsupportedMaintenanceObjectsSQL)
	if err != nil {
		return err
	}
	if len(unsupported) > 0 {
		return fmt.Errorf("%w: unsupported cluster inventory changed: %s", mvutypes.ErrUpgradeFlowFailed, strings.Join(unsupported, ","))
	}
	return nil
}

func verifyCutoverSubscriptionRoster(ctx context.Context, conn pginfra.Conn, databases []declaredDatabase, logicalName string, side environmentSide) error {
	actual, err := queryStrings(ctx, conn, cutoverSubscriptionRosterSQL)
	if err != nil {
		return err
	}
	var expected []string
	if side == sideGreen {
		for _, db := range databases {
			expected = append(expected, db.name+"/"+subscriptionName(logicalName, db.name))
		}
		slices.Sort(expected)
	}
	if !slices.Equal(actual, expected) {
		return fmt.Errorf("%w: cluster subscription roster changed during cutover", mvutypes.ErrUpgradeFlowFailed)
	}
	return nil
}

func (s *postgresSwitchoverSQL) verifySupportedObjects(ctx context.Context, conn pginfra.Conn, cluster *cnpgv1.Cluster, db declaredDatabase, id cutoverIdentity, side environmentSide) error {
	var unsupported bool
	if err := queryOne(ctx, conn, &unsupported, unsupportedDatabaseObjectsSQL); err != nil {
		return err
	}
	if unsupported {
		return fmt.Errorf("%w: unsupported database objects appeared during cutover", mvutypes.ErrUpgradeFlowFailed)
	}
	if err := checkUnloggedTables(ctx, conn, db.name); err != nil {
		return err
	}
	if err := checkDatabaseExtensions(ctx, conn, db); err != nil {
		return err
	}
	foreign, err := queryStrings(ctx, conn, foreignObjectsSQL)
	if err != nil {
		return err
	}
	if len(foreign) > 0 {
		return fmt.Errorf("%w: foreign objects appeared during cutover", mvutypes.ErrUpgradeFlowFailed)
	}
	if side == sideBlue {
		p := newPostgresSourcePreflight(s.client, s.logicalName, s.owner)
		name, err := p.attemptPublication(ctx, cluster, db.name, id.AttemptID)
		if err != nil {
			return err
		}
		var valid bool
		err = queryOne(ctx, conn, &valid, cutoverPublicationInventorySQL, name)
		if err != nil {
			return err
		}
		if !valid {
			return fmt.Errorf("%w: publication inventory changed during cutover", mvutypes.ErrUpgradeFlowFailed)
		}
	} else {
		var valid bool
		err = queryOne(ctx, conn, &valid, cutoverSubscriptionInventorySQL, subscriptionName(s.logicalName, db.name), replicationSlotName(s.logicalName, db.name))
		if err != nil {
			return err
		}
		if !valid {
			return fmt.Errorf("%w: subscription inventory changed during cutover", mvutypes.ErrUpgradeFlowFailed)
		}
		// CNPG's schema-only pg_dump/pg_restore includes the source's managed
		// publication. Only that exact mirrored contract is supported on green;
		// the source path above independently proves its attempt-owned CR.
		if err := queryOne(ctx, conn, &valid, cutoverPublicationInventorySQL, publicationName(s.logicalName, db.name)); err != nil {
			return err
		}
		if !valid {
			return fmt.Errorf("%w: unsupported green publication appeared during cutover", mvutypes.ErrUpgradeFlowFailed)
		}
	}
	return nil
}
