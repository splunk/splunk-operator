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
	"fmt"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	//+kubebuilder:scaffold:imports
)

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var k8sManager ctrl.Manager

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Controller Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func() {
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

	err = enterpriseApi.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = enterpriseApiV3.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = enterpriseApi.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = enterpriseApi.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = enterpriseApiV3.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = enterpriseApi.AddToScheme(scheme.Scheme)
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

}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	testEnv.Stop()
})

func mainFunction(scheme *runtime.Scheme) (manager.Manager, error) {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	// Logging setup
	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.RFC3339NanoTimeEncoder,
	}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog := ctrl.Log.WithName("setup")

	options := ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "270bec8c.splunk.com",
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), config.ManagerOptionsWithNamespaces(setupLog, options))
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return nil, fmt.Errorf("unable to start manager")
	}

	if err = (&ClusterManagerReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterManager")
		return nil, fmt.Errorf("unable to start manager")
	}
	if err = (&IndexerClusterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "IndexerCluster")
		return nil, fmt.Errorf("unable to start manager")
	}
	if err = (&LicenseManagerReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "LicenseManager")
		return nil, fmt.Errorf("unable to start manager")
	}
	if err = (&MonitoringConsoleReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "MonitoringConsole")
		return nil, fmt.Errorf("unable to create controller")
	}
	if err = (&SearchHeadClusterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "SearchHeadCluster")
		return nil, fmt.Errorf("unable to create controller")
	}
	if err = (&StandaloneReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Standalone")
		return nil, fmt.Errorf("unable to create controller")
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		return nil, fmt.Errorf("unable to create controller")
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		return nil, fmt.Errorf("unable to create controller")
	}

	return mgr, nil
}
