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

	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
)

const scramSHA256 = "scram-sha-256"

// withSCRAMPasswordEncoding lets PostgreSQL hash the upgrade passwords. Only
// this preparation connection changes; restore its previous setting even when
// preparation fails. The connection owner closes it if restoration fails.
func withSCRAMPasswordEncoding(ctx context.Context, conn pginfra.Conn, prepare func() error) (resultErr error) {
	var previousEncoding string
	if err := queryOne(ctx, conn, &previousEncoding, "SHOW password_encryption"); err != nil {
		return fmt.Errorf("reading upgrade password encoding: %w", err)
	}
	if previousEncoding == scramSHA256 {
		return prepare()
	}
	if _, err := conn.Exec(ctx, "SET password_encryption = "+quoteSQLStringLiteral(scramSHA256)); err != nil {
		return fmt.Errorf("selecting upgrade password encoding: %w", err)
	}
	defer func() {
		if _, err := conn.Exec(ctx, "SET password_encryption = "+quoteSQLStringLiteral(previousEncoding)); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("restoring upgrade password encoding: %w", err))
		}
	}()
	return prepare()
}
