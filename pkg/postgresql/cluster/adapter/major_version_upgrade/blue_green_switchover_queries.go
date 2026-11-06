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
	"strconv"

	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
)

type cutoverHelper struct{ name, body string }

// Helpers accept no caller-controlled SQL, identifiers, or process IDs. Their
// owner is postgres, but only the attempt identity may execute them.
func cutoverHelpers(id cutoverIdentity) []cutoverHelper {
	fenceRules := cutoverFenceRules(id.Role, replicationRoleName(id.AttemptID))
	caller := "IF session_user <> " + quoteSQLStringLiteral(id.Role) + " THEN RAISE EXCEPTION 'invalid cutover identity'; END IF;\n"
	return []cutoverHelper{
		{controlTerminateFunction, "BEGIN\n" + caller + "PERFORM pg_catalog.pg_terminate_backend(pid) FROM pg_catalog.pg_stat_activity WHERE " + cutoverSessionPredicate(id) + "; RETURN true; END;"},
		{controlRemoveGuardsFunction, "DECLARE item record; BEGIN\n" + caller + `
FOR item IN SELECT ns.nspname, rel.relname FROM pg_catalog.pg_trigger tr
JOIN pg_catalog.pg_class rel ON rel.oid=tr.tgrelid
JOIN pg_catalog.pg_namespace ns ON ns.oid=rel.relnamespace
JOIN pg_catalog.pg_proc fn ON fn.oid=tr.tgfoid
JOIN pg_catalog.pg_namespace fns ON fns.oid=fn.pronamespace
WHERE NOT tr.tgisinternal AND tr.tgname=` + quoteSQLStringLiteral(candidateGuardTrigger) + `
AND fns.nspname=` + quoteSQLStringLiteral(candidateGuardSchema) + ` AND fn.proname=` + quoteSQLStringLiteral(candidateGuardFunction) + `
LOOP EXECUTE pg_catalog.format('DROP TRIGGER %I ON %I.%I', ` + quoteSQLStringLiteral(candidateGuardTrigger) + `,item.nspname,item.relname); END LOOP;
IF pg_catalog.to_regprocedure(` + quoteSQLStringLiteral(candidateGuardSchema+"."+candidateGuardFunction+"()") + `) IS NOT NULL THEN
EXECUTE 'DROP FUNCTION ` + pginfra.QuoteIdentifier(candidateGuardSchema) + "." + pginfra.QuoteIdentifier(candidateGuardFunction) + `()'; END IF;
RETURN true; END;`},
		{controlFenceFunction, "BEGIN\n" + caller + `RETURN NOT EXISTS (SELECT 1 FROM pg_catalog.pg_hba_file_rules WHERE error IS NOT NULL)
AND (SELECT array_agg(type || ':' || array_to_string(database,',') || ':' || array_to_string(user_name,',') || ':' || address || ':' || auth_method ORDER BY rule_number)
FROM (SELECT * FROM pg_catalog.pg_hba_file_rules WHERE type LIKE 'host%'
AND user_name NOT IN (ARRAY['streaming_replica']::text[],ARRAY['cnpg_pooler_pgbouncer']::text[]) ORDER BY rule_number LIMIT ` + strconv.Itoa(len(fenceRules)) + `) rules)
= ` + fenceRuleProofSQL(fenceRules) + `; END;`},
	}
}

func cutoverSessionPredicate(id cutoverIdentity) string {
	return "pid <> pg_catalog.pg_backend_pid() AND backend_type = 'client backend' AND usename NOT IN (" +
		quoteSQLStringLiteral(id.Role) + "," + quoteSQLStringLiteral(replicationRoleName(id.AttemptID)) + ",'streaming_replica','cnpg_pooler_pgbouncer') AND (client_addr IS NOT NULL OR usename <> 'postgres')"
}

const controlRoleSQL = `SELECT COALESCE((SELECT rolcanlogin AND NOT rolsuper AND rolinherit AND NOT rolcreaterole AND NOT rolcreatedb AND NOT rolreplication AND NOT rolbypassrls AND rolconnlimit=-1 AND rolvaliduntil IS NULL FROM pg_catalog.pg_roles WHERE rolname=$1),false)`

const controlHelperProofSQL = `SELECT COALESCE((SELECT p.prosecdef AND p.prokind='f'
 AND p.prorettype='pg_catalog.bool'::regtype AND p.pronargs=0 AND l.lanname='plpgsql'
 AND pg_catalog.pg_get_userbyid(p.proowner)='postgres' AND p.prosrc=$3
 AND p.proconfig=ARRAY['search_path=pg_catalog']::text[]
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.aclexplode(COALESCE(p.proacl,pg_catalog.acldefault('f',p.proowner))) acl
 WHERE acl.grantee=0 OR acl.grantee NOT IN (p.proowner,(SELECT oid FROM pg_catalog.pg_roles WHERE rolname=$4))
 OR (acl.grantee<>p.proowner AND acl.is_grantable))
 AND pg_catalog.has_function_privilege($4,p.oid,'EXECUTE')
 FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace
 JOIN pg_catalog.pg_language l ON l.oid=p.prolang
 WHERE n.nspname=$1 AND p.proname=$2 AND p.pronargs=0),false)`

const controlSchemaProofSQL = `SELECT COALESCE((SELECT pg_catalog.pg_get_userbyid(n.nspowner)='postgres'
 AND NOT pg_catalog.has_schema_privilege($2,n.oid,'CREATE')
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.aclexplode(COALESCE(n.nspacl,pg_catalog.acldefault('n',n.nspowner))) a
 WHERE a.grantee NOT IN (n.nspowner,(SELECT oid FROM pg_catalog.pg_roles WHERE rolname=$2))
 OR (a.grantee<>n.nspowner AND (a.privilege_type<>'USAGE' OR a.is_grantable)))
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_proc p WHERE p.pronamespace=n.oid AND (p.pronargs<>0 OR p.proname<>ALL($3::text[])))
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_class c WHERE c.relnamespace=n.oid)
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_type t WHERE t.typnamespace=n.oid)
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_operator o WHERE o.oprnamespace=n.oid)
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_collation c WHERE c.collnamespace=n.oid)
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_conversion c WHERE c.connamespace=n.oid)
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_ts_config c WHERE c.cfgnamespace=n.oid)
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_ts_dict d WHERE d.dictnamespace=n.oid)
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_ts_parser p WHERE p.prsnamespace=n.oid)
 AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_ts_template t WHERE t.tmplnamespace=n.oid)
 FROM pg_catalog.pg_namespace n WHERE n.nspname=$1),false)`

// Direct grants which disappear from normalized fingerprints are checked
// against the exact object kinds used by control operations first.
const controlGrantInventorySQL = `WITH identity AS (SELECT oid FROM pg_catalog.pg_roles WHERE rolname=$1)
SELECT NOT EXISTS (
 SELECT 1 FROM pg_catalog.pg_database d CROSS JOIN LATERAL pg_catalog.aclexplode(d.datacl) a
 WHERE a.grantee=(SELECT oid FROM identity) AND (a.privilege_type<>'CONNECT' OR a.is_grantable OR d.datistemplate)
 UNION ALL
 SELECT 1 FROM pg_catalog.pg_namespace n CROSS JOIN LATERAL pg_catalog.aclexplode(n.nspacl) a
 WHERE a.grantee=(SELECT oid FROM identity) AND (a.privilege_type<>'USAGE' OR a.is_grantable OR (n.nspname<>$2 AND NOT EXISTS
  (SELECT 1 FROM pg_catalog.pg_class c WHERE c.relnamespace=n.oid AND (c.relkind='S' OR (c.relkind IN ('r','p') AND c.relpersistence='u')))))
 UNION ALL
 SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace CROSS JOIN LATERAL pg_catalog.aclexplode(c.relacl) a
 WHERE a.grantee=(SELECT oid FROM identity) AND
 (n.nspname IN ('pg_catalog','information_schema') OR a.is_grantable OR NOT ((c.relkind='S' AND a.privilege_type IN ('SELECT','UPDATE')) OR (c.relkind IN ('r','p') AND c.relpersistence='u' AND a.privilege_type='SELECT')))
 UNION ALL
 SELECT 1 FROM pg_catalog.pg_attribute att CROSS JOIN LATERAL pg_catalog.aclexplode(att.attacl) a
 JOIN pg_catalog.pg_class c ON c.oid=att.attrelid JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
 WHERE a.grantee=(SELECT oid FROM identity) AND
 (n.nspname<>'pg_catalog' OR c.relname<>'pg_subscription' OR a.privilege_type<>'SELECT' OR a.is_grantable
 OR att.attname NOT IN ('oid','subname','subenabled','subfailover','subslotname','subdbid'))
 UNION ALL
 SELECT 1 FROM pg_catalog.pg_default_acl d CROSS JOIN LATERAL pg_catalog.aclexplode(d.defaclacl) a WHERE a.grantee=(SELECT oid FROM identity)
 UNION ALL
 SELECT 1 FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace CROSS JOIN LATERAL pg_catalog.aclexplode(p.proacl) a
 WHERE a.grantee=(SELECT oid FROM identity) AND n.nspname<>$2
)`

const controlGuardFunctionExistsSQL = `SELECT pg_catalog.to_regprocedure($1) IS NOT NULL`

const controlObjectInventorySQL = `SELECT ns.nspname,c.relname,c.relkind='S' FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace ns ON ns.oid=c.relnamespace WHERE (c.relkind='S' OR (c.relkind IN ('r','p') AND c.relpersistence='u')) AND ns.nspname NOT IN ('pg_catalog','information_schema') AND ns.nspname NOT LIKE 'pg_toast%' ORDER BY 1,2`

const controlSequenceGrantsSQL = `SELECT NOT EXISTS (
 SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
 WHERE c.relkind='S' AND n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%'
 AND (NOT EXISTS (SELECT 1 FROM pg_catalog.aclexplode(c.relacl) a WHERE a.grantee=(SELECT oid FROM pg_catalog.pg_roles WHERE rolname=$1) AND a.privilege_type='SELECT' AND NOT a.is_grantable)
 OR EXISTS (SELECT 1 FROM pg_catalog.aclexplode(c.relacl) a WHERE a.grantee=(SELECT oid FROM pg_catalog.pg_roles WHERE rolname=$1) AND a.privilege_type='UPDATE') <> $2)
)`

const controlMembershipProofSQL = `SELECT NOT EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m JOIN pg_catalog.pg_roles r ON r.oid=m.roleid JOIN pg_catalog.pg_roles member ON member.oid=m.member WHERE (member.rolname=$1 AND (r.rolname<>'pg_read_all_stats' OR m.admin_option OR NOT m.inherit_option OR NOT m.set_option)) OR r.rolname=$1) AND pg_catalog.pg_has_role($1,'pg_read_all_stats','MEMBER')`

const cutoverRoleFenceSQL = `SELECT count(*)=cardinality($1::text[]) AND COALESCE(bool_and(NOT rolcanlogin),true) FROM pg_catalog.pg_roles WHERE rolname=ANY($1::text[])`

const cutoverPrimaryWritableSQL = `SELECT NOT pg_catalog.pg_is_in_recovery() AND current_setting('transaction_read_only')='off'`

const cutoverRoleLoginsRestoredSQL = `SELECT NOT EXISTS (SELECT 1 FROM jsonb_to_recordset($1::jsonb) AS original("roleName" text,login boolean) LEFT JOIN pg_catalog.pg_roles r ON r.rolname=original."roleName" WHERE r.oid IS NULL OR r.rolcanlogin<>original.login)`

const cutoverWriteProbeCreateSQL = "CREATE TEMP TABLE pg_temp.splunk_operator_cutover_probe (value boolean NOT NULL) ON COMMIT DROP"

const cutoverWriteProbeVerifySQL = "SELECT COALESCE(bool_and(value),false) FROM pg_temp.splunk_operator_cutover_probe"
