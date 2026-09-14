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

// DatabaseRoleNames contains the effective PostgreSQL roles for one database.
type DatabaseRoleNames struct {
	Admin string
	RW    string
}

// DBRepo is the port for direct database grant operations that require a
// superuser connection, bypassing any connection pooler.
type DBRepo interface {
	AssignRequiredPermissionsToRole(ctx context.Context, dbName string, roles DatabaseRoleNames) error
}

// NewDBRepoFunc constructs a DBRepo adapter for the given host and database.
type NewDBRepoFunc func(ctx context.Context, host, dbName, password string) (DBRepo, error)

// ErrDBRepoTerminal marks user-actionable database repository errors where retrying
// the same spec is not expected to succeed.
var ErrDBRepoTerminal = errors.New("terminal reconciliation error")
