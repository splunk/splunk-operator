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
package scssanity

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splunkutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/test/testenv"
)

// ingestorDiscoveryLabels selects the IngestorCluster CR managed by the operator. Matches the
// label set the operator itself uses on ingestor-owned objects — see
// GetLabelTypes()["manager"]/["component"] in pkg/splunk/common/names.go and
// ApplyIngestorPodDisruptionBudget in pkg/splunk/enterprise/util.go.
var ingestorDiscoveryLabels = client.MatchingLabels{
	"app.kubernetes.io/managed-by": "splunk-operator",
	"app.kubernetes.io/component":  "ingestor",
}

// discoverIngestorCluster resolves the target IngestorCluster CR. When name is non-empty (the
// SCS_INGESTOR_NAME override) it is fetched directly from fallbackNamespace (or namespace, if
// set); otherwise the CR is discovered by label selector, scoped to namespace when non-empty,
// and must resolve to exactly one match — an empty or ambiguous result is a hard failure rather
// than a guess, since this suite must never act against the wrong tenant.
func discoverIngestorCluster(ctx context.Context, kubeClient client.Client, name, namespace, fallbackNamespace string) (*enterpriseApi.IngestorCluster, error) {
	if name != "" {
		ns := namespace
		if ns == "" {
			ns = fallbackNamespace
		}
		ic := &enterpriseApi.IngestorCluster{}
		if err := kubeClient.Get(ctx, client.ObjectKey{Name: name, Namespace: ns}, ic); err != nil {
			return nil, fmt.Errorf("failed to get IngestorCluster %s/%s (from SCS_INGESTOR_NAME override): %w", ns, name, err)
		}
		return ic, nil
	}

	ns := namespace
	if ns == "" {
		ns = fallbackNamespace
	}
	var list enterpriseApi.IngestorClusterList
	listOpts := []client.ListOption{ingestorDiscoveryLabels}
	if ns != "" {
		listOpts = append(listOpts, client.InNamespace(ns))
	}
	if err := kubeClient.List(ctx, &list, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list IngestorClusters by label %v: %w", ingestorDiscoveryLabels, err)
	}
	switch len(list.Items) {
	case 0:
		return nil, fmt.Errorf("no IngestorCluster found matching label selector %v; set SCS_INGESTOR_NAME/SCS_INGESTOR_NAMESPACE to override discovery", ingestorDiscoveryLabels)
	case 1:
		return &list.Items[0], nil
	default:
		return nil, fmt.Errorf("ambiguous IngestorCluster discovery: %d matches for label selector %v; set SCS_INGESTOR_NAME/SCS_INGESTOR_NAMESPACE to disambiguate", len(list.Items), ingestorDiscoveryLabels)
	}
}

// ingestorPodName returns the pod name of ingestor replica 0 for the given IngestorCluster.
func ingestorPodName(ic *enterpriseApi.IngestorCluster) string {
	return fmt.Sprintf(testenv.IngestorPod, ic.GetName(), 0)
}

// hecResponse mirrors the JSON body returned by the HEC /services/collector/event endpoint.
// AckID is only populated when the request carried a ?channel= parameter against an
// ACK-enabled token (see postHECEventWithChannel/pollHECAck below); it is silently omitted
// (zero value) for plain acks against a non-ACK token.
type hecResponse struct {
	Text  string `json:"text"`
	Code  int    `json:"code"`
	AckID int    `json:"ackId"`
}

// parseHECResponse extracts the JSON body from a curl -i response (status line + headers +
// blank line + body) and confirms it is the HEC "Success" acknowledgement.
func parseHECResponse(curlOutput string) (*hecResponse, error) {
	idx := strings.Index(curlOutput, "{")
	if idx == -1 {
		return nil, fmt.Errorf("no JSON body found in HEC response: %s", curlOutput)
	}
	var resp hecResponse
	if err := json.Unmarshal([]byte(curlOutput[idx:]), &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal HEC response %q: %w", curlOutput[idx:], err)
	}
	return &resp, nil
}

// scsSanityAckTokenName/scsSanityAckIndex name the dedicated ACK-enabled HEC token this suite
// provisions on the tenant ingestor (see ensureAckHECToken). Routed to the built-in _internal
// index rather than main: the ingestor role disables the ruleset/typing pipeline stages for all
// local indexing (pkg/splunk/splunkconfig/smartbus.go), so an event posted here is
// HEC-acknowledged but never durably indexed anywhere — it can't land in a customer-facing index.
const (
	scsSanityAckTokenName = "sok_scs_sanity_ack"
	scsSanityAckIndex     = "_internal"
)

// scsSanityAckTokenValueEnvVar is the CI/CD-managed secret supplying the ACK token's value. Must
// never be hardcoded: this token is provisioned on shared SCS tenant ingestors, so a value
// checked into the repo would be a live credential for every tenant this gate runs against.
const scsSanityAckTokenValueEnvVar = "SCS_SANITY_ACK_HEC_TOKEN"

// scsSanityAckTokenValue reads and validates the ACK token secret; the single place all callers
// (ensureAckHECToken, postHECEventWithChannel, pollHECAckOnce) get it from. Enforcing the HEC
// token format (pkg/splunk/util.ValidateHECToken) rejects shell-significant characters before the
// value is interpolated into a /bin/sh command by every caller.
func scsSanityAckTokenValue() (string, error) {
	v := os.Getenv(scsSanityAckTokenValueEnvVar)
	if v == "" {
		return "", fmt.Errorf("%s must be set to a high-entropy secret value to use the scs-sanity ACK HEC token", scsSanityAckTokenValueEnvVar)
	}
	if err := splunkutil.ValidateHECToken([]byte(v)); err != nil {
		return "", fmt.Errorf("%s is not a valid HEC token: %w", scsSanityAckTokenValueEnvVar, err)
	}
	return v, nil
}

// managementAPICurlStdin builds the /bin/sh stdin for a curl call against the pod's local
// management port (8089) authenticated as admin via the pod's own splunk-secrets password file.
// Shared by ensureAckHECToken's lookup and create calls so both stay in sync on how the admin
// credential is read and passed to curl.
func managementAPICurlStdin(pathAndArgs string) string {
	return fmt.Sprintf(
		`PW=$(cat /mnt/splunk-secrets/password); `+
			`curl -sk -o /dev/null -w '%%{http_code}' -u admin:$PW %s`,
		pathAndArgs,
	)
}

// ensureAckHECToken provisions (idempotently) a dedicated HEC token on the ingestor pod with
// useACK=1, routed to the _internal index so it can never land in a customer-facing index (see
// scsSanityAckIndex above). This exists solely so assertHECIngest can get an unambiguous,
// per-request acknowledgement from splunkd's HTTP input handler itself (see pollHECAck) — the
// ACK protocol is tracked before the ruleset/typing pipeline stages, which the ingestor role
// disables for all local indexing (see pkg/splunk/splunkconfig/smartbus.go), so this is the only
// in-pod signal that ties a specific probe request to a specific accepted-by-splunkd result.
// Never touches the tenant's own HEC token(s) or any customer-facing index.
func ensureAckHECToken(ctx context.Context, dep *testenv.Deployment, podName, namespace string) error {
	tokenValue, err := scsSanityAckTokenValue()
	if err != nil {
		return err
	}

	checkStdin := managementAPICurlStdin(fmt.Sprintf(
		"https://localhost:8089/servicesNS/nobody/splunk_httpinput/data/inputs/http/http%%3A%%2F%%2F%s",
		scsSanityAckTokenName,
	))
	stdout, _, err := dep.PodExecCommandInNamespace(ctx, podName, namespace, []string{"/bin/sh"}, checkStdin, false)
	if err != nil {
		return fmt.Errorf("failed to exec HEC token lookup on pod %s: %w", podName, err)
	}
	if strings.TrimSpace(stdout) == "200" {
		// Token already exists from a prior run of this gate against the same tenant.
		return nil
	}

	createStdin := managementAPICurlStdin(fmt.Sprintf(
		"https://localhost:8089/services/data/inputs/http -d name=%s -d useACK=1 -d index=%s -d token=%s",
		scsSanityAckTokenName, scsSanityAckIndex, tokenValue,
	))
	stdout, _, err = dep.PodExecCommandInNamespace(ctx, podName, namespace, []string{"/bin/sh"}, createStdin, false)
	if err != nil {
		return fmt.Errorf("failed to exec HEC token provisioning on pod %s: %w", podName, err)
	}
	status := strings.TrimSpace(stdout)
	if status != "200" && status != "201" {
		return fmt.Errorf("failed to provision scs-sanity ACK HEC token on pod %s: unexpected HTTP status %q", podName, status)
	}
	return nil
}

// postHECEventWithChannel POSTs a uniquely-tagged event to the dedicated ACK-enabled sanity
// token (see ensureAckHECToken), tagged with a fresh HEC channel GUID, and returns the parsed
// acknowledgement (which carries an ackId when the token has useACK=1).
func postHECEventWithChannel(ctx context.Context, dep *testenv.Deployment, podName, namespace, marker, channel string) (*hecResponse, error) {
	tokenValue, err := scsSanityAckTokenValue()
	if err != nil {
		return nil, err
	}

	stdin := fmt.Sprintf(
		`curl -sik -H 'Authorization: Splunk %s' `+
			`'https://localhost:8088/services/collector/event?channel=%s' `+
			`-d '{"event":"scs-sanity marker=%s","sourcetype":"scs:sanity:ack"}'`,
		tokenValue, channel, marker,
	)
	stdout, _, err := dep.PodExecCommandInNamespace(ctx, podName, namespace, []string{"/bin/sh"}, stdin, false)
	if err != nil {
		return nil, fmt.Errorf("failed to exec HEC ACK-channel POST on pod %s: %w", podName, err)
	}
	return parseHECResponse(stdout)
}

// hecAckResponse mirrors the JSON body returned by the HEC /services/collector/ack endpoint.
type hecAckResponse struct {
	Acks map[string]bool `json:"acks"`
}

// pollHECAckOnce issues a single GET against /services/collector/ack for the given channel and
// ackID, returning whether splunkd has durably accepted that specific request. This is the
// unambiguous signal assertHECIngest gates on: it is tied to one exact POST (by channel+ackId,
// not by a counter or log line that any other request could also move), and it fires before the
// ruleset/typing pipeline stages that are disabled for the ingestor role — so it works even
// though the event itself is never locally indexed or searchable.
func pollHECAckOnce(ctx context.Context, dep *testenv.Deployment, podName, namespace, channel string, ackID int) (bool, error) {
	tokenValue, err := scsSanityAckTokenValue()
	if err != nil {
		return false, err
	}

	stdin := fmt.Sprintf(
		`curl -sk -H 'Authorization: Splunk %s' `+
			`'https://localhost:8088/services/collector/ack?channel=%s' -d '{"acks":[%d]}'`,
		tokenValue, channel, ackID,
	)
	stdout, _, err := dep.PodExecCommandInNamespace(ctx, podName, namespace, []string{"/bin/sh"}, stdin, false)
	if err != nil {
		return false, fmt.Errorf("failed to exec HEC ack poll on pod %s: %w", podName, err)
	}
	var resp hecAckResponse
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		return false, fmt.Errorf("failed to unmarshal HEC ack response %q: %w", stdout, err)
	}
	return resp.Acks[fmt.Sprintf("%d", ackID)], nil
}

// operatorDeploymentHealthy fetches the operator Deployment and reports whether its rollout is
// fully healthy: all replicas ready, and (when targetImage is non-empty) the manager container
// running that exact image.
func operatorDeploymentHealthy(ctx context.Context, kubeClient client.Client, namespace, name, targetImage string) error {
	dep := &appsv1.Deployment{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, dep); err != nil {
		return fmt.Errorf("failed to get operator Deployment %s/%s: %w", namespace, name, err)
	}
	if dep.Status.ReadyReplicas != dep.Status.Replicas || dep.Status.Replicas == 0 {
		return fmt.Errorf("operator Deployment %s/%s not fully ready: readyReplicas=%d replicas=%d",
			namespace, name, dep.Status.ReadyReplicas, dep.Status.Replicas)
	}
	if targetImage == "" {
		return nil
	}
	for _, c := range dep.Spec.Template.Spec.Containers {
		if c.Name == "manager" {
			if c.Image != targetImage {
				return fmt.Errorf("operator Deployment %s/%s manager container image is %s, expected %s",
					namespace, name, c.Image, targetImage)
			}
			return nil
		}
	}
	return fmt.Errorf("operator Deployment %s/%s has no container named %q", namespace, name, "manager")
}

// operatorLeaderElected fetches the operator's leader-election Lease (id "270bec8c.splunk.com",
// see cmd/main.go's LeaderElectionID) and confirms it has a holder that is still actively
// renewing, not a stale holder left behind by a replica that stopped renewing during upgrade.
// holderIdentity itself is never cleared on its own, so staleness is checked the same way
// client-go's leaderelection.go does internally: renewTime + leaseDurationSeconds must still be
// in the future.
func operatorLeaderElected(ctx context.Context, kubeClient client.Client, namespace string) error {
	lease := &coordinationv1.Lease{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Name: "270bec8c.splunk.com", Namespace: namespace}, lease); err != nil {
		return fmt.Errorf("failed to get operator leader-election lease in %s: %w", namespace, err)
	}
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "" {
		return fmt.Errorf("operator leader-election lease in %s has no holder", namespace)
	}
	if lease.Spec.RenewTime == nil || lease.Spec.LeaseDurationSeconds == nil {
		return fmt.Errorf("operator leader-election lease in %s (holder %s) is missing renewTime/leaseDurationSeconds, cannot confirm it is current",
			namespace, *lease.Spec.HolderIdentity)
	}
	leaseDuration := time.Duration(*lease.Spec.LeaseDurationSeconds) * time.Second
	expiry := lease.Spec.RenewTime.Time.Add(leaseDuration)
	if time.Now().After(expiry) {
		return fmt.Errorf("operator leader-election lease in %s (holder %s) is stale: last renewed at %s, lease duration %s, expired at %s",
			namespace, *lease.Spec.HolderIdentity, lease.Spec.RenewTime.Time, leaseDuration, expiry)
	}
	return nil
}

// podRestartSnapshot captures restart counts for a pod, keyed by container name, so a later
// snapshot can detect any restart across the deploy window.
type podRestartSnapshot map[string]int32

// snapshotPodRestarts returns the current restart count for every container in the named pod.
func snapshotPodRestarts(ctx context.Context, kubeClient client.Client, namespace, podName string) (podRestartSnapshot, error) {
	pod := &corev1.Pod{}
	if err := kubeClient.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, pod); err != nil {
		return nil, fmt.Errorf("failed to get pod %s/%s for restart snapshot: %w", namespace, podName, err)
	}
	snap := make(podRestartSnapshot, len(pod.Status.ContainerStatuses))
	for _, cs := range pod.Status.ContainerStatuses {
		snap[cs.Name] = cs.RestartCount
	}
	return snap, nil
}

// diffPodRestarts returns an error naming every container whose restart count increased
// between before and after — i.e. any disruption caused within the observed window.
func diffPodRestarts(podName string, before, after podRestartSnapshot) error {
	var restarted []string
	for container, beforeCount := range before {
		if afterCount, ok := after[container]; ok && afterCount > beforeCount {
			restarted = append(restarted, fmt.Sprintf("%s (%d->%d)", container, beforeCount, afterCount))
		}
	}
	if len(restarted) > 0 {
		return fmt.Errorf("pod %s: unexpected container restart(s) during deploy window: %s", podName, strings.Join(restarted, ", "))
	}
	return nil
}

// tenantBaseline is the pre-upgrade snapshot of the tenant IngestorCluster, written to disk by
// the "capture baseline" spec and read back by the "verify non-disruption" spec — the two run
// as separate ginkgo invocations (see gitlab-ci/scs-sanity-gate.sh) with the actual Helm upgrade
// happening in between, so this state cannot be threaded through in-process Ordered specs.
type tenantBaseline struct {
	IngestorNamespace string                            `json:"ingestorNamespace"`
	IngestorName      string                            `json:"ingestorName"`
	Phase             enterpriseApi.Phase               `json:"phase"`
	Replicas          int32                             `json:"replicas"`
	ReadyReplicas     int32                             `json:"readyReplicas"`
	ResourceVersion   string                            `json:"resourceVersion"`
	Spec              enterpriseApi.IngestorClusterSpec `json:"spec"`
	PodRestarts       podRestartSnapshot                `json:"podRestarts"`
}

// writeTenantBaseline serializes the baseline to path, creating parent directories as needed.
func writeTenantBaseline(path string, baseline tenantBaseline) error {
	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tenant baseline: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create directory for tenant baseline %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write tenant baseline to %s: %w", path, err)
	}
	return nil
}

// readTenantBaseline reads back a baseline written by writeTenantBaseline.
func readTenantBaseline(path string) (*tenantBaseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read tenant baseline from %s: %w", path, err)
	}
	var baseline tenantBaseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tenant baseline from %s: %w", path, err)
	}
	return &baseline, nil
}

// discoverIngestor resolves the target IngestorCluster/pod, returning the discovered CR (and its
// pod/namespace, via ingestorPodName/GetNamespace on the result) rather than reaching into
// package-level state — callers in scs_sanity_test.go decide how the result is threaded across
// specs and assert on the returned error themselves. Both the pre-upgrade and post-upgrade
// phases call this independently — they are separate ginkgo invocations (see
// gitlab-ci/scs-sanity-gate.sh) with a real Helm upgrade in between, so nothing about the
// discovered tenant can be assumed to already be in memory.
func discoverIngestor(ctx context.Context, kubeClient client.Client, name, namespace, fallbackNamespace string) (*enterpriseApi.IngestorCluster, error) {
	return discoverIngestorCluster(ctx, kubeClient, name, namespace, fallbackNamespace)
}

// waitIngestorReady blocks until the named IngestorCluster reaches a steady Ready phase,
// returning the last-observed object (refreshed on every poll) once it does, or the last
// observed error if it never does within timeout/pollInterval.
func waitIngestorReady(ctx context.Context, kubeClient client.Client, name, namespace string, timeout, pollInterval time.Duration) (*enterpriseApi.IngestorCluster, error) {
	var (
		ic      enterpriseApi.IngestorCluster
		lastErr error
	)
	waitErr := wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		lastErr = func() error {
			if err := kubeClient.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, &ic); err != nil {
				return err
			}
			if ic.Status.Phase != enterpriseApi.PhaseReady {
				return fmt.Errorf("IngestorCluster %s/%s phase is %s, expected %s", namespace, name, ic.Status.Phase, enterpriseApi.PhaseReady)
			}
			if ic.Status.ReadyReplicas != ic.Status.Replicas || ic.Status.Replicas == 0 {
				return fmt.Errorf("IngestorCluster %s/%s not fully ready: readyReplicas=%d replicas=%d", namespace, name, ic.Status.ReadyReplicas, ic.Status.Replicas)
			}
			return testenv.VerifyCRConditionsForPhase("IngestorCluster", name, ic.Status.Conditions, enterpriseApi.PhaseReady)
		}()
		return lastErr == nil, nil
	})
	if waitErr != nil {
		return nil, fmt.Errorf("IngestorCluster %s/%s did not reach a steady Ready phase within %s: %w", namespace, name, timeout, lastErr)
	}
	return &ic, nil
}

// assertHECIngest POSTs a uniquely-tagged HEC event to the ingestor pod's dedicated ACK-enabled
// sanity token and confirms splunkd durably accepted that exact request, returning an error
// (rather than failing the spec directly) so both pre- and post-upgrade specs in
// scs_sanity_test.go can share this gate and assert on it themselves.
//
// index=_internal can't be used to verify ingest here since the ingestor role disables the
// ruleset/typing pipeline stages for all local indexing (pkg/splunk/splunkconfig/smartbus.go).
// HEC's ACK protocol is tracked before those stages, so polling by channel+ackId (both random
// per call) gives an unambiguous signal tied to this exact request.
func assertHECIngest(ctx context.Context, dep *testenv.Deployment, podName, namespace, marker string, timeout, pollInterval time.Duration) error {
	if err := ensureAckHECToken(ctx, dep, podName, namespace); err != nil {
		return fmt.Errorf("failed to provision dedicated ACK HEC token on ingestor pod %s: %w", podName, err)
	}

	channel := uuid.NewString()
	resp, err := postHECEventWithChannel(ctx, dep, podName, namespace, marker, channel)
	if err != nil {
		return fmt.Errorf("failed to POST HEC event to ingestor pod %s: %w", podName, err)
	}
	if resp.Code != 0 {
		return fmt.Errorf("HEC did not acknowledge success for pod %s: %+v", podName, resp)
	}

	// Mandatory gate: splunkd's HTTP input handler durably accepted this exact
	// channel+ackId — proof the specific probe request (not just HEC-in-general) was handled.
	var lastPollErr error
	waitErr := wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		acked, pollErr := pollHECAckOnce(ctx, dep, podName, namespace, channel, resp.AckID)
		lastPollErr = pollErr
		return pollErr == nil && acked, nil
	})
	if waitErr != nil {
		if lastPollErr != nil {
			return fmt.Errorf("ingestor pod %s never durably acknowledged HEC channel=%s ackId=%d: %w", podName, channel, resp.AckID, lastPollErr)
		}
		return fmt.Errorf("ingestor pod %s never durably acknowledged HEC channel=%s ackId=%d", podName, channel, resp.AckID)
	}
	return nil
}
