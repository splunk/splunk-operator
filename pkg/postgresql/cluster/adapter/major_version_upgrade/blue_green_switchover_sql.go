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
	"crypto/tls"
	"crypto/x509"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/jackc/pgx/v5/pgconn"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
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

func (s *postgresSwitchoverSQL) VerifyControl(ctx context.Context, cluster *cnpgv1.Cluster, id cutoverIdentity) error {
	sequenceWrites, err := s.sequenceWrites(cluster, id)
	if err != nil {
		return err
	}
	databases, err := listManagedDatabases(ctx, s.client, types.NamespacedName{Namespace: s.owner.Namespace, Name: s.owner.Name})
	if err != nil {
		return err
	}
	names := []string{"postgres"}
	for _, db := range databases {
		names = append(names, db.name)
	}
	for _, name := range names {
		if err := s.withConn(ctx, cluster, "", name, id, func(conn pginfra.Conn) error {
			if err := verifyControlDatabase(ctx, conn, id); err != nil {
				return err
			}
			return verifyControlSequenceGrants(ctx, conn, id, sequenceWrites)
		}); err != nil {
			return err
		}
	}
	return nil
}

type cutoverInstanceTarget struct {
	name    string
	address string
}

func (s *postgresSwitchoverSQL) instanceHosts(ctx context.Context, cluster *cnpgv1.Cluster) ([]cutoverInstanceTarget, error) {
	if !sourceReadyForFailover(cluster) {
		return nil, nil
	}
	names, pods, err := healthyInstancePods(ctx, s.client, cluster)
	if err != nil {
		return nil, err
	}
	return cutoverInstanceTargets(names, pods), nil
}

// Retired source availability is independent of its former HA topology. The
// caller must first prove that the committed source retains its durable fence.
func retainedSourceHosts(ctx context.Context, reader client.Reader, cluster *cnpgv1.Cluster) ([]cutoverInstanceTarget, error) {
	names, pods, err := retainedInstancePods(ctx, reader, cluster)
	if err != nil {
		return nil, err
	}
	return cutoverInstanceTargets(names, pods), nil
}

func cutoverInstanceTargets(names []string, pods map[string]corev1.Pod) []cutoverInstanceTarget {
	targets := make([]cutoverInstanceTarget, 0, len(names))
	for _, name := range names {
		targets = append(targets, cutoverInstanceTarget{name: name, address: pods[name].Status.PodIP})
	}
	return targets
}

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
			if err := queryCutoverBoolean(ctx, conn, &valid, "SELECT "+pginfra.QuoteIdentifier(candidateGuardSchema)+"."+pginfra.QuoteIdentifier(controlFenceFunction)+"()"); err != nil {
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

// Transport failure cannot undo the previously verified durable LOGIN/HBA
// fence. Authentication and certificate failures are contradictory live state
// and must never be treated as an absent retired instance.
func retainedInstanceUnavailable(err error) bool {
	return allCutoverErrorLeaves(err, func(cause error) bool {
		switch failure := cause.(type) {
		case *pgconn.PgError:
			return failure.Code == "57P01" || failure.Code == "57P02" || failure.Code == "57P03"
		case *net.DNSError:
			return true
		}
		switch cause {
		case syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.ECONNABORTED,
			syscall.ETIMEDOUT, syscall.ENETUNREACH, syscall.EHOSTUNREACH,
			syscall.ENETDOWN, syscall.EPIPE, context.DeadlineExceeded,
			io.EOF, io.ErrUnexpectedEOF:
			return true
		}
		return false
	})
}

// pgx can join failures from multiple connection attempts. Every branch must
// prove the expected failure: finding one permitted error cannot conceal an
// authentication, TLS, or unknown failure in another branch.
func allCutoverErrorLeaves(err error, allowed func(error) bool) bool {
	if err == nil {
		return false
	}
	switch failure := err.(type) {
	case *net.OpError:
		if failure.Op == "remote error" || failure.Op == "local error" {
			return false
		}
	case tls.AlertError, *tls.AlertError, *tls.CertificateVerificationError,
		x509.UnknownAuthorityError, *x509.UnknownAuthorityError,
		x509.HostnameError, *x509.HostnameError,
		x509.CertificateInvalidError, *x509.CertificateInvalidError,
		tls.RecordHeaderError, *tls.RecordHeaderError:
		return false
	}
	switch unwrapped := err.(type) {
	case interface{ Unwrap() []error }:
		causes := unwrapped.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !allCutoverErrorLeaves(cause, allowed) {
				return false
			}
		}
		return true
	case interface{ Unwrap() error }:
		if cause := unwrapped.Unwrap(); cause != nil {
			return allCutoverErrorLeaves(cause, allowed)
		}
	}
	return allowed(err)
}

func intentionalFenceDenial(err error) bool {
	return allCutoverErrorLeaves(err, func(cause error) bool {
		pgErr, ok := cause.(*pgconn.PgError)
		if !ok || pgErr.Code != "28000" {
			return false
		}
		message := strings.ToLower(pgErr.Message)
		return strings.Contains(message, "pg_hba.conf rejects connection") || strings.Contains(message, "not permitted to log in")
	})
}

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
		if err := queryCutoverBoolean(ctx, conn, &sent, "SELECT "+pginfra.QuoteIdentifier(candidateGuardSchema)+"."+pginfra.QuoteIdentifier(controlTerminateFunction)+"()"); err != nil {
			return false, err
		}
	}
	var drained bool
	err := queryCutoverBoolean(ctx, conn, &drained, "SELECT NOT EXISTS (SELECT 1 FROM pg_catalog.pg_stat_activity WHERE "+cutoverSessionPredicate(id)+") AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_prepared_xacts)")
	return drained, err
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
