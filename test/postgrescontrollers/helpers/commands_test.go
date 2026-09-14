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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutePostgresPodCommandResolvesPodOnEveryAttempt(t *testing.T) {
	resolveCalls := 0
	resolvePod := func(context.Context) (string, error) {
		resolveCalls++
		if resolveCalls == 1 {
			return "old-primary", nil
		}
		return "new-primary", nil
	}
	exec := func(_ context.Context, pod string, _ []string, _ string, _ bool) (string, string, error) {
		if pod == "old-primary" {
			return "", "", errors.New("pod not found")
		}
		return "ok", "", nil
	}

	stdout, stderr, err := executePostgresPodCommand(t.Context(), exec, resolvePod, []string{"psql"}, "", time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, "ok", stdout)
	assert.Empty(t, stderr)
	assert.Equal(t, 2, resolveCalls)
}

func TestExecutePostgresPodCommandStopsOnCommandStderr(t *testing.T) {
	wantErr := errors.New("command exited with status 1")
	execCalls := 0
	exec := func(context.Context, string, []string, string, bool) (string, string, error) {
		execCalls++
		return "", "ERROR: relation does not exist", wantErr
	}

	_, stderr, err := executePostgresPodCommand(
		t.Context(),
		exec,
		func(context.Context) (string, error) { return "primary", nil },
		[]string{"psql"},
		"",
		time.Millisecond,
	)
	require.ErrorIs(t, err, wantErr)
	assert.Contains(t, stderr, "relation does not exist")
	assert.Equal(t, 1, execCalls)
}
