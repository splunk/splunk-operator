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
package testenv

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

const (
	// ingestorRestartPollInterval is how often to re-check during rolling-restart waits.
	ingestorRestartPollInterval = 15 * time.Second

	// ingestorRestartTimeout is the maximum time to wait for a full rolling restart (3 replicas).
	ingestorRestartTimeout = 15 * time.Minute
)

// TriggerIngestorRestartRequired execs into each ingestor pod and POSTs a conf change
// that causes Splunk to set restart_required. The curl runs inside the pod against
// localhost:8089 so no port-forwarding is needed.
// It resets indexAndForward to true first so the subsequent false always re-triggers the flag
// even if the setting was already false from a prior run.
func TriggerIngestorRestartRequired(ctx context.Context, deployment *Deployment, icName string, replicas int) error {
	for i := 0; i < replicas; i++ {
		podName := fmt.Sprintf("splunk-%s-ingestor-%d", icName, i)

		resetCmd := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) " +
			"-X POST https://localhost:8089/services/data/outputs/tcp/default/tcpout " +
			"-d indexAndForward=true -o /dev/null -w '%{http_code}'"
		resetStdout, _, err := deployment.PodExecCommand(ctx, podName, []string{"/bin/sh"}, resetCmd, false)
		if err != nil {
			return fmt.Errorf("reset indexAndForward on pod %s: %w", podName, err)
		}
		if !strings.Contains(resetStdout, "200") {
			return fmt.Errorf("reset indexAndForward on pod %s: unexpected HTTP status %q", podName, resetStdout)
		}

		triggerCmd := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) " +
			"-X POST https://localhost:8089/services/data/outputs/tcp/default/tcpout " +
			"-d indexAndForward=false -o /dev/null -w '%{http_code}'"
		stdout, _, err := deployment.PodExecCommand(ctx, podName, []string{"/bin/sh"}, triggerCmd, false)
		if err != nil {
			return fmt.Errorf("trigger restart_required on pod %s: %w", podName, err)
		}
		if !strings.Contains(stdout, "200") {
			return fmt.Errorf("trigger restart_required on pod %s: unexpected HTTP status %q", podName, stdout)
		}
	}
	return nil
}

// WaitForIngestorRollingRestartComplete polls until the IngestorCluster is back in
// Ready phase with Restarting=False/RollingRestartComplete. notBefore rejects stale
// conditions set before the restart was triggered.
func WaitForIngestorRollingRestartComplete(ctx context.Context, testcaseEnvInst *TestCaseEnv, icName string, notBefore time.Time) error {
	ic := &enterpriseApi.IngestorCluster{}
	return wait.PollUntilContextTimeout(ctx, ingestorRestartPollInterval, ingestorRestartTimeout, true, func(ctx context.Context) (bool, error) {
		if err := testcaseEnvInst.GetKubeClient().Get(ctx,
			types.NamespacedName{Name: icName, Namespace: testcaseEnvInst.GetName()}, ic); err != nil {
			return false, nil
		}
		if ic.Status.Phase != enterpriseApi.PhaseReady {
			return false, nil
		}
		for _, cond := range ic.Status.Conditions {
			if cond.Type == string(enterpriseApi.ConditionRestarting) &&
				cond.Status == metav1.ConditionFalse &&
				cond.Reason == string(enterpriseApi.ReasonRollingRestartComplete) &&
				!cond.LastTransitionTime.Before(&metav1.Time{Time: notBefore}) {
				return true, nil
			}
		}
		return false, nil
	})
}

// VerifyIngestorRestartCleared asserts that each pod's Splunk REST endpoint reports no
// restart_required. HTTP 404 on /services/messages/restart_required means the message is
// absent (clean).
func VerifyIngestorRestartCleared(ctx context.Context, deployment *Deployment, replicas int, icName string) error {
	for i := 0; i < replicas; i++ {
		podName := fmt.Sprintf("splunk-%s-ingestor-%d", icName, i)
		checkCmd := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) " +
			"https://localhost:8089/services/messages/restart_required?output_mode=json " +
			"-o /dev/null -w '%{http_code}'"
		stdout, _, err := deployment.PodExecCommand(ctx, podName, []string{"/bin/sh"}, checkCmd, false)
		if err != nil {
			return fmt.Errorf("check restart_required on pod %s: %w", podName, err)
		}
		// 404 = message absent = pod restarted cleanly; anything else = still pending.
		if !strings.Contains(stdout, "404") {
			return fmt.Errorf("pod %s still reports restart_required (HTTP %s)", podName, stdout)
		}
	}
	return nil
}
