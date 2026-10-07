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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	noahclient "github.com/splunk/splunk-operator/pkg/splunk/client/noah"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	configworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
	"github.com/splunk/splunk-operator/test/testenv"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

type c3Topology struct {
	LicenseManagerPod string
	IndexerPods       []string
	SearchHeadPods    []string
}

func (topology *c3Topology) splunkPods() []string {
	pods := make([]string, 0, 1+len(topology.IndexerPods)+len(topology.SearchHeadPods))
	pods = append(pods, topology.LicenseManagerPod)
	pods = append(pods, topology.IndexerPods...)
	return append(pods, topology.SearchHeadPods...)
}

type terminalReadinessError struct {
	err error
}

func (err *terminalReadinessError) Error() string {
	return err.err.Error()
}

func (err *terminalReadinessError) Unwrap() error {
	return err.err
}

func isTerminalReadinessError(err error) bool {
	var terminal *terminalReadinessError
	return errors.As(err, &terminal)
}

// inspectC3Ready makes one non-blocking observation. Gomega's Eventually owns
// retry timing; a current Stalled=True condition is returned as terminal so the
// test can stop immediately rather than consume the entire readiness timeout.
func inspectC3Ready(
	ctx context.Context,
	kubeClient client.Client,
	namespace string,
	name string,
	operatorDeployment string,
	noahDeployment string,
) (*c3Topology, error) {
	operator := &appsv1.Deployment{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: operatorDeployment}, operator); err != nil {
		return nil, fmt.Errorf("get operator Deployment %s/%s: %w", namespace, operatorDeployment, err)
	}
	if err := testenv.VerifyDeploymentReady(operator); err != nil {
		return nil, err
	}

	noah := &appsv1.Deployment{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: noahDeployment}, noah); err != nil {
		return nil, fmt.Errorf("get Noah Deployment %s/%s: %w", namespace, noahDeployment, err)
	}
	if err := testenv.VerifyDeploymentReady(noah); err != nil {
		return nil, err
	}

	idxc := &enterpriseApi.IndexerCluster{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, idxc); err != nil {
		return nil, fmt.Errorf("get IndexerCluster %s/%s: %w", namespace, name, err)
	}
	if err := testenv.VerifyCRNotStalledForGeneration("IndexerCluster", name, idxc.Status.Conditions, idxc.Generation); err != nil {
		return nil, &terminalReadinessError{err: err}
	}
	if !idxc.Spec.NoahEnabled() || idxc.Spec.NoahClusterRef.Name == "" {
		return nil, fmt.Errorf("IndexerCluster %s/%s is not Noah enabled", namespace, name)
	}
	if idxc.Spec.LicenseManagerRef.Name == "" {
		return nil, fmt.Errorf("IndexerCluster %s/%s has no LicenseManager reference", namespace, name)
	}
	if err := testenv.VerifyIndexerClusterReadyStatus(
		idxc,
		enterpriseApi.ConditionNoahDependencyResolved,
		enterpriseApi.ConditionNoahPeersReady,
	); err != nil {
		return nil, err
	}

	indexerPods, err := testenv.ReadyIndexerClusterPods(idxc)
	if err != nil {
		return nil, err
	}

	shc := &enterpriseApi.SearchHeadCluster{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, shc); err != nil {
		return nil, fmt.Errorf("get SearchHeadCluster %s/%s: %w", namespace, name, err)
	}
	if err := testenv.VerifyCRNotStalledForGeneration("SearchHeadCluster", name, shc.Status.Conditions, shc.Generation); err != nil {
		return nil, &terminalReadinessError{err: err}
	}
	if !shc.Spec.NoahEnabled() || shc.Spec.NoahClusterRef.Name == "" {
		return nil, fmt.Errorf("SearchHeadCluster %s/%s is not Noah enabled", namespace, name)
	}
	if shc.Spec.NoahClusterRef.Name != idxc.Spec.NoahClusterRef.Name {
		return nil, fmt.Errorf(
			"SearchHeadCluster %s/%s references NoahCluster %q; IndexerCluster references %q",
			namespace,
			name,
			shc.Spec.NoahClusterRef.Name,
			idxc.Spec.NoahClusterRef.Name,
		)
	}
	if shc.Spec.LicenseManagerRef.Name != idxc.Spec.LicenseManagerRef.Name {
		return nil, fmt.Errorf(
			"SearchHeadCluster %s/%s references LicenseManager %q; IndexerCluster references %q",
			namespace,
			name,
			shc.Spec.LicenseManagerRef.Name,
			idxc.Spec.LicenseManagerRef.Name,
		)
	}
	if err := testenv.VerifySearchHeadClusterReadyStatus(
		shc,
		enterpriseApi.ConditionNoahDependencyResolved,
	); err != nil {
		return nil, err
	}

	searchHeadPods, err := testenv.ReadySearchHeadClusterPods(shc)
	if err != nil {
		return nil, err
	}

	lmName := idxc.Spec.LicenseManagerRef.Name
	lm := &enterpriseApi.LicenseManager{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: lmName}, lm); err != nil {
		return nil, fmt.Errorf("get LicenseManager %s/%s: %w", namespace, lmName, err)
	}
	if err := testenv.VerifyCRNotStalledForGeneration("LicenseManager", lmName, lm.Status.Conditions, lm.Generation); err != nil {
		return nil, &terminalReadinessError{err: err}
	}
	if err := testenv.VerifyLicenseManagerReadyStatus(lm); err != nil {
		return nil, err
	}

	topology := &c3Topology{
		LicenseManagerPod: splutil.GetSplunkStatefulsetPodName(splcommon.SplunkLicenseManager, lmName, 0),
		IndexerPods:       indexerPods,
		SearchHeadPods:    searchHeadPods,
	}
	for _, podName := range topology.splunkPods() {
		pod := &corev1.Pod{}
		if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: podName}, pod); err != nil {
			return nil, fmt.Errorf("get Pod %s/%s: %w", namespace, podName, err)
		}
		if err := testenv.VerifyPodReady(pod, "splunk"); err != nil {
			return nil, err
		}
	}

	return topology, nil
}

// uncachedClient reads directly from the API server. The shared test client is
// backed by a cluster-wide cache, so its first read of a type starts an
// informer that lists and watches that type in every namespace. Use this
// client for Secrets and for reads that must observe the latest state.
func uncachedClient() client.Client {
	GinkgoHelper()
	kubeConfig, err := config.GetConfig()
	Expect(err).NotTo(HaveOccurred())
	kubeClient, err := client.New(kubeConfig, client.Options{Scheme: testcaseEnvInstance.GetKubeClient().Scheme()})
	Expect(err).NotTo(HaveOccurred())
	return kubeClient
}

func getIndexerCluster(ctx context.Context) *enterpriseApi.IndexerCluster {
	GinkgoHelper()
	idxc := &enterpriseApi.IndexerCluster{}
	Expect(testcaseEnvInstance.GetKubeClient().Get(ctx,
		client.ObjectKey{Namespace: operatorNamespace, Name: clusterName}, idxc)).To(Succeed())
	return idxc
}

// Use the Service proxy so an attached suite needs no local DNS or port-forward.
func newNoahClient(ctx context.Context) *noahclient.Client {
	GinkgoHelper()
	idxc := getIndexerCluster(ctx)
	// Resolve the NoahCluster and its auth Secret with namespaced GETs, so the
	// test needs only read access to that Secret rather than every Secret.
	runtime, err := configworkflow.ResolveNoahRuntime(ctx, uncachedClient(), operatorNamespace, *idxc.Spec.NoahClusterRef)
	Expect(err).NotTo(HaveOccurred())
	spec := runtime.Spec()
	endpoint, err := url.Parse(spec.Endpoint)
	Expect(err).NotTo(HaveOccurred())
	port := endpoint.Port()
	if port == "" {
		port = "80"
		if endpoint.Scheme == "https" {
			port = "443"
		}
	}
	kubeConfig, err := config.GetConfig()
	Expect(err).NotTo(HaveOccurred())
	proxyURL, err := url.JoinPath(kubeConfig.Host, "api/v1/namespaces", operatorNamespace,
		"services", fmt.Sprintf("%s:%s:%s", endpoint.Scheme, noahService, port), "proxy")
	Expect(err).NotTo(HaveOccurred())
	baseURL, err := url.Parse(proxyURL)
	Expect(err).NotTo(HaveOccurred())
	httpClient, err := rest.HTTPClientFor(kubeConfig)
	Expect(err).NotTo(HaveOccurred())
	auth, err := noahclient.NewHMACV3Authenticator(runtime.Credential())
	Expect(err).NotTo(HaveOccurred())
	noah, err := noahclient.NewClient(spec.Endpoint, spec.Tenant, auth,
		noahclient.WithHTTPClient(&noahServiceProxy{client: httpClient, baseURL: baseURL}))
	Expect(err).NotTo(HaveOccurred())
	return noah
}

type noahServiceProxy struct {
	client  noahclient.HTTPClient
	baseURL *url.URL
}

func (proxy *noahServiceProxy) Do(request *http.Request) (*http.Response, error) {
	forwarded := request.Clone(request.Context())
	forwarded.URL = proxy.baseURL.Clone()
	forwarded.URL.Path = strings.TrimRight(proxy.baseURL.Path, "/") + request.URL.Path
	forwarded.URL.RawPath = ""
	forwarded.URL.RawQuery = request.URL.RawQuery
	forwarded.Host = proxy.baseURL.Host
	return proxy.client.Do(forwarded)
}

// stopOnTerminalNoahError ends the enclosing Eventually poll when Noah rejects
// a request in a way that retrying cannot fix, such as an authentication or
// authorization failure. Call it only from inside an Eventually function.
func stopOnTerminalNoahError(err error, message string) {
	if apiError, ok := errors.AsType[*noahclient.Error](err); ok && !apiError.Retryable() {
		StopTrying(message).Wrap(err).Now()
	}
}

func dumpC3FailureState(ctx context.Context, kubeClient client.Client, namespace, name string, writer io.Writer) {
	lmName := name
	idxc := &enterpriseApi.IndexerCluster{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, idxc); err != nil {
		fmt.Fprintf(writer, "failure diagnostics: get IndexerCluster %s/%s: %v\n", namespace, name, err)
	} else {
		fmt.Fprintf(writer, "IndexerCluster %s generation=%d status=%+v\n", name, idxc.Generation, idxc.Status)
		if idxc.Spec.LicenseManagerRef.Name != "" {
			lmName = idxc.Spec.LicenseManagerRef.Name
		}
	}

	shc := &enterpriseApi.SearchHeadCluster{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, shc); err != nil {
		fmt.Fprintf(writer, "failure diagnostics: get SearchHeadCluster %s/%s: %v\n", namespace, name, err)
	} else {
		fmt.Fprintf(writer, "SearchHeadCluster %s generation=%d status=%+v\n", name, shc.Generation, shc.Status)
	}

	lm := &enterpriseApi.LicenseManager{}
	if err := kubeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: lmName}, lm); err != nil {
		fmt.Fprintf(writer, "failure diagnostics: get LicenseManager %s/%s: %v\n", namespace, lmName, err)
	} else {
		fmt.Fprintf(writer, "LicenseManager %s generation=%d status=%+v\n", lmName, lm.Generation, lm.Status)
	}

	pods := &corev1.PodList{}
	if err := kubeClient.List(ctx, pods, client.InNamespace(namespace)); err != nil {
		fmt.Fprintf(writer, "failure diagnostics: list Pods in %s: %v\n", namespace, err)
	} else {
		for i := range pods.Items {
			pod := &pods.Items[i]
			fmt.Fprintf(writer, "Pod %s phase=%s deleting=%t conditions=%+v containers=%+v\n",
				pod.Name, pod.Status.Phase, pod.DeletionTimestamp != nil, pod.Status.Conditions, pod.Status.ContainerStatuses)
		}
	}

	events := &corev1.EventList{}
	if err := kubeClient.List(ctx, events, client.InNamespace(namespace)); err != nil {
		fmt.Fprintf(writer, "failure diagnostics: list Events in %s: %v\n", namespace, err)
		return
	}
	for i := range events.Items {
		event := &events.Items[i]
		fmt.Fprintf(writer, "Event %s/%s type=%s reason=%s count=%d message=%q\n",
			event.InvolvedObject.Kind, event.InvolvedObject.Name, event.Type, event.Reason, event.Count, event.Message)
	}
}

type httpClientFunc func(*http.Request) (*http.Response, error)

func (fn httpClientFunc) Do(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestNoahServiceProxy(t *testing.T) {
	baseURL, err := url.Parse("https://kubernetes.test/api/v1/namespaces/test/services/http:noah:8443/proxy")
	assert.NoError(t, err)
	auth, err := noahclient.NewHMACV3Authenticator([]byte(t.Name()))
	assert.NoError(t, err)
	proxy := &noahServiceProxy{baseURL: baseURL, client: httpClientFunc(func(request *http.Request) (*http.Response, error) {
		assert.Equal(t, baseURL.Host, request.Host)
		assert.Equal(t, baseURL.Path+"/tenant/noah/v1/peers", request.URL.Path)
		assert.True(t, strings.HasPrefix(request.Header.Get("x-splunk-digest"), "v3,"))
		assert.True(t, strings.HasPrefix(request.Header.Get("x-splunk-digest-key-params"), "v3,@salt="))
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("[]"))}, nil
	})}
	noah, err := noahclient.NewClient("http://noah.test:8443", "tenant", auth, noahclient.WithHTTPClient(proxy))
	assert.NoError(t, err)
	_, err = noah.ListPeers(t.Context())
	assert.NoError(t, err)
	assert.Equal(t, "https://kubernetes.test/api/v1/namespaces/test/services/http:noah:8443/proxy", baseURL.String(), "proxy base must not mutate")
}

// TestNoahStopOnTerminalNoahError proves both polls end on the first terminal
// Noah failure but keep retrying a transient one.
func TestNoahStopOnTerminalNoahError(t *testing.T) {
	auth, err := noahclient.NewHMACV3Authenticator([]byte(t.Name()))
	assert.NoError(t, err)
	observe := func(status int) error {
		noah, err := noahclient.NewClient("http://noah.test:8443", "tenant", auth, noahclient.WithHTTPClient(
			httpClientFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}, nil
			})))
		assert.NoError(t, err)
		_, err = noah.ListPeers(t.Context())
		assert.Error(t, err)
		return err
	}
	poll := func(observed error) (attempts int, failure string) {
		g := NewGomega(func(message string, _ ...int) { failure = message })
		g.Eventually(func() error {
			attempts++
			stopOnTerminalNoahError(observed, "Noah rejected the observation")
			return observed
		}).WithTimeout(200 * time.Millisecond).WithPolling(10 * time.Millisecond).Should(Succeed())
		return attempts, failure
	}

	attempts, failure := poll(observe(http.StatusUnauthorized))
	assert.Equal(t, 1, attempts, "an authentication failure must not be retried")
	assert.Contains(t, failure, "Noah rejected the observation")

	attempts, _ = poll(observe(http.StatusServiceUnavailable))
	assert.Greater(t, attempts, 1, "an unavailable Noah must be retried")

	attempts, _ = poll(errors.New("Kubernetes read failed"))
	assert.Greater(t, attempts, 1, "a non-Noah error must be retried")
}
