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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	enterprisev3 "github.com/splunk/splunk-operator/api/v3"
	"github.com/splunk/splunk-operator/pkg/config"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
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
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	//Expect(os.Setenv("TEST_ASSET_KUBE_APISERVER", "../testbin/bin/kube-apiserver")).To(Succeed())
	//Expect(os.Setenv("TEST_ASSET_ETCD", "../testbin/bin/etcd")).To(Succeed())
	//Expect(os.Setenv("TEST_ASSET_KUBECTL", "../testbin/bin/kubectl")).To(Succeed())

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = enterprisev3.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = enterprisev3.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = enterprisev3.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = enterprisev3.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = enterprisev3.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = enterprisev3.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme
	/*
		k8sManager, err := mainFunction()
		Expect(err).ToNot(HaveOccurred())

		go func() {
			err = k8sManager.Start(ctrl.SetupSignalHandler())
			Expect(err).ToNot(HaveOccurred())
		}()

		Expect(err).ToNot(HaveOccurred())
	*/
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

func mainFunction() (manager.Manager, error) {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(enterprisev3.AddToScheme(scheme))

	// Logging setup
	ctrl.SetLogger(zap.New())
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

	if err = (&ClusterMasterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterMaster")
		return nil, fmt.Errorf("unable to start manager")
	}
	if err = (&IndexerClusterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "IndexerCluster")
		return nil, fmt.Errorf("unable to start manager")
	}
	if err = (&LicenseMasterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "LicenseMaster")
		return nil, fmt.Errorf("unable to start manager")
	}
	if err = (&MonitoringConsoleReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "MonitoringConsole")
		return nil, fmt.Errorf("unable to start manager")
	}
	if err = (&SearchHeadClusterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "SearchHeadCluster")
		return nil, fmt.Errorf("unable to start manager")
	}
	if err = (&StandaloneReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Standalone")
		return nil, fmt.Errorf("unable to start manager")
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		return nil, fmt.Errorf("unable to start manager")
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		return nil, fmt.Errorf("unable to start manager")
	}

	return mgr, nil
}
