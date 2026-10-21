/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package majorupgradeadapter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/jackc/pgx/v5/pgconn"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	databasecore "github.com/splunk/splunk-operator/pkg/postgresql/database/core"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	replicationSecretUsernameKey  = "username"
	replicationSecretPasswordKey  = "password"
	sourceDatabasesSQL            = `SELECT datname FROM pg_database WHERE datname NOT IN ('postgres', 'template0', 'template1') ORDER BY datname`
	sourceRolesSQL                = `SELECT rolname FROM pg_roles ORDER BY rolname`
	unsupportedDatabaseObjectsSQL = `
SELECT EXISTS (
  SELECT 1 FROM pg_largeobject_metadata
  UNION ALL SELECT 1 FROM pg_matviews
  UNION ALL SELECT 1 FROM pg_class WHERE relkind IN ('r', 'p') AND (relrowsecurity OR relforcerowsecurity)
  UNION ALL SELECT 1 FROM pg_class c
  WHERE c.relkind IN ('r', 'p') AND c.relnamespace NOT IN
    (SELECT oid FROM pg_namespace WHERE nspname IN ('pg_catalog', 'information_schema'))
    AND (c.relreplident = 'n' OR (c.relreplident = 'd' AND NOT EXISTS
      (SELECT 1 FROM pg_index i WHERE i.indrelid = c.oid AND i.indisprimary)))
)`
	foreignObjectsSQL = `
SELECT 'foreign-server:' || srvname FROM pg_foreign_server
UNION ALL SELECT 'user-mapping:' || umuser::text || '@' || srvname FROM pg_user_mappings
ORDER BY 1`
)

// sourcePreflight is the source-side SQL boundary. Prepare does not mutate
// publications or replication slots until Inspect completes successfully.
type sourcePreflight interface {
	Inspect(context.Context, *cnpgv1.Cluster, []declaredDatabase, string) ([]databasePreflight, error)
	EnsureReplicationRole(context.Context, *cnpgv1.Cluster, []declaredDatabase, string, string) error
}

type databasePreflight struct {
	name              string
	schemaFingerprint string
	accessFingerprint string
}

type postgresConnector func(context.Context, string, string, string, string, []byte) (pginfra.Conn, error)

type postgresSourcePreflight struct {
	client      client.Client
	connect     postgresConnector
	logicalName string
	owner       *platformv1alpha1.PostgresCluster
}

func newPostgresSourcePreflight(c client.Client, logicalName string, owner *platformv1alpha1.PostgresCluster) *postgresSourcePreflight {
	return &postgresSourcePreflight{client: c, connect: pginfra.ConnectVerified, logicalName: logicalName, owner: owner}
}

func (p *postgresSourcePreflight) Inspect(ctx context.Context, source *cnpgv1.Cluster, databases []declaredDatabase, attemptID string) ([]databasePreflight, error) {
	return p.inspect(ctx, source, databases, attemptID, true)
}

// inspectPrepared repeats the read-only preflight inventory after the
// attempt's subscriptions have created their logical slots. Slot inventory is
// verified separately once the initial table copy has completed.
func (p *postgresSourcePreflight) inspectPrepared(ctx context.Context, source *cnpgv1.Cluster, databases []declaredDatabase, attemptID string) ([]databasePreflight, error) {
	return p.inspect(ctx, source, databases, attemptID, false)
}

func (p *postgresSourcePreflight) inspect(ctx context.Context, source *cnpgv1.Cluster, databases []declaredDatabase, attemptID string, rejectLogicalSlots bool) ([]databasePreflight, error) {
	conn, err := p.connectSource(ctx, source, "postgres")
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)
	if err := checkSourceCapacityAndDatabases(ctx, conn, databases, source.Spec.Instances-1); err != nil {
		return nil, err
	}
	roles, err := checkSourceClusterObjects(ctx, conn, rejectLogicalSlots)
	if err != nil {
		return nil, err
	}

	result := make([]databasePreflight, 0, len(databases))
	for _, database := range databases {
		check, err := p.inspectDatabase(ctx, source, database, attemptID, !rejectLogicalSlots)
		if err != nil {
			return nil, err
		}
		result = append(result, check)
	}
	if err := validateSourceRoles(roles, declaredRoleNames(databases, attemptID)); err != nil {
		return nil, err
	}
	return result, nil
}

func checkSourceCapacityAndDatabases(ctx context.Context, conn pginfra.Conn, databases []declaredDatabase, sourceReplicas int) error {
	var capacityOK bool
	if err := queryOne(ctx, conn, &capacityOK, `
WITH capacity AS (
  SELECT current_setting('max_replication_slots')::int AS slots,
         current_setting('max_wal_senders')::int AS senders,
         current_setting('max_logical_replication_workers')::int AS logical_workers,
         current_setting('max_worker_processes')::int AS workers,
         current_setting('max_active_replication_origins')::int AS origins,
         current_setting('max_sync_workers_per_subscription')::int AS sync_per_subscription,
         GREATEST($2::int, (SELECT count(*) FROM pg_replication_slots WHERE slot_type = 'physical')) AS physical_slots,
         GREATEST($2::int, (SELECT count(*) FROM pg_stat_replication r WHERE NOT EXISTS (
           SELECT 1 FROM pg_replication_slots s WHERE s.active_pid = r.pid AND s.slot_type = 'logical'
         ))) AS physical_senders
), peak AS (
  SELECT *, $1::int + LEAST($1::int * sync_per_subscription, logical_workers - $1::int) AS logical_slots
  FROM capacity
)
SELECT sync_per_subscription > 0 AND logical_workers > $1::int
   AND slots >= physical_slots + logical_slots
   AND senders >= physical_senders + logical_slots
   AND origins >= logical_slots AND workers >= logical_slots + 1
   AND current_setting('max_slot_wal_keep_size') <> '-1'
   AND pg_size_bytes(current_setting('max_slot_wal_keep_size')) >=
       (SELECT COALESCE(sum(pg_database_size(datname)), 0)
        FROM pg_database WHERE datname NOT IN ('postgres', 'template0', 'template1'))
FROM peak`, len(databases), max(sourceReplicas, 0)); err != nil {
		return fmt.Errorf("checking source logical-replication capacity: %w", err)
	}
	if !capacityOK {
		return fmt.Errorf("%w: source worker, replication-slot, or WAL-retention settings cannot support this attempt", mvutypes.ErrUpgradeFlowFailed)
	}

	actualDatabases, err := queryStrings(ctx, conn, sourceDatabasesSQL)
	if err != nil {
		return fmt.Errorf("listing source databases: %w", err)
	}
	declared := make(map[string]declaredDatabase, len(databases))
	for _, database := range databases {
		declared[database.name] = database
	}
	actual := stringSet(actualDatabases)
	for _, name := range actualDatabases {
		if _, ok := declared[name]; !ok {
			return fmt.Errorf("%w: source database %q is not managed by a PostgresDatabase", mvutypes.ErrUpgradeFlowFailed, name)
		}
	}
	for _, database := range databases {
		if _, ok := actual[database.name]; !ok {
			return fmt.Errorf("%w: managed database %q does not exist on the source", mvutypes.ErrUpgradeFlowFailed, database.name)
		}
	}
	return nil
}

func checkSourceClusterObjects(ctx context.Context, conn pginfra.Conn, rejectLogicalSlots bool) ([]string, error) {
	roles, err := queryStrings(ctx, conn, sourceRolesSQL)
	if err != nil {
		return nil, fmt.Errorf("listing source roles: %w", err)
	}
	unsupported, err := queryStrings(ctx, conn, `
SELECT 'tablespace:' || spcname FROM pg_tablespace WHERE spcname NOT IN ('pg_default', 'pg_global')
UNION ALL SELECT 'logical-slot:' || slot_name FROM pg_replication_slots WHERE slot_type = 'logical' AND $1::boolean
ORDER BY 1`, rejectLogicalSlots)
	if err != nil {
		return nil, fmt.Errorf("listing source cluster-scoped replication objects: %w", err)
	}
	if len(unsupported) != 0 {
		return nil, fmt.Errorf("%w: source contains unmanaged %s", mvutypes.ErrUpgradeFlowFailed, strings.Join(unsupported, ", "))
	}
	subscriptions, err := queryStrings(ctx, conn, `SELECT subname FROM pg_subscription ORDER BY subname`)
	if err != nil {
		return nil, fmt.Errorf("listing source subscriptions: %w", err)
	}
	if len(subscriptions) != 0 {
		return nil, fmt.Errorf("%w: source contains unmanaged subscriptions %s", mvutypes.ErrUpgradeFlowFailed, strings.Join(subscriptions, ", "))
	}
	maintenanceObjects, err := queryStrings(ctx, conn, `
SELECT 'maintenance-object:' || n.nspname || '.' || c.relname
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
  AND c.relkind IN ('r', 'p', 'v', 'm', 'S')
ORDER BY 1`)
	if err != nil {
		return nil, fmt.Errorf("listing maintenance-database objects: %w", err)
	}
	if len(maintenanceObjects) != 0 {
		return nil, fmt.Errorf("%w: source postgres database contains unsupported %s", mvutypes.ErrUpgradeFlowFailed, strings.Join(maintenanceObjects, ", "))
	}
	return roles, nil
}

func validateSourceRoles(roles []string, allowed map[string]struct{}) error {
	for _, role := range roles {
		if databasecore.IsReservedRoleName(role) {
			continue
		}
		if _, ok := allowed[role]; !ok {
			return fmt.Errorf("%w: source role %q is not declared by a managed database", mvutypes.ErrUpgradeFlowFailed, role)
		}
	}
	return nil
}

func (p *postgresSourcePreflight) inspectDatabase(ctx context.Context, source *cnpgv1.Cluster, database declaredDatabase, attemptID string, prepared bool) (databasePreflight, error) {
	conn, err := p.connectSource(ctx, source, database.name)
	if err != nil {
		return databasePreflight{}, err
	}
	defer conn.Close(ctx)
	if err := p.checkDatabaseObjects(ctx, conn, source, database.name, attemptID); err != nil {
		return databasePreflight{}, err
	}
	if prepared {
		var complete bool
		if err := queryOne(ctx, conn, &complete, `SELECT COALESCE((SELECT puballtables AND pubinsert AND pubupdate AND pubdelete AND pubtruncate AND NOT pubviaroot FROM pg_catalog.pg_publication WHERE pubname = $1), false)`, publicationName(p.logicalName, database.name)); err != nil {
			return databasePreflight{}, fmt.Errorf("checking prepared publication for %q: %w", database.name, err)
		}
		if !complete {
			return databasePreflight{}, fmt.Errorf("%w: source publication for %q is missing or does not publish all changes", mvutypes.ErrUpgradeFlowFailed, database.name)
		}
	}
	if err := checkDatabaseExtensions(ctx, conn, database); err != nil {
		return databasePreflight{}, err
	}
	var reservedGuardSchema bool
	if err := queryOne(ctx, conn, &reservedGuardSchema, `SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)`, candidateGuardSchema); err != nil {
		return databasePreflight{}, fmt.Errorf("checking reserved candidate-guard schema for %q: %w", database.name, err)
	}
	if reservedGuardSchema {
		return databasePreflight{}, fmt.Errorf("%w: database %q already contains the reserved candidate-guard schema", mvutypes.ErrUpgradeFlowFailed, database.name)
	}
	var schema, access string
	if err := queryOne(ctx, conn, &schema, schemaFingerprintSQL, candidateGuardTrigger, candidateGuardSchema, candidateGuardFunction); err != nil {
		return databasePreflight{}, fmt.Errorf("fingerprinting source schema for %q: %w", database.name, err)
	}
	if err := queryOne(ctx, conn, &access, accessFingerprintSQL, replicationRoleName(attemptID)); err != nil {
		return databasePreflight{}, fmt.Errorf("fingerprinting source access policy for %q: %w", database.name, err)
	}
	return databasePreflight{name: database.name, schemaFingerprint: schema, accessFingerprint: access}, nil
}

func (p *postgresSourcePreflight) checkDatabaseObjects(ctx context.Context, conn pginfra.Conn, source *cnpgv1.Cluster, databaseName, attemptID string) error {
	var unsupported bool
	if err := queryOne(ctx, conn, &unsupported, unsupportedDatabaseObjectsSQL); err != nil {
		return fmt.Errorf("checking logical-replication inventory for %q: %w", databaseName, err)
	}
	if unsupported {
		return fmt.Errorf("%w: database %q contains large objects, materialized views, row-level security, or relations without replica identity", mvutypes.ErrUpgradeFlowFailed, databaseName)
	}
	if err := checkUnloggedTables(ctx, conn, databaseName); err != nil {
		return err
	}
	foreignObjects, err := queryStrings(ctx, conn, foreignObjectsSQL)
	if err != nil {
		return fmt.Errorf("listing foreign objects for %q: %w", databaseName, err)
	}
	if len(foreignObjects) != 0 {
		return fmt.Errorf("%w: database %q contains unsupported %s", mvutypes.ErrUpgradeFlowFailed, databaseName, strings.Join(foreignObjects, ", "))
	}
	publicationObjects, err := queryStrings(ctx, conn, `SELECT 'publication:' || pubname FROM pg_publication ORDER BY 1`)
	if err != nil {
		return fmt.Errorf("listing publication and subscription objects for %q: %w", databaseName, err)
	}
	allowedPublication, err := p.attemptPublication(ctx, source, databaseName, attemptID)
	if err != nil {
		return err
	}
	for _, object := range publicationObjects {
		if allowedPublication != "" && object == "publication:"+allowedPublication {
			continue
		}
		return fmt.Errorf("%w: database %q contains unmanaged %s", mvutypes.ErrUpgradeFlowFailed, databaseName, object)
	}
	return nil
}

func checkDatabaseExtensions(ctx context.Context, conn pginfra.Conn, database declaredDatabase) error {
	extensions, err := queryStrings(ctx, conn, `SELECT extname FROM pg_extension ORDER BY extname`)
	if err != nil {
		return fmt.Errorf("listing extensions for %q: %w", database.name, err)
	}
	allowed := map[string]struct{}{"plpgsql": {}}
	for _, extension := range database.definition.Extensions {
		allowed[extension] = struct{}{}
	}
	for _, extension := range extensions {
		if _, ok := allowed[extension]; !ok {
			return fmt.Errorf("%w: extension %q in database %q is not declared by the managed database", mvutypes.ErrUpgradeFlowFailed, extension, database.name)
		}
		if requiresBackgroundWorker(extension) {
			return fmt.Errorf("%w: extension %q in database %q requires background-worker or shared-preload qualification", mvutypes.ErrUpgradeFlowFailed, extension, database.name)
		}
	}
	return nil
}

func checkUnloggedTables(ctx context.Context, conn pginfra.Conn, databaseName string) error {
	tables, err := queryStrings(ctx, conn, `
SELECT format('%I.%I', n.nspname, c.relname)
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relpersistence = 'u' AND c.relkind IN ('r', 'p')
ORDER BY n.nspname, c.relname`)
	if err != nil {
		return fmt.Errorf("listing unlogged tables for %q: %w", databaseName, err)
	}
	for _, table := range tables {
		var nonempty bool
		if err := queryOne(ctx, conn, &nonempty, "SELECT EXISTS (SELECT 1 FROM "+table+")"); err != nil {
			return fmt.Errorf("checking unlogged table %s in %q: %w", table, databaseName, err)
		}
		if nonempty {
			return fmt.Errorf("%w: database %q contains nonempty unlogged table %s", mvutypes.ErrUpgradeFlowFailed, databaseName, table)
		}
	}
	return nil
}

// attemptPublication returns the only publication name the SQL inventory may
// accept on retry. A name alone is not sufficient evidence: it must belong to
// this attempt's CNPG Publication resource.
func (p *postgresSourcePreflight) attemptPublication(ctx context.Context, source *cnpgv1.Cluster, databaseName, attemptID string) (string, error) {
	name := publicationName(p.logicalName, databaseName)
	publication := &cnpgv1.Publication{}
	if source == nil {
		return "", fmt.Errorf("%w: source CNPG Cluster is unavailable", mvutypes.ErrUpgradeFlowFailed)
	}
	err := p.client.Get(ctx, types.NamespacedName{Namespace: source.Namespace, Name: name}, publication)
	if apierrors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading source publication %q: %w", name, err)
	}
	if !publicationMatchesAttempt(publication, p.owner, source.Name, databaseName, attemptID) {
		return "", fmt.Errorf("%w: publication %q does not belong to this blue/green attempt", mvutypes.ErrUpgradeFlowFailed, name)
	}
	return name, nil
}

func requiresBackgroundWorker(extension string) bool {
	switch extension {
	case "pg_partman", "pg_cron", "pglogical", "pgactive":
		return true
	default:
		return false
	}
}

func (p *postgresSourcePreflight) EnsureReplicationRole(ctx context.Context, source *cnpgv1.Cluster, databases []declaredDatabase, role, password string) error {
	conn, err := p.connectSource(ctx, source, "postgres")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if err := ensureReplicationRole(ctx, conn, role, password); err != nil {
		return err
	}
	quotedRole := pginfra.QuoteIdentifier(role)
	for _, database := range databases {
		if err := p.grantDatabaseAccess(ctx, source, conn, database.name, quotedRole); err != nil {
			return err
		}
	}
	return nil
}

func (p *postgresSourcePreflight) grantDatabaseAccess(ctx context.Context, source *cnpgv1.Cluster, conn pginfra.Conn, databaseName, quotedRole string) error {
	if _, err := conn.Exec(ctx, fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", pginfra.QuoteIdentifier(databaseName), quotedRole)); err != nil {
		return fmt.Errorf("granting replication role database access for %q: %w", databaseName, err)
	}
	databaseConn, err := p.connectSource(ctx, source, databaseName)
	if err != nil {
		return err
	}
	defer databaseConn.Close(ctx)
	schemas, err := queryStrings(ctx, databaseConn, `SELECT nspname FROM pg_namespace WHERE nspname NOT IN ('pg_catalog', 'information_schema') AND nspname NOT LIKE 'pg_toast%' ORDER BY nspname`)
	if err != nil {
		return err
	}
	for _, schema := range schemas {
		if err := grantSchemaAccess(ctx, databaseConn, databaseName, schema, quotedRole); err != nil {
			return err
		}
	}
	return nil
}

func grantSchemaAccess(ctx context.Context, conn pginfra.Conn, databaseName, schema, quotedRole string) error {
	quotedSchema := pginfra.QuoteIdentifier(schema)
	if _, err := conn.Exec(ctx, fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s", quotedSchema, quotedRole)); err != nil {
		return fmt.Errorf("granting replication role schema access for %q/%q: %w", databaseName, schema, err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf("GRANT SELECT ON ALL TABLES IN SCHEMA %s TO %s", quotedSchema, quotedRole)); err != nil {
		return fmt.Errorf("granting replication role publication access for %q/%q: %w", databaseName, schema, err)
	}
	return nil
}

func ensureReplicationRole(ctx context.Context, conn pginfra.Conn, role, password string) error {
	quotedRole := pginfra.QuoteIdentifier(role)
	quotedPassword := quoteSQLStringLiteral(password)
	if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE ROLE %s LOGIN REPLICATION PASSWORD %s", quotedRole, quotedPassword)); err != nil {
		// ALTER below makes retry safe. SQLSTATE 42710 is duplicate_object.
		if !isDuplicateRole(err) {
			return fmt.Errorf("creating attempt replication role: %w", err)
		}
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER ROLE %s LOGIN REPLICATION PASSWORD %s", quotedRole, quotedPassword)); err != nil {
		return fmt.Errorf("updating attempt replication role: %w", err)
	}
	return nil
}

// Role options do not accept bind parameters. Use an escape string literal so
// quotes and backslashes remain safe regardless of standard_conforming_strings.
func quoteSQLStringLiteral(value string) string {
	return "E'" + strings.NewReplacer(`\`, `\\`, `'`, `''`).Replace(value) + "'"
}

func (p *postgresSourcePreflight) connectSource(ctx context.Context, source *cnpgv1.Cluster, database string) (pginfra.Conn, error) {
	if p == nil || p.client == nil || p.connect == nil {
		return nil, fmt.Errorf("%w: source preflight wiring is incomplete", mvutypes.ErrUpgradeFlowFailed)
	}
	secret := &corev1.Secret{}
	if err := p.client.Get(ctx, types.NamespacedName{Name: source.GetSuperuserSecretName(), Namespace: source.Namespace}, secret); err != nil {
		return nil, fmt.Errorf("reading source superuser Secret: %w", err)
	}
	password, ok := secret.Data["password"]
	if !ok || len(password) == 0 {
		return nil, fmt.Errorf("%w: source superuser Secret %q has no password", mvutypes.ErrUpgradeFlowFailed, secret.Name)
	}
	host, err := pgconninfo.ServiceFQDN(source.GetServiceReadWriteName(), source.Namespace)
	if err != nil {
		return nil, fmt.Errorf("resolving source write service: %w", err)
	}
	serverCA, err := clusterServerCA(ctx, p.client, source)
	if err != nil {
		return nil, err
	}
	conn, err := p.connect(ctx, host, database, string(password), host, serverCA)
	if err != nil {
		return nil, fmt.Errorf("connecting to source database %q: %w", database, err)
	}
	return conn, nil
}

func queryOne(ctx context.Context, conn pginfra.Conn, dest any, sql string, args ...any) error {
	rows, err := conn.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return fmt.Errorf("query returned no rows")
	}
	if err := rows.Scan(dest); err != nil {
		return err
	}
	return rows.Err()
}

func queryStrings(ctx context.Context, conn pginfra.Conn, sql string, args ...any) ([]string, error) {
	rows, err := conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func declaredRoleNames(databases []declaredDatabase, attemptID string) map[string]struct{} {
	roles := make(map[string]struct{})
	if attemptID != "" {
		roles[replicationRoleName(attemptID)] = struct{}{}
	}
	for _, database := range databases {
		definition := database.definition
		definition.Name = database.name
		effectiveRoles := databasecore.EffectiveRoleNames(definition)
		roles[effectiveRoles.Admin] = struct{}{}
		roles[effectiveRoles.RW] = struct{}{}
		for _, info := range database.parent.Status.Databases {
			if info.Name != database.name {
				continue
			}
			for _, role := range info.Roles {
				if role.Exists {
					roles[role.Name] = struct{}{}
				}
			}
		}
	}
	return roles
}

// The source remains writable during preparation, so catalog fingerprints are
// the durable guard against direct SQL DDL/DCL changes that admission cannot
// observe. Hash each object definition before aggregation to keep the final
// value bounded even for a large customer schema. PostgreSQL 18 additionally
// represents NOT NULL as a constraint; attnotnull already records that fact.
const schemaFingerprintSQL = `
SELECT encode(sha256(convert_to(COALESCE(string_agg(encode(sha256(convert_to(definition, 'UTF8')), 'hex'), '' ORDER BY definition), ''), 'UTF8')), 'hex')
FROM (
  SELECT format('extension:%I:%s', extname, extversion) AS definition FROM pg_extension
  UNION ALL
  SELECT format('schema:%I:%s', nspname, pg_get_userbyid(nspowner)) FROM pg_namespace
    WHERE nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND nspname NOT LIKE 'pg_toast%'
  UNION ALL
  SELECT format('relation:%I.%I:%s:%s:%s:%s:%s:%s:%s:%s', n.nspname, c.relname, c.relkind, pg_get_userbyid(c.relowner), c.relpersistence,
    c.relreplident, c.relrowsecurity, c.relforcerowsecurity, c.relispartition,
    CASE WHEN c.relkind IN ('v', 'm') THEN COALESCE(pg_get_viewdef(c.oid, true), '') ELSE '' END)
    FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND n.nspname NOT LIKE 'pg_toast%'
  UNION ALL
  SELECT format('inheritance:%I.%I:%I.%I:%s:%s', child_ns.nspname, child.relname, parent_ns.nspname, parent.relname,
    child.relispartition, COALESCE(pg_get_expr(child.relpartbound, child.oid), ''))
    FROM pg_inherits inheritance
    JOIN pg_class child ON child.oid = inheritance.inhrelid
    JOIN pg_namespace child_ns ON child_ns.oid = child.relnamespace
    JOIN pg_class parent ON parent.oid = inheritance.inhparent
    JOIN pg_namespace parent_ns ON parent_ns.oid = parent.relnamespace
    WHERE child_ns.nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND child_ns.nspname NOT LIKE 'pg_toast%'
  UNION ALL
  SELECT format('column:%I.%I.%I:%s:%s:%s:%s', n.nspname, c.relname, a.attname,
    format_type(a.atttypid, a.atttypmod), a.attnotnull, a.attidentity, COALESCE(pg_get_expr(d.adbin, d.adrelid), ''))
    FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
    LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
    WHERE a.attnum > 0 AND NOT a.attisdropped AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND n.nspname NOT LIKE 'pg_toast%'
  UNION ALL
  SELECT format('constraint:%I.%I:%I:%s', n.nspname, c.relname, con.conname, pg_get_constraintdef(con.oid, true))
    FROM pg_constraint con JOIN pg_class c ON c.oid = con.conrelid JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE con.contype <> 'n' AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND n.nspname NOT LIKE 'pg_toast%'
  UNION ALL
  SELECT format('index:%I.%I:%s', n.nspname, indexClass.relname, pg_get_indexdef(i.indexrelid))
    FROM pg_index i JOIN pg_class c ON c.oid = i.indrelid JOIN pg_namespace n ON n.oid = c.relnamespace
    JOIN pg_class indexClass ON indexClass.oid = i.indexrelid
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND n.nspname NOT LIKE 'pg_toast%'
  UNION ALL
  SELECT format('sequence:%I.%I:%s:%s:%s:%s:%s:%s:%s', n.nspname, c.relname, seq.seqstart, seq.seqincrement,
    seq.seqmax, seq.seqmin, seq.seqcache, seq.seqcycle, format_type(seq.seqtypid, NULL))
    FROM pg_sequence seq JOIN pg_class c ON c.oid = seq.seqrelid JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND n.nspname NOT LIKE 'pg_toast%'
  UNION ALL
  SELECT format('function:%I.%I(%s):%s:%s', n.nspname, p.proname, pg_get_function_identity_arguments(p.oid), pg_get_userbyid(p.proowner), pg_get_functiondef(p.oid))
    FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND n.nspname NOT LIKE 'pg_toast%' AND p.prokind IN ('f', 'p')
  UNION ALL
  SELECT format('trigger:%I.%I:%I:%s', n.nspname, c.relname, t.tgname, pg_get_triggerdef(t.oid, true))
    FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid JOIN pg_namespace n ON n.oid = c.relnamespace
    JOIN pg_proc trigger_function ON trigger_function.oid = t.tgfoid
    JOIN pg_namespace trigger_function_schema ON trigger_function_schema.oid = trigger_function.pronamespace
    WHERE NOT t.tgisinternal AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND n.nspname NOT LIKE 'pg_toast%'
      AND NOT (t.tgname = $1 AND trigger_function_schema.nspname = $2 AND trigger_function.proname = $3)
  UNION ALL
  SELECT format('type:%I.%I:%s:%s:%s:%s', n.nspname, t.typname, t.typtype, t.typcategory, t.typnotnull, COALESCE(t.typdefault, ''))
    FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND n.nspname NOT LIKE 'pg_toast%'
  UNION ALL
  SELECT format('enum:%I.%I:%s:%s', n.nspname, t.typname, e.enumsortorder, e.enumlabel)
    FROM pg_enum e JOIN pg_type t ON t.oid = e.enumtypid JOIN pg_namespace n ON n.oid = t.typnamespace
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND n.nspname NOT LIKE 'pg_toast%'
  UNION ALL
  SELECT format('rule:%I.%I:%I:%s', n.nspname, c.relname, r.rulename, pg_get_ruledef(r.oid, true))
    FROM pg_rewrite r JOIN pg_class c ON c.oid = r.ev_class JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND n.nspname NOT LIKE 'pg_toast%'
  UNION ALL
  SELECT format('policy:%I.%I:%I:%s:%s:%s:%s:%s', n.nspname, c.relname, pol.polname,
    pol.polcmd, pol.polpermissive,
    array_to_string(ARRAY(SELECT CASE WHEN roleid = 0 THEN 'PUBLIC' ELSE quote_ident(pg_get_userbyid(roleid)) END
      FROM unnest(pol.polroles) AS policy_roles(roleid) ORDER BY 1), ','),
    COALESCE(pg_get_expr(pol.polqual, pol.polrelid), ''), COALESCE(pg_get_expr(pol.polwithcheck, pol.polrelid), ''))
    FROM pg_policy pol JOIN pg_class c ON c.oid = pol.polrelid JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND n.nspname NOT LIKE 'pg_toast%'
) definitions`

// Compare effective ACL entries so a NULL ACL and the explicit defaults left
// by GRANT hash identically. Only this attempt's expected replication grants
// are excluded; other privilege changes still invalidate the baseline.
const accessFingerprintSQL = `
WITH attempt_role AS (
  SELECT (SELECT oid FROM pg_roles WHERE rolname = $1) AS oid
)
SELECT encode(sha256(convert_to(COALESCE(string_agg(encode(sha256(convert_to(definition, 'UTF8')), 'hex'), '' ORDER BY definition), ''), 'UTF8')), 'hex')
FROM (
  SELECT format('database:%I:%s', datname, pg_get_userbyid(datdba)) AS definition
    FROM pg_database WHERE datname = current_database()
  UNION ALL
  SELECT format('database-acl:%I:%s:%s:%s:%s', d.datname, pg_get_userbyid(acl.grantor),
    CASE WHEN acl.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(acl.grantee) END, acl.privilege_type, acl.is_grantable)
    FROM pg_database d CROSS JOIN LATERAL aclexplode(COALESCE(d.datacl, acldefault('d', d.datdba))) acl
    WHERE d.datname = current_database()
      AND NOT (acl.grantee IS NOT DISTINCT FROM (SELECT oid FROM attempt_role) AND acl.privilege_type = 'CONNECT' AND NOT acl.is_grantable)
  UNION ALL
  SELECT format('role:%I:%s:%s:%s:%s:%s:%s:%s:%s', rolname, rolsuper, rolinherit, rolcreaterole, rolcreatedb,
    rolcanlogin, rolreplication, rolbypassrls, rolconnlimit) FROM pg_roles
    WHERE rolname <> $1 AND left(rolname, 3) <> 'pg_'
      AND rolname NOT IN ('streaming_replica', 'cnpg_pooler_pgbouncer')
  UNION ALL
  SELECT format('attempt-role-drift:%I:%s:%s:%s:%s:%s:%s:%s:%s', rolname, rolsuper, rolinherit, rolcreaterole, rolcreatedb,
    rolcanlogin, rolreplication, rolbypassrls, rolconnlimit) FROM pg_roles
    WHERE rolname = $1 AND (rolsuper OR NOT rolinherit OR rolcreaterole OR rolcreatedb
      OR NOT rolcanlogin OR NOT rolreplication OR rolbypassrls OR rolconnlimit <> -1)
  UNION ALL
  SELECT format('membership:%s:%s:%s:%s:%s', role.rolname, member.rolname, membership.admin_option, membership.inherit_option, membership.set_option)
    FROM pg_auth_members membership
    JOIN pg_roles role ON role.oid = membership.roleid
    JOIN pg_roles member ON member.oid = membership.member
    WHERE (left(role.rolname, 3) <> 'pg_' OR left(member.rolname, 3) <> 'pg_')
      AND role.rolname NOT IN ('streaming_replica', 'cnpg_pooler_pgbouncer')
      AND member.rolname NOT IN ('streaming_replica', 'cnpg_pooler_pgbouncer')
  UNION ALL
  SELECT format('schema-acl:%I:%s:%s:%s:%s', n.nspname, pg_get_userbyid(acl.grantor),
    CASE WHEN acl.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(acl.grantee) END, acl.privilege_type, acl.is_grantable)
    FROM pg_namespace n CROSS JOIN LATERAL aclexplode(COALESCE(n.nspacl, acldefault('n', n.nspowner))) acl
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND n.nspname NOT LIKE 'pg_toast%'
      AND NOT (acl.grantee IS NOT DISTINCT FROM (SELECT oid FROM attempt_role) AND acl.privilege_type = 'USAGE' AND NOT acl.is_grantable)
  UNION ALL
  SELECT format('relation-acl:%I.%I:%s:%s:%s:%s', n.nspname, c.relname, pg_get_userbyid(acl.grantor),
    CASE WHEN acl.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(acl.grantee) END, acl.privilege_type, acl.is_grantable)
    FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
    CROSS JOIN LATERAL aclexplode(COALESCE(c.relacl, acldefault((CASE WHEN c.relkind = 'S' THEN 'S' ELSE 'r' END)::"char", c.relowner))) acl
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND n.nspname NOT LIKE 'pg_toast%'
      AND c.relkind IN ('r', 'p', 'v', 'm', 'f', 'S')
      AND NOT (c.relkind <> 'S' AND acl.grantee IS NOT DISTINCT FROM (SELECT oid FROM attempt_role)
        AND acl.privilege_type = 'SELECT' AND NOT acl.is_grantable)
  UNION ALL
  SELECT format('column-acl:%I.%I.%I:%s:%s:%s:%s', n.nspname, c.relname, a.attname,
    pg_get_userbyid(acl.grantor), CASE WHEN acl.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(acl.grantee) END,
    acl.privilege_type, acl.is_grantable)
    FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
    CROSS JOIN LATERAL aclexplode(a.attacl) acl
    WHERE a.attnum > 0 AND NOT a.attisdropped
      AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND n.nspname NOT LIKE 'pg_toast%'
  UNION ALL
  SELECT format('default-acl:%s:%s:%s:%s', pg_get_userbyid(defaclrole), COALESCE(namespace.nspname, ''), defaclobjtype,
    CASE WHEN defaclacl IS NULL THEN '<default>' ELSE COALESCE((SELECT string_agg(item::text, ',' ORDER BY item::text) FROM unnest(defaclacl) AS acl(item)), '') END)
    FROM pg_default_acl acl LEFT JOIN pg_namespace namespace ON namespace.oid = acl.defaclnamespace
  UNION ALL
  SELECT format('function-acl:%I.%I(%s):%s', n.nspname, p.proname, pg_get_function_identity_arguments(p.oid),
    CASE WHEN p.proacl IS NULL THEN '<default>' ELSE COALESCE((SELECT string_agg(item::text, ',' ORDER BY item::text) FROM unnest(p.proacl) AS acl(item)), '') END)
    FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND n.nspname NOT LIKE 'pg_toast%'
) definitions`

func isDuplicateRole(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42710"
}
