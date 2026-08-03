// Copyright (c) 2026 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/require"
)

func TestIndexerReadinessWithdrawalSetsLifecycleHold(t *testing.T) {
	ctx := context.Background()
	command := "mkdir -p /tmp/splunk_operator_k8s/probes/; " +
		"printf 'export K8_OPERATOR_LIVENESS_LEVEL=1\\n" +
		"export SPLUNK_OPERATOR_INDEXER_SERVING_READINESS=true\\n" +
		"export SPLUNK_OPERATOR_LIFECYCLE_HOLD=true\\n' > " +
		"/tmp/splunk_operator_k8s/probes/k8_liveness_driver.sh"
	podExecClient := &spltest.MockPodExecClient{
		TargetPodName: "splunk-example-indexer-2",
	}
	podExecClient.AddMockPodExecReturnContext(
		ctx,
		command,
		&spltest.MockPodExecReturnContext{},
	)

	require.NoError(
		t,
		setIndexerReadinessWithdrawalOnSplunkPod(ctx, podExecClient),
	)
	podExecClient.CheckPodExecCommands(
		t,
		"setIndexerReadinessWithdrawalOnSplunkPod",
	)
}

func TestIndexerReadinessWithdrawalRequiresExplicitLifecycleHold(t *testing.T) {
	scriptPath := filepath.Join(
		"..",
		"..",
		"..",
		"tools",
		"k8_probes",
		"readinessProbe.sh",
	)
	script, err := os.ReadFile(scriptPath)
	require.NoError(t, err)

	require.Contains(
		t,
		string(script),
		`if [[ "true" == "$SPLUNK_OPERATOR_LIFECYCLE_HOLD" ]]; then`,
	)
	require.NotContains(
		t,
		string(script),
		`if [[ "1" == "$K8_OPERATOR_LIVENESS_LEVEL" ]]; then
            echo "Indexer is in an Operator-owned lifecycle transition"`,
	)
}

func TestLivenessProbeLifecycleHold(t *testing.T) {
	scriptPath := filepath.Join(
		"..",
		"..",
		"..",
		"tools",
		"k8_probes",
		"livenessProbe.sh",
	)

	t.Run("initialized container remains live without splunkd", func(t *testing.T) {
		output, err := runLivenessProbe(
			t,
			scriptPath,
			"started\n",
			"export K8_OPERATOR_LIVENESS_LEVEL=1\n"+
				"export SPLUNK_OPERATOR_LIFECYCLE_HOLD=true\n",
		)
		require.NoError(t, err, string(output))
	})

	t.Run("incomplete container fails closed", func(t *testing.T) {
		output, err := runLivenessProbe(
			t,
			scriptPath,
			"failed\n",
			"export K8_OPERATOR_LIVENESS_LEVEL=1\n"+
				"export SPLUNK_OPERATOR_LIFECYCLE_HOLD=true\n",
		)
		require.Error(t, err)
		require.Contains(
			t,
			string(output),
			"Splunk container initialization is incomplete during lifecycle hold",
		)
	})

	t.Run("missing container state fails closed", func(t *testing.T) {
		output, err := runLivenessProbe(
			t,
			scriptPath,
			"",
			"export K8_OPERATOR_LIVENESS_LEVEL=1\n"+
				"export SPLUNK_OPERATOR_LIFECYCLE_HOLD=true\n",
		)
		require.Error(t, err)
		require.Contains(
			t,
			string(output),
			"Splunk container state is unavailable during lifecycle hold",
		)
	})

	t.Run("level one without hold still requires splunkd", func(t *testing.T) {
		fakeBin := writeFakeProcessTable(
			t,
			"32005 coder ssh vworkstation.splunkdev.net --disable-autostart\n",
		)
		output, err := runLivenessProbe(
			t,
			scriptPath,
			"started\n",
			"export K8_OPERATOR_LIVENESS_LEVEL=1\n",
			"PATH="+fakeBin+":/usr/bin:/bin",
		)
		require.Error(t, err)
		require.Contains(t, string(output), "Splunkd not running")
	})

	t.Run("level one accepts the real splunkd start command", func(t *testing.T) {
		fakeBin := writeFakeProcessTable(
			t,
			"4242 ? S 0:00 /opt/splunk/bin/splunkd -p 8089 start\n",
		)
		output, err := runLivenessProbe(
			t,
			scriptPath,
			"started\n",
			"export K8_OPERATOR_LIVENESS_LEVEL=1\n",
			"PATH="+fakeBin+":/usr/bin:/bin",
		)
		require.NoError(t, err, string(output))
	})
}

func runLivenessProbe(
	t *testing.T,
	scriptPath string,
	state string,
	driver string,
	extraEnvironment ...string,
) ([]byte, error) {
	t.Helper()
	artifactDir := t.TempDir()
	driverPath := filepath.Join(t.TempDir(), "k8_liveness_driver.sh")
	if state != "" {
		require.NoError(
			t,
			os.WriteFile(
				filepath.Join(artifactDir, "splunk-container.state"),
				[]byte(state),
				0o600,
			),
		)
	}
	require.NoError(t, os.WriteFile(driverPath, []byte(driver), 0o600))

	command := exec.Command("/bin/bash", scriptPath)
	command.Env = append(
		filterProbeEnvironment(os.Environ()),
		"NO_HEALTHCHECK=",
		"CONTAINER_ARTIFACT_DIR="+artifactDir,
		"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH="+driverPath,
	)
	command.Env = append(command.Env, extraEnvironment...)
	return command.CombinedOutput()
}

func writeFakeProcessTable(t *testing.T, processTable string) string {
	t.Helper()
	fakeBin := t.TempDir()
	psPath := filepath.Join(fakeBin, "ps")
	psScript := "#!/bin/bash\nprintf '%s' " + shellSingleQuote(processTable) + "\n"
	require.NoError(t, os.WriteFile(psPath, []byte(psScript), 0o700))
	return fakeBin
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func filterProbeEnvironment(environment []string) []string {
	filtered := make([]string, 0, len(environment))
	for _, variable := range environment {
		if strings.HasPrefix(variable, "NO_HEALTHCHECK=") ||
			strings.HasPrefix(variable, "CONTAINER_ARTIFACT_DIR=") ||
			strings.HasPrefix(variable, "PATH=") ||
			strings.HasPrefix(variable, "SPLUNK_OPERATOR_LIFECYCLE_HOLD=") ||
			strings.HasPrefix(
				variable,
				"SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH=",
			) {
			continue
		}
		filtered = append(filtered, variable)
	}
	return filtered
}
