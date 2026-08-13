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

package testenv

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// DefaultCertManagerVersion is the default cert-manager release to install.
	DefaultCertManagerVersion = "v1.17.2"

	certManagerReadyTimeout = 120 * time.Second
)

// certManagerVersion returns the cert-manager version to install,
// preferring the CERT_MANAGER_VERSION env var over the compiled default.
func certManagerVersion() string {
	if v := os.Getenv("CERT_MANAGER_VERSION"); v != "" {
		return v
	}
	return DefaultCertManagerVersion
}

// SetupCertManagerDeps installs cert-manager on the cluster and waits for
// its deployments to be ready. It is idempotent — safe to call on a cluster
// that already has cert-manager installed.
//
// Call from BeforeSuite() in test suites that exercise cert-manager-backed
// auto-generation:
//
//	var _ = BeforeSuite(func() {
//	    testenvInstance, err = testenv.NewDefaultTestEnv(testSuiteName)
//	    Expect(err).ToNot(HaveOccurred())
//	    Expect(testenv.SetupCertManagerDeps(ctx)).To(Succeed())
//	})
func SetupCertManagerDeps(ctx context.Context) error {
	if err := installCertManager(ctx); err != nil {
		return fmt.Errorf("install cert-manager: %w", err)
	}
	return nil
}

// TeardownCertManagerDeps uninstalls cert-manager from the cluster.
// Call from AfterSuite() in test suites that called SetupCertManagerDeps.
func TeardownCertManagerDeps(ctx context.Context) error {
	if err := uninstallCertManager(ctx); err != nil {
		return fmt.Errorf("uninstall cert-manager: %w", err)
	}
	return nil
}

// installCertManager applies the cert-manager manifest and waits for readiness.
func installCertManager(ctx context.Context) error {
	version := certManagerVersion()
	manifestURL := fmt.Sprintf(
		"https://github.com/cert-manager/cert-manager/releases/download/%s/cert-manager.yaml",
		version,
	)

	logf.Log.Info("Installing cert-manager", "version", version)
	if out, err := exec.CommandContext(ctx, "kubectl", "apply", "-f", manifestURL).CombinedOutput(); err != nil {
		return fmt.Errorf("kubectl apply cert-manager: %w\n%s", err, out)
	}

	for _, deploy := range []string{"cert-manager", "cert-manager-webhook", "cert-manager-cainjector"} {
		logf.Log.Info("Waiting for cert-manager deployment", "deployment", deploy)
		waitCtx, cancel := context.WithTimeout(ctx, certManagerReadyTimeout)
		out, err := exec.CommandContext(waitCtx, "kubectl", "rollout", "status",
			fmt.Sprintf("deployment/%s", deploy),
			"-n", "cert-manager",
			fmt.Sprintf("--timeout=%s", certManagerReadyTimeout),
		).CombinedOutput()
		cancel()
		if err != nil {
			return fmt.Errorf("waiting for %s: %w\n%s", deploy, err, out)
		}
	}

	if err := waitForWebhookReady(ctx); err != nil {
		return fmt.Errorf("waiting for cert-manager webhook to become trusted: %w", err)
	}

	logf.Log.Info("cert-manager ready", "version", version)
	return nil
}

// waitForWebhookReady polls the cert-manager-webhook's admission endpoint by
// dry-run applying a throwaway Issuer until it stops failing with
// "x509: certificate signed by unknown authority". Deployment rollout status
// (checked above) only reflects the webhook pod's readiness probe — it does
// not guarantee cert-manager-cainjector has finished injecting the webhook's
// caBundle into the ValidatingWebhookConfiguration. Creating a cert-manager
// resource in that window fails admission with a TLS trust error even though
// every Deployment reports ready.
func waitForWebhookReady(ctx context.Context) error {
	const probeManifest = `apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: webhook-readiness-probe
  namespace: kube-system
spec:
  selfSigned: {}
`
	logf.Log.Info("Waiting for cert-manager webhook to become trusted")
	waitCtx, cancel := context.WithTimeout(ctx, certManagerReadyTimeout)
	defer cancel()

	var lastOut []byte
	var lastErr error
	for {
		cmd := exec.CommandContext(waitCtx, "kubectl", "apply", "--dry-run=server", "-f", "-")
		cmd.Stdin = strings.NewReader(probeManifest)
		out, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		lastOut, lastErr = out, err

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("timed out waiting for cert-manager webhook: %w\n%s", lastErr, lastOut)
		case <-time.After(2 * time.Second):
		}
	}
}

// uninstallCertManager deletes the cert-manager manifest from the cluster.
func uninstallCertManager(ctx context.Context) error {
	version := certManagerVersion()
	manifestURL := fmt.Sprintf(
		"https://github.com/cert-manager/cert-manager/releases/download/%s/cert-manager.yaml",
		version,
	)

	logf.Log.Info("Uninstalling cert-manager", "version", version)
	out, err := exec.CommandContext(ctx, "kubectl", "delete", "-f", manifestURL, "--ignore-not-found").CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl delete cert-manager: %w\n%s", err, out)
	}
	return nil
}
