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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
)

type subscriptionRetirementSQL interface {
	RemoveSubscriptions(context.Context, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus, cutoverIdentity) (bool, error)
}

func (s *postgresSwitchoverSQL) RemoveSubscriptions(ctx context.Context, green *cnpgv1.Cluster, bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus, id cutoverIdentity) (bool, error) {
	verifier := newPostgresCandidateVerifier(s.client, s.logicalName, s.owner)
	verifier.connect = s.bootstrap
	connect := verifier.connectCluster
	hasIdentity := bg.Switchover != nil && bg.Switchover.ControlIdentity != nil
	restricted := hasIdentity && bg.Switchover.ControlIdentity.PreparedAt != nil
	if restricted {
		connect = func(ctx context.Context, cluster *cnpgv1.Cluster, host, database string) (pginfra.Conn, error) {
			return s.connectCluster(ctx, cluster, host, database, id)
		}
	} else if candidateFenced(green) || hasIdentity && !accessMatches(green, bg, sideGreen, fenceOpen) {
		return false, fmt.Errorf("candidate bootstrap is incomplete and ordinary management access is not proved unfenced")
	}
	for _, db := range bg.DatabaseReplication {
		done := false
		host := green.Status.CurrentPrimary
		_, pods, err := retainedInstancePods(ctx, s.client, green)
		if err != nil {
			return false, err
		}
		pod := pods[host]
		if pod.UID == "" || !instancePodReady(&pod) {
			return false, nil
		}
		conn, err := connectTeardownInstance(ctx, s.client, s.owner, green, pod.Status.PodIP, db.DatabaseName, connect)
		if missingTeardownDatabase(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		err = withCutoverConn(ctx, conn, func(conn pginfra.Conn) error {
			helper := retireSubscriptionHelper(id, db.DatabaseName)
			if restricted {
				if err := verifyControlHelper(ctx, conn, helper, id); err != nil {
					return fmt.Errorf("attempt %s candidate %s requires its recorded subscription-retirement helper; preserve credentials and inspect the helper through authorized administrative access: %w", bg.AttemptID, green.Name, err)
				}
			} else {
				// Resume only the management bootstrap authorized before fencing.
				if hasIdentity {
					if err := prepareControlRole(ctx, conn, id); err != nil {
						return err
					}
				}
				if _, err := conn.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+pginfra.QuoteIdentifier(candidateGuardSchema)+" AUTHORIZATION postgres"); err != nil {
					return err
				}
				var exists bool
				if err := queryOne(ctx, conn, &exists, controlGuardFunctionExistsSQL, candidateGuardSchema+"."+helper.name+"()"); err != nil {
					return err
				}
				if !exists {
					if err := installCutoverHelper(ctx, conn, helper, id); err != nil {
						return err
					}
				} else if err := verifyControlHelper(ctx, conn, helper, id); err != nil {
					return err
				}
			}
			if !restricted && hasIdentity {
				for _, statement := range []string{"GRANT USAGE ON SCHEMA " + pginfra.QuoteIdentifier(candidateGuardSchema) + " TO " + pginfra.QuoteIdentifier(id.Role), "GRANT CONNECT ON DATABASE " + pginfra.QuoteIdentifier(db.DatabaseName) + " TO " + pginfra.QuoteIdentifier(id.Role), "SET SESSION AUTHORIZATION " + pginfra.QuoteIdentifier(id.Role)} {
					if _, err := conn.Exec(ctx, statement); err != nil {
						return err
					}
				}
			}
			return queryOne(ctx, conn, &done, controlHelperCallSQL(helper.name))
		})
		if err != nil || !done {
			return false, err
		}
	}
	return true, nil
}
