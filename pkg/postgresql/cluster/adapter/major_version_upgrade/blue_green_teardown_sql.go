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
	"slices"
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	teardownInactiveSlotsSQL = `SELECT slot_name FROM pg_catalog.pg_replication_slots WHERE slot_name = ANY($1::text[]) AND NOT active`
	teardownSlotCountSQL     = `SELECT count(*) FROM pg_catalog.pg_replication_slots WHERE slot_name = ANY($1::text[])`
	teardownRoleSQL          = `SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = $1)`
	// Attempt roles create nothing; DROP OWNED would delete foreign objects.
	teardownRoleOwnsSQL = `SELECT EXISTS (
  SELECT 1 FROM pg_catalog.pg_shdepend d JOIN pg_catalog.pg_roles r ON r.oid = d.refobjid
  WHERE r.rolname = $1 AND d.deptype = 'o'
    AND d.dbid IN (0, (SELECT oid FROM pg_catalog.pg_database WHERE datname = pg_catalog.current_database())))`
)

type environmentConnector func(ctx context.Context, cluster *cnpgv1.Cluster, host, database string) (pginfra.Conn, error)

// postgresTeardownSQL connects as blue's superuser.
type postgresTeardownSQL struct {
	client  client.Client
	connect environmentConnector
}

func newPostgresTeardownSQL(c client.Client, logicalName string, owner *platformv1alpha1.PostgresCluster) *postgresTeardownSQL {
	v := newPostgresCandidateVerifier(c, logicalName, owner)
	return &postgresTeardownSQL{client: c, connect: func(ctx context.Context, cluster *cnpgv1.Cluster, host, database string) (pginfra.Conn, error) {
		return connectTeardownInstance(ctx, c, owner, cluster, host, database, v.connectCluster)
	}}
}

func connectTeardownInstance(ctx context.Context, c client.Client, owner *platformv1alpha1.PostgresCluster, cluster *cnpgv1.Cluster, host, database string, connect environmentConnector) (pginfra.Conn, error) {
	// A Service can be repointed to a same-name replacement between the
	// environment check and connection. Pin an owned instance IP and verify
	// both identities after connecting, before executing any SQL.
	_, pods, err := retainedInstancePods(ctx, c, cluster)
	if err != nil {
		return nil, err
	}
	service, err := pgconninfo.ServiceFQDN(cluster.GetServiceReadWriteName(), cluster.Namespace)
	if err != nil {
		return nil, err
	}
	var target corev1.Pod
	for _, pod := range pods {
		if host == service && pod.Name == cluster.Status.CurrentPrimary || host == pod.Status.PodIP {
			target = pod
			break
		}
	}
	if target.UID == "" {
		return nil, fmt.Errorf("%w: teardown SQL has no owned instance for %q", mvutypes.ErrUpgradeFlowFailed, host)
	}
	conn, err := connect(ctx, cluster, target.Status.PodIP, database)
	if err != nil && !missingTeardownDatabase(err) {
		return nil, err
	}
	connectionErr := err
	liveCluster, livePod := &cnpgv1.Cluster{}, &corev1.Pod{}
	err = c.Get(ctx, client.ObjectKeyFromObject(cluster), liveCluster)
	if err == nil {
		err = c.Get(ctx, client.ObjectKeyFromObject(&target), livePod)
	}
	if err == nil && (liveCluster.UID != cluster.UID || !metav1.IsControlledBy(liveCluster, owner) || livePod.UID != target.UID || !metav1.IsControlledBy(livePod, cluster) || livePod.Status.PodIP != target.Status.PodIP) {
		err = fmt.Errorf("%w: teardown SQL environment or instance was replaced", mvutypes.ErrUpgradeFlowFailed)
	}
	if err != nil {
		if conn != nil {
			err = errors.Join(err, conn.Close(ctx))
		}
		return nil, err
	}
	return conn, connectionErr
}

// StopReplication prevents retained green subscriptions from reconnecting,
// then terminates sessions using that attempt-only role. This includes copy
// workers that have not created or acquired their synchronization slot yet.
func (s *postgresTeardownSQL) StopReplication(ctx context.Context, source *cnpgv1.Cluster, role string, slots []string) error {
	return s.withConn(ctx, source, "", "postgres", func(conn pginfra.Conn) error {
		exists, err := roleExists(ctx, conn, role)
		if err != nil {
			return err
		}
		if exists {
			if _, err := conn.Exec(ctx, "ALTER ROLE "+pginfra.QuoteIdentifier(role)+" NOLOGIN"); err != nil {
				return err
			}
		}
		var foreign bool
		if err := queryOne(ctx, conn, &foreign, teardownForeignConsumerSQL, slots, role); err != nil {
			return err
		}
		if foreign {
			return fmt.Errorf("%w: attempt slot is used by another role", mvutypes.ErrUpgradeFlowFailed)
		}
		var stopped bool
		if err := queryOne(ctx, conn, &stopped, teardownStopConsumersSQL, role); err != nil {
			return err
		}
		if !stopped {
			return fmt.Errorf("%w: waiting for attempt replication consumers to stop", mvutypes.ErrUpgradeFlowPending)
		}
		var remaining int64
		if err := queryOne(ctx, conn, &remaining, teardownConsumerCountSQL, role); err != nil {
			return err
		}
		if remaining != 0 {
			return fmt.Errorf("%w: waiting for attempt replication sessions to exit", mvutypes.ErrUpgradeFlowPending)
		}
		return nil
	})
}

const teardownForeignConsumerSQL = `SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_replication_slots s JOIN pg_catalog.pg_stat_activity a ON a.pid = s.active_pid WHERE s.slot_name = ANY($1::text[]) AND a.usename <> $2)`
const teardownStopConsumersSQL = `SELECT COALESCE(bool_and(pg_catalog.pg_terminate_backend(pid)), true) FROM pg_catalog.pg_stat_activity WHERE usename = $1 AND pid <> pg_catalog.pg_backend_pid()`
const teardownConsumerCountSQL = `SELECT count(*) FROM pg_catalog.pg_stat_activity WHERE usename = $1 AND pid <> pg_catalog.pg_backend_pid()`

// Green is a dedicated, attempt-owned subscriber. Its system identifier,
// together with the frozen source database and pgoutput plugin, distinguishes
// its initial-copy slots from other subscribers and unrelated slot types.
const teardownSyncSlotsSQL = `SELECT slot_name FROM pg_catalog.pg_replication_slots WHERE slot_type = 'logical' AND plugin = 'pgoutput' AND database = ANY($1::text[]) AND slot_name ~ '^pg_[0-9]+_sync_[0-9]+_[0-9]+$'`

func sourceSlotNames(ctx context.Context, conn pginfra.Conn, inventory teardownSlotInventory) ([]string, error) {
	names := slices.Clone(inventory.names)
	if len(inventory.databases) == 0 {
		return names, nil
	}
	syncSlots, err := queryStrings(ctx, conn, teardownSyncSlotsSQL, inventory.databases)
	if err != nil {
		return nil, err
	}
	if len(syncSlots) > 0 && inventory.systemID == "" {
		return nil, fmt.Errorf("%w: synchronization slots remain without a proved subscriber identity", mvutypes.ErrUpgradeFlowFailed)
	}
	for _, slot := range syncSlots {
		if slot[strings.LastIndexByte(slot, '_')+1:] == inventory.systemID {
			names = append(names, slot)
		}
	}
	return names, nil
}

// RemovePublications checks the frozen preflight inventory after every CR is
// gone, including SQL created before its first successful status write.
func (s *postgresTeardownSQL) RemovePublications(ctx context.Context, source *cnpgv1.Cluster, logicalName string, databases []string) error {
	for _, database := range databases {
		if err := s.withConn(ctx, source, "", database, func(conn pginfra.Conn) error {
			_, err := conn.Exec(ctx, "DROP PUBLICATION IF EXISTS "+pginfra.QuoteIdentifier(publicationName(logicalName, database)))
			return err
		}); err != nil && !missingTeardownDatabase(err) {
			return fmt.Errorf("removing attempt publication in %q: %w", database, err)
		}
	}
	return nil
}

// Only an unambiguous invalid-catalog response proves database absence. In
// particular, joined connection errors must not hide auth or network errors.
func missingTeardownDatabase(err error) bool {
	return allCutoverErrorLeaves(err, func(cause error) bool {
		pgErr, ok := cause.(interface{ SQLState() string })
		return ok && pgErr.SQLState() == "3D000"
	})
}

// withConn owns the connection lifecycle, including close errors. An empty
// host targets the environment's read-write Service.
func (s *postgresTeardownSQL) withConn(ctx context.Context, cluster *cnpgv1.Cluster, host, database string, action func(pginfra.Conn) error) error {
	if host == "" {
		var err error
		if host, err = pgconninfo.ServiceFQDN(cluster.GetServiceReadWriteName(), cluster.Namespace); err != nil {
			return err
		}
	}
	conn, err := s.connect(ctx, cluster, host, database)
	if err != nil {
		return err
	}
	return withCutoverConn(ctx, conn, action)
}

// RemoveSourceSlots drops inactive attempt slots on the primary, then waits
// until no attempt slot remains on the primary or a ready standby. Active
// slots still have a consumer and are waited for. Slot synchronization
// removes standby copies; they are never dropped directly.
func (s *postgresTeardownSQL) RemoveSourceSlots(ctx context.Context, source *cnpgv1.Cluster, inventory teardownSlotInventory) (bool, error) {
	if len(inventory.names) == 0 && len(inventory.databases) == 0 {
		return true, nil
	}
	var remaining int64
	if err := s.withConn(ctx, source, "", "postgres", func(conn pginfra.Conn) error {
		slots, err := sourceSlotNames(ctx, conn, inventory)
		if err != nil {
			return err
		}
		inactive, err := queryStrings(ctx, conn, teardownInactiveSlotsSQL, slots)
		if err != nil {
			return err
		}
		for _, name := range inactive {
			if _, err := conn.Exec(ctx, `SELECT pg_catalog.pg_drop_replication_slot($1)`, name); err != nil {
				return fmt.Errorf("dropping attempt slot %q: %w", name, err)
			}
		}
		return queryOne(ctx, conn, &remaining, teardownSlotCountSQL, slots)
	}); err != nil {
		return false, fmt.Errorf("removing attempt slots on blue: %w", err)
	}
	if remaining > 0 {
		return false, nil
	}
	standbys, err := readyStandbys(ctx, s.client, source)
	if err != nil {
		return false, err
	}
	for _, standby := range standbys {
		if err := s.withConn(ctx, source, standby.address, "postgres", func(conn pginfra.Conn) error {
			slots, err := sourceSlotNames(ctx, conn, inventory)
			if err != nil {
				return err
			}
			return queryOne(ctx, conn, &remaining, teardownSlotCountSQL, slots)
		}); err != nil {
			return false, fmt.Errorf("checking attempt slots on blue instance %q: %w", standby.name, err)
		}
		if remaining > 0 {
			return false, nil
		}
	}
	return true, nil
}

// DropAttemptRoles drops the control helpers and reserved schema, revokes the
// roles' privileges in every database, then drops the roles.
func (s *postgresTeardownSQL) DropAttemptRoles(ctx context.Context, cluster *cnpgv1.Cluster, databases, roles []string) error {
	names := append([]string{"postgres"}, databases...)
	slices.Sort(names)
	for _, database := range slices.Compact(names) {
		if err := s.withConn(ctx, cluster, "", database, func(conn pginfra.Conn) error {
			return revokeInDatabase(ctx, conn, roles)
		}); err != nil {
			if missingTeardownDatabase(err) {
				continue
			}
			return fmt.Errorf("removing attempt roles in %q: %w", database, err)
		}
	}
	return s.withConn(ctx, cluster, "", "postgres", func(conn pginfra.Conn) error {
		for _, role := range roles {
			exists, err := roleExists(ctx, conn, role)
			if err != nil {
				return err
			}
			if !exists {
				continue
			}
			if _, err := conn.Exec(ctx, "DROP ROLE "+pginfra.QuoteIdentifier(role)); err != nil {
				return fmt.Errorf("dropping attempt role %q: %w", role, err)
			}
		}
		return nil
	})
}

// revokeInDatabase drops the control helpers and the attempt roles'
// privileges. RESTRICT fails closed on objects the workflow did not create.
func revokeInDatabase(ctx context.Context, conn pginfra.Conn, roles []string) error {
	schema := pginfra.QuoteIdentifier(candidateGuardSchema)
	for _, helper := range cutoverHelpers(cutoverIdentity{}, "") {
		if _, err := conn.Exec(ctx, "DROP FUNCTION IF EXISTS "+schema+"."+pginfra.QuoteIdentifier(helper.name)+"()"); err != nil {
			return err
		}
	}
	if _, err := conn.Exec(ctx, "DROP SCHEMA IF EXISTS "+schema+" RESTRICT"); err != nil {
		return err
	}
	for _, role := range roles {
		exists, err := roleExists(ctx, conn, role)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		var owns bool
		if err := queryOne(ctx, conn, &owns, teardownRoleOwnsSQL, role); err != nil {
			return err
		}
		if owns {
			return fmt.Errorf("%w: attempt role %q owns objects the workflow did not create", mvutypes.ErrUpgradeFlowFailed, role)
		}
		if _, err := conn.Exec(ctx, "DROP OWNED BY "+pginfra.QuoteIdentifier(role)); err != nil {
			return err
		}
	}
	return nil
}

// DatabaseExists distinguishes confirmed absence from a failed connection to
// a database whose publication controller cannot finish its apply operation.
func (s *postgresTeardownSQL) DatabaseExists(ctx context.Context, source *cnpgv1.Cluster, database string) (bool, error) {
	var exists bool
	err := s.withConn(ctx, source, "", "postgres", func(conn pginfra.Conn) error {
		return queryOne(ctx, conn, &exists, teardownDatabaseExistsSQL, database)
	})
	return exists, err
}

const teardownDatabaseExistsSQL = `SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_database WHERE datname = $1)`

func roleExists(ctx context.Context, conn pginfra.Conn, role string) (bool, error) {
	var exists bool
	err := queryOne(ctx, conn, &exists, teardownRoleSQL, role)
	return exists, err
}

// readyStandbys returns blue's ready, addressable standbys using canonical
// instance discovery. Absent and unready instances cannot be checked; slot
// synchronization reconciles their copies when they rejoin.
func readyStandbys(ctx context.Context, reader client.Reader, cluster *cnpgv1.Cluster) ([]cutoverInstanceTarget, error) {
	names, pods, err := retainedInstancePods(ctx, reader, cluster)
	if err != nil {
		return nil, err
	}
	var standbys []cutoverInstanceTarget
	for _, name := range names {
		pod := pods[name]
		if name == cluster.Status.CurrentPrimary || !pod.DeletionTimestamp.IsZero() || pod.Status.Phase != corev1.PodRunning || !instancePodReady(&pod) {
			continue
		}
		standbys = append(standbys, cutoverInstanceTarget{name: name, address: pod.Status.PodIP})
	}
	return standbys, nil
}
