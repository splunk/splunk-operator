package k8s

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetDeploymentImage returns the first container image from a deployment.
func (c *Client) GetDeploymentImage(ctx context.Context, namespace, name string) (string, error) {
	deploy := &appsv1.Deployment{}
	if err := c.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, deploy); err != nil {
		return "", err
	}
	if len(deploy.Spec.Template.Spec.Containers) == 0 {
		return "", nil
	}
	return deploy.Spec.Template.Spec.Containers[0].Image, nil
}
