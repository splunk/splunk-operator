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
	"errors"
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
)

func (s *postgresSwitchoverSQL) PrepareControl(ctx context.Context, cluster *cnpgv1.Cluster, databases []declaredDatabase, id cutoverIdentity) error {
	if id.Role == "" || id.Password == "" || id.AttemptID == "" || s == nil || s.client == nil || s.bootstrap == nil {
		return fmt.Errorf("%w: incomplete cutover control identity", mvutypes.ErrUpgradeFlowFailed)
	}
	sequenceWrites, err := s.sequenceWrites(cluster, id)
	if err != nil {
		return err
	}
	v := newPostgresCandidateVerifier(s.client, s.logicalName, s.owner)
	v.connect = s.bootstrap
	host, err := pgconninfo.ServiceFQDN(cluster.GetServiceReadWriteName(), cluster.Namespace)
	if err != nil {
		return err
	}
	conn, err := v.connectCluster(ctx, cluster, host, "postgres")
	if err != nil {
		return err
	}
	if err := withCutoverConn(ctx, conn, func(conn pginfra.Conn) error { return prepareControlRole(ctx, conn, id) }); err != nil {
		return err
	}
	names := []string{"postgres"}
	for _, db := range databases {
		names = append(names, db.name)
	}
	for _, name := range names {
		conn, err := v.connectCluster(ctx, cluster, host, name)
		if err != nil {
			return err
		}
		if err := withCutoverConn(ctx, conn, func(conn pginfra.Conn) error { return s.installControlDatabase(ctx, conn, name, id, sequenceWrites) }); err != nil {
			return fmt.Errorf("preparing cutover SQL in %q: %w", name, err)
		}
	}
	return nil
}

func prepareControlRole(ctx context.Context, conn pginfra.Conn, id cutoverIdentity) error {
	return withSCRAMPasswordEncoding(ctx, conn, func() error {
		quotedRole := pginfra.QuoteIdentifier(id.Role)
		passwordLiteral := quoteSQLStringLiteral(id.Password)
		if _, err := conn.Exec(ctx, "CREATE ROLE "+quotedRole+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD "+passwordLiteral); err != nil && !isDuplicateRole(err) {
			return fmt.Errorf("creating cutover role: %w", err)
		}
		// Existing identities must be proven safe before refreshing credentials.
		var safe bool
		if err := queryOne(ctx, conn, &safe, controlRoleSQL, id.Role); err != nil || !safe {
			return fmt.Errorf("%w: cutover role has unexpected capabilities: %v", mvutypes.ErrUpgradeFlowFailed, err)
		}
		for _, statement := range []string{"ALTER ROLE " + quotedRole + " PASSWORD " + passwordLiteral, "GRANT pg_read_all_stats TO " + quotedRole} {
			if _, err := conn.Exec(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *postgresSwitchoverSQL) installControlDatabase(ctx context.Context, conn pginfra.Conn, database string, id cutoverIdentity, writeSequences bool) error {
	schema := pginfra.QuoteIdentifier(candidateGuardSchema)
	role := pginfra.QuoteIdentifier(id.Role)
	if _, err := conn.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+schema+" AUTHORIZATION postgres"); err != nil {
		return err
	}
	// Do not overwrite workflow SQL on retry. A partial first bootstrap may
	// leave a subset of helpers, but every existing helper must match exactly.
	for _, helper := range cutoverHelpers(id) {
		var exists bool
		if err := queryOne(ctx, conn, &exists, controlGuardFunctionExistsSQL, candidateGuardSchema+"."+helper.name+"()"); err != nil {
			return err
		}
		if exists {
			if err := verifyControlHelper(ctx, conn, helper, id); err != nil {
				return err
			}
			continue
		}
		if err := installCutoverHelper(ctx, conn, helper, id); err != nil {
			return err
		}
	}
	for _, sql := range []string{"REVOKE CREATE ON SCHEMA " + schema + " FROM PUBLIC", "GRANT USAGE ON SCHEMA " + schema + " TO " + role, "GRANT CONNECT ON DATABASE " + pginfra.QuoteIdentifier(database) + " TO " + role,
		"GRANT SELECT (oid,subname,subenabled,subfailover,subslotname,subdbid) ON pg_catalog.pg_subscription TO " + role} {
		if _, err := conn.Exec(ctx, sql); err != nil {
			return err
		}
	}
	// Data reads are limited to unsupported unlogged-table detection and
	// sequence state; ordinary application tables need no control privilege.
	rows, err := conn.Query(ctx, controlObjectInventorySQL)
	if err != nil {
		return err
	}
	type grantObject struct {
		schema, name string
		sequence     bool
	}
	var objects []grantObject
	for rows.Next() {
		var o grantObject
		if err := rows.Scan(&o.schema, &o.name, &o.sequence); err != nil {
			rows.Close()
			return err
		}
		objects = append(objects, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, o := range objects {
		if _, err := conn.Exec(ctx, "GRANT USAGE ON SCHEMA "+pginfra.QuoteIdentifier(o.schema)+" TO "+role); err != nil {
			return err
		}
		privilege := "SELECT ON TABLE "
		if o.sequence {
			privilege = "SELECT ON SEQUENCE "
			if writeSequences {
				privilege = "SELECT, UPDATE ON SEQUENCE "
			}
		}
		if _, err := conn.Exec(ctx, "GRANT "+privilege+pginfra.QuoteIdentifier(o.schema)+"."+pginfra.QuoteIdentifier(o.name)+" TO "+role); err != nil {
			return err
		}
	}
	if err := verifyControlDatabase(ctx, conn, id); err != nil {
		return err
	}
	return verifyControlSequenceGrants(ctx, conn, id, writeSequences)
}

func installCutoverHelper(ctx context.Context, conn pginfra.Conn, helper cutoverHelper, id cutoverIdentity) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	name := pginfra.QuoteIdentifier(candidateGuardSchema) + "." + pginfra.QuoteIdentifier(helper.name) + "()"
	statements := []string{
		"CREATE FUNCTION " + name + " RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS " + quoteSQLStringLiteral(helper.body),
		"REVOKE ALL ON FUNCTION " + name + " FROM PUBLIC",
		"GRANT EXECUTE ON FUNCTION " + name + " TO " + pginfra.QuoteIdentifier(id.Role),
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return errors.Join(err, tx.Rollback(ctx))
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return errors.Join(err, tx.Rollback(ctx))
	}
	return nil
}

func (s *postgresSwitchoverSQL) sequenceWrites(cluster *cnpgv1.Cluster, id cutoverIdentity) (bool, error) {
	if s.owner != nil && cluster != nil {
		for _, entry := range s.owner.Status.PostgresMajorUpgradeStatus {
			bg := entry.BlueGreen
			if bg == nil || bg.AttemptID != id.AttemptID {
				continue
			}
			for _, environment := range []struct {
				status *platformv1alpha1.BlueGreenEnvironmentStatus
				writes bool
			}{{bg.Blue, false}, {bg.Green, true}} {
				if environment.status != nil && environment.status.Ref.Name == cluster.Name &&
					(environment.status.Ref.Namespace == "" || environment.status.Ref.Namespace == cluster.Namespace) &&
					(environment.status.Ref.UID == "" || environment.status.Ref.UID == cluster.UID) {
					return environment.writes, nil
				}
			}
		}
	}
	return false, fmt.Errorf("%w: cutover sequence privileges have no durable environment identity", mvutypes.ErrUpgradeFlowFailed)
}

func verifyControlSequenceGrants(ctx context.Context, conn pginfra.Conn, id cutoverIdentity, writes bool) error {
	var valid bool
	err := queryOne(ctx, conn, &valid, controlSequenceGrantsSQL, id.Role, writes)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("%w: cutover sequence grants differ from the environment's required scope", mvutypes.ErrUpgradeFlowFailed)
	}
	return nil
}

func verifyControlHelper(ctx context.Context, conn pginfra.Conn, helper cutoverHelper, id cutoverIdentity) error {
	var valid bool
	err := queryCutoverBoolean(ctx, conn, &valid, controlHelperProofSQL, candidateGuardSchema, helper.name, helper.body, id.Role)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("%w: cutover helper %q ownership, body, or privileges changed", mvutypes.ErrUpgradeFlowFailed, helper.name)
	}
	return nil
}

func verifyControlDatabase(ctx context.Context, conn pginfra.Conn, id cutoverIdentity) error {
	var safe bool
	if err := queryCutoverBoolean(ctx, conn, &safe, controlRoleSQL, id.Role); err != nil {
		return err
	}
	if !safe {
		return fmt.Errorf("%w: cutover role has unexpected capabilities", mvutypes.ErrUpgradeFlowFailed)
	}
	if err := queryCutoverBoolean(ctx, conn, &safe, controlMembershipProofSQL, id.Role); err != nil {
		return err
	}
	if !safe {
		return fmt.Errorf("%w: cutover role membership changed", mvutypes.ErrUpgradeFlowFailed)
	}
	for _, helper := range cutoverHelpers(id) {
		if err := verifyControlHelper(ctx, conn, helper, id); err != nil {
			return err
		}
	}
	if err := queryCutoverBoolean(ctx, conn, &safe, controlSchemaProofSQL, candidateGuardSchema, id.Role, []string{controlTerminateFunction, controlRemoveGuardsFunction, controlFenceFunction, candidateGuardFunction}); err != nil {
		return err
	}
	if !safe {
		return fmt.Errorf("%w: reserved cutover schema ownership or inventory changed", mvutypes.ErrUpgradeFlowFailed)
	}
	if err := queryCutoverBoolean(ctx, conn, &safe, controlGrantInventorySQL, id.Role, candidateGuardSchema); err != nil {
		return err
	}
	if !safe {
		return fmt.Errorf("%w: cutover control grants exceed their owned overlay", mvutypes.ErrUpgradeFlowFailed)
	}
	return nil
}
