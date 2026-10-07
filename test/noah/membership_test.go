// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package noah

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"

	noahclient "github.com/splunk/splunk-operator/pkg/splunk/client/noah"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/splunk/splunk-operator/test/testenv"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Noah membership", Serial,
	Label("tier:noah-e2e", "sva:c3", "cloud:kraken", "variant:noah", "feature:noah", "scenario:membership"),
	func() {
		It("reports current peers at their advertised management addresses", NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {
			topology := waitForC3Ready(ctx)
			Expect(topology.IndexerPods).NotTo(BeEmpty())
			noah := newNoahClient(ctx)
			waitForMembership(ctx, noah, topology.IndexerPods)
		})
	})

func waitForMembership(ctx context.Context, noah *noahclient.Client, podNames []string) {
	GinkgoHelper()
	Eventually(func() error {
		err := inspectMembership(ctx, noah, podNames)
		stopOnTerminalNoahError(err, "Noah rejected the membership observation")
		return err
	}).WithContext(ctx).WithTimeout(testenv.ShortTimeout).
		WithPolling(testenv.PollInterval).Should(Succeed())
}

func inspectMembership(ctx context.Context, noah *noahclient.Client, podNames []string) error {
	peers, err := noah.ListPeers(ctx)
	if err != nil {
		return err
	}
	for _, name := range podNames {
		pod := &corev1.Pod{}
		if err := testcaseEnvInstance.GetKubeClient().Get(ctx,
			client.ObjectKey{Namespace: operatorNamespace, Name: name}, pod); err != nil {
			return err
		}
		if err := verifyPeer(pod, peers); err != nil {
			return err
		}
	}
	return nil
}

// verifyPeer deliberately re-implements the accepted identity and incarnation
// contract rather than calling the operator's membership evaluation. An
// end-to-end check that shared the production predicate could not detect a bug
// in it.
func verifyPeer(pod *corev1.Pod, peers []noahclient.Peer) error {
	if err := testenv.VerifyPodReady(pod, "splunk"); err != nil {
		return err
	}
	statusIndex := slices.IndexFunc(pod.Status.ContainerStatuses, func(status corev1.ContainerStatus) bool {
		return status.Name == "splunk"
	})
	status := pod.Status.ContainerStatuses[statusIndex]
	if status.State.Running == nil || status.State.Running.StartedAt.IsZero() || pod.Spec.Subdomain == "" {
		return fmt.Errorf("Pod %s has no running incarnation or governing Service", pod.Name)
	}
	id := indexerPeerID(pod)
	expected := "https://" + id + ":8089"
	configured, err := advertisedAddress(pod)
	if err != nil {
		return err
	}
	if configured != expected {
		return fmt.Errorf("Pod %s configures advertised address %q, expected %q", pod.Name, configured, expected)
	}
	var matching []noahclient.Peer
	for _, peer := range peers {
		if peer.ID == id {
			matching = append(matching, peer)
		}
	}
	if len(matching) != 1 {
		return fmt.Errorf("peer %s has %d records; expected exactly one", id, len(matching))
	}
	peer := matching[0]
	if peer.Data.ID != "" && peer.Data.ID != id {
		return fmt.Errorf("peer %s reports contradictory identity %q", id, peer.Data.ID)
	}
	startedAt := status.State.Running.StartedAt.Unix()
	if peer.Status != noahclient.PeerStatusUp || peer.Data.StartTime < startedAt || peer.LastHeartbeat <= startedAt {
		return fmt.Errorf(
			"peer %s is not up with evidence from the current Pod incarnation: status=%q startTime=%d lastHeartbeat=%d, container started at %d",
			id, peer.Status, peer.Data.StartTime, peer.LastHeartbeat, startedAt)
	}
	var info struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal([]byte(peer.Data.Info), &info); err != nil {
		return fmt.Errorf("decode management address for peer %s: %w", id, err)
	}
	if info.URI != expected {
		return fmt.Errorf("peer %s advertises %q, expected %q", id, info.URI, expected)
	}
	return nil
}

func indexerPeerID(pod *corev1.Pod) string {
	domain := "cluster.local"
	for _, container := range pod.Spec.Containers {
		if container.Name != "splunk" {
			continue
		}
		for _, env := range container.Env {
			if env.Name == resources.ClusterDomainEnvName && env.Value != "" {
				domain = env.Value
			}
		}
	}
	return fmt.Sprintf("%s.%s.%s.svc.%s", pod.Name, pod.Spec.Subdomain, pod.Namespace, domain)
}

// advertisedAddress returns the splunk container's SPLUNK_NOAH_ADVERTISED_ADDR
// as Kubernetes expands it at container start. The operator's template refers
// only to the Downward API Pod name and namespace. Any other reference is
// rejected so that a template change fails loudly instead of being guessed.
func advertisedAddress(pod *corev1.Pod) (string, error) {
	expand := strings.NewReplacer(
		"$("+resources.PodNameEnvName+")", pod.Name,
		"$("+resources.PodNamespaceEnvName+")", pod.Namespace,
	)
	for _, container := range pod.Spec.Containers {
		if container.Name != "splunk" {
			continue
		}
		for _, env := range container.Env {
			if env.Name != resources.NoahAdvertisedAddressEnvName {
				continue
			}
			address := expand.Replace(env.Value)
			if strings.Contains(address, "$(") {
				return "", fmt.Errorf("Pod %s has an unresolved reference in %s %q",
					pod.Name, resources.NoahAdvertisedAddressEnvName, env.Value)
			}
			return address, nil
		}
	}
	return "", fmt.Errorf("Pod %s does not configure %s", pod.Name, resources.NoahAdvertisedAddressEnvName)
}

func TestNoahVerifyPeer(t *testing.T) {
	pod := newVerifyPeerPod("cluster.local")
	id := indexerPeerID(pod)
	peer := newCurrentPeer(id)
	for _, test := range []struct {
		name   string
		change func(*noahclient.Peer)
		want   string
	}{
		{name: "current"},
		{name: "stale incarnation", change: func(peer *noahclient.Peer) { peer.Data.StartTime = 99 }, want: "startTime=99"},
		{name: "stale heartbeat", change: func(peer *noahclient.Peer) { peer.LastHeartbeat = 100 }, want: "lastHeartbeat=100"},
		{name: "not up", change: func(peer *noahclient.Peer) { peer.Status = noahclient.PeerStatusWarming }, want: `status="warming"`},
		{name: "wrong address", change: func(peer *noahclient.Peer) { peer.Data.Info = `{"uri":"https://wrong:8089"}` }, want: "advertises"},
		{name: "malformed info", change: func(peer *noahclient.Peer) { peer.Data.Info = "not json" }, want: "decode management address"},
		{name: "contradictory identity", change: func(peer *noahclient.Peer) { peer.Data.ID = "other" }, want: "contradictory identity"},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := peer
			if test.change == nil {
				assert.NoError(t, verifyPeer(pod, []noahclient.Peer{candidate}))
				return
			}
			test.change(&candidate)
			assert.ErrorContains(t, verifyPeer(pod, []noahclient.Peer{candidate}), test.want)
		})
	}
	assert.Error(t, verifyPeer(pod, nil), "missing peer")
	assert.Error(t, verifyPeer(pod, []noahclient.Peer{peer, peer}), "duplicate peer")
	assert.NoError(t, verifyPeer(pod, []noahclient.Peer{peer, {ID: "foreign"}}))

	t.Run("custom cluster domain", func(t *testing.T) {
		custom := newVerifyPeerPod("example.test")
		customID := indexerPeerID(custom)
		assert.True(t, strings.HasSuffix(customID, ".svc.example.test"), customID)
		assert.NoError(t, verifyPeer(custom, []noahclient.Peer{newCurrentPeer(customID)}))
		assert.ErrorContains(t, verifyPeer(custom, []noahclient.Peer{peer}), "has 0 records",
			"a peer registered under the default domain must not satisfy a custom-domain Pod")
	})

	for _, test := range []struct {
		name    string
		address *string
		want    string
	}{
		{name: "missing advertised address", want: "does not configure"},
		{name: "advertised address names another host", address: new("https://other.test:8089"), want: "expected"},
		{name: "advertised address uses HTTP", address: new("http://" + id + ":8089"), want: "expected"},
		{name: "advertised address wrong port", address: new("https://" + id + ":8443"), want: "expected"},
		{name: "advertised address path", address: new("https://" + id + ":8089/api"), want: "expected"},
		{name: "advertised address query", address: new("https://" + id + ":8089?query=value"), want: "expected"},
		{name: "advertised address userinfo", address: new("https://" + t.Name() + "@" + id + ":8089"), want: "expected"},
		{name: "unresolved advertised address reference", address: new("https://$(UNDEFINED).test:8089"), want: "unresolved reference"},
	} {
		t.Run(test.name, func(t *testing.T) {
			configured := pod.DeepCopy()
			container := &configured.Spec.Containers[0]
			container.Env = slices.DeleteFunc(container.Env, func(env corev1.EnvVar) bool {
				return env.Name == resources.NoahAdvertisedAddressEnvName
			})
			candidate := peer
			if test.address != nil {
				container.Env = append(container.Env, corev1.EnvVar{Name: resources.NoahAdvertisedAddressEnvName, Value: *test.address})
				candidate.Data.Info = fmt.Sprintf(`{"uri":%q}`, *test.address)
			}
			assert.ErrorContains(t, verifyPeer(configured, []noahclient.Peer{candidate}), test.want)
		})
	}
	pod.Status.ContainerStatuses = nil
	assert.Error(t, verifyPeer(pod, []noahclient.Peer{peer}), "missing container must not panic")
}

// newVerifyPeerPod returns a ready indexer Pod whose splunk container is
// configured exactly as the operator configures it for clusterDomain.
func newVerifyPeerPod(clusterDomain string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "splunk-c3-indexer-0", Namespace: "test"},
		Spec:       corev1.PodSpec{Subdomain: "splunk-c3-indexer-headless"},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: "splunk", Ready: true,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(time.Unix(100, 0))}}}},
		},
	}
	statefulSet := &appsv1.StatefulSet{Spec: appsv1.StatefulSetSpec{
		ServiceName: pod.Spec.Subdomain,
		Template:    corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "splunk"}}}},
	}}
	resources.WithNoahIndexerIdentity(clusterDomain)(statefulSet)
	pod.Spec.Containers = statefulSet.Spec.Template.Spec.Containers
	return pod
}

// newCurrentPeer returns an up peer whose evidence comes from the Pod
// incarnation started at time 100 by newVerifyPeerPod.
func newCurrentPeer(id string) noahclient.Peer {
	return noahclient.Peer{ID: id, Status: noahclient.PeerStatusUp, LastHeartbeat: 101,
		Data: noahclient.PeerData{ID: id, StartTime: 100, Info: fmt.Sprintf(`{"uri":"https://%s:8089"}`, id)}}
}
