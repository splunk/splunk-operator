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

import "strings"

// Each variant selects explicit SQL contributions. No query depends on finding
// and rewriting text inside another variant.
type accessFingerprintOptions struct {
	side    environmentSide
	cutover bool
}

var accessFingerprintSQL = buildAccessFingerprintSQL(accessFingerprintOptions{side: sideBlue})
var greenAccessFingerprintSQL = buildAccessFingerprintSQL(accessFingerprintOptions{side: sideGreen})
var blueCutoverAccessFingerprintSQL = buildAccessFingerprintSQL(accessFingerprintOptions{side: sideBlue, cutover: true})
var greenCutoverAccessFingerprintSQL = buildAccessFingerprintSQL(accessFingerprintOptions{side: sideGreen, cutover: true})

func cutoverAccessFingerprintSQL(side environmentSide) string {
	if side == sideBlue {
		return blueCutoverAccessFingerprintSQL
	}
	return greenCutoverAccessFingerprintSQL
}

func buildAccessFingerprintSQL(options accessFingerprintOptions) string {
	controlRole := ""
	loginProjection := "rolcanlogin"
	grantRole := fingerprintRoleGrantSQL
	roleFilter := fingerprintRoleFilterSQL
	membershipFilter := fingerprintMembershipFilterSQL
	relationKinds := fingerprintRelationKindsSQL
	publisherDrift := sourcePublisherRoleDrift
	if options.side == sideGreen {
		publisherDrift = greenPublisherRoleDrift
	}
	if options.cutover {
		controlRole = fingerprintControlRoleSQL
		loginProjection = fingerprintOriginalLoginSQL
		grantRole = fingerprintControlGrantSQL
		roleFilter = fingerprintCutoverRoleFilterSQL
		membershipFilter += fingerprintControlMembershipSQL
		relationKinds += fingerprintControlSequenceGrantSQL
	}
	definitions := []string{
		fingerprintDatabaseSQL,
		fingerprintDatabaseACLSQLStart + grantRole + fingerprintDatabaseACLSQLEnd,
		fingerprintRolesSQLStart + loginProjection + `, rolreplication, rolbypassrls, rolconnlimit) FROM pg_roles
    ` + roleFilter + fingerprintRolesSQLEnd,
		fingerprintPublisherDriftSQLStart + loginProjection + `, rolreplication, rolbypassrls, rolconnlimit) FROM pg_roles` + fingerprintPublisherDriftSQLMiddle + publisherDrift + fingerprintPublisherDriftSQLEnd,
	}
	if options.side == sideGreen {
		definitions = append(definitions, fingerprintMissingPublisherSQL)
	}
	definitions = append(definitions,
		fingerprintMembershipSQLStart+membershipFilter,
		fingerprintSchemaACLSQLStart+grantRole+fingerprintSchemaACLSQLEnd,
		fingerprintRelationACLSQLStart+relationKinds+fingerprintRelationACLSQLMiddle+grantRole+fingerprintRelationACLSQLEnd,
		fingerprintColumnACLSQL,
		fingerprintDefaultACLSQL,
		fingerprintFunctionACLSQL,
	)
	return "\nWITH " + controlRole + fingerprintAttemptRoleSQL + fingerprintAggregateSQL + strings.Join(definitions, "\n  UNION ALL\n") + "\n) definitions"
}

const fingerprintAttemptRoleSQL = `attempt_role AS (
  SELECT (SELECT oid FROM pg_roles WHERE rolname = $1) AS oid
)
`

const fingerprintControlRoleSQL = `control_role AS (SELECT (SELECT oid FROM pg_roles WHERE rolname=$2) AS oid), original_logins AS (SELECT * FROM jsonb_to_recordset($3::jsonb) AS r("roleName" text, login boolean)), `

const fingerprintRoleGrantSQL = `acl.grantee IS NOT DISTINCT FROM (SELECT oid FROM attempt_role)`

const fingerprintControlGrantSQL = `(acl.grantee IS NOT DISTINCT FROM (SELECT oid FROM attempt_role) OR acl.grantee IS NOT DISTINCT FROM (SELECT oid FROM control_role))`

const fingerprintOriginalLoginSQL = `COALESCE((SELECT login FROM original_logins WHERE "roleName"=rolname),rolcanlogin)`

const fingerprintRoleFilterSQL = `WHERE rolname <> $1 AND left(rolname, 3)`

const fingerprintCutoverRoleFilterSQL = `WHERE rolname <> $1 AND rolname <> $2 AND left(rolname, 3)`

const fingerprintMembershipFilterSQL = `AND member.rolname NOT IN ('streaming_replica', 'cnpg_pooler_pgbouncer')`

const fingerprintControlMembershipSQL = ` AND member.rolname<>$2 AND role.rolname<>$2`

const fingerprintRelationKindsSQL = `AND c.relkind IN ('r', 'p', 'v', 'm', 'f', 'S')`

const fingerprintControlSequenceGrantSQL = ` AND NOT (acl.grantee IS NOT DISTINCT FROM (SELECT oid FROM control_role) AND c.relkind='S' AND acl.privilege_type IN ('SELECT','UPDATE') AND NOT acl.is_grantable)`

const fingerprintMissingPublisherSQL = `  SELECT format('attempt-role-missing:%I', $1) WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`

const sourcePublisherRoleDrift = `rolsuper OR NOT rolinherit OR rolcreaterole OR rolcreatedb
      OR NOT rolcanlogin OR NOT rolreplication OR rolbypassrls OR rolconnlimit <> -1`

const greenPublisherRoleDrift = `rolsuper OR NOT rolinherit OR rolcreaterole OR rolcreatedb
      OR rolcanlogin OR rolreplication OR rolbypassrls OR rolconnlimit <> -1`

const fingerprintAggregateSQL = `SELECT encode(sha256(convert_to(COALESCE(string_agg(encode(sha256(convert_to(definition, 'UTF8')), 'hex'), '' ORDER BY definition), ''), 'UTF8')), 'hex')
FROM (
`

const fingerprintDatabaseSQL = `  SELECT format('database:%I:%s', datname, pg_get_userbyid(datdba)) AS definition
    FROM pg_database WHERE datname = current_database()`

const fingerprintDatabaseACLSQLStart = `  SELECT format('database-acl:%I:%s:%s:%s:%s', d.datname, pg_get_userbyid(acl.grantor),
    CASE WHEN acl.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(acl.grantee) END, acl.privilege_type, acl.is_grantable)
    FROM pg_database d CROSS JOIN LATERAL aclexplode(COALESCE(d.datacl, acldefault('d', d.datdba))) acl
    WHERE d.datname = current_database()
      AND NOT (`

const fingerprintDatabaseACLSQLEnd = ` AND acl.privilege_type = 'CONNECT' AND NOT acl.is_grantable)`

const fingerprintRolesSQLStart = `  SELECT format('role:%I:%s:%s:%s:%s:%s:%s:%s:%s', rolname, rolsuper, rolinherit, rolcreaterole, rolcreatedb,
    `

const fingerprintRolesSQLEnd = ` <> 'pg_'
      AND rolname NOT IN ('streaming_replica', 'cnpg_pooler_pgbouncer')`

const fingerprintPublisherDriftSQLStart = `  SELECT format('attempt-role-drift:%I:%s:%s:%s:%s:%s:%s:%s:%s', rolname, rolsuper, rolinherit, rolcreaterole, rolcreatedb,
    `

const fingerprintPublisherDriftSQLMiddle = `
    WHERE rolname = $1 AND (`

const fingerprintPublisherDriftSQLEnd = `)`

const fingerprintMembershipSQLStart = `  SELECT format('membership:%s:%s:%s:%s:%s', role.rolname, member.rolname, membership.admin_option, membership.inherit_option, membership.set_option)
    FROM pg_auth_members membership
    JOIN pg_roles role ON role.oid = membership.roleid
    JOIN pg_roles member ON member.oid = membership.member
    WHERE (left(role.rolname, 3) <> 'pg_' OR left(member.rolname, 3) <> 'pg_')
      AND role.rolname NOT IN ('streaming_replica', 'cnpg_pooler_pgbouncer')
      `

const fingerprintSchemaACLSQLStart = `  SELECT format('schema-acl:%I:%s:%s:%s:%s', n.nspname, pg_get_userbyid(acl.grantor),
    CASE WHEN acl.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(acl.grantee) END, acl.privilege_type, acl.is_grantable)
    FROM pg_namespace n CROSS JOIN LATERAL aclexplode(COALESCE(n.nspacl, acldefault('n', n.nspowner))) acl
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND n.nspname NOT LIKE 'pg_toast%'
      AND NOT (`

const fingerprintSchemaACLSQLEnd = ` AND acl.privilege_type = 'USAGE' AND NOT acl.is_grantable)`

const fingerprintRelationACLSQLStart = `  SELECT format('relation-acl:%I.%I:%s:%s:%s:%s', n.nspname, c.relname, pg_get_userbyid(acl.grantor),
    CASE WHEN acl.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(acl.grantee) END, acl.privilege_type, acl.is_grantable)
    FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
    CROSS JOIN LATERAL aclexplode(COALESCE(c.relacl, acldefault((CASE WHEN c.relkind = 'S' THEN 's' ELSE 'r' END)::"char", c.relowner))) acl
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND n.nspname NOT LIKE 'pg_toast%'
      `

const fingerprintRelationACLSQLMiddle = `
      AND NOT (c.relkind <> 'S' AND `

const fingerprintRelationACLSQLEnd = `
        AND acl.privilege_type = 'SELECT' AND NOT acl.is_grantable)`

const fingerprintColumnACLSQL = `  SELECT format('column-acl:%I.%I.%I:%s:%s:%s:%s', n.nspname, c.relname, a.attname,
    pg_get_userbyid(acl.grantor), CASE WHEN acl.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(acl.grantee) END,
    acl.privilege_type, acl.is_grantable)
    FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
    CROSS JOIN LATERAL aclexplode(a.attacl) acl
    WHERE a.attnum > 0 AND NOT a.attisdropped
      AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND n.nspname NOT LIKE 'pg_toast%'`

const fingerprintDefaultACLSQL = `  SELECT format('default-acl:%s:%s:%s:%s', pg_get_userbyid(defaclrole), COALESCE(namespace.nspname, ''), defaclobjtype,
    CASE WHEN defaclacl IS NULL THEN '<default>' ELSE COALESCE((SELECT string_agg(item::text, ',' ORDER BY item::text) FROM unnest(defaclacl) AS acl(item)), '') END)
    FROM pg_default_acl acl LEFT JOIN pg_namespace namespace ON namespace.oid = acl.defaclnamespace`

const fingerprintFunctionACLSQL = `  SELECT format('function-acl:%I.%I(%s):%s', n.nspname, p.proname, pg_get_function_identity_arguments(p.oid),
    CASE WHEN p.proacl IS NULL THEN '<default>' ELSE COALESCE((SELECT string_agg(item::text, ',' ORDER BY item::text) FROM unnest(p.proacl) AS acl(item)), '') END)
    FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
    WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'splunk_operator_bluegreen') AND n.nspname NOT LIKE 'pg_toast%'`
