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
	"cmp"
	"context"
	"encoding/json/v2"
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (s *postgresSwitchoverSQL) roleSecret(ctx context.Context, cluster *cnpgv1.Cluster, role string) (*corev1.Secret, error) {
	name := ""
	if role == "postgres" {
		name = cluster.GetSuperuserSecretName()
	} else if cluster.Spec.Managed != nil {
		for _, managed := range cluster.Spec.Managed.Roles {
			if managed.Name == role && managed.PasswordSecret != nil {
				name = managed.PasswordSecret.Name
				break
			}
		}
	}
	if name == "" {
		return nil, fmt.Errorf("%w: role %q has no managed credential", mvutypes.ErrUpgradeFlowFailed, role)
	}
	secret := &corev1.Secret{}
	if err := s.client.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: name}, secret); err != nil {
		return nil, err
	}
	if len(secret.Data["password"]) == 0 {
		return nil, fmt.Errorf("%w: role %q credential has no password", mvutypes.ErrUpgradeFlowFailed, role)
	}
	return secret, nil
}

func (s *postgresSwitchoverSQL) VerifyOpen(ctx context.Context, cluster *cnpgv1.Cluster, id cutoverIdentity, originalRoles []platformv1alpha1.BlueGreenRoleLoginStatus) (bool, error) {
	hosts, err := s.instanceHosts(ctx, cluster)
	if err != nil || len(hosts) == 0 {
		return false, err
	}
	databases, err := listManagedDatabases(ctx, s.client, types.NamespacedName{Namespace: s.owner.Namespace, Name: s.owner.Name})
	if err != nil {
		return false, err
	}
	databaseByRole := make(map[string]string)
	for _, db := range databases {
		for role := range declaredRoleNames([]declaredDatabase{db}, "") {
			if _, exists := databaseByRole[role]; !exists {
				databaseByRole[role] = db.name
			}
		}
	}
	loginJSON, err := json.Marshal(append([]platformv1alpha1.BlueGreenRoleLoginStatus{}, originalRoles...))
	if err != nil {
		return false, err
	}
	roles := append([]platformv1alpha1.BlueGreenRoleLoginStatus{{RoleName: "postgres", Login: true}}, originalRoles...)
	for _, target := range hosts {
		for _, role := range roles {
			if !role.Login {
				continue
			}
			secret, err := s.roleSecret(ctx, cluster, role.RoleName)
			if err != nil {
				return false, err
			}
			database := cmp.Or(databaseByRole[role.RoleName], "postgres")
			restored := true
			err = s.withConn(ctx, cluster, target.address, database, cutoverIdentity{Role: role.RoleName, Password: string(secret.Data["password"]), AttemptID: id.AttemptID}, func(conn pginfra.Conn) error {
				var writable bool
				if err := queryOne(ctx, conn, &writable, cutoverPrimaryWritableSQL); err != nil {
					return err
				}
				if role.RoleName == "postgres" {
					if err := queryOne(ctx, conn, &restored, cutoverRoleLoginsRestoredSQL, string(loginJSON)); err != nil {
						return err
					}
					if !restored {
						return nil
					}
				}
				if target.name == cluster.Status.CurrentPrimary && !writable {
					restored = false
					return nil
				}
				if role.RoleName == "postgres" && target.name == cluster.Status.CurrentPrimary {
					return proveCutoverWritable(ctx, conn)
				}
				return nil
			})
			if err != nil {
				return false, err
			}
			if !restored {
				return false, nil
			}
		}
	}
	return true, nil
}

// This runs only on a fresh original postgres connection after its remote
// access has reopened. The table and inserted row are temporary, and the
// transaction always rolls back. Application roles need no extra privileges.
func proveCutoverWritable(ctx context.Context, conn pginfra.Conn) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, statement := range []string{
		"SET TRANSACTION READ WRITE",
		cutoverWriteProbeCreateSQL,
		"INSERT INTO pg_temp.splunk_operator_cutover_probe (value) VALUES (true)",
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("verifying reopened PostgreSQL write access: %w", err)
		}
	}
	// Conn and Tx intentionally share this single connection, so this read is
	// inside the transaction containing the temporary write.
	var observed bool
	if err := queryOne(ctx, conn, &observed, cutoverWriteProbeVerifySQL); err != nil {
		return err
	}
	if !observed {
		return fmt.Errorf("%w: reopened PostgreSQL did not return its write probe", mvutypes.ErrUpgradeFlowFailed)
	}
	return tx.Rollback(ctx)
}
