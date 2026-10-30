#!/usr/bin/env bash
# Runs only inside the real splunk container. Never trace credential reads.
set -euo pipefail
umask 077

fail() {
    printf '%s\n' "$1" >&2
    exit 1
}

home="${SPLUNK_HOME:?SPLUNK_HOME is required}"
database="${1:?database argument is required}"
root="$home/var/run/supervisor/postgresok"
metadata="$root/$database"
psql="$home/bin/psql"
[[ -x "$psql" ]] || fail "The Splunk test image must contain SPLUNK_HOME/bin/psql."
[[ "${SPLUNK_NODE_SIDECAR_POSTGRES_DISABLED:-}" == true ]] ||
    fail "The legacy Postgres sidecar must be disabled."

for key in CLUSTER_RW_ENDPOINT DEFAULT_CLUSTER_PORT DATABASE_NAME ADMIN_USER_NAME RW_USER_NAME; do
    [[ -s "$metadata/$key" ]] || fail "A required mounted connection field is missing."
done
for file in "$database-admin" "$database-rw" "$database-ca.crt"; do
    [[ -s "$root/$file" ]] || fail "A required mounted credential or CA file is missing."
done

unset PGPASSWORD PGHOSTADDR PGSERVICE PGSERVICEFILE PGOPTIONS PGSSLCERT PGSSLKEY
PGHOST="$(< "$metadata/CLUSTER_RW_ENDPOINT")"
PGPORT="$(< "$metadata/DEFAULT_CLUSTER_PORT")"
PGDATABASE="$(< "$metadata/DATABASE_NAME")"
export PGHOST PGPORT PGDATABASE
export LC_ALL=C
export PGSSLMODE=verify-full
export PGSSLROOTCERT="$root/$database-ca.crt"
export PGCONNECT_TIMEOUT=10
export LD_LIBRARY_PATH="$home/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
[[ "$PGDATABASE" == "$database" ]] || fail "Mounted database name differs from the requested database."
case "$PGHOST" in
    localhost|127.*|::1) fail "Mounted endpoint must not be localhost." ;;
esac

temporary="$(mktemp -d)"
trap 'rm -rf -- "$temporary"' EXIT
export PGPASSFILE="$temporary/pgpass"

# PGPASSFILE keeps passwords out of the exec argv, psql argv, and output.
# Escape the two special characters required by libpq's password-file format.
pgpass_escape() {
    local value="$1"
    value="${value//\\/\\\\}"
    printf '%s' "${value//:/\\:}"
}
use_role() {
    local username="$1" password_file="$2"
    export PGUSER="$username"
    {
        printf '*:'
        pgpass_escape "$PGPORT"
        printf ':'
        pgpass_escape "$PGDATABASE"
        printf ':'
        pgpass_escape "$PGUSER"
        printf ':'
        pgpass_escape "$(< "$password_file")"
        printf '\n'
    } > "$PGPASSFILE"
}

options=(--no-password --no-psqlrc --quiet --no-align --tuples-only --set=ON_ERROR_STOP=1)
sql() {
    "$psql" "${options[@]}" "$@" 2>"$temporary/psql.err" ||
        fail "PostgreSQL operation using mounted connection details failed."
}

admin="$(< "$metadata/ADMIN_USER_NAME")"
rw="$(< "$metadata/RW_USER_NAME")"
use_role "$admin" "$root/$database-admin"
# The admin owns the table; PostgreSOK's default privileges grant subsequent
# tables' CRUD permissions to the generated RW role.
sql --command='CREATE TABLE IF NOT EXISTS public.postgresok_mount_probe (id integer PRIMARY KEY, value text NOT NULL); TRUNCATE public.postgresok_mount_probe;'

use_role "$rw" "$root/$database-rw"
[[ "$(sql --command='SELECT current_user;')" == "$rw" ]] ||
    fail "The connection did not use the mounted RW role."
[[ "$(sql --command='SELECT current_database();')" == "$database" ]] ||
    fail "The connection did not use the mounted database."
[[ "$(sql --command='SELECT ssl FROM pg_stat_ssl WHERE pid = pg_backend_pid();')" == t ]] ||
    fail "The PostgreSQL connection did not use TLS."

actual="$(sql --single-transaction <<'POSTGRESOK_SQL'
INSERT INTO public.postgresok_mount_probe VALUES (1, 'created');
SELECT value FROM public.postgresok_mount_probe WHERE id = 1;
UPDATE public.postgresok_mount_probe SET value = 'updated' WHERE id = 1 RETURNING value;
DELETE FROM public.postgresok_mount_probe WHERE id = 1 RETURNING value;
SELECT count(*) FROM public.postgresok_mount_probe;
POSTGRESOK_SQL
)"
[[ "$actual" == "$(printf 'created\nupdated\nupdated\n0')" ]] ||
    fail "RW CRUD results did not match the expected values."
printf 'CRUD_OK\n'

# Supply a valid but unrelated public CA, not an unreadable/invalid CA file.
cat > "$temporary/untrusted-ca.crt" <<'POSTGRESOK_UNTRUSTED_CA'
__POSTGRESOK_UNTRUSTED_CA_PEM__
POSTGRESOK_UNTRUSTED_CA
if PGSSLROOTCERT="$temporary/untrusted-ca.crt" "$psql" "${options[@]}" --command='SELECT 1;' >"$temporary/tls.out" 2>"$temporary/tls.err"; then
    fail "verify-full incorrectly accepted an unrelated CA."
fi
grep -Eiq 'certificate verify failed|certificate verification failed|self.signed certificate|unable to get local issuer' "$temporary/tls.err" ||
    fail "The wrong-CA attempt did not fail certificate verification."
printf 'WRONG_CA_REJECTED\n'

# Connect to the same server address using a deliberately incorrect hostname.
# PGHOSTADDR avoids DNS failure falsely passing this negative test.
server_address="$(sql --command='SELECT inet_server_addr();')"
[[ -n "$server_address" ]] || fail "The verified TCP connection returned no server address."
if PGHOST=postgresok-hostname-mismatch.invalid PGHOSTADDR="$server_address" "$psql" "${options[@]}" --command='SELECT 1;' >"$temporary/tls.out" 2>"$temporary/tls.err"; then
    fail "verify-full incorrectly accepted a hostname mismatch."
fi
grep -Eiq 'server certificate.*does not match host name|certificate.*hostname.*mismatch' "$temporary/tls.err" ||
    fail "The hostname-mismatch attempt did not fail hostname verification."
printf 'HOSTNAME_MISMATCH_REJECTED\n'

use_role "$admin" "$root/$database-admin"
sql --command='DROP TABLE public.postgresok_mount_probe;'
