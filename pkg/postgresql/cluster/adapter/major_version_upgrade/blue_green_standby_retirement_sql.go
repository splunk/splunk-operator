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

	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
)

const controlRetireStandbySlotFunction = "cutover_retire_standby_slot"

// A demoted primary can retain local failover slots that the slot sync worker
// cannot adopt or remove. Keep this capability separate from the existing
// persisted helper so older attempts can still verify their access fences.
func retireStandbySlotHelper(id cutoverIdentity, database string) cutoverHelper {
	return cutoverHelper{controlRetireStandbySlotFunction, `DECLARE slot text := ` + quoteSQLStringLiteral(replicationSlotName(id.LogicalName, database)) + `; BEGIN
IF session_user <> ` + quoteSQLStringLiteral(id.Role) + ` THEN RAISE EXCEPTION 'invalid cutover identity'; END IF;
IF NOT pg_catalog.pg_is_in_recovery() THEN RETURN false; END IF;
IF EXISTS (SELECT 1 FROM pg_catalog.pg_replication_slots WHERE database=pg_catalog.current_database() AND slot_name ~ '^pg_[0-9]+_sync_[0-9]+_[0-9]+$') THEN RETURN false; END IF;
IF EXISTS (SELECT 1 FROM pg_catalog.pg_replication_slots WHERE slot_name=slot AND (slot_type<>'logical' OR plugin<>'pgoutput' OR database<>pg_catalog.current_database() OR temporary OR NOT failover)) THEN RAISE EXCEPTION 'replication slot identity changed'; END IF;
IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_replication_slots WHERE slot_name=slot) THEN RETURN true; END IF;
IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname=` + quoteSQLStringLiteral(replicationRoleName(id.AttemptID)) + ` AND rolcanlogin) THEN RETURN false; END IF;
IF EXISTS (SELECT 1 FROM pg_catalog.pg_replication_slots WHERE slot_name=slot AND (active OR synced)) THEN RETURN false; END IF;
PERFORM pg_catalog.pg_drop_replication_slot(slot);
RETURN false; END;`}
}

// retireStandbySlot runs only after the original cleanup reports pending. The
// new no-argument helper cannot mutate a primary or a synchronized/active slot.
func retireStandbySlot(ctx context.Context, conn pginfra.Conn, id cutoverIdentity, database string) (bool, error) {
	var standby bool
	if err := queryOne(ctx, conn, &standby, `SELECT pg_catalog.pg_is_in_recovery()`); err != nil || !standby {
		return false, err
	}
	helper := retireStandbySlotHelper(id, database)
	var exists bool
	if err := queryOne(ctx, conn, &exists, controlGuardFunctionExistsSQL, candidateGuardSchema+"."+helper.name+"()"); err != nil {
		return false, err
	}
	if !exists {
		var localSlot bool
		if err := queryOne(ctx, conn, &localSlot, `SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_replication_slots WHERE slot_name=$1 AND NOT synced)`, replicationSlotName(id.LogicalName, database)); err != nil {
			return false, err
		}
		if localSlot {
			return false, fmt.Errorf("retained source database %q has a local slot but this attempt was prepared without the standby slot retirement helper; keep green authoritative and arrange operator-assisted recovery", database)
		}
		return false, nil
	}
	if err := verifyControlHelper(ctx, conn, helper, id); err != nil {
		return false, err
	}
	var removed bool
	err := queryOne(ctx, conn, &removed, controlHelperCallSQL(helper.name))
	return removed, err
}
