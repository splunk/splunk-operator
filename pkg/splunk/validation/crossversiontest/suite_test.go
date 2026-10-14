/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package crossversiontest drives the real validation webhook server from a
// real API server, with the CRDs at conversion strategy None, to prove that
// v3 writes cannot silently prune v4-only fields.
package crossversiontest

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApiV4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/splunk/validation"
)

var apiServerClient client.Client

func TestMain(m *testing.M) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		os.Exit(m.Run())
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(enterpriseApiV4.AddToScheme(scheme))
	utilruntime.Must(enterpriseApiV3.AddToScheme(scheme))
	// config/default-with-webhook deploys the v4 entry with ValidationWebhook on.
	utilruntime.Must(config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.ValidationWebhook): true}))

	// The shipped v4 entry from config/webhook/manifests.yaml (default matchPolicy Equivalent) plus
	// the guard entry, both on /validate. envtest prefixes the path with "/", so it has none here.
	clientConfig := admissionregistrationv1.WebhookClientConfig{
		Service: &admissionregistrationv1.ServiceReference{Name: "webhook-service", Namespace: "system", Path: ptr.To("validate")},
	}
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			LocalServingHost: "127.0.0.1",
			ValidatingWebhooks: []*admissionregistrationv1.ValidatingWebhookConfiguration{{
				ObjectMeta: metav1.ObjectMeta{Name: "validating-webhook-configuration"},
				Webhooks: []admissionregistrationv1.ValidatingWebhook{
					{
						Name:                    "vsplunk.enterprise.splunk.com",
						AdmissionReviewVersions: []string{"v1"},
						ClientConfig:            clientConfig,
						FailurePolicy:           ptr.To(admissionregistrationv1.Fail),
						SideEffects:             ptr.To(admissionregistrationv1.SideEffectClassNone),
						Rules: []admissionregistrationv1.RuleWithOperations{{
							Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update},
							Rule: admissionregistrationv1.Rule{
								APIGroups:   []string{"enterprise.splunk.com"},
								APIVersions: []string{"v4"},
								Resources: []string{"standalones", "indexerclusters", "searchheadclusters",
									"clustermanagers", "licensemanagers", "monitoringconsoles"},
							},
						}},
					},
					{
						Name:                    "vcrossversion.enterprise.splunk.com",
						AdmissionReviewVersions: []string{"v1"},
						ClientConfig:            clientConfig,
						FailurePolicy:           ptr.To(admissionregistrationv1.Fail),
						SideEffects:             ptr.To(admissionregistrationv1.SideEffectClassNone),
						MatchPolicy:             ptr.To(admissionregistrationv1.Exact),
						Rules: []admissionregistrationv1.RuleWithOperations{{
							Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Update, admissionregistrationv1.Delete},
							Rule: admissionregistrationv1.Rule{
								APIGroups:   []string{"enterprise.splunk.com"},
								APIVersions: []string{"v3"},
								Resources: []string{"indexerclusters", "indexerclusters/*",
									"searchheadclusters", "searchheadclusters/*"},
							},
						}},
					},
				},
			}},
		},
	}

	restConfig, err := testEnv.Start()
	if err != nil {
		fmt.Fprintln(os.Stderr, "start envtest:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if apiServerClient, err = client.New(restConfig, client.Options{Scheme: scheme}); err == nil {
		// An uncached client is what mgr.GetAPIReader() returns in the operator.
		options := &testEnv.WebhookInstallOptions
		validator := validation.NewWebhookServer(validation.WebhookServerOptions{
			Validators:        validation.DefaultValidators,
			CrossVersionGuard: validation.NewCrossVersionFieldGuard(apiServerClient),
		})
		server := webhook.NewServer(webhook.Options{
			Host:    options.LocalServingHost,
			Port:    options.LocalServingPort,
			CertDir: options.LocalServingCertDir,
		})
		server.Register("/validate", http.HandlerFunc(validator.HandleValidate))
		go func() { _ = server.Start(ctx) }()

		started := server.StartedChecker()
		for range 50 {
			if err = started(nil); err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	code := 1
	if err == nil {
		code = m.Run()
	} else {
		fmt.Fprintln(os.Stderr, err)
	}

	cancel()
	if stopErr := testEnv.Stop(); stopErr != nil {
		fmt.Fprintln(os.Stderr, "stop envtest:", stopErr)
	}
	os.Exit(code)
}
