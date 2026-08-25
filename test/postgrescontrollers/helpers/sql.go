// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package helpers

import (
	"context"
	"fmt"
	"strings"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/test/testenv"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ExecutePostgresSQLInDatabase executes SQL in the current primary and resolves
// the primary again before every retry.
func ExecutePostgresSQLInDatabase(
	ctx context.Context,
	kubeClient client.Client,
	deployment *testenv.Deployment,
	clusterKey types.NamespacedName,
	databaseName, sql string,
) (string, error) {
	command := []string{
		"psql",
		"--username=postgres",
		"--dbname=" + databaseName,
		"--no-password",
		"--no-psqlrc",
		"--no-align",
		"--tuples-only",
		"--single-transaction",
		"--set=ON_ERROR_STOP=1",
		"--command", sql,
	}

	resolvePrimary := func(attemptCtx context.Context) (string, error) {
		cluster := &enterprisev4.PostgresCluster{}
		if err := kubeClient.Get(attemptCtx, clusterKey, cluster); err != nil {
			return "", fmt.Errorf("getting PostgresCluster primary: %w", err)
		}
		if cluster.Status.CurrentPrimary == nil || *cluster.Status.CurrentPrimary == "" {
			return "", fmt.Errorf("PostgresCluster %s has no current primary", clusterKey)
		}
		return *cluster.Status.CurrentPrimary, nil
	}

	stdout, stderr, err := ExecutePostgresPodCommand(ctx, deployment, resolvePrimary, command, "")
	if err != nil {
		return "", fmt.Errorf("executing PostgreSQL verification query in database %q after retries: %w (stderr: %s)",
			databaseName, err, strings.TrimSpace(stderr))
	}
	return strings.TrimSpace(stdout), nil
}
