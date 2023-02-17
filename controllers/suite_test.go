/*
Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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

package controllers

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap/zapcore"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/envtest"
	//"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	//+kubebuilder:scaffold:imports
)

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var k8sManager ctrl.Manager

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")

}

var _ = BeforeSuite(func(ctx context.Context) {
	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.RFC3339NanoTimeEncoder,
	}
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true), zap.UseFlagOptions(&opts)))

	By("bootstrapping test environment")

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error

	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = enterpriseApi.AddToScheme(clientgoscheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = enterpriseApiV3.AddToScheme(clientgoscheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = enterpriseApi.AddToScheme(clientgoscheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = enterpriseApi.AddToScheme(clientgoscheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = enterpriseApiV3.AddToScheme(clientgoscheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = enterpriseApi.AddToScheme(clientgoscheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	// Create New Manager for controllers
	k8sManager, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: clientgoscheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())
	if err := (&ClusterManagerReconciler{
		Client: k8sManager.GetClient(),
		Scheme: k8sManager.GetScheme(),
	}).SetupWithManager(k8sManager); err != nil {
		Expect(err).NotTo(HaveOccurred())
	}
	if err := (&ClusterMasterReconciler{
		Client: k8sManager.GetClient(),
		Scheme: k8sManager.GetScheme(),
	}).SetupWithManager(k8sManager); err != nil {
		Expect(err).NotTo(HaveOccurred())
	}
	if err := (&IndexerClusterReconciler{
		Client: k8sManager.GetClient(),
		Scheme: k8sManager.GetScheme(),
	}).SetupWithManager(k8sManager); err != nil {
		Expect(err).NotTo(HaveOccurred())
	}
	if err := (&LicenseManagerReconciler{
		Client: k8sManager.GetClient(),
		Scheme: k8sManager.GetScheme(),
	}).SetupWithManager(k8sManager); err != nil {
		Expect(err).NotTo(HaveOccurred())
	}
	if err := (&LicenseMasterReconciler{
		Client: k8sManager.GetClient(),
		Scheme: k8sManager.GetScheme(),
	}).SetupWithManager(k8sManager); err != nil {
		Expect(err).NotTo(HaveOccurred())
	}
	if err := (&MonitoringConsoleReconciler{
		Client: k8sManager.GetClient(),
		Scheme: k8sManager.GetScheme(),
	}).SetupWithManager(k8sManager); err != nil {
		Expect(err).NotTo(HaveOccurred())
	}
	if err := (&SearchHeadClusterReconciler{
		Client: k8sManager.GetClient(),
		Scheme: k8sManager.GetScheme(),
	}).SetupWithManager(k8sManager); err != nil {
		Expect(err).NotTo(HaveOccurred())
	}
	if err := (&StandaloneReconciler{
		Client: k8sManager.GetClient(),
		Scheme: k8sManager.GetScheme(),
	}).SetupWithManager(k8sManager); err != nil {
		Expect(err).NotTo(HaveOccurred())
	}

	go func() {
		err = k8sManager.Start(ctrl.SetupSignalHandler())
		fmt.Printf("error %v", err.Error())
		Expect(err).ToNot(HaveOccurred())
	}()

	Expect(err).ToNot(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: clientgoscheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

}, NodeTimeout(260))

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	testEnv.Stop()
})
