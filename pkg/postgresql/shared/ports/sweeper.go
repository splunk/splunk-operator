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
package ports

import (
	"context"
	"errors"
)

// RoleSweeper is the port for post-restore credential sweeping. It lives in shared/ports so the
// cluster domain can depend on it without importing the database adapter that implements it.
type RoleSweeper interface {
	// SweepUnmanagedRolesAfterRestore disables login for every non-system role after a restore.
	// CNPG restores password hashes verbatim, so without this step all pre-restore credentials
	// would still work. The ManagedRoles reconciler runs immediately after and re-enables only
	// the roles declared by PostgresDatabase CRs, with fresh credentials.
	SweepUnmanagedRolesAfterRestore(ctx context.Context) error
}

// NewRoleSweeperFunc constructs a RoleSweeper for a direct superuser connection.
type NewRoleSweeperFunc func(ctx context.Context, host, dbName, password string) (RoleSweeper, error)

// ErrSweeperConnectTerminal marks a sweeper connect failure that retrying will not fix (e.g. bad
// superuser credentials). A NewRoleSweeperFunc wraps such errors with it so the consumer surfaces
// Failed instead of requeuing forever.
var ErrSweeperConnectTerminal = errors.New("role sweeper connection failure (terminal)")
