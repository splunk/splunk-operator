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
	"time"

	"github.com/splunk/splunk-operator/test/testenv"
	"k8s.io/apimachinery/pkg/util/wait"
)

type PodResolver func(context.Context) (string, error)

type podCommand func(context.Context, string, []string, string, bool) (string, string, error)

const (
	postgresExecAttemptTimeout = 40 * time.Second
	postgresExecRetryTimeout   = 2 * time.Minute
)

// ExecutePostgresPodCommand retries transient pod-exec startup failures while
// returning command failures with stderr immediately.
func ExecutePostgresPodCommand(
	ctx context.Context,
	deployment *testenv.Deployment,
	resolvePod PodResolver,
	command []string,
	stdin string,
) (string, string, error) {
	return executePostgresPodCommand(ctx, deployment.PodExecCommand, resolvePod, command, stdin, testenv.PollInterval)
}

func executePostgresPodCommand(
	ctx context.Context,
	exec podCommand,
	resolvePod PodResolver,
	command []string,
	stdin string,
	retryInterval time.Duration,
) (string, string, error) {
	var stdout, stderr string
	var execErr error
	pollErr := wait.PollUntilContextTimeout(ctx, retryInterval, postgresExecRetryTimeout, true, func(attemptCtx context.Context) (bool, error) {
		pod, err := resolvePod(attemptCtx)
		if err != nil {
			execErr = fmt.Errorf("resolving PostgreSQL pod: %w", err)
			return false, nil
		}
		if pod == "" {
			execErr = fmt.Errorf("resolving PostgreSQL pod: resolver returned an empty name")
			return false, nil
		}

		execCtx, cancel := context.WithTimeout(attemptCtx, postgresExecAttemptTimeout)
		defer cancel()
		stdout, stderr, execErr = exec(execCtx, pod, command, stdin, false)
		if execErr == nil {
			return true, nil
		}
		if strings.TrimSpace(stderr) != "" {
			return false, execErr
		}
		return false, nil
	})
	if pollErr != nil {
		if execErr != nil {
			return stdout, stderr, execErr
		}
		return stdout, stderr, pollErr
	}
	return stdout, stderr, nil
}
