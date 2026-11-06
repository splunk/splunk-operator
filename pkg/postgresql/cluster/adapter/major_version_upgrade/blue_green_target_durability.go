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
	"slices"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const targetWALPositionSQL = `SELECT pg_current_wal_insert_lsn()::text WHERE NOT pg_is_in_recovery()`
const targetPrimaryDurabilitySQL = `SELECT NOT pg_is_in_recovery() AND pg_current_wal_flush_lsn() >= $1::pg_lsn`
const targetStandbyDurabilitySQL = `SELECT pg_is_in_recovery() AND COALESCE(pg_last_wal_receive_lsn() >= $1::pg_lsn AND pg_last_wal_replay_lsn() >= $1::pg_lsn, false)`

// VerifyTargetDurability runs after final logical apply, sequence synchronization
// and guard removal, immediately before canonical commit. Healthy asynchronous
// replicas can still lag those changes: every possible green failover target
// must replay them before blue can cease to be authoritative. No success is
// cached across reconciles, and this read-only barrier never runs after commit.
func (s *postgresSwitchoverSQL) VerifyTargetDurability(ctx context.Context, target *cnpgv1.Cluster, id cutoverIdentity) (bool, error) {
	if target == nil || !sourceReadyForFailover(target) {
		return false, nil
	}
	names, pods, err := healthyInstancePods(ctx, s.client, target)
	if err != nil {
		return false, err
	}
	primary := pods[target.Status.CurrentPrimary]
	conn, err := s.connectCluster(ctx, target, primary.Status.PodIP, "postgres", id)
	if err != nil {
		return false, err
	}
	var position string
	err = queryOne(ctx, conn, &position, targetWALPositionSQL)
	if err := errors.Join(err, conn.Close(ctx)); err != nil {
		return false, err
	}
	for _, name := range names {
		pod := pods[name]
		conn, err := s.connectCluster(ctx, target, pod.Status.PodIP, "postgres", id)
		if err != nil {
			return false, err
		}
		var durable bool
		query := targetStandbyDurabilitySQL
		if name == primary.Name {
			query = targetPrimaryDurabilitySQL
		}
		err = queryOne(ctx, conn, &durable, query, position)
		if err := errors.Join(err, conn.Close(ctx)); err != nil || !durable {
			return false, err
		}
	}
	// A replacement Pod or promotion during the probes invalidates this pass.
	// LSNs from different timelines must not silently become equivalent proof.
	fresh := new(cnpgv1.Cluster)
	if err := s.client.Get(ctx, client.ObjectKeyFromObject(target), fresh); err != nil {
		return false, err
	}
	if fresh.UID != target.UID || fresh.DeletionTimestamp != nil || !sourceReadyForFailover(fresh) || fresh.Status.CurrentPrimary != primary.Name {
		return false, nil
	}
	currentNames, currentPods, err := healthyInstancePods(ctx, s.client, fresh)
	if err != nil || !slices.Equal(names, currentNames) {
		return false, err
	}
	for _, name := range names {
		before, after := pods[name], currentPods[name]
		if before.UID != after.UID || before.Status.PodIP != after.Status.PodIP {
			return false, nil
		}
	}
	return true, nil
}
