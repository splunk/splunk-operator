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
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// postgresSwitchoverSQL is the attempt-scoped SQL boundary. Only bootstrap uses
// postgres. Once the fence starts, every operation uses the restricted identity.
type postgresSwitchoverSQL struct {
	client      client.Client
	logicalName string
	owner       *platformv1alpha1.PostgresCluster
	connect     cutoverConnector
	bootstrap   postgresConnector
}

func newPostgresSwitchoverSQL(c client.Client, logicalName string, owner *platformv1alpha1.PostgresCluster) (*postgresSwitchoverSQL, error) {
	if c == nil || owner == nil || owner.UID == "" || owner.Namespace == "" || logicalName == "" || logicalName != owner.Name {
		return nil, fmt.Errorf("cutover SQL requires a client and matching persisted parent identity")
	}
	return &postgresSwitchoverSQL{client: c, logicalName: logicalName, owner: owner, connect: pginfra.ConnectAsVerified, bootstrap: pginfra.ConnectVerified}, nil
}

type cutoverIdentity struct {
	Role, Password string
	AttemptID      string
}

type cutoverConnector func(context.Context, string, string, string, string, string, []byte) (pginfra.Conn, error)

const controlTerminateFunction = "cutover_terminate_sessions"

const controlRemoveGuardsFunction = "cutover_remove_guards"

const controlFenceFunction = "cutover_fence_file_valid"

// A completed false row remains a convergence result for fence/drain callers.
// If the cursor fails after that row, preserve the observed contradiction as
// an invariant error rather than reducing it to an unavailable connection.
func queryCutoverBoolean(ctx context.Context, conn pginfra.Conn, dest *bool, sql string, args ...any) error {
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
	var observed bool
	if err := rows.Scan(&observed); err != nil {
		return err
	}
	*dest = observed
	if rows.Next() {
		return fmt.Errorf("%w: cutover proof query returned multiple rows", mvutypes.ErrUpgradeFlowFailed)
	}
	if err := rows.Err(); err != nil {
		if !observed {
			return fmt.Errorf("%w: observed negative cutover proof before cursor failed: %v", mvutypes.ErrUpgradeFlowFailed, err)
		}
		return err
	}
	return nil
}

func (s *postgresSwitchoverSQL) connectCluster(ctx context.Context, cluster *cnpgv1.Cluster, host, database string, id cutoverIdentity) (pginfra.Conn, error) {
	if s == nil || s.connect == nil || cluster == nil || id.Role == "" || id.Password == "" {
		return nil, fmt.Errorf("%w: incomplete cutover SQL connection", mvutypes.ErrUpgradeFlowFailed)
	}
	ca, err := clusterServerCA(ctx, s.client, cluster)
	if err != nil {
		return nil, err
	}
	serverName, err := pgconninfo.ServiceFQDN(cluster.GetServiceReadWriteName(), cluster.Namespace)
	if err != nil {
		return nil, err
	}
	if host == "" {
		host = serverName
	}
	return s.connect(ctx, host, database, id.Role, id.Password, serverName, ca)
}

// withConn owns the complete lifecycle of a restricted control connection.
func (s *postgresSwitchoverSQL) withConn(ctx context.Context, cluster *cnpgv1.Cluster, host, database string, id cutoverIdentity, action func(pginfra.Conn) error) error {
	conn, err := s.connectCluster(ctx, cluster, host, database, id)
	if err != nil {
		return err
	}
	return withCutoverConn(ctx, conn, action)
}

// withCutoverConn also serves bootstrap and retained-instance callers, which
// must distinguish a failed dial from an error after the connection succeeds.
func withCutoverConn(ctx context.Context, conn pginfra.Conn, action func(pginfra.Conn) error) error {
	return errors.Join(action(conn), conn.Close(ctx))
}
