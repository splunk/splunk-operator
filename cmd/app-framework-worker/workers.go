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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appframeworkv1 "github.com/splunk/splunk-operator/api/appframework/v1"
	"github.com/splunk/splunk-operator/pkg/controller/appframework"
)

// RepositorySyncWorker handles repository synchronization
type RepositorySyncWorker struct {
	Client         client.Client
	RepositoryName string
	Namespace      string
}

// Run executes the repository sync operation
func (w *RepositorySyncWorker) Run(ctx context.Context) error {
	logger := log.FromContext(ctx).WithValues("worker", "repository-sync", "repository", w.RepositoryName)
	logger.Info("Starting repository sync")

	// Get the repository
	repository := &appframeworkv1.AppFrameworkRepository{}
	key := types.NamespacedName{Name: w.RepositoryName, Namespace: w.Namespace}
	if err := w.Client.Get(ctx, key, repository); err != nil {
		return fmt.Errorf("failed to get repository: %w", err)
	}

	// Create remote client manager
	clientManager := appframework.NewRemoteClientManager(w.Client)

	// Test connectivity
	if err := clientManager.ValidateConnection(ctx, repository); err != nil {
		return fmt.Errorf("repository connectivity test failed: %w", err)
	}

	// Get apps list from repository
	apps, err := clientManager.GetAppsList(ctx, repository, repository.Spec.Path)
	if err != nil {
		return fmt.Errorf("failed to get apps list: %w", err)
	}

	logger.Info("Repository sync completed", "appsFound", len(apps))
	return nil
}

// AppDownloadWorker handles app downloads
type AppDownloadWorker struct {
	Client         client.Client
	AppName        string
	Source         string
	RepositoryName string
	Namespace      string
}

// Run executes the app download operation
func (w *AppDownloadWorker) Run(ctx context.Context) error {
	logger := log.FromContext(ctx).WithValues("worker", "app-download", "app", w.AppName)
	logger.Info("Starting app download")

	// Get the repository
	repository := &appframeworkv1.AppFrameworkRepository{}
	key := types.NamespacedName{Name: w.RepositoryName, Namespace: w.Namespace}
	if err := w.Client.Get(ctx, key, repository); err != nil {
		return fmt.Errorf("failed to get repository: %w", err)
	}

	// Create remote client manager
	clientManager := appframework.NewRemoteClientManager(w.Client)

	// Create local download directory
	downloadDir := filepath.Join("/app-cache", w.Namespace, w.RepositoryName, w.Source)
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		return fmt.Errorf("failed to create download directory: %w", err)
	}

	// Build app key (full path in repository)
	appKey := filepath.Join(repository.Spec.Path, w.Source, w.AppName)
	localPath := filepath.Join(downloadDir, w.AppName)

	// Download the app
	if err := clientManager.DownloadApp(ctx, repository, appKey, localPath); err != nil {
		return fmt.Errorf("failed to download app: %w", err)
	}

	// Verify download
	if _, err := os.Stat(localPath); err != nil {
		return fmt.Errorf("downloaded app file not found: %w", err)
	}

	logger.Info("App download completed", "localPath", localPath)
	return nil
}

// AppInstallWorker handles app installations
type AppInstallWorker struct {
	Client     client.Client
	AppName    string
	Scope      string
	TargetPods []string
	Namespace  string
}

// Run executes the app installation operation
func (w *AppInstallWorker) Run(ctx context.Context) error {
	logger := log.FromContext(ctx).WithValues("worker", "app-install", "app", w.AppName)
	logger.Info("Starting app installation")

	for _, podName := range w.TargetPods {
		logger.Info("Installing app on pod", "pod", podName)

		if err := w.installAppOnPod(ctx, podName); err != nil {
			logger.Error(err, "Failed to install app on pod", "pod", podName)
			return fmt.Errorf("failed to install app on pod %s: %w", podName, err)
		}

		logger.Info("App installed successfully on pod", "pod", podName)
	}

	logger.Info("App installation completed on all pods")
	return nil
}

// installAppOnPod installs an app on a specific pod
func (w *AppInstallWorker) installAppOnPod(ctx context.Context, podName string) error {
	logger := log.FromContext(ctx).WithValues("pod", podName, "app", w.AppName)

	// Find the app package in cache
	appPackagePath, err := w.findAppPackage()
	if err != nil {
		return fmt.Errorf("failed to find app package: %w", err)
	}

	// Copy app to pod
	targetPath := fmt.Sprintf("/operator-staging/appframework/%s", filepath.Base(appPackagePath))
	if err := w.copyAppToPod(ctx, podName, appPackagePath, targetPath); err != nil {
		return fmt.Errorf("failed to copy app to pod: %w", err)
	}

	// Install app based on scope
	switch w.Scope {
	case "local":
		return w.installLocalApp(ctx, podName, targetPath)
	case "cluster":
		return w.installClusterApp(ctx, podName, targetPath)
	case "premiumApps":
		return w.installPremiumApp(ctx, podName, targetPath)
	default:
		return fmt.Errorf("unsupported installation scope: %s", w.Scope)
	}
}

// findAppPackage finds the app package in the cache
func (w *AppInstallWorker) findAppPackage() (string, error) {
	// Look for the app in various possible locations
	searchPaths := []string{
		filepath.Join("/app-cache", w.Namespace, "*", "*", w.AppName),
		filepath.Join("/app-cache", "*", "*", "*", w.AppName),
	}

	for _, pattern := range searchPaths {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		if len(matches) > 0 {
			return matches[0], nil
		}
	}

	return "", fmt.Errorf("app package not found: %s", w.AppName)
}

// copyAppToPod copies an app package to a pod
func (w *AppInstallWorker) copyAppToPod(ctx context.Context, podName, sourcePath, targetPath string) error {
	// Use kubectl cp to copy the file
	cmd := exec.CommandContext(ctx, "kubectl", "cp", sourcePath,
		fmt.Sprintf("%s/%s:%s", w.Namespace, podName, targetPath))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl cp failed: %w, output: %s", err, string(output))
	}

	return nil
}

// installLocalApp installs an app locally on a pod
func (w *AppInstallWorker) installLocalApp(ctx context.Context, podName, appPath string) error {
	// Extract app name without extension for installation
	appName := strings.TrimSuffix(filepath.Base(appPath), filepath.Ext(appPath))
	appName = strings.TrimSuffix(appName, ".tar")

	// Install the app using Splunk CLI
	installCmd := fmt.Sprintf("/opt/splunk/bin/splunk install app %s -auth admin:`cat /mnt/splunk-secrets/password`", appPath)

	if err := w.execOnPod(ctx, podName, installCmd); err != nil {
		return fmt.Errorf("failed to install app: %w", err)
	}

	// Restart Splunk to ensure app is loaded
	restartCmd := "/opt/splunk/bin/splunk restart"
	if err := w.execOnPod(ctx, podName, restartCmd); err != nil {
		return fmt.Errorf("failed to restart Splunk: %w", err)
	}

	return nil
}

// installClusterApp installs an app for cluster distribution
func (w *AppInstallWorker) installClusterApp(ctx context.Context, podName, appPath string) error {
	// For cluster apps, we need to place them in the appropriate cluster directory
	// and then apply the cluster bundle

	// Determine the cluster directory based on pod type
	var clusterDir string
	if strings.Contains(podName, "cluster-manager") || strings.Contains(podName, "cluster-master") {
		clusterDir = "/opt/splunk/etc/master-apps"
	} else if strings.Contains(podName, "deployer") {
		clusterDir = "/opt/splunk/etc/shcluster/apps"
	} else {
		return fmt.Errorf("unsupported pod type for cluster app installation: %s", podName)
	}

	// Extract app to cluster directory
	appName := strings.TrimSuffix(filepath.Base(appPath), filepath.Ext(appPath))
	appName = strings.TrimSuffix(appName, ".tar")

	extractCmd := fmt.Sprintf("cd %s && tar -xzf %s", clusterDir, appPath)
	if err := w.execOnPod(ctx, podName, extractCmd); err != nil {
		return fmt.Errorf("failed to extract app to cluster directory: %w", err)
	}

	// Apply cluster bundle
	var bundleCmd string
	if strings.Contains(podName, "cluster-manager") || strings.Contains(podName, "cluster-master") {
		bundleCmd = "/opt/splunk/bin/splunk apply cluster-bundle -auth admin:`cat /mnt/splunk-secrets/password` --answer-yes"
	} else if strings.Contains(podName, "deployer") {
		// For search head cluster, we need the captain URL
		bundleCmd = "/opt/splunk/bin/splunk apply shcluster-bundle -auth admin:`cat /mnt/splunk-secrets/password`"
	}

	if err := w.execOnPod(ctx, podName, bundleCmd); err != nil {
		return fmt.Errorf("failed to apply cluster bundle: %w", err)
	}

	return nil
}

// installPremiumApp installs a premium app (like Enterprise Security)
func (w *AppInstallWorker) installPremiumApp(ctx context.Context, podName, appPath string) error {
	// Premium apps may require special handling
	// For now, treat them like local apps but with additional configuration

	if err := w.installLocalApp(ctx, podName, appPath); err != nil {
		return err
	}

	// Additional premium app configuration could go here
	// For example, Enterprise Security might need specific setup steps

	return nil
}

// execOnPod executes a command on a pod
func (w *AppInstallWorker) execOnPod(ctx context.Context, podName, command string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "exec", "-n", w.Namespace, podName, "--", "sh", "-c", command)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command execution failed: %w, output: %s", err, string(output))
	}

	return nil
}

// AppUninstallWorker handles app uninstallations with enhanced safety
type AppUninstallWorker struct {
	Client     client.Client
	AppName    string
	Scope      string
	TargetPods []string
	Namespace  string
}

// Run executes the app uninstallation operation
func (w *AppUninstallWorker) Run(ctx context.Context) error {
	logger := log.FromContext(ctx).WithValues("worker", "app-uninstall", "app", w.AppName)
	logger.Info("Starting app uninstallation")

	// Create backup before uninstallation
	if err := w.createBackup(ctx); err != nil {
		logger.Error(err, "Failed to create backup, proceeding with caution")
		// Don't fail the uninstall for backup issues, but log the error
	}

	for _, podName := range w.TargetPods {
		logger.Info("Uninstalling app from pod", "pod", podName)

		if err := w.uninstallAppFromPod(ctx, podName); err != nil {
			logger.Error(err, "Failed to uninstall app from pod", "pod", podName)
			return fmt.Errorf("failed to uninstall app from pod %s: %w", podName, err)
		}

		logger.Info("App uninstalled successfully from pod", "pod", podName)
	}

	// Clean up cached app files
	if err := w.cleanupCache(); err != nil {
		logger.Error(err, "Failed to cleanup cache")
		// Don't fail the uninstall for cleanup issues
	}

	logger.Info("App uninstallation completed on all pods")
	return nil
}

// createBackup creates a backup of the app before uninstallation
func (w *AppUninstallWorker) createBackup(ctx context.Context) error {
	logger := log.FromContext(ctx).WithValues("app", w.AppName)

	backupDir := filepath.Join("/app-cache", "backups", w.Namespace, fmt.Sprintf("%s-%d", w.AppName, time.Now().Unix()))
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// For each target pod, backup the app directory
	for _, podName := range w.TargetPods {
		podBackupDir := filepath.Join(backupDir, podName)
		if err := os.MkdirAll(podBackupDir, 0755); err != nil {
			continue // Skip this pod backup
		}

		// Backup app directory from pod
		appName := strings.TrimSuffix(w.AppName, filepath.Ext(w.AppName))
		appName = strings.TrimSuffix(appName, ".tar")

		backupCmd := fmt.Sprintf("kubectl cp %s/%s:/opt/splunk/etc/apps/%s %s",
			w.Namespace, podName, appName, podBackupDir)

		cmd := exec.CommandContext(ctx, "sh", "-c", backupCmd)
		if err := cmd.Run(); err != nil {
			logger.Error(err, "Failed to backup app from pod", "pod", podName)
			// Continue with other pods
		}
	}

	logger.Info("App backup completed", "backupDir", backupDir)
	return nil
}

// uninstallAppFromPod uninstalls an app from a specific pod
func (w *AppUninstallWorker) uninstallAppFromPod(ctx context.Context, podName string) error {
	logger := log.FromContext(ctx).WithValues("pod", podName, "app", w.AppName)

	// Extract app name without extension
	appName := strings.TrimSuffix(w.AppName, filepath.Ext(w.AppName))
	appName = strings.TrimSuffix(appName, ".tar")

	// Uninstall based on scope
	switch w.Scope {
	case "local":
		return w.uninstallLocalApp(ctx, podName, appName)
	case "cluster":
		return w.uninstallClusterApp(ctx, podName, appName)
	case "premiumApps":
		return w.uninstallPremiumApp(ctx, podName, appName)
	default:
		return fmt.Errorf("unsupported uninstallation scope: %s", w.Scope)
	}
}

// uninstallLocalApp uninstalls a local app from a pod
func (w *AppUninstallWorker) uninstallLocalApp(ctx context.Context, podName, appName string) error {
	// Remove the app using Splunk CLI
	removeCmd := fmt.Sprintf("/opt/splunk/bin/splunk remove app %s -auth admin:`cat /mnt/splunk-secrets/password`", appName)

	if err := w.execOnPod(ctx, podName, removeCmd); err != nil {
		// If remove command fails, try manual directory removal
		logger := log.FromContext(ctx)
		logger.Info("Splunk remove command failed, trying manual removal", "app", appName)

		manualRemoveCmd := fmt.Sprintf("rm -rf /opt/splunk/etc/apps/%s", appName)
		if err := w.execOnPod(ctx, podName, manualRemoveCmd); err != nil {
			return fmt.Errorf("failed to remove app directory: %w", err)
		}
	}

	// Restart Splunk to ensure app is unloaded
	restartCmd := "/opt/splunk/bin/splunk restart"
	if err := w.execOnPod(ctx, podName, restartCmd); err != nil {
		return fmt.Errorf("failed to restart Splunk: %w", err)
	}

	return nil
}

// uninstallClusterApp uninstalls a cluster app
func (w *AppUninstallWorker) uninstallClusterApp(ctx context.Context, podName, appName string) error {
	// Remove app from cluster directory
	var clusterDir string
	if strings.Contains(podName, "cluster-manager") || strings.Contains(podName, "cluster-master") {
		clusterDir = "/opt/splunk/etc/master-apps"
	} else if strings.Contains(podName, "deployer") {
		clusterDir = "/opt/splunk/etc/shcluster/apps"
	} else {
		return fmt.Errorf("unsupported pod type for cluster app uninstallation: %s", podName)
	}

	// Remove app directory
	removeCmd := fmt.Sprintf("rm -rf %s/%s", clusterDir, appName)
	if err := w.execOnPod(ctx, podName, removeCmd); err != nil {
		return fmt.Errorf("failed to remove app from cluster directory: %w", err)
	}

	// Apply cluster bundle to propagate changes
	var bundleCmd string
	if strings.Contains(podName, "cluster-manager") || strings.Contains(podName, "cluster-master") {
		bundleCmd = "/opt/splunk/bin/splunk apply cluster-bundle -auth admin:`cat /mnt/splunk-secrets/password` --answer-yes"
	} else if strings.Contains(podName, "deployer") {
		bundleCmd = "/opt/splunk/bin/splunk apply shcluster-bundle -auth admin:`cat /mnt/splunk-secrets/password`"
	}

	if err := w.execOnPod(ctx, podName, bundleCmd); err != nil {
		return fmt.Errorf("failed to apply cluster bundle: %w", err)
	}

	return nil
}

// uninstallPremiumApp uninstalls a premium app
func (w *AppUninstallWorker) uninstallPremiumApp(ctx context.Context, podName, appName string) error {
	// Premium apps may require special cleanup
	// For now, treat them like local apps but with additional cleanup

	if err := w.uninstallLocalApp(ctx, podName, appName); err != nil {
		return err
	}

	// Additional premium app cleanup could go here
	// For example, Enterprise Security might need specific cleanup steps

	return nil
}

// cleanupCache cleans up cached app files
func (w *AppUninstallWorker) cleanupCache() error {
	// Remove app from cache directories
	cachePattern := filepath.Join("/app-cache", "*", "*", "*", w.AppName)
	matches, err := filepath.Glob(cachePattern)
	if err != nil {
		return fmt.Errorf("failed to find cache files: %w", err)
	}

	for _, match := range matches {
		if err := os.Remove(match); err != nil {
			// Log error but continue with other files
			continue
		}
	}

	return nil
}

// execOnPod executes a command on a pod (shared with AppInstallWorker)
func (w *AppUninstallWorker) execOnPod(ctx context.Context, podName, command string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "exec", "-n", w.Namespace, podName, "--", "sh", "-c", command)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command execution failed: %w, output: %s", err, string(output))
	}

	return nil
}

// AppValidationWorker handles app validation
type AppValidationWorker struct {
	Client     client.Client
	AppName    string
	TargetPods []string
	Namespace  string
}

// Run executes the app validation operation
func (w *AppValidationWorker) Run(ctx context.Context) error {
	logger := log.FromContext(ctx).WithValues("worker", "app-validation", "app", w.AppName)
	logger.Info("Starting app validation")

	for _, podName := range w.TargetPods {
		logger.Info("Validating app on pod", "pod", podName)

		if err := w.validateAppOnPod(ctx, podName); err != nil {
			logger.Error(err, "App validation failed on pod", "pod", podName)
			return fmt.Errorf("app validation failed on pod %s: %w", podName, err)
		}

		logger.Info("App validation passed on pod", "pod", podName)
	}

	logger.Info("App validation completed on all pods")
	return nil
}

// validateAppOnPod validates an app on a specific pod
func (w *AppValidationWorker) validateAppOnPod(ctx context.Context, podName string) error {
	// Extract app name without extension
	appName := strings.TrimSuffix(w.AppName, filepath.Ext(w.AppName))
	appName = strings.TrimSuffix(appName, ".tar")

	// Check if app is installed and enabled
	checkCmd := fmt.Sprintf("/opt/splunk/bin/splunk list app %s -auth admin:`cat /mnt/splunk-secrets/password` | grep ENABLED", appName)

	cmd := exec.CommandContext(ctx, "kubectl", "exec", "-n", w.Namespace, podName, "--", "sh", "-c", checkCmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("app validation check failed: %w, output: %s", err, string(output))
	}

	if !strings.Contains(string(output), "ENABLED") {
		return fmt.Errorf("app %s is not enabled on pod %s", appName, podName)
	}

	return nil
}
