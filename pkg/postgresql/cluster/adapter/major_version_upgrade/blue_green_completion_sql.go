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
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const controlRetireReplicationFunction = "cutover_retire_replication"

// retireReplicationHelper defines no-argument, attempt-only source cleanup.
func retireReplicationHelper(id cutoverIdentity, database string) cutoverHelper {
	publication := quoteSQLStringLiteral(publicationName(id.LogicalName, database))
	slot := quoteSQLStringLiteral(replicationSlotName(id.LogicalName, database))
	role := quoteSQLStringLiteral(replicationRoleName(id.AttemptID))
	return cutoverHelper{controlRetireReplicationFunction, `DECLARE pub text := ` + publication + `; slot text := ` + slot + `; consumer integer; BEGIN
IF session_user <> ` + quoteSQLStringLiteral(id.Role) + ` THEN RAISE EXCEPTION 'invalid cutover identity'; END IF;
IF EXISTS (SELECT 1 FROM pg_catalog.pg_replication_slots WHERE database=pg_catalog.current_database() AND slot_name ~ '^pg_[0-9]+_sync_[0-9]+_[0-9]+$') THEN RETURN false; END IF;
IF pg_catalog.pg_is_in_recovery() THEN RETURN NOT EXISTS (SELECT 1 FROM pg_catalog.pg_replication_slots WHERE slot_name=slot); END IF;
IF EXISTS (SELECT 1 FROM pg_catalog.pg_publication WHERE pubname=pub AND (pubowner<>(SELECT oid FROM pg_catalog.pg_roles WHERE rolname='postgres') OR NOT puballtables)) THEN RAISE EXCEPTION 'publication ownership changed'; END IF;
IF EXISTS (SELECT 1 FROM pg_catalog.pg_replication_slots WHERE slot_name=slot AND (slot_type<>'logical' OR plugin<>'pgoutput' OR database<>pg_catalog.current_database())) THEN RAISE EXCEPTION 'replication slot identity changed'; END IF;
IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname=` + role + `) THEN EXECUTE pg_catalog.format('ALTER ROLE %I NOLOGIN PASSWORD NULL',` + role + `); END IF;
FOR consumer IN SELECT pid FROM pg_catalog.pg_stat_activity WHERE usename=` + role + ` LOOP PERFORM pg_catalog.pg_terminate_backend(consumer); END LOOP;
IF EXISTS (SELECT 1 FROM pg_catalog.pg_replication_slots WHERE slot_name=slot AND active) THEN RETURN false; END IF;
EXECUTE pg_catalog.format('DROP PUBLICATION IF EXISTS %I',pub);
IF EXISTS (SELECT 1 FROM pg_catalog.pg_replication_slots WHERE slot_name=slot) THEN PERFORM pg_catalog.pg_drop_replication_slot(slot); END IF;
RETURN true; END;`}
}

type postgresCompletionSQL struct {
	switchover *postgresSwitchoverSQL
	access     cutoverAccess
	teardown   *postgresTeardownSQL
}

// SourceStopped verifies every surviving source restarted with archiving disabled.
func (p *postgresCompletionSQL) SourceStopped(ctx context.Context, blue *cnpgv1.Cluster, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	return p.observeSource(ctx, blue, status, []string{"postgres"}, status.Completion.StartedAt, func(conn pginfra.Conn, _ cutoverIdentity, _ string) (bool, error) {
		var stopped bool
		err := queryOne(ctx, conn, &stopped, `SELECT pg_catalog.pg_postmaster_start_time() >= $1 AND current_setting('archive_mode')='off'`, status.Completion.StartedAt.Time)
		return stopped, err
	})
}

var archivedWALPattern = regexp.MustCompile(`^[0-9A-F]{24}$`)

// VerifyRecovery checks target identity and archived WAL through the backup's end.
func (p *postgresCompletionSQL) VerifyRecovery(ctx context.Context, green *cnpgv1.Cluster, backup *cnpgv1.Backup, target string) (bool, error) {
	if !archivedWALPattern.MatchString(backup.Status.EndWal) || backup.Status.StartedAt == nil || backup.Status.StoppedAt == nil || green.Status.SystemID == "" {
		return false, fmt.Errorf("target backup does not yet expose its ending WAL and system identity")
	}
	major := mvutypes.PostgresMajorVersion(target)
	if major <= 0 {
		return false, fmt.Errorf("invalid PostgreSQL target version %q", target)
	}
	var verified bool
	err := p.teardown.withConn(ctx, green, "", "postgres", func(conn pginfra.Conn) error {
		return queryOne(ctx, conn, &verified, `SELECT current_setting('server_version_num')::integer/10000=$1
AND (SELECT system_identifier::text FROM pg_catalog.pg_control_system())=$2
AND current_setting('archive_mode') IN ('on','always') AND current_setting('archive_command') NOT IN ('','/bin/true','false')
AND EXISTS (SELECT 1 FROM pg_catalog.pg_stat_archiver WHERE last_archived_wal >= $3
 AND last_archived_time >= $4 AND (last_failed_time IS NULL OR last_failed_time <= last_archived_time))`, major, green.Status.SystemID, backup.Status.EndWal, backup.Status.StartedAt.Time)
	})
	return verified, err
}

// RemoveGreenReplication detaches and drops green subscriptions and publications.
func (p *postgresCompletionSQL) RemoveGreenReplication(ctx context.Context, green *cnpgv1.Cluster, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	for _, db := range status.DatabaseReplication {
		if err := p.teardown.withConn(ctx, green, "", db.DatabaseName, func(conn pginfra.Conn) error {
			name := subscriptionName(p.switchover.logicalName, db.DatabaseName)
			var exists bool
			if err := queryOne(ctx, conn, &exists, `SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_subscription WHERE subname=$1 AND subdbid=(SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database()))`, name); err != nil {
				return err
			}
			if !exists {
				return nil
			}
			for _, statement := range []string{
				"ALTER SUBSCRIPTION " + pginfra.QuoteIdentifier(name) + " DISABLE",
				"ALTER SUBSCRIPTION " + pginfra.QuoteIdentifier(name) + " SET (slot_name = NONE)",
				"DROP SUBSCRIPTION " + pginfra.QuoteIdentifier(name),
			} {
				if _, err := conn.Exec(ctx, statement); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return p.teardown.RemovePublications(ctx, green, p.switchover.logicalName, (&BlueGreenReplicationTeardown{}).databaseNames(status))
}

// RemoveSourceReplication runs restricted cleanup and verifies standby slot removal.
func (p *postgresCompletionSQL) RemoveSourceReplication(ctx context.Context, blue *cnpgv1.Cluster, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	return p.observeSource(ctx, blue, status, (&BlueGreenReplicationTeardown{}).databaseNames(status), nil, func(conn pginfra.Conn, id cutoverIdentity, database string) (bool, error) {
		if err := verifyControlHelper(ctx, conn, retireReplicationHelper(id, database), id); err != nil {
			return false, err
		}
		var removed bool
		err := queryOne(ctx, conn, &removed, controlHelperCallSQL(controlRetireReplicationFunction))
		return removed, err
	})
}

// observeSource checks owned, ready source instances and closes each pinned connection.
func (p *postgresCompletionSQL) observeSource(ctx context.Context, blue *cnpgv1.Cluster, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, databases []string, restartedAt *metav1.Time, observe func(pginfra.Conn, cutoverIdentity, string) (bool, error)) (bool, error) {
	id, err := p.access.Credentials(ctx, status)
	if err != nil {
		return false, err
	}
	names, pods, err := retainedInstancePods(ctx, p.switchover.client, blue)
	if err != nil || len(names) == 0 {
		return false, err
	}
	allDone := true
	// Pending standbys must not prevent cleanup on a later primary.
	for _, name := range names {
		pod := pods[name]
		if !instancePodReady(&pod) || pod.DeletionTimestamp != nil {
			return false, nil
		}
		if restartedAt != nil && pod.Annotations[cnpgutils.ClusterRestartAnnotationName] != restartedAt.UTC().Format(time.RFC3339Nano) {
			return false, nil
		}
		for _, database := range databases {
			conn, err := connectTeardownInstance(ctx, p.switchover.client, p.switchover.owner, blue, pod.Status.PodIP, database, func(ctx context.Context, cluster *cnpgv1.Cluster, host, database string) (pginfra.Conn, error) {
				return p.switchover.connectCluster(ctx, cluster, host, database, id)
			})
			if err != nil {
				return false, err
			}
			done, err := observe(conn, id, database)
			err = errors.Join(err, conn.Close(ctx))
			if err != nil {
				return false, err
			}
			allDone = allDone && done
		}
	}
	return allDone, nil
}

// GreenAccessible verifies ordinary green access with a rolled-back write.
func (p *postgresCompletionSQL) GreenAccessible(ctx context.Context, green *cnpgv1.Cluster, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	return p.switchover.VerifyOpen(ctx, green, cutoverIdentity{AttemptID: status.AttemptID}, status.Switchover.GreenOriginalRoleLogins)
}
