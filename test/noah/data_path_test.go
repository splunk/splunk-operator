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
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const dataPathSourcetype = "noah_c3_data_path"

type dataPathSearchSummary struct {
	Events         int
	IngestedPeers  []string
	SearchPeerPods []string
}

type dataPathSearchRow struct {
	Raw          string `json:"_raw"`
	SplunkServer string `json:"splunk_server"`
}

var _ = Describe(
	"Noah-backed C3 data path",
	Serial,
	Label("tier:noah-e2e", "sva:c3", "cloud:kraken", "variant:noah", "feature:noah", "scenario:data-path"),
	func() {
		// This scenario keeps each oneshot source in place while rolling main and
		// polling through the SHC. Once the marker is searchable, it captures the
		// exact bucket ID, verifies that bucket's remote receipt in Kraken S3, and
		// finally searches all markers through the SHC. It does not claim to test
		// a cold-cache download or SQS message delivery.
		It(
			"persists one marked event per indexer to S3 and finds all markers through the search head",
			NodeTimeout(testenv.MediumLongTimeout),
			func(ctx SpecContext) {
				dataPathTimeout, err := time.ParseDuration(
					testenv.GetEnvWithDefault("NOAH_TEST_DATA_PATH_TIMEOUT", "3m"),
				)
				Expect(err).NotTo(HaveOccurred(), "NOAH_TEST_DATA_PATH_TIMEOUT must be a Go duration")

				_, err = checkKrakenExternalStorage(ctx)
				Expect(err).NotTo(HaveOccurred(), "Kraken external storage is not wired as expected")
				topology := waitForC3Ready(ctx)
				Expect(topology.IndexerPods).NotTo(BeEmpty())
				Expect(topology.SearchHeadPods).NotTo(BeEmpty())
				searchHead := topology.SearchHeadPods[0]
				noah := newNoahClient(ctx)

				marker := "noahc3data" + testenv.RandomDNSName(10)
				for _, podName := range topology.IndexerPods {
					pod := &corev1.Pod{}
					Expect(testcaseEnvInstance.GetKubeClient().Get(
						ctx, client.ObjectKey{Namespace: operatorNamespace, Name: podName}, pod,
					)).To(Succeed(), "failed to get Pod %s", podName)
					peerID := indexerPeerID(pod)

					By("ingesting a unique marker on " + podName)
					markerFile, err := os.CreateTemp("", marker+"-*.log")
					Expect(err).NotTo(HaveOccurred(), "failed to create marker file for %s", podName)
					markerPath := markerFile.Name()
					remoteMarkerPath := "/tmp/" + filepath.Base(markerPath)
					DeferCleanup(func() {
						_ = markerFile.Close()
						if removeErr := os.Remove(markerPath); removeErr != nil && !os.IsNotExist(removeErr) {
							GinkgoWriter.Printf("failed to remove local marker file %s: %v\n", markerPath, removeErr)
						}
					})
					DeferCleanup(func(cleanupCtx SpecContext) {
						_, stderr, cleanupErr := deployment.PodExecCommand(
							cleanupCtx,
							podName,
							[]string{"rm", "-f", remoteMarkerPath},
							"",
							false,
						)
						Expect(cleanupErr).NotTo(HaveOccurred(), "failed to remove marker file from %s: %s", podName, stderr)
					})

					_, err = fmt.Fprintf(markerFile, "marker=%s peer=%s\n", marker, podName)
					Expect(err).NotTo(HaveOccurred(), "failed to write marker file for %s", podName)
					Expect(markerFile.Close()).To(Succeed(), "failed to close marker file for %s", podName)

					err = testenv.IngestFileViaOneshotAtPathWithSourcetype(
						ctx,
						deployment,
						markerPath,
						remoteMarkerPath,
						"main",
						dataPathSourcetype,
						podName,
					)
					Expect(err).To(Succeed(), "failed to ingest marker on %s", podName)

					// The oneshot command acknowledges that the input was queued, not
					// that Splunk finished reading it. Keep the source, roll hot buckets,
					// and search through the SHC until the marker's bucket is visible.
					By("waiting for the marker from " + podName + " to be searchable through " + searchHead)
					bucketID := waitForRolledMarkerBucket(
						ctx,
						searchHead,
						podName,
						peerID,
						noah,
						marker,
						dataPathTimeout,
					)

					_, stderr, err := deployment.PodExecCommand(
						ctx,
						podName,
						[]string{"rm", "-f", remoteMarkerPath},
						"",
						false,
					)
					Expect(err).To(Succeed(), "failed to remove marker file from %s: %s", podName, stderr)

					By("verifying the exact rolled bucket through Splunk's S3 client on " + podName)
					Expect(testenv.WaitForRemoteBucket(ctx, deployment, podName, bucketID, dataPathTimeout)).To(
						Succeed(),
						"bucket %s was not persisted to Kraken S3 by %s",
						bucketID,
						podName,
					)
				}

				search := fmt.Sprintf(
					"index=main sourcetype=%s %s | table _raw splunk_server",
					dataPathSourcetype,
					marker,
				)
				expectedIndexerPods := append([]string(nil), topology.IndexerPods...)
				sort.Strings(expectedIndexerPods)
				allowedSearchPeers := make(map[string]struct{}, len(expectedIndexerPods))
				for _, podName := range expectedIndexerPods {
					allowedSearchPeers[podName] = struct{}{}
				}

				By("waiting for every indexer's marker to be searchable through " + searchHead)
				searchCtx, cancelSearch := context.WithTimeout(ctx, dataPathTimeout)
				defer cancelSearch()
				Eventually(func() error {
					response, searchErr := testenv.PerformSearchSync(searchCtx, deployment, searchHead, search)
					if searchErr != nil {
						return searchErr
					}
					results, parseErr := testenv.ParseSearchExportResults[dataPathSearchRow](response)
					if parseErr != nil {
						return parseErr
					}

					ingestedPeers := make(map[string]struct{}, len(results))
					searchPeerPods := make(map[string]struct{}, len(results))
					for _, result := range results {
						peer, peerErr := markerPeer(result.Raw)
						if peerErr != nil {
							return peerErr
						}
						ingestedPeers[peer] = struct{}{}
						searchPeerPod := strings.SplitN(result.SplunkServer, ".", 2)[0]
						searchPeerPods[searchPeerPod] = struct{}{}
					}

					ingestedPeerNames := make([]string, 0, len(ingestedPeers))
					for peer := range ingestedPeers {
						ingestedPeerNames = append(ingestedPeerNames, peer)
					}
					sort.Strings(ingestedPeerNames)
					searchPeerNames := make([]string, 0, len(searchPeerPods))
					for peer := range searchPeerPods {
						searchPeerNames = append(searchPeerNames, peer)
					}
					sort.Strings(searchPeerNames)

					summary := dataPathSearchSummary{
						Events:         len(results),
						IngestedPeers:  ingestedPeerNames,
						SearchPeerPods: searchPeerNames,
					}
					if summary.Events != len(expectedIndexerPods) {
						return fmt.Errorf("expected %d marker events, got %d", len(expectedIndexerPods), summary.Events)
					}
					if !slices.Equal(summary.IngestedPeers, expectedIndexerPods) {
						return fmt.Errorf("expected marker peers %v, got %v", expectedIndexerPods, summary.IngestedPeers)
					}
					if len(summary.SearchPeerPods) == 0 {
						return fmt.Errorf("search returned no serving indexer")
					}
					for _, searchPeer := range summary.SearchPeerPods {
						if _, allowed := allowedSearchPeers[searchPeer]; !allowed {
							return fmt.Errorf("search was served by unexpected indexer %q; ready indexers are %v", searchPeer, expectedIndexerPods)
						}
					}

					return nil
				}).
					WithContext(searchCtx).
					WithTimeout(dataPathTimeout).
					WithPolling(testenv.PollInterval).
					Should(Succeed())
			},
		)
	},
)
