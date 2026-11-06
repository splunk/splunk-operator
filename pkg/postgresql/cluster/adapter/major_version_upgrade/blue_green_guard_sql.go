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

func verifyCutoverGuardState(ctx context.Context, conn pginfra.Conn, switchover *platformv1alpha1.BlueGreenSwitchoverStatus, side environmentSide) error {
	var present bool
	if err := queryOne(ctx, conn, &present, cutoverGuardPresenceSQL, candidateGuardSchema+"."+candidateGuardFunction+"()", candidateGuardTrigger); err != nil {
		return err
	}
	if side == sideBlue {
		if present {
			return fmt.Errorf("%w: unexpected candidate guard on blue", mvutypes.ErrUpgradeFlowFailed)
		}
		return nil
	}
	if switchover.CandidateGuardsRemovedAt != nil {
		if present {
			return fmt.Errorf("%w: candidate guards reappeared after verified removal", mvutypes.ErrUpgradeFlowFailed)
		}
		return nil
	}
	if present {
		return verifyCandidateDMLGuards(ctx, conn)
	}
	if switchover.CandidateGuardsRemovalStartedAt == nil {
		return fmt.Errorf("%w: candidate guards disappeared without a durable removal intent", mvutypes.ErrUpgradeFlowFailed)
	}
	return nil
}

func (s *postgresSwitchoverSQL) RemoveGuards(ctx context.Context, target *cnpgv1.Cluster, databases []declaredDatabase, id cutoverIdentity) error {
	for _, db := range databases {
		if err := s.withConn(ctx, target, "", db.name, id, func(conn pginfra.Conn) error {
			if err := verifyControlDatabase(ctx, conn, id); err != nil {
				return err
			}
			var present bool
			if err := queryOne(ctx, conn, &present, controlGuardFunctionExistsSQL, candidateGuardSchema+"."+candidateGuardFunction+"()"); err != nil {
				return err
			}
			if present {
				if err := verifyCandidateDMLGuards(ctx, conn); err != nil {
					return err
				}
			}
			var removed bool
			if err := queryOne(ctx, conn, &removed, controlHelperCallSQL(controlRemoveGuardsFunction)); err != nil {
				return err
			}
			if err := queryOne(ctx, conn, &present, cutoverGuardsRemainSQL, candidateGuardTrigger, candidateGuardSchema+"."+candidateGuardFunction+"()"); err != nil {
				return err
			}
			if present {
				return fmt.Errorf("%w: green candidate guards remain", mvutypes.ErrUpgradeFlowFailed)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}
