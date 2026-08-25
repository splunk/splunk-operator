/*
Copyright 2026.

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

package controller

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	gozapcore "go.uber.org/zap/zapcore"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment

func resolveCNPGModuleDir() string {
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", "-m", "github.com/cloudnative-pg/cloudnative-pg")
	output, err := cmd.Output()
	Expect(err).NotTo(HaveOccurred())

	return strings.TrimSpace(string(output))
}

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Platform Controller Suite")
}

var _ = BeforeSuite(func(context.Context) {
	opts := zap.Options{
		Development: true,
		TimeEncoder: gozapcore.RFC3339NanoTimeEncoder,
	}
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true), zap.UseFlagOptions(&opts)))

	By("bootstrapping the platform controller test environment")

	cnpgModuleDir := resolveCNPGModuleDir()
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "config", "crd", "bases"),
			filepath.Join(cnpgModuleDir, "config", "crd", "bases"),
			// Minimal barman-cloud ObjectStore CRD; the plugin's real CRD is not
			// vendored, so the object-storage backup specs register a trimmed copy.
			filepath.Join("testdata"),
		},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	Expect(platformApi.AddToScheme(clientgoscheme.Scheme)).To(Succeed())
	Expect(cnpgv1.AddToScheme(clientgoscheme.Scheme)).To(Succeed())

	k8sClient, err = client.New(cfg, client.Options{Scheme: clientgoscheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
}, NodeTimeout(500*time.Second))

var _ = AfterSuite(func(context.Context) {
	By("tearing down the platform controller test environment")
	Expect(testEnv.Stop()).To(Succeed())
}, NodeTimeout(60*time.Second))
