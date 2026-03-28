package core

import "context"

// DBRepo is the port for all direct database operations that require a
// superuser connection, bypassing any connection pooler.
// Adapters implementing this port live in adapter/.
type DBRepo interface {
	ExecGrants(ctx context.Context, dbName string) error
}
