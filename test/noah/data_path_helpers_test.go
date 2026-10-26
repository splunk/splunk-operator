// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package noah

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	noahclient "github.com/splunk/splunk-operator/pkg/splunk/client/noah"
	"github.com/splunk/splunk-operator/test/testenv"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type dataPathBucketRow struct {
	BucketID string `json:"bucket_id"`
}

func waitForRolledMarkerBucket(
	ctx context.Context,
	searchHead string,
	podName string,
	peerID string,
	noah *noahclient.Client,
	marker string,
	timeout time.Duration,
) string {
	GinkgoHelper()

	searchCtx, cancelSearch := context.WithTimeout(ctx, timeout)
	defer cancelSearch()

	search := fmt.Sprintf(
		"index=main sourcetype=%s %s %s | head 1 | eval bucket_id=_bkt | table bucket_id",
		dataPathSourcetype,
		marker,
		podName,
	)
	var bucketID string
	DeferCleanup(func() {
		// Only the invocation that never found a bucket is the cause of a
		// failed spec; later indexers in the loop never ran their own search.
		if bucketID != "" || !CurrentSpecReport().Failed() {
			return
		}
		dumpDataPathIndexingState(searchHead, podName, peerID, noah)
	})
	Eventually(func() (string, error) {
		if !testenv.RollHotToWarm(searchCtx, deployment, podName, "main") {
			return "", fmt.Errorf("roll the main index hot buckets on %s", podName)
		}

		// Eventually reports the last error on timeout, so name which of the
		// IndexerCluster status, Noah, or search disagreed.
		if err := checkNoahPeerReady(searchCtx, podName); err != nil {
			return "", err
		}
		peer, err := noah.GetPeer(searchCtx, peerID)
		if err != nil {
			stopOnTerminalNoahError(err, "Noah rejected the peer observation while waiting for the data path")
			return "", fmt.Errorf("get Noah peer %s: %w", peerID, err)
		}
		if peer.Status != noahclient.PeerStatusUp {
			return "", fmt.Errorf("Noah reports peer %s as %q, not up", peerID, peer.Status)
		}

		response, err := testenv.PerformSearchSync(searchCtx, deployment, searchHead, search)
		if err != nil {
			return "", err
		}
		rows, err := testenv.ParseSearchExportResults[dataPathBucketRow](response)
		if err != nil {
			return "", err
		}
		if len(rows) != 1 {
			return "", fmt.Errorf("expected one bucket for marker on %s, got %d", podName, len(rows))
		}
		bucketID = strings.TrimSpace(rows[0].BucketID)
		return bucketID, nil
	}).
		WithContext(searchCtx).
		WithTimeout(timeout).
		WithPolling(testenv.PollInterval).
		ShouldNot(BeEmpty(), "marker did not become searchable on %s", podName)

	return bucketID
}

// checkNoahPeerReady requires the same generation-current Noah conditions as
// waitForC3Ready plus a searchable peer entry. It deliberately ignores
// IndexingReady: Noah-backed clusters have no ClusterManager to set it.
func checkNoahPeerReady(ctx context.Context, podName string) error {
	idxc := &enterpriseApi.IndexerCluster{}
	if err := testcaseEnvInstance.GetKubeClient().Get(
		ctx, client.ObjectKey{Namespace: operatorNamespace, Name: clusterName}, idxc,
	); err != nil {
		return fmt.Errorf("get IndexerCluster %s/%s: %w", operatorNamespace, clusterName, err)
	}
	if err := testenv.VerifyIndexerClusterReadyStatus(
		idxc,
		enterpriseApi.ConditionNoahDependencyResolved,
		enterpriseApi.ConditionNoahPeersReady,
	); err != nil {
		return err
	}
	for _, peer := range idxc.Status.Peers {
		if peer.Name != podName {
			continue
		}
		if peer.Status != "Up" || !peer.Searchable {
			return fmt.Errorf(
				"IndexerCluster reports peer %s as status=%q searchable=%t",
				podName, peer.Status, peer.Searchable,
			)
		}
		return nil
	}
	return fmt.Errorf("IndexerCluster does not report peer %s", podName)
}

// dumpDataPathIndexingState searches through the search head, scoped to one
// peer, because a Noah indexer returns no results to searches run locally on
// it. It also dumps Noah's peer/bucket-map view. Everything written here lands
// in the published JUnit unsanitized, so it must stay limited to identifiers
// and statuses (no raw splunkd logs or free-form fields).
func dumpDataPathIndexingState(searchHead string, podName string, peerID string, noah *noahclient.Client) {
	probeCtx, cancel := context.WithTimeout(context.Background(), testenv.BestEffortProbeTimeout)
	defer cancel()

	peerFilter := fmt.Sprintf("splunk_server=%s.*", podName)
	sourcetypeSearch := fmt.Sprintf(
		"index=main %s sourcetype=%s | stats count by host | table host count",
		peerFilter,
		dataPathSourcetype,
	)
	runDataPathDiagnosticSearch(probeCtx, searchHead, "sourcetype-filtered", sourcetypeSearch)

	// Unfiltered by sourcetype, so a sourcetype override elsewhere in the
	// stack shows up as different metadata instead of no results.
	rawSearch := fmt.Sprintf("index=main %s | head 5 | table _time sourcetype source host _raw", peerFilter)
	runDataPathDiagnosticSearch(probeCtx, searchHead, "filter-free", rawSearch)

	dumpNoahPeerState(probeCtx, podName, peerID, noah)
}

// dumpNoahPeerState surfaces Noah's own view of the peer and the latest
// bucket map, independent of both Splunk search and the ClusterManager
// status already dumped by dumpC3FailureState, so a failure can be told
// apart as "Noah itself does not consider this peer current" versus
// "Noah agrees the peer is up, yet search still finds nothing".
func dumpNoahPeerState(ctx context.Context, podName string, peerID string, noah *noahclient.Client) {
	peer, err := noah.GetPeer(ctx, peerID)
	if err != nil {
		fmt.Fprintf(GinkgoWriter, "data-path failure diagnostics: Noah peer %s (pod %s) lookup failed: %v\n", peerID, podName, err)
	} else {
		fmt.Fprintf(GinkgoWriter,
			"data-path failure diagnostics: Noah peer %s (pod %s): status=%s lastHeartbeat=%d startTime=%d siteID=%q decommissionReady=%q\n",
			peerID, podName, peer.Status, peer.LastHeartbeat, peer.Data.StartTime, peer.Data.SiteID, peer.Data.DecommissionReady)
	}

	bucketMap, err := noah.GetLatestBucketMap(ctx)
	if err != nil {
		fmt.Fprintf(GinkgoWriter, "data-path failure diagnostics: Noah latest bucket map lookup failed: %v\n", err)
		return
	}
	fmt.Fprintf(GinkgoWriter, "data-path failure diagnostics: Noah latest bucket map id=%d status=%s peerIDs=%v\n",
		bucketMap.ID, bucketMap.Status, bucketMap.PeerIDs)
}

func runDataPathDiagnosticSearch(ctx context.Context, searchHead string, label string, search string) {
	response, err := testenv.PerformSearchSync(ctx, deployment, searchHead, search)
	if err != nil {
		fmt.Fprintf(GinkgoWriter, "data-path failure diagnostics: %s search via %s failed: %v\n", label, searchHead, err)
		return
	}
	fmt.Fprintf(GinkgoWriter, "data-path failure diagnostics: %s search via %s:\n%s\n", label, searchHead, response)
}

func markerPeer(raw string) (string, error) {
	for _, field := range strings.Fields(raw) {
		if strings.HasPrefix(field, "peer=") {
			peer := strings.TrimPrefix(field, "peer=")
			if peer != "" {
				return peer, nil
			}
		}
	}
	return "", fmt.Errorf("marker event has no peer field: %q", raw)
}
