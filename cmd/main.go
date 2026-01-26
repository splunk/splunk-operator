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

package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	intController "github.com/splunk/splunk-operator/internal/controller"
	"github.com/splunk/splunk-operator/internal/controller/debug"
	"github.com/splunk/splunk-operator/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"

	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	//+kubebuilder:scaffold:imports
	//extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(enterpriseApi.AddToScheme(scheme))
	utilruntime.Must(enterpriseApiV3.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
	//utilruntime.Must(extapi.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var secureMetrics bool
	var enableLeaderElection bool
	var probeAddr string
	var pprofActive bool
	var logEncoder string
	var logLevel int

	var leaseDuration time.Duration
	var renewDeadline time.Duration
	var leaseDurationSecond int
	var renewDeadlineSecond int

	var tlsOpts []func(*tls.Config)

	// TLS certificate configuration for webhooks and metrics
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string

	flag.StringVar(&logEncoder, "log-encoder", "json", "log encoding ('json' or 'console')")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&pprofActive, "pprof", true, "Enable pprof endpoint")
	flag.IntVar(&logLevel, "log-level", int(zapcore.InfoLevel), "set log level")
	flag.IntVar(&leaseDurationSecond, "lease-duration", leaseDurationSecond, "manager lease duration in seconds")
	flag.IntVar(&renewDeadlineSecond, "renew-duration", renewDeadlineSecond, "manager renew duration in seconds")
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")

	// TLS certificate flags for webhooks and metrics server
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "", "The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		// TODO(user): TLSOpts is used to allow configuring the TLS config used for the server. If certificates are
		// not provided, self-signed certificates will be generated by default. This option is not recommended for
		// production environments as self-signed certificates do not offer the same level of trust and security
		// as certificates issued by a trusted Certificate Authority (CA). The primary risk is potentially allowing
		// unauthorized access to sensitive metrics data. Consider replacing with CertDir, CertName, and KeyName
		// to provide certificates, ensuring the server communicates using trusted and secure certificates.
		TLSOpts:        tlsOpts,
		FilterProvider: filters.WithAuthenticationAndAuthorization,
	}

	// TODO: enable https for /metrics endpoint by default
	// if secureMetrics {
	// 	// FilterProvider is used to protect the metrics endpoint with authn/authz.
	// 	// These configurations ensure that only authorized users and service accounts
	// 	// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
	// 	// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/metrics/filters#WithAuthenticationAndAuthorization
	// 	metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	// }

	// see https://github.com/operator-framework/operator-sdk/issues/1813
	if leaseDurationSecond < 30 {
		leaseDuration = 30 * time.Second
	} else {
		leaseDuration = time.Duration(leaseDurationSecond) * time.Second
	}

	if renewDeadlineSecond < 20 {
		renewDeadline = 20 * time.Second
	} else {
		renewDeadline = time.Duration(renewDeadlineSecond) * time.Second
	}

	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.RFC3339NanoTimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	// Logging setup
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Initialize certificate watchers for webhooks and metrics
	var metricsCertWatcher, webhookCertWatcher *certwatcher.CertWatcher
	webhookTLSOpts := tlsOpts

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		var err error
		webhookCertWatcher, err = certwatcher.New(
			filepath.Join(webhookCertPath, webhookCertName),
			filepath.Join(webhookCertPath, webhookCertKey),
		)
		if err != nil {
			setupLog.Error(err, "Failed to initialize webhook certificate watcher")
			os.Exit(1)
		}

		webhookTLSOpts = append(webhookTLSOpts, func(config *tls.Config) {
			config.GetCertificate = webhookCertWatcher.GetCertificate
		})
	}

	// Configure metrics certificate watcher if metrics certs are provided
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		var err error
		metricsCertWatcher, err = certwatcher.New(
			filepath.Join(metricsCertPath, metricsCertName),
			filepath.Join(metricsCertPath, metricsCertKey),
		)
		if err != nil {
			setupLog.Error(err, "Failed to initialize metrics certificate watcher")
			os.Exit(1)
		}

		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, func(config *tls.Config) {
			config.GetCertificate = metricsCertWatcher.GetCertificate
		})
	}

	// Configure webhook server options
	webhookServerOptions := webhook.Options{
		TLSOpts: webhookTLSOpts,
	}

	baseOptions := ctrl.Options{
		Metrics:                metricsServerOptions,
		Scheme:                 scheme,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "270bec8c.splunk.com",
		LeaseDuration:          &leaseDuration,
		RenewDeadline:          &renewDeadline,
		WebhookServer:          webhook.NewServer(webhookServerOptions),
	}

	// Apply namespace-specific configuration
	managerOptions := config.ManagerOptionsWithNamespaces(setupLog, baseOptions)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), managerOptions)

	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&intController.ClusterManagerReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		fmt.Printf(" error - %v", err)
		setupLog.Error(err, "unable to create controller", "controller", "ClusterManager ")
		os.Exit(1)
	}
	fmt.Printf("%v", err)
	if err = (&intController.ClusterMasterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterMaster")
		os.Exit(1)
	}
	if err = (&intController.IndexerClusterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "IndexerCluster")
		os.Exit(1)
	}
	if err = (&intController.LicenseMasterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "LicenseMaster")
		os.Exit(1)
	}
	if err = (&intController.LicenseManagerReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "LicenseManager")
		os.Exit(1)
	}
	if err = (&intController.MonitoringConsoleReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "MonitoringConsole")
		os.Exit(1)
	}
	if err = (&intController.SearchHeadClusterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "SearchHeadCluster")
		os.Exit(1)
	}
	if err = (&intController.StandaloneReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Standalone")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	// Register certificate watchers with the manager
	if metricsCertWatcher != nil {
		setupLog.Info("Adding metrics certificate watcher to manager")
		if err := mgr.Add(metricsCertWatcher); err != nil {
			setupLog.Error(err, "Unable to add metrics certificate watcher to manager")
			os.Exit(1)
		}
	}

	if webhookCertWatcher != nil {
		setupLog.Info("Adding webhook certificate watcher to manager")
		if err := mgr.Add(webhookCertWatcher); err != nil {
			setupLog.Error(err, "Unable to add webhook certificate watcher to manager")
			os.Exit(1)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	if err := customSetupEndpoints(pprofActive, mgr); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// Note that these endpoints meant to be sensitive and shouldn't be exposed publicly.
func customSetupEndpoints(pprofActive bool, mgr manager.Manager) error {
	if pprofActive {
		if err := debug.RegisterEndpoint(mgr.AddMetricsServerExtraHandler, nil); err != nil {
			setupLog.Error(err, "Unable to register pprof endpoint")
			return err
		}
	}
	return nil
}
