// Copyright (c) 2018-2024 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	appframeworkv1 "github.com/splunk/splunk-operator/api/appframework/v1"
	"github.com/splunk/splunk-operator/pkg/controller/appframework"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = appframeworkv1.AddToScheme(scheme)
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)

	rootCmd := &cobra.Command{
		Use:   "app-framework-worker",
		Short: "App Framework Worker for Kubernetes Jobs",
		Long:  "A worker process that handles app framework operations in Kubernetes Jobs",
	}

	// Add subcommands
	rootCmd.AddCommand(newSyncRepositoryCmd())
	rootCmd.AddCommand(newDownloadAppCmd())
	rootCmd.AddCommand(newInstallAppCmd())
	rootCmd.AddCommand(newUninstallAppCmd())
	rootCmd.AddCommand(newValidateAppCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// newSyncRepositoryCmd creates the sync-repository command
func newSyncRepositoryCmd() *cobra.Command {
	var repositoryName, namespace string

	cmd := &cobra.Command{
		Use:   "sync-repository",
		Short: "Sync apps from a repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSyncRepository(cmd.Context(), repositoryName, namespace)
		},
	}

	cmd.Flags().StringVar(&repositoryName, "repository", "", "Repository name to sync")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Repository namespace")
	cmd.MarkFlagRequired("repository")
	cmd.MarkFlagRequired("namespace")

	return cmd
}

// newDownloadAppCmd creates the download-app command
func newDownloadAppCmd() *cobra.Command {
	var appName, source, repository, namespace string

	cmd := &cobra.Command{
		Use:   "download-app",
		Short: "Download an app from repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDownloadApp(cmd.Context(), appName, source, repository, namespace)
		},
	}

	cmd.Flags().StringVar(&appName, "app", "", "App name to download")
	cmd.Flags().StringVar(&source, "source", "", "App source name")
	cmd.Flags().StringVar(&repository, "repository", "", "Repository name")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Namespace")
	cmd.MarkFlagRequired("app")
	cmd.MarkFlagRequired("source")
	cmd.MarkFlagRequired("repository")
	cmd.MarkFlagRequired("namespace")

	return cmd
}

// newInstallAppCmd creates the install-app command
func newInstallAppCmd() *cobra.Command {
	var appName, scope, targetPodsStr, namespace string

	cmd := &cobra.Command{
		Use:   "install-app",
		Short: "Install an app on target pods",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstallApp(cmd.Context(), appName, scope, targetPodsStr, namespace)
		},
	}

	cmd.Flags().StringVar(&appName, "app", "", "App name to install")
	cmd.Flags().StringVar(&scope, "scope", "", "Installation scope (local, cluster)")
	cmd.Flags().StringVar(&targetPodsStr, "target-pods", "", "Comma-separated list of target pods")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Namespace")
	cmd.MarkFlagRequired("app")
	cmd.MarkFlagRequired("scope")
	cmd.MarkFlagRequired("target-pods")
	cmd.MarkFlagRequired("namespace")

	return cmd
}

// newUninstallAppCmd creates the uninstall-app command
func newUninstallAppCmd() *cobra.Command {
	var appName, scope, targetPodsStr, namespace string

	cmd := &cobra.Command{
		Use:   "uninstall-app",
		Short: "Uninstall an app from target pods",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUninstallApp(cmd.Context(), appName, scope, targetPodsStr, namespace)
		},
	}

	cmd.Flags().StringVar(&appName, "app", "", "App name to uninstall")
	cmd.Flags().StringVar(&scope, "scope", "", "Installation scope (local, cluster)")
	cmd.Flags().StringVar(&targetPodsStr, "target-pods", "", "Comma-separated list of target pods")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Namespace")
	cmd.MarkFlagRequired("app")
	cmd.MarkFlagRequired("scope")
	cmd.MarkFlagRequired("target-pods")
	cmd.MarkFlagRequired("namespace")

	return cmd
}

// newValidateAppCmd creates the validate-app command
func newValidateAppCmd() *cobra.Command {
	var appName, targetPodsStr, namespace string

	cmd := &cobra.Command{
		Use:   "validate-app",
		Short: "Validate app installation",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValidateApp(cmd.Context(), appName, targetPodsStr, namespace)
		},
	}

	cmd.Flags().StringVar(&appName, "app", "", "App name to validate")
	cmd.Flags().StringVar(&targetPodsStr, "target-pods", "", "Comma-separated list of target pods")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Namespace")
	cmd.MarkFlagRequired("app")
	cmd.MarkFlagRequired("target-pods")
	cmd.MarkFlagRequired("namespace")

	return cmd
}

// runSyncRepository syncs apps from a repository
func runSyncRepository(ctx context.Context, repositoryName, namespace string) error {
	setupLog.Info("Starting repository sync", "repository", repositoryName, "namespace", namespace)

	// Set up signal handling
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Create Kubernetes client
	config := ctrl.GetConfigOrDie()
	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Create worker
	worker := &RepositorySyncWorker{
		Client:         k8sClient,
		RepositoryName: repositoryName,
		Namespace:      namespace,
	}

	// Run the sync
	if err := worker.Run(ctx); err != nil {
		setupLog.Error(err, "Repository sync failed")
		return err
	}

	setupLog.Info("Repository sync completed successfully")
	return nil
}

// runDownloadApp downloads an app from repository
func runDownloadApp(ctx context.Context, appName, source, repository, namespace string) error {
	setupLog.Info("Starting app download", "app", appName, "source", source, "repository", repository)

	// Set up signal handling
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Create Kubernetes client
	config := ctrl.GetConfigOrDie()
	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Create worker
	worker := &AppDownloadWorker{
		Client:         k8sClient,
		AppName:        appName,
		Source:         source,
		RepositoryName: repository,
		Namespace:      namespace,
	}

	// Run the download
	if err := worker.Run(ctx); err != nil {
		setupLog.Error(err, "App download failed")
		return err
	}

	setupLog.Info("App download completed successfully")
	return nil
}

// runInstallApp installs an app on target pods
func runInstallApp(ctx context.Context, appName, scope, targetPodsStr, namespace string) error {
	setupLog.Info("Starting app installation", "app", appName, "scope", scope, "targetPods", targetPodsStr)

	// Set up signal handling
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Create Kubernetes client
	config := ctrl.GetConfigOrDie()
	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Parse target pods
	targetPods := parseTargetPods(targetPodsStr)

	// Create worker
	worker := &AppInstallWorker{
		Client:     k8sClient,
		AppName:    appName,
		Scope:      scope,
		TargetPods: targetPods,
		Namespace:  namespace,
	}

	// Run the installation
	if err := worker.Run(ctx); err != nil {
		setupLog.Error(err, "App installation failed")
		return err
	}

	setupLog.Info("App installation completed successfully")
	return nil
}

// runUninstallApp uninstalls an app from target pods
func runUninstallApp(ctx context.Context, appName, scope, targetPodsStr, namespace string) error {
	setupLog.Info("Starting app uninstallation", "app", appName, "scope", scope, "targetPods", targetPodsStr)

	// Set up signal handling
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Create Kubernetes client
	config := ctrl.GetConfigOrDie()
	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Parse target pods
	targetPods := parseTargetPods(targetPodsStr)

	// Create worker
	worker := &AppUninstallWorker{
		Client:     k8sClient,
		AppName:    appName,
		Scope:      scope,
		TargetPods: targetPods,
		Namespace:  namespace,
	}

	// Run the uninstallation
	if err := worker.Run(ctx); err != nil {
		setupLog.Error(err, "App uninstallation failed")
		return err
	}

	setupLog.Info("App uninstallation completed successfully")
	return nil
}

// runValidateApp validates app installation
func runValidateApp(ctx context.Context, appName, targetPodsStr, namespace string) error {
	setupLog.Info("Starting app validation", "app", appName, "targetPods", targetPodsStr)

	// Set up signal handling
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Create Kubernetes client
	config := ctrl.GetConfigOrDie()
	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Parse target pods
	targetPods := parseTargetPods(targetPodsStr)

	// Create worker
	worker := &AppValidationWorker{
		Client:     k8sClient,
		AppName:    appName,
		TargetPods: targetPods,
		Namespace:  namespace,
	}

	// Run the validation
	if err := worker.Run(ctx); err != nil {
		setupLog.Error(err, "App validation failed")
		return err
	}

	setupLog.Info("App validation completed successfully")
	return nil
}

// parseTargetPods parses comma-separated target pods string
func parseTargetPods(targetPodsStr string) []string {
	if targetPodsStr == "" {
		return nil
	}

	pods := make([]string, 0)
	for _, pod := range strings.Split(targetPodsStr, ",") {
		pod = strings.TrimSpace(pod)
		if pod != "" {
			pods = append(pods, pod)
		}
	}
	return pods
}
