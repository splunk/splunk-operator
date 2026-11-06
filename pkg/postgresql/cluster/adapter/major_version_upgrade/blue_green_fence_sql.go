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
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
)

func (s *postgresSwitchoverSQL) VerifyFence(ctx context.Context, cluster *cnpgv1.Cluster, id cutoverIdentity, disabledRoles []string) (bool, error) {
	hosts, err := s.instanceHosts(ctx, cluster)
	if err != nil || len(hosts) == 0 {
		return false, err
	}
	return s.verifyFenceHosts(ctx, cluster, id, disabledRoles, hosts, false)
}

// VerifyRetainedSourceFence uses the already verified precommit fence for
// absent instances and verifies every currently reachable healthy source pod.
func (s *postgresSwitchoverSQL) VerifyRetainedSourceFence(ctx context.Context, cluster *cnpgv1.Cluster, id cutoverIdentity, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := verifyRetainedSourceProof(cluster, id, status); err != nil {
		return false, err
	}
	hosts, err := retainedSourceHosts(ctx, s.client, cluster)
	if err != nil {
		return false, err
	}
	return s.verifyFenceHosts(ctx, cluster, id, roleNames(status.Switchover.OriginalRoleLogins), hosts, true)
}

func (s *postgresSwitchoverSQL) verifyFenceHosts(ctx context.Context, cluster *cnpgv1.Cluster, id cutoverIdentity, disabledRoles []string, targets []cutoverInstanceTarget, retired bool) (bool, error) {
	for _, target := range targets {
		host := target.address
		conn, err := s.connectCluster(ctx, cluster, host, "postgres", id)
		if err != nil {
			if retired && ctx.Err() == nil && retainedInstanceUnavailable(err) {
				continue
			}
			return false, err
		}
		var valid bool
		err = withCutoverConn(ctx, conn, func(conn pginfra.Conn) error {
			if err := verifyControlDatabase(ctx, conn, id); err != nil {
				return err
			}
			if err := queryCutoverBoolean(ctx, conn, &valid, controlHelperCallSQL(controlFenceFunction)); err != nil {
				return err
			}
			if !valid {
				return nil
			}
			return queryCutoverBoolean(ctx, conn, &valid, cutoverRoleFenceSQL, disabledRoles)
		})
		if err != nil {
			return false, err
		}
		if !valid {
			return false, nil
		}
		// Catalog/file inspection cannot prove the HBA was loaded. These fresh
		// attempts must fail specifically because HBA or LOGIN rejects access.
		for _, role := range append([]string{"postgres"}, disabledRoles...) {
			secret, err := s.roleSecret(ctx, cluster, role)
			if err != nil {
				return false, err
			}
			conn, err = s.connectCluster(ctx, cluster, host, "postgres", cutoverIdentity{Role: role, Password: string(secret.Data["password"]), AttemptID: id.AttemptID})
			if err == nil {
				// A successful connection contradicts the fence; retain its close error.
				return false, conn.Close(ctx)
			}
			if !intentionalFenceDenial(err) {
				return false, fmt.Errorf("verifying denied login for %q: %w", role, err)
			}
		}
	}
	return true, nil
}

func verifyRetainedSourceProof(cluster *cnpgv1.Cluster, id cutoverIdentity, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	if cluster == nil || !mvutypes.HasRetainedSourceFenceProof(status, id.AttemptID, id.Role) ||
		status.Blue == nil || status.Blue.Ref.Name != cluster.Name || status.Blue.Ref.UID == "" || status.Blue.Ref.UID != cluster.UID ||
		(status.Blue.Ref.Namespace != "" && status.Blue.Ref.Namespace != cluster.Namespace) || cluster.DeletionTimestamp != nil ||
		!accessMatches(cluster, status, sideBlue, fenceApplied) {
		return fmt.Errorf("%w: retired source lacks its exact committed fence and drain proof", mvutypes.ErrUpgradeFlowFailed)
	}
	return nil
}

func (s *postgresSwitchoverSQL) Drain(ctx context.Context, cluster *cnpgv1.Cluster, id cutoverIdentity, terminate bool) (bool, error) {
	hosts, err := s.instanceHosts(ctx, cluster)
	if err != nil || len(hosts) == 0 {
		return false, err
	}
	return s.drainHosts(ctx, cluster, id, terminate, hosts, false)
}

// DrainRetainedSource never requires a missing old primary or replica to return.
// A returning instance remains bound to the exact retained CNPG fence overlay.
func (s *postgresSwitchoverSQL) DrainRetainedSource(ctx context.Context, cluster *cnpgv1.Cluster, id cutoverIdentity, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := verifyRetainedSourceProof(cluster, id, status); err != nil {
		return false, err
	}
	hosts, err := retainedSourceHosts(ctx, s.client, cluster)
	if err != nil {
		return false, err
	}
	return s.drainHosts(ctx, cluster, id, true, hosts, true)
}

func (s *postgresSwitchoverSQL) drainHosts(ctx context.Context, cluster *cnpgv1.Cluster, id cutoverIdentity, terminate bool, hosts []cutoverInstanceTarget, retired bool) (bool, error) {
	drained := true
	for _, target := range hosts {
		conn, err := s.connectCluster(ctx, cluster, target.address, "postgres", id)
		if err != nil {
			if retired && ctx.Err() == nil && retainedInstanceUnavailable(err) {
				continue
			}
			return false, err
		}
		var complete bool
		err = withCutoverConn(ctx, conn, func(conn pginfra.Conn) error {
			var err error
			complete, err = drainCutoverInstance(ctx, conn, id, terminate)
			return err
		})
		if err != nil {
			return false, err
		}
		drained = drained && complete
	}
	return drained, nil
}

func drainCutoverInstance(ctx context.Context, conn pginfra.Conn, id cutoverIdentity, terminate bool) (bool, error) {
	if err := verifyControlDatabase(ctx, conn, id); err != nil {
		return false, err
	}
	if terminate {
		var sent bool
		if err := queryCutoverBoolean(ctx, conn, &sent, controlHelperCallSQL(controlTerminateFunction)); err != nil {
			return false, err
		}
	}
	var drained bool
	err := queryCutoverBoolean(ctx, conn, &drained, "SELECT NOT EXISTS (SELECT 1 FROM pg_catalog.pg_stat_activity WHERE "+cutoverSessionPredicate(id)+") AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_prepared_xacts)")
	return drained, err
}
