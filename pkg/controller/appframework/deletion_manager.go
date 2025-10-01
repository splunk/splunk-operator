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

package appframework

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appframeworkv1 "github.com/splunk/splunk-operator/api/appframework/v1"
)

// DeletionManager handles app deletion safety mechanisms and policies
type DeletionManager struct {
	Client client.Client
}

// NewDeletionManager creates a new DeletionManager
func NewDeletionManager(client client.Client) *DeletionManager {
	return &DeletionManager{
		Client: client,
	}
}

// CheckSafeguards checks deletion safeguards before allowing app deletion
func (dm *DeletionManager) CheckSafeguards(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment) error {
	logger := log.FromContext(ctx).WithValues("app", deployment.Spec.AppName)

	if deployment.Spec.DeletionPolicy == nil || deployment.Spec.DeletionPolicy.Safeguards == nil {
		return nil // No safeguards configured
	}

	safeguards := deployment.Spec.DeletionPolicy.Safeguards

	// Check if app is in protected list
	if err := dm.checkProtectedApps(deployment.Spec.AppName, safeguards.ProtectedApps); err != nil {
		return err
	}

	// Check if app requires confirmation
	if err := dm.checkConfirmationRequired(ctx, deployment, safeguards.RequireConfirmation); err != nil {
		return err
	}

	// Check bulk deletion threshold
	if err := dm.checkBulkDeletionThreshold(ctx, deployment, safeguards.BulkDeletionThreshold); err != nil {
		return err
	}

	// Check approval workflow if required
	if safeguards.ApprovalWorkflow != nil && safeguards.ApprovalWorkflow.Required {
		if err := dm.checkApprovalWorkflow(ctx, deployment, safeguards.ApprovalWorkflow); err != nil {
			return err
		}
	}

	logger.Info("All deletion safeguards passed")
	return nil
}

// checkProtectedApps checks if the app is in the protected apps list
func (dm *DeletionManager) checkProtectedApps(appName string, protectedApps []string) error {
	for _, protectedApp := range protectedApps {
		if strings.EqualFold(appName, protectedApp) {
			return fmt.Errorf("app %s is protected and cannot be automatically deleted", appName)
		}

		// Support wildcard matching
		if strings.Contains(protectedApp, "*") {
			if matched := dm.wildcardMatch(appName, protectedApp); matched {
				return fmt.Errorf("app %s matches protected pattern %s and cannot be automatically deleted", appName, protectedApp)
			}
		}
	}
	return nil
}

// wildcardMatch performs simple wildcard matching
func (dm *DeletionManager) wildcardMatch(text, pattern string) bool {
	// Simple implementation - in production you might want to use a more robust pattern matching library
	if pattern == "*" {
		return true
	}

	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") {
		middle := pattern[1 : len(pattern)-1]
		return strings.Contains(text, middle)
	}

	if strings.HasPrefix(pattern, "*") {
		suffix := pattern[1:]
		return strings.HasSuffix(text, suffix)
	}

	if strings.HasSuffix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		return strings.HasPrefix(text, prefix)
	}

	return text == pattern
}

// checkConfirmationRequired checks if the app requires manual confirmation
func (dm *DeletionManager) checkConfirmationRequired(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment, requireConfirmation []string) error {
	appName := deployment.Spec.AppName

	for _, confirmApp := range requireConfirmation {
		if strings.EqualFold(appName, confirmApp) || dm.wildcardMatch(appName, confirmApp) {
			// Check if confirmation has been provided via annotation
			if deployment.Annotations == nil {
				return fmt.Errorf("app %s requires manual confirmation for deletion", appName)
			}

			confirmationKey := "appframework.splunk.com/deletion-confirmed"
			if confirmed, exists := deployment.Annotations[confirmationKey]; !exists || confirmed != "true" {
				return fmt.Errorf("app %s requires manual confirmation for deletion (add annotation %s=true)", appName, confirmationKey)
			}

			// Check confirmation timestamp to ensure it's recent
			timestampKey := "appframework.splunk.com/deletion-confirmed-at"
			if timestamp, exists := deployment.Annotations[timestampKey]; exists {
				if confirmTime, err := time.Parse(time.RFC3339, timestamp); err == nil {
					if time.Since(confirmTime) > time.Hour*24 {
						return fmt.Errorf("deletion confirmation for app %s has expired (older than 24 hours)", appName)
					}
				}
			}
		}
	}

	return nil
}

// checkBulkDeletionThreshold checks if too many apps are being deleted simultaneously
func (dm *DeletionManager) checkBulkDeletionThreshold(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment, threshold *int32) error {
	if threshold == nil {
		return nil
	}

	// Count concurrent deletion deployments in the same namespace
	deploymentList := &appframeworkv1.AppFrameworkDeploymentList{}
	if err := dm.Client.List(ctx, deploymentList, &client.ListOptions{
		Namespace: deployment.Namespace,
	}); err != nil {
		return fmt.Errorf("failed to list deployments for bulk deletion check: %w", err)
	}

	concurrentDeletions := int32(0)
	for _, dep := range deploymentList.Items {
		if dep.Spec.Operation == "uninstall" &&
			(dep.Status.Phase == "Pending" || dep.Status.Phase == "Running" || dep.Status.Phase == "Scheduled") {
			concurrentDeletions++
		}
	}

	if concurrentDeletions > *threshold {
		return fmt.Errorf("bulk deletion threshold exceeded: %d concurrent deletions (max: %d)", concurrentDeletions, *threshold)
	}

	return nil
}

// checkApprovalWorkflow checks if approval workflow requirements are met
func (dm *DeletionManager) checkApprovalWorkflow(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment, workflow *appframeworkv1.ApprovalWorkflow) error {
	if !workflow.Required {
		return nil
	}

	// Check if approval has been granted via ConfigMap or annotation
	approvalKey := "appframework.splunk.com/deletion-approved"
	approverKey := "appframework.splunk.com/deletion-approved-by"
	approvalTimeKey := "appframework.splunk.com/deletion-approved-at"

	if deployment.Annotations == nil {
		return fmt.Errorf("app %s requires approval workflow for deletion", deployment.Spec.AppName)
	}

	approved, approvalExists := deployment.Annotations[approvalKey]
	approver, approverExists := deployment.Annotations[approverKey]
	approvalTime, timeExists := deployment.Annotations[approvalTimeKey]

	if !approvalExists || approved != "true" {
		return fmt.Errorf("app %s requires approval for deletion", deployment.Spec.AppName)
	}

	if !approverExists {
		return fmt.Errorf("app %s deletion approval missing approver information", deployment.Spec.AppName)
	}

	// Validate approver is in the allowed list
	if len(workflow.Approvers) > 0 {
		validApprover := false
		for _, validApproverName := range workflow.Approvers {
			if approver == validApproverName {
				validApprover = true
				break
			}
		}
		if !validApprover {
			return fmt.Errorf("app %s deletion approved by unauthorized user %s", deployment.Spec.AppName, approver)
		}
	}

	// Check approval timeout
	if timeExists && workflow.Timeout != nil {
		if approvalTimestamp, err := time.Parse(time.RFC3339, approvalTime); err == nil {
			if time.Since(approvalTimestamp) > workflow.Timeout.Duration {
				return fmt.Errorf("app %s deletion approval has expired", deployment.Spec.AppName)
			}
		}
	}

	return nil
}

// CheckApproval checks if manual approval has been granted for deletion
func (dm *DeletionManager) CheckApproval(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment) (bool, error) {
	logger := log.FromContext(ctx).WithValues("app", deployment.Spec.AppName)

	// Check for approval annotation
	approvalKey := "appframework.splunk.com/deletion-approved"
	if deployment.Annotations != nil {
		if approved, exists := deployment.Annotations[approvalKey]; exists && approved == "true" {
			logger.Info("Manual deletion approval found")
			return true, nil
		}
	}

	// Check for approval in ConfigMap (alternative approval method)
	approvalConfigMap := &corev1.ConfigMap{}
	configMapKey := types.NamespacedName{
		Name:      "app-framework-approvals",
		Namespace: deployment.Namespace,
	}

	if err := dm.Client.Get(ctx, configMapKey, approvalConfigMap); err == nil {
		if approvalConfigMap.Data != nil {
			appApprovalKey := fmt.Sprintf("delete-%s", deployment.Spec.AppName)
			if approved, exists := approvalConfigMap.Data[appApprovalKey]; exists && approved == "true" {
				logger.Info("Manual deletion approval found in ConfigMap")
				return true, nil
			}
		}
	}

	logger.Info("No manual deletion approval found")
	return false, nil
}

// CreateDeletionRequest creates a deletion request for manual approval
func (dm *DeletionManager) CreateDeletionRequest(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment) error {
	logger := log.FromContext(ctx).WithValues("app", deployment.Spec.AppName)

	// Create or update approval request ConfigMap
	configMapName := "app-framework-deletion-requests"
	configMap := &corev1.ConfigMap{}
	configMapKey := types.NamespacedName{
		Name:      configMapName,
		Namespace: deployment.Namespace,
	}

	err := dm.Client.Get(ctx, configMapKey, configMap)
	if err != nil {
		// Create new ConfigMap
		configMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: deployment.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/name":      "app-framework",
					"app.kubernetes.io/component": "deletion-requests",
				},
			},
			Data: make(map[string]string),
		}
	}

	// Add deletion request
	requestKey := fmt.Sprintf("delete-%s", deployment.Spec.AppName)
	requestData := fmt.Sprintf(`{
		"app": "%s",
		"deployment": "%s",
		"requestedAt": "%s",
		"reason": "App deleted from repository",
		"status": "pending"
	}`, deployment.Spec.AppName, deployment.Name, time.Now().Format(time.RFC3339))

	if configMap.Data == nil {
		configMap.Data = make(map[string]string)
	}
	configMap.Data[requestKey] = requestData

	// Create or update the ConfigMap
	if err := dm.Client.Update(ctx, configMap); err != nil {
		if err := dm.Client.Create(ctx, configMap); err != nil {
			return fmt.Errorf("failed to create deletion request: %w", err)
		}
	}

	logger.Info("Created deletion request for manual approval")
	return nil
}

// ValidateDeletionConditions validates that deletion conditions are met
func (dm *DeletionManager) ValidateDeletionConditions(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment) error {
	logger := log.FromContext(ctx).WithValues("app", deployment.Spec.AppName)

	for _, condition := range deployment.Spec.DeletionConditions {
		logger.Info("Checking deletion condition", "type", condition.Type)

		satisfied, err := dm.checkDeletionCondition(ctx, deployment, condition)
		if err != nil {
			return fmt.Errorf("failed to check deletion condition %s: %w", condition.Type, err)
		}

		if !satisfied {
			if condition.FailurePolicy == "Ignore" {
				logger.Info("Deletion condition not satisfied but ignoring", "type", condition.Type)
				continue
			}
			return fmt.Errorf("deletion condition %s not satisfied", condition.Type)
		}

		logger.Info("Deletion condition satisfied", "type", condition.Type)
	}

	return nil
}

// checkDeletionCondition checks a single deletion condition
func (dm *DeletionManager) checkDeletionCondition(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment, condition appframeworkv1.DeletionCondition) (bool, error) {
	// This is a placeholder implementation
	// In a real implementation, you would execute the check command on the target pods
	// and verify the result matches the expected result

	switch condition.Type {
	case "AppNotUsed":
		// Check if app has been used recently by examining Splunk logs
		return dm.checkAppUsage(ctx, deployment, condition)
	case "NoActiveDashboards":
		// Check if app has active dashboards
		return dm.checkActiveDashboards(ctx, deployment, condition)
	case "NoActiveSearches":
		// Check if app has active searches
		return dm.checkActiveSearches(ctx, deployment, condition)
	default:
		// Generic command execution
		return dm.executeConditionCheck(ctx, deployment, condition)
	}
}

// checkAppUsage checks if the app has been used recently
func (dm *DeletionManager) checkAppUsage(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment, condition appframeworkv1.DeletionCondition) (bool, error) {
	// Placeholder implementation
	// In reality, this would execute a Splunk search to check app usage
	return true, nil
}

// checkActiveDashboards checks if the app has active dashboards
func (dm *DeletionManager) checkActiveDashboards(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment, condition appframeworkv1.DeletionCondition) (bool, error) {
	// Placeholder implementation
	// In reality, this would check for dashboard files in the app
	return true, nil
}

// checkActiveSearches checks if the app has active searches
func (dm *DeletionManager) checkActiveSearches(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment, condition appframeworkv1.DeletionCondition) (bool, error) {
	// Placeholder implementation
	// In reality, this would check for active searches using the app
	return true, nil
}

// executeConditionCheck executes a generic condition check command
func (dm *DeletionManager) executeConditionCheck(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment, condition appframeworkv1.DeletionCondition) (bool, error) {
	// Placeholder implementation
	// In reality, this would execute the check command on the target pods
	// and compare the result with the expected result
	return true, nil
}

// RecordDeletionEvent records a deletion event for audit purposes
func (dm *DeletionManager) RecordDeletionEvent(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment, reason string) error {
	logger := log.FromContext(ctx).WithValues("app", deployment.Spec.AppName)

	// Create or update deletion history ConfigMap
	configMapName := "app-framework-deletion-history"
	configMap := &corev1.ConfigMap{}
	configMapKey := types.NamespacedName{
		Name:      configMapName,
		Namespace: deployment.Namespace,
	}

	err := dm.Client.Get(ctx, configMapKey, configMap)
	if err != nil {
		// Create new ConfigMap
		configMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: deployment.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/name":      "app-framework",
					"app.kubernetes.io/component": "deletion-history",
				},
			},
			Data: make(map[string]string),
		}
	}

	// Add deletion record
	recordKey := fmt.Sprintf("%s-%d", deployment.Spec.AppName, time.Now().Unix())
	recordData := fmt.Sprintf(`{
		"app": "%s",
		"deployment": "%s",
		"deletedAt": "%s",
		"reason": "%s",
		"targetPods": %q,
		"scope": "%s"
	}`, deployment.Spec.AppName, deployment.Name, time.Now().Format(time.RFC3339), reason, deployment.Spec.TargetPods, deployment.Spec.Scope)

	if configMap.Data == nil {
		configMap.Data = make(map[string]string)
	}
	configMap.Data[recordKey] = recordData

	// Create or update the ConfigMap
	if err := dm.Client.Update(ctx, configMap); err != nil {
		if err := dm.Client.Create(ctx, configMap); err != nil {
			return fmt.Errorf("failed to record deletion event: %w", err)
		}
	}

	logger.Info("Recorded deletion event", "reason", reason)
	return nil
}
