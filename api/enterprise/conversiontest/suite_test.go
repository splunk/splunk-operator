// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
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

// Package conversiontest exercises the v3 <-> v4 conversion webhook end to end
// against a real API server. It lives outside api/enterprise/v4 because the CRDs
// here are patched to carry spec.conversion, which the other suite deliberately
// does not have.
package conversiontest

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/webhook/conversion"

	v3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	v4 "github.com/splunk/splunk-operator/api/enterprise/v4"
)

var (
	apiServerClient client.Client
	scheme          = runtime.NewScheme()
)

// convertedKinds are the CRDs patched to route conversion at the webhook.
var convertedKinds = []string{"indexerclusters", "searchheadclusters"}

func TestMain(m *testing.M) {
	code, err := runTests(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(code)
}

func runTests(m *testing.M) (int, error) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		return m.Run(), nil
	}

	if err := v4.AddToScheme(scheme); err != nil {
		return 0, fmt.Errorf("register enterprise v4 scheme: %w", err)
	}
	if err := v3.AddToScheme(scheme); err != nil {
		return 0, fmt.Errorf("register enterprise v3 scheme: %w", err)
	}
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		return 0, fmt.Errorf("register apiextensions scheme: %w", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		return 0, fmt.Errorf("register core scheme: %w", err)
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		WebhookInstallOptions: envtest.WebhookInstallOptions{LocalServingHost: "127.0.0.1"},
	}

	restConfig, err := testEnv.Start()
	if err != nil {
		return 0, fmt.Errorf("start envtest: %w", err)
	}
	defer func() {
		if stopErr := testEnv.Stop(); stopErr != nil {
			fmt.Fprintf(os.Stderr, "stop envtest: %v\n", stopErr)
		}
	}()

	if apiServerClient, err = client.New(restConfig, client.Options{Scheme: scheme}); err != nil {
		return 0, fmt.Errorf("create envtest client: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	webhookOptions := &testEnv.WebhookInstallOptions
	if err := serveConversionWebhook(ctx, webhookOptions); err != nil {
		return 0, fmt.Errorf("serve conversion webhook: %w", err)
	}
	if err := pointCRDsAtWebhook(ctx, webhookOptions); err != nil {
		return 0, fmt.Errorf("enable CRD conversion: %w", err)
	}

	return m.Run(), nil
}

// serveConversionWebhook starts the controller-runtime conversion handler on the
// port envtest allocated, using the certificate envtest generated for it.
func serveConversionWebhook(ctx context.Context, options *envtest.WebhookInstallOptions) error {
	registry := conversion.NewRegistry()
	handler := conversion.NewWebhookHandler(scheme, registry)

	certificate, err := tls.LoadX509KeyPair(
		filepath.Join(options.LocalServingCertDir, "tls.crt"),
		filepath.Join(options.LocalServingCertDir, "tls.key"))
	if err != nil {
		return fmt.Errorf("load serving certificate: %w", err)
	}

	address := net.JoinHostPort(options.LocalServingHost, fmt.Sprint(options.LocalServingPort))
	listener, err := tls.Listen("tcp", address, &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{certificate},
	})
	if err != nil {
		return fmt.Errorf("listen on %s: %w", address, err)
	}

	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()

	return waitForWebhook(address)
}

func waitForWebhook(address string) error {
	for range 50 {
		conn, err := tls.Dial("tcp", address, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // test-only reachability probe
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("conversion webhook did not become reachable at %s", address)
}

// pointCRDsAtWebhook patches spec.conversion onto the two CRDs under test, which
// is what config/crd/patches/webhook_in_*.yaml does in a real deployment.
func pointCRDsAtWebhook(ctx context.Context, options *envtest.WebhookInstallOptions) error {
	url := fmt.Sprintf("https://%s/convert",
		net.JoinHostPort(options.LocalServingHost, fmt.Sprint(options.LocalServingPort)))

	for _, plural := range convertedKinds {
		patch := map[string]any{
			"spec": map[string]any{
				"conversion": map[string]any{
					"strategy": "Webhook",
					"webhook": map[string]any{
						"conversionReviewVersions": []string{"v1"},
						"clientConfig": map[string]any{
							"url":      url,
							"caBundle": options.LocalServingCAData,
						},
					},
				},
			},
		}
		raw, err := json.Marshal(patch)
		if err != nil {
			return err
		}

		crd := &apiextensionsv1.CustomResourceDefinition{}
		crd.Name = plural + ".enterprise.splunk.com"
		if err := apiServerClient.Patch(ctx, crd, client.RawPatch(types.MergePatchType, raw)); err != nil {
			return fmt.Errorf("patch %s: %w", crd.Name, err)
		}
	}

	return nil
}

func requireAPIServer(t *testing.T) client.Client {
	t.Helper()
	if apiServerClient == nil {
		t.Skip("KUBEBUILDER_ASSETS is not set; run via `make test` or `make setup-envtest`")
	}
	return apiServerClient
}
