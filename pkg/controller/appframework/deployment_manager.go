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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appframeworkv1 "github.com/splunk/splunk-operator/api/appframework/v1"
)

// DeploymentManager manages app deployments
type DeploymentManager struct {
	Client client.Client
}

// NewDeploymentManager creates a new DeploymentManager
func NewDeploymentManager(client client.Client) *DeploymentManager {
	return &DeploymentManager{
		Client: client,
	}
}

// CreateDeployment creates a new app deployment
func (dm *DeploymentManager) CreateDeployment(ctx context.Context, syncName, namespace string, appName, appSource, operation string, targetPods []string) (*appframeworkv1.AppFrameworkDeployment, error) {
	logger := log.FromContext(ctx)

	deployment := &appframeworkv1.AppFrameworkDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-%s", operation, appName, syncName),
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "app-framework",
				"app.kubernetes.io/component":  "deployment",
				"appframework.splunk.com/sync": syncName,
				"appframework.splunk.com/app":  appName,
			},
		},
		Spec: appframeworkv1.AppFrameworkDeploymentSpec{
			AppName:    appName,
			AppSource:  appSource,
			TargetPods: targetPods,
			Operation:  operation,
			Scope:      "local", // Default scope
			Priority:   1,
		},
	}

	if err := dm.Client.Create(ctx, deployment); err != nil {
		return nil, fmt.Errorf("failed to create deployment: %w", err)
	}

	logger.Info("Created app deployment", "deployment", deployment.Name, "app", appName, "operation", operation)
	return deployment, nil
}

// GetDeployment gets a deployment by name and namespace
func (dm *DeploymentManager) GetDeployment(ctx context.Context, name, namespace string) (*appframeworkv1.AppFrameworkDeployment, error) {
	deployment := &appframeworkv1.AppFrameworkDeployment{}
	key := types.NamespacedName{Name: name, Namespace: namespace}

	if err := dm.Client.Get(ctx, key, deployment); err != nil {
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	return deployment, nil
}

// UpdateDeploymentStatus updates the status of a deployment
func (dm *DeploymentManager) UpdateDeploymentStatus(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment, phase string, message string) error {
	deployment.Status.Phase = phase
	deployment.Status.Message = message

	if err := dm.Client.Status().Update(ctx, deployment); err != nil {
		return fmt.Errorf("failed to update deployment status: %w", err)
	}

	return nil
}

// ListDeploymentsForSync lists all deployments for a sync
func (dm *DeploymentManager) ListDeploymentsForSync(ctx context.Context, syncName, namespace string) ([]*appframeworkv1.AppFrameworkDeployment, error) {
	deploymentList := &appframeworkv1.AppFrameworkDeploymentList{}

	labelSelector := client.MatchingLabels{
		"appframework.splunk.com/sync": syncName,
	}

	if err := dm.Client.List(ctx, deploymentList, client.InNamespace(namespace), labelSelector); err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	deployments := make([]*appframeworkv1.AppFrameworkDeployment, len(deploymentList.Items))
	for i := range deploymentList.Items {
		deployments[i] = &deploymentList.Items[i]
	}

	return deployments, nil
}
