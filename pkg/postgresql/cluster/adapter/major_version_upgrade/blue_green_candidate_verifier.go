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
	"fmt"
	"slices"
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	clusterCnpg "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/cnpg"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type postgresCandidateVerifier struct {
	client      client.Client
	connect     postgresConnector
	logicalName string
	owner       *platformv1alpha1.PostgresCluster
}

func newPostgresCandidateVerifier(c client.Client, logicalName string, owner *platformv1alpha1.PostgresCluster) *postgresCandidateVerifier {
	return &postgresCandidateVerifier{client: c, connect: pginfra.ConnectVerified, logicalName: logicalName, owner: owner}
}

// VerifyAndGuard verifies the import before it adds any workflow-owned SQL.
// The target fingerprint is intentionally taken before guards are installed:
// those guards are an operator overlay, not imported customer schema.
func (v *postgresCandidateVerifier) VerifyAndGuard(ctx context.Context, target *cnpgv1.Cluster, databases []declaredDatabase, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	if v == nil || v.client == nil || v.connect == nil || target == nil || status == nil {
		return fmt.Errorf("%w: green candidate verification wiring is incomplete", mvutypes.ErrUpgradeFlowFailed)
	}
	byDatabase := make(map[string]int, len(status.DatabaseReplication))
	for index, replication := range status.DatabaseReplication {
		byDatabase[replication.DatabaseName] = index
	}
	for _, database := range databases {
		index, found := byDatabase[database.name]
		if !found || status.DatabaseReplication[index].SourceSchemaFingerprint == nil || status.DatabaseReplication[index].SourceAccessPolicyFingerprint == nil {
			return fmt.Errorf("%w: green candidate has no frozen source receipt for database %q", mvutypes.ErrUpgradeFlowFailed, database.name)
		}
		replication := &status.DatabaseReplication[index]
		guardsInstalled := replication.SchemaImported != nil && *replication.SchemaImported
		if guardsInstalled && (replication.TargetSchemaFingerprint == nil || replication.TargetAccessPolicyFingerprint == nil) {
			return fmt.Errorf("%w: green candidate has an incomplete guard installation receipt for database %q", mvutypes.ErrUpgradeFlowFailed, database.name)
		}
		conn, err := v.connectTarget(ctx, target, database.name)
		if err != nil {
			return err
		}
		var schema, access string
		if err := queryOne(ctx, conn, &schema, schemaFingerprintSQL, candidateGuardTrigger, candidateGuardSchema, candidateGuardFunction); err != nil {
			conn.Close(ctx)
			return fmt.Errorf("fingerprinting green schema for %q: %w", database.name, err)
		}
		if err := queryOne(ctx, conn, &access, accessFingerprintSQL, replicationRoleName(status.AttemptID)); err != nil {
			conn.Close(ctx)
			return fmt.Errorf("fingerprinting green access policy for %q: %w", database.name, err)
		}
		if schema != *replication.SourceSchemaFingerprint || access != *replication.SourceAccessPolicyFingerprint {
			conn.Close(ctx)
			return fmt.Errorf("%w: schema-only import for database %q does not match the frozen blue receipt", mvutypes.ErrUpgradeFlowFailed, database.name)
		}
		if !guardsInstalled {
			// The SQL transaction may have committed before its status receipt was
			// saved. Existing guard objects must be verified, never replaced.
			present, err := candidateGuardArtifactsExist(ctx, conn)
			if err != nil {
				conn.Close(ctx)
				return fmt.Errorf("checking green DML guards for %q: %w", database.name, err)
			}
			if !present {
				if err := installCandidateDMLGuards(ctx, conn); err != nil {
					conn.Close(ctx)
					return fmt.Errorf("installing green DML guards for %q: %w", database.name, err)
				}
			}
		}
		if err := verifyCandidateDMLGuards(ctx, conn); err != nil {
			conn.Close(ctx)
			return fmt.Errorf("verifying green DML guards for %q: %w", database.name, err)
		}
		replication.TargetSchemaFingerprint = new(schema)
		replication.TargetAccessPolicyFingerprint = new(access)
		replication.SchemaImported = new(true)
		// Preserve each completed installation receipt if a later database
		// fails, so a retry verifies these guards instead of recreating them.
		if err := conn.Close(ctx); err != nil {
			return fmt.Errorf("closing green database connection for %q: %w", database.name, err)
		}
	}
	return nil
}

func (v *postgresCandidateVerifier) connectTarget(ctx context.Context, target *cnpgv1.Cluster, database string) (pginfra.Conn, error) {
	host, err := pgconninfo.ServiceFQDN(target.GetServiceReadWriteName(), target.Namespace)
	if err != nil {
		return nil, fmt.Errorf("resolving green write Service: %w", err)
	}
	return v.connectCluster(ctx, target, host, database)
}

func (v *postgresCandidateVerifier) connectCluster(ctx context.Context, cluster *cnpgv1.Cluster, host, database string) (pginfra.Conn, error) {
	if cluster == nil {
		return nil, fmt.Errorf("%w: PostgreSQL Cluster is unavailable", mvutypes.ErrUpgradeFlowFailed)
	}
	secretName := cluster.GetSuperuserSecretName()
	if secretName == "" {
		return nil, fmt.Errorf("%w: PostgreSQL superuser Secret is unavailable", mvutypes.ErrUpgradeFlowFailed)
	}
	secret := &corev1.Secret{}
	if err := v.client.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: secretName}, secret); err != nil {
		return nil, fmt.Errorf("reading PostgreSQL superuser Secret: %w", err)
	}
	password := secret.Data[replicationSecretPasswordKey]
	if len(password) == 0 {
		return nil, fmt.Errorf("%w: PostgreSQL superuser Secret %q has no password", mvutypes.ErrUpgradeFlowFailed, secretName)
	}
	serverCA, err := clusterServerCA(ctx, v.client, cluster)
	if err != nil {
		return nil, err
	}
	serverName, err := pgconninfo.ServiceFQDN(cluster.GetServiceReadWriteName(), cluster.Namespace)
	if err != nil {
		return nil, fmt.Errorf("resolving PostgreSQL certificate service name: %w", err)
	}
	conn, err := v.connect(ctx, host, database, string(password), serverName, serverCA)
	if err != nil {
		return nil, fmt.Errorf("connecting to PostgreSQL database %q: %w", database, err)
	}
	return conn, nil
}

func clusterServerCA(ctx context.Context, reader client.Reader, cluster *cnpgv1.Cluster) ([]byte, error) {
	secretName := cluster.Status.Certificates.ServerCASecret
	if secretName == "" && cluster.Spec.Certificates != nil {
		secretName = cluster.Spec.Certificates.ServerCASecret
	}
	if secretName == "" {
		return nil, fmt.Errorf("%w: PostgreSQL server CA Secret is unavailable", mvutypes.ErrUpgradeFlowFailed)
	}
	secret := &corev1.Secret{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: secretName}, secret); err != nil {
		return nil, fmt.Errorf("reading PostgreSQL server CA Secret: %w", err)
	}
	serverCA := secret.Data["ca.crt"]
	if len(serverCA) == 0 {
		return nil, fmt.Errorf("%w: PostgreSQL server CA Secret %q has no ca.crt", mvutypes.ErrUpgradeFlowFailed, secretName)
	}
	return serverCA, nil
}

// VerifySourceBaseline reuses the read-only preflight inventory and compares
// its catalog receipts with the frozen source baseline. It never replays
// source preparation's role, publication, or slot-setting mutations.
func (v *postgresCandidateVerifier) VerifySourceBaseline(ctx context.Context, source *cnpgv1.Cluster, databases []declaredDatabase, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	if v == nil || v.client == nil || v.connect == nil || source == nil || status == nil {
		return fmt.Errorf("%w: source baseline verification wiring is incomplete", mvutypes.ErrUpgradeFlowFailed)
	}
	preflight := &postgresSourcePreflight{client: v.client, connect: v.connect, logicalName: v.logicalName, owner: v.owner}
	checks, err := preflight.inspectPrepared(ctx, source, databases, status.AttemptID)
	if err != nil {
		return err
	}
	byDatabase := make(map[string]platformv1alpha1.BlueGreenDatabaseReplicationStatus, len(status.DatabaseReplication))
	for _, replication := range status.DatabaseReplication {
		byDatabase[replication.DatabaseName] = replication
	}
	for _, check := range checks {
		baseline, found := byDatabase[check.name]
		if !found || baseline.SourceSchemaFingerprint == nil || baseline.SourceAccessPolicyFingerprint == nil {
			return fmt.Errorf("%w: blue database %q has no frozen catalog receipt", mvutypes.ErrUpgradeFlowFailed, check.name)
		}
		if check.schemaFingerprint != *baseline.SourceSchemaFingerprint || check.accessFingerprint != *baseline.SourceAccessPolicyFingerprint {
			return fmt.Errorf("%w: source schema or access policy for database %q changed after blue/green preflight", mvutypes.ErrUpgradeFlowFailed, check.name)
		}
	}
	return nil
}

// VerifyReplication proves both halves of CNPG's native logical replication:
// the subscriber has completed its consistent copy and keeps applying, and
// the named source slot is valid on each healthy source instance.
func (v *postgresCandidateVerifier) VerifyReplication(ctx context.Context, source, target *cnpgv1.Cluster, databases []declaredDatabase, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	if v == nil || v.client == nil || v.connect == nil || source == nil || target == nil || status == nil {
		return false, fmt.Errorf("%w: logical replication verification wiring is incomplete", mvutypes.ErrUpgradeFlowFailed)
	}
	// Candidate Ready must retain source HA, not merely prove the slot on the
	// surviving primary while an intended failover target is unavailable.
	if !sourceReadyForFailover(source) {
		for i := range status.DatabaseReplication {
			status.DatabaseReplication[i].SlotHealthy = new(false)
			status.DatabaseReplication[i].ReplicaSlots = nil
		}
		return false, nil
	}
	healthy, pods, err := v.healthySourcePods(ctx, source)
	if err != nil {
		return false, err
	}
	if len(healthy) == 0 {
		return false, nil
	}
	// Blue stays writable. A fresh sample makes readiness wait until this pass
	// has caught up; final switchover parity is checked after blue is fenced.
	sourceLSN, err := v.sourceWALPosition(ctx, source)
	if err != nil {
		return false, err
	}
	byDatabase := make(map[string]int, len(status.DatabaseReplication))
	for index, replication := range status.DatabaseReplication {
		byDatabase[replication.DatabaseName] = index
	}
	ready := true
	for _, database := range databases {
		index, found := byDatabase[database.name]
		if !found {
			return false, fmt.Errorf("%w: no candidate replication status for database %q", mvutypes.ErrUpgradeFlowFailed, database.name)
		}
		subscriptionReady, copyComplete, err := v.subscriptionState(ctx, target, database.name, sourceLSN)
		if err != nil {
			return false, err
		}
		slots, slotsReady, err := v.slotState(ctx, source, database.name, sourceLSN, healthy, pods)
		if err != nil {
			return false, err
		}
		status.DatabaseReplication[index].SubscriptionHealthy = new(subscriptionReady)
		status.DatabaseReplication[index].InitialCopyComplete = new(copyComplete)
		status.DatabaseReplication[index].SlotHealthy = new(slotsReady)
		status.DatabaseReplication[index].ReplicaSlots = slots
		ready = ready && subscriptionReady && copyComplete && slotsReady
	}
	if ready {
		inventoryReady, err := v.verifySourceSlotInventory(ctx, source, databases)
		if err != nil {
			return false, err
		}
		ready = inventoryReady
	}
	return ready, nil
}

func sourceReadyForFailover(source *cnpgv1.Cluster) bool {
	return clusterCnpg.ClusterReady(source) && source.Status.Instances == source.Spec.Instances &&
		len(source.Status.InstancesStatus[cnpgv1.PodHealthy]) == source.Spec.Instances &&
		slices.Contains(source.Status.InstancesStatus[cnpgv1.PodHealthy], source.Status.CurrentPrimary)
}

func (v *postgresCandidateVerifier) sourceWALPosition(ctx context.Context, source *cnpgv1.Cluster) (string, error) {
	host, err := pgconninfo.ServiceFQDN(source.GetServiceReadWriteName(), source.Namespace)
	if err != nil {
		return "", fmt.Errorf("resolving blue write Service for replication catch-up: %w", err)
	}
	conn, err := v.connectCluster(ctx, source, host, "postgres")
	if err != nil {
		return "", fmt.Errorf("connecting to blue for replication catch-up: %w", err)
	}
	defer conn.Close(ctx)
	var position string
	if err := queryOne(ctx, conn, &position, `SELECT pg_current_wal_lsn()::text`); err != nil {
		return "", fmt.Errorf("sampling blue WAL position: %w", err)
	}
	return position, nil
}

// verifySourceSlotInventory runs only after initial copy, when PostgreSQL has
// normally removed table-synchronization slots. A lingering sync slot keeps
// the candidate pending; any other extra logical slot breaks the inventory.
func (v *postgresCandidateVerifier) verifySourceSlotInventory(ctx context.Context, source *cnpgv1.Cluster, databases []declaredDatabase) (bool, error) {
	host, err := pgconninfo.ServiceFQDN(source.GetServiceReadWriteName(), source.Namespace)
	if err != nil {
		return false, fmt.Errorf("resolving blue write Service for slot inventory: %w", err)
	}
	conn, err := v.connectCluster(ctx, source, host, "postgres")
	if err != nil {
		return false, fmt.Errorf("connecting to blue maintenance database for slot inventory: %w", err)
	}
	actual, queryErr := queryStrings(ctx, conn, `SELECT slot_name || '/' || database FROM pg_replication_slots WHERE slot_type = 'logical' ORDER BY 1`)
	closeErr := conn.Close(ctx)
	if queryErr != nil {
		return false, fmt.Errorf("listing blue logical slots: %w", queryErr)
	}
	if closeErr != nil {
		return false, fmt.Errorf("closing blue maintenance connection for slot inventory: %w", closeErr)
	}
	expected := make(map[string]struct{}, len(databases))
	for _, database := range databases {
		expected[subscriptionName(v.logicalName, database.name)+"/"+database.name] = struct{}{}
	}
	pendingSyncSlot := false
	for _, slot := range actual {
		if _, ok := expected[slot]; ok {
			delete(expected, slot)
			continue
		}
		name, _, _ := strings.Cut(slot, "/")
		if strings.HasPrefix(name, "pg_") && strings.Contains(name, "_sync_") {
			pendingSyncSlot = true
			continue
		}
		return false, fmt.Errorf("%w: blue logical-slot inventory changed after preflight", mvutypes.ErrUpgradeFlowFailed)
	}
	return len(expected) == 0 && !pendingSyncSlot, nil
}

func (v *postgresCandidateVerifier) healthySourcePods(ctx context.Context, source *cnpgv1.Cluster) ([]string, map[string]corev1.Pod, error) {
	healthy := source.Status.InstancesStatus[cnpgv1.PodHealthy]
	podList := &corev1.PodList{}
	if err := v.client.List(ctx, podList, client.InNamespace(source.Namespace), client.MatchingLabels{"cnpg.io/cluster": source.Name}); err != nil {
		return nil, nil, fmt.Errorf("listing blue instances for logical-slot verification: %w", err)
	}
	pods := make(map[string]corev1.Pod, len(podList.Items))
	for _, pod := range podList.Items {
		pods[pod.Name] = pod
	}
	for _, name := range healthy {
		if _, found := pods[name]; !found {
			return nil, nil, fmt.Errorf("%w: blue healthy instance %q is not discoverable", mvutypes.ErrUpgradeFlowFailed, name)
		}
	}
	return healthy, pods, nil
}

func (v *postgresCandidateVerifier) subscriptionState(ctx context.Context, target *cnpgv1.Cluster, database, sourceLSN string) (bool, bool, error) {
	conn, err := v.connectTarget(ctx, target, database)
	if err != nil {
		return false, false, err
	}
	defer conn.Close(ctx)
	name := subscriptionName(v.logicalName, database)
	var subscriptionHealthy bool
	if err := queryOne(ctx, conn, &subscriptionHealthy, `SELECT EXISTS (
  SELECT 1 FROM pg_subscription sub
  JOIN pg_stat_subscription worker ON worker.subid = sub.oid
  WHERE sub.subname = $1 AND sub.subenabled AND sub.subfailover AND sub.subslotname = $1
    AND worker.relid IS NULL AND worker.pid IS NOT NULL
    AND worker.latest_end_lsn >= $2::pg_lsn
)`, name, sourceLSN); err != nil {
		return false, false, fmt.Errorf("checking green subscription %q: %w", name, err)
	}
	var copyComplete bool
	if err := queryOne(ctx, conn, &copyComplete, `SELECT NOT EXISTS (
  SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE c.relkind = 'r' AND c.relpersistence = 'p'
    AND n.nspname NOT IN ('pg_catalog', 'information_schema')
    AND n.nspname NOT LIKE 'pg_toast%' AND n.nspname <> $2
    AND NOT EXISTS (
      SELECT 1 FROM pg_subscription_rel rel JOIN pg_subscription sub ON sub.oid = rel.srsubid
      WHERE sub.subname = $1 AND rel.srrelid = c.oid AND rel.srsubstate = 'r'
    )
)`, name, candidateGuardSchema); err != nil {
		return false, false, fmt.Errorf("checking initial copy for green subscription %q: %w", name, err)
	}
	return subscriptionHealthy, copyComplete, nil
}

func (v *postgresCandidateVerifier) slotState(ctx context.Context, source *cnpgv1.Cluster, database, sourceLSN string, healthy []string, pods map[string]corev1.Pod) ([]platformv1alpha1.BlueGreenReplicaSlotStatus, bool, error) {
	service, err := pgconninfo.ServiceFQDN(source.GetServiceAnyName(), source.Namespace)
	if err != nil {
		return nil, false, fmt.Errorf("resolving blue headless Service for logical-slot verification: %w", err)
	}
	slotName := subscriptionName(v.logicalName, database)
	result := make([]platformv1alpha1.BlueGreenReplicaSlotStatus, 0, len(healthy))
	ready := true
	for _, instance := range healthy {
		pod := pods[instance]
		host := instance + "." + service
		conn, err := v.connectCluster(ctx, source, host, database)
		if err != nil {
			return result, false, fmt.Errorf("connecting to blue instance %q for logical-slot verification: %w", instance, err)
		}
		state, err := logicalSlotState(ctx, conn, slotName, sourceLSN)
		closeErr := conn.Close(ctx)
		if err != nil {
			return result, false, fmt.Errorf("checking blue logical slot %q on %q: %w", slotName, instance, err)
		}
		if closeErr != nil {
			return result, false, fmt.Errorf("closing blue instance connection %q: %w", instance, closeErr)
		}
		if state.invalidation != "" && state.invalidation != "missing" {
			return result, false, fmt.Errorf("%w: blue logical slot %q on %q is invalidated: %s", mvutypes.ErrUpgradeFlowFailed, slotName, instance, state.invalidation)
		}
		valid := logicalSlotReady(state, instance == source.Status.CurrentPrimary)
		now := metav1.Now()
		replica := platformv1alpha1.BlueGreenReplicaSlotStatus{ReplicaName: instance, ReplicaUID: string(pod.UID), SlotName: slotName, Valid: new(valid), VerifiedAt: &now}
		if !valid {
			failure := "logical slot is not yet synchronized and caught up on the standby"
			if instance == source.Status.CurrentPrimary {
				failure = "primary logical slot is not active, caught up, and failover-enabled"
			}
			replica.Failure = &failure
		}
		result = append(result, replica)
		ready = ready && valid
	}
	return result, ready, nil
}

type logicalSlotObservation struct {
	active          bool
	confirmed       bool
	failover        bool
	synced          bool
	temporary       bool
	positionCovered bool
	invalidation    string
}

func logicalSlotReady(state logicalSlotObservation, primary bool) bool {
	if state.invalidation != "" || !state.confirmed || !state.failover || state.temporary || !state.positionCovered {
		return false
	}
	if primary {
		return state.active
	}
	return state.synced
}

func logicalSlotState(ctx context.Context, conn pginfra.Conn, slotName, sourceLSN string) (logicalSlotObservation, error) {
	// On blue's primary, confirmed_flush_lsn is the subscriber's acknowledged
	// position. On a standby, the slot must be synchronized and replay must
	// cover that position without trailing the slot's confirmed LSN.
	rows, err := conn.Query(ctx, `SELECT active, confirmed_flush_lsn IS NOT NULL,
  COALESCE(invalidation_reason, ''), failover, synced, temporary,
  CASE WHEN pg_is_in_recovery() THEN
    COALESCE(pg_last_wal_replay_lsn() >= $2::pg_lsn AND confirmed_flush_lsn <= pg_last_wal_replay_lsn(), false)
  ELSE COALESCE(confirmed_flush_lsn >= $2::pg_lsn, false) END
FROM pg_replication_slots WHERE slot_name = $1 AND slot_type = 'logical'`, slotName, sourceLSN)
	if err != nil {
		return logicalSlotObservation{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return logicalSlotObservation{}, err
		}
		return logicalSlotObservation{invalidation: "missing"}, nil
	}
	var state logicalSlotObservation
	if err := rows.Scan(&state.active, &state.confirmed, &state.invalidation, &state.failover, &state.synced, &state.temporary, &state.positionCovered); err != nil {
		return logicalSlotObservation{}, err
	}
	if rows.Next() {
		return logicalSlotObservation{}, fmt.Errorf("unexpected multiple logical-slot rows for %q", slotName)
	}
	return state, rows.Err()
}

const candidateGuardBody = `
BEGIN
  IF pg_catalog.pg_replication_origin_session_is_setup() THEN
    RETURN NULL;
  END IF;
  RAISE EXCEPTION 'ordinary writes are disabled while this blue/green candidate is synchronizing';
END;
`

func candidateGuardArtifactsExist(ctx context.Context, conn pginfra.Conn) (bool, error) {
	var present bool
	err := queryOne(ctx, conn, &present, `SELECT EXISTS (
  SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname = $1
) OR EXISTS (
  SELECT 1 FROM pg_catalog.pg_trigger WHERE tgname = $2 AND NOT tgisinternal
)`, candidateGuardSchema, candidateGuardTrigger)
	return present, err
}

func installCandidateDMLGuards(ctx context.Context, conn pginfra.Conn) error {
	tables, err := candidateTables(ctx, conn)
	if err != nil {
		return err
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	guardSchema := pginfra.QuoteIdentifier(candidateGuardSchema)
	guardFunction := pginfra.QuoteIdentifier(candidateGuardFunction)
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+guardSchema); err != nil {
		return err
	}
	functionSQL := fmt.Sprintf("CREATE FUNCTION %s.%s() RETURNS trigger LANGUAGE plpgsql AS $guard$%s$guard$", guardSchema, guardFunction, candidateGuardBody)
	if _, err := tx.Exec(ctx, functionSQL); err != nil {
		return err
	}
	for _, table := range tables {
		triggerSQL := fmt.Sprintf("CREATE TRIGGER %s BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON %s.%s FOR EACH STATEMENT EXECUTE FUNCTION %s.%s()",
			pginfra.QuoteIdentifier(candidateGuardTrigger), pginfra.QuoteIdentifier(table.schema), pginfra.QuoteIdentifier(table.name), guardSchema, guardFunction)
		if _, err := tx.Exec(ctx, triggerSQL); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Once the installation receipt is persisted, reconciliation must only read
// guards. Repairing a missing guard would hide any ordinary write made while
// it was absent, which logical replication cannot detect.
func verifyCandidateDMLGuards(ctx context.Context, conn pginfra.Conn) error {
	var functionValid bool
	if err := queryOne(ctx, conn, &functionValid, `SELECT COALESCE((
  SELECT language.lanname = 'plpgsql' AND function.prokind = 'f'
    AND function.prorettype = 'pg_catalog.trigger'::regtype
    AND function.pronargs = 0 AND NOT function.prosecdef
    AND function.proconfig IS NULL AND function.prosrc = $3
  FROM pg_catalog.pg_proc function
  JOIN pg_catalog.pg_namespace schema ON schema.oid = function.pronamespace
  JOIN pg_catalog.pg_language language ON language.oid = function.prolang
  WHERE schema.nspname = $1 AND function.proname = $2 AND function.pronargs = 0
), false)`, candidateGuardSchema, candidateGuardFunction, candidateGuardBody); err != nil {
		return err
	}
	if !functionValid {
		return fmt.Errorf("%w: green DML guard function is missing or changed", mvutypes.ErrUpgradeFlowFailed)
	}
	tables, err := candidateTables(ctx, conn)
	if err != nil {
		return err
	}
	for _, table := range tables {
		installed, err := candidateGuardInstalled(ctx, conn, table.schema, table.name)
		if err != nil {
			return err
		}
		if !installed {
			return fmt.Errorf("%w: green DML guard on %s.%s is missing", mvutypes.ErrUpgradeFlowFailed, table.schema, table.name)
		}
	}
	return nil
}

type candidateTable struct{ schema, name string }

func candidateTables(ctx context.Context, conn pginfra.Conn) ([]candidateTable, error) {
	rows, err := conn.Query(ctx, `SELECT n.nspname, c.relname
FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r', 'p')
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
  AND n.nspname <> $1
ORDER BY n.nspname, c.relname`, candidateGuardSchema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []candidateTable
	for rows.Next() {
		var table candidateTable
		if err := rows.Scan(&table.schema, &table.name); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}
	return tables, rows.Err()
}

func candidateGuardInstalled(ctx context.Context, conn pginfra.Conn, schema, table string) (bool, error) {
	var state string
	err := queryOne(ctx, conn, &state, `WITH named AS (
  SELECT trigger.tgtype, trigger.tgenabled, trigger.tgqual, trigger.tgattr, trigger.tgnargs,
         function_schema.nspname AS function_schema, function.proname AS function_name
  FROM pg_catalog.pg_trigger trigger
  JOIN pg_catalog.pg_class relation ON relation.oid = trigger.tgrelid
  JOIN pg_catalog.pg_namespace relation_schema ON relation_schema.oid = relation.relnamespace
  JOIN pg_catalog.pg_proc function ON function.oid = trigger.tgfoid
  JOIN pg_catalog.pg_namespace function_schema ON function_schema.oid = function.pronamespace
  WHERE NOT trigger.tgisinternal
    AND relation_schema.nspname = $1 AND relation.relname = $2
    AND trigger.tgname = $3
)
SELECT CASE
  WHEN NOT EXISTS (SELECT 1 FROM named) THEN 'missing'
  WHEN EXISTS (SELECT 1 FROM named WHERE tgtype = 62 AND tgenabled IN ('O', 'A')
    AND tgqual IS NULL AND pg_catalog.cardinality(tgattr) = 0 AND tgnargs = 0
    AND function_schema = $4 AND function_name = $5) THEN 'valid'
  ELSE 'invalid'
END`, schema, table, candidateGuardTrigger, candidateGuardSchema, candidateGuardFunction)
	if err != nil {
		return false, err
	}
	if state == "invalid" {
		return false, fmt.Errorf("%w: candidate DML guard on %s.%s was altered or disabled", mvutypes.ErrUpgradeFlowFailed, schema, table)
	}
	if state != "valid" && state != "missing" {
		return false, fmt.Errorf("%w: unexpected candidate DML guard state %q on %s.%s", mvutypes.ErrUpgradeFlowFailed, state, schema, table)
	}
	return state == "valid", nil
}
