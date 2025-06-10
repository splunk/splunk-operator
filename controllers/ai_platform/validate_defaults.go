/*
File: controllers/validate.go
*/
package ai_platform

import (
	"context"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
)

// Validate checks required fields and backfills defaults on the AIPlatform spec.
func (r *AIPlatformReconciler) validate(ctx context.Context, p *enterpriseApi.AIPlatform) error {
	// Required volume paths
	if p.Spec.AppsVolume.Path == "" {
		return fmt.Errorf("AppsVolume.Path is required")
	}
	if p.Spec.ArtifactsVolume.Path == "" {
		return fmt.Errorf("ArtifactsVolume.Path is required")
	}

	// Default image registries
	if p.Spec.HeadGroupSpec.ImageRegistry == "" {
		p.Spec.HeadGroupSpec.ImageRegistry = SetImageRegistry("RELATED_RAY_HEAD_IMAGE", "667741767953.dkr.ecr.us-west-2.amazonaws.com/ml-platform/ray:latest") // FIXME
	}
	if p.Spec.WorkerGroupSpec.ImageRegistry == "" {
		p.Spec.WorkerGroupSpec.ImageRegistry = SetImageRegistry("RELATED_RAY_WORKER_IMAGE", "667741767953.dkr.ecr.us-west-2.amazonaws.com/ml-platform/ray:latest") //FIXME
	}

	// Ensure at least one GPUConfig exists
	if len(p.Spec.WorkerGroupSpec.GPUConfigs) == 0 {
		p.Spec.WorkerGroupSpec.GPUConfigs = []enterpriseApi.GPUConfig{{
			Tier:        "default",
			MinReplicas: 1,
			MaxReplicas: 10,
			GPUsPerPod:  4,
		}}
	}

	if p.Spec.DefaultAcceleratorType == "" {
		p.Spec.DefaultAcceleratorType = "L40S"
	}
	// Per-tier defaults
	for i := range p.Spec.WorkerGroupSpec.GPUConfigs {
		cfg := &p.Spec.WorkerGroupSpec.GPUConfigs[i]
		if cfg.MinReplicas == 0 {
			cfg.MinReplicas = 1
		}
		if cfg.MaxReplicas == 0 {
			cfg.MaxReplicas = 10
		}
		if cfg.GPUsPerPod == 0 {
			cfg.GPUsPerPod = 4
		}
		if p.Spec.DefaultAcceleratorType == "" {
			p.Spec.DefaultAcceleratorType = "L40S"
		}

		// Default resource requests/limits for workers
		if cfg.Resources.Requests == nil {
			cfg.Resources.Requests = corev1.ResourceList{
				corev1.ResourceCPU:              resource.MustParse("1"),
				corev1.ResourceMemory:           resource.MustParse("16Gi"),
				corev1.ResourceEphemeralStorage: resource.MustParse("50Gi"),
				"nvidia.com/gpu":                resource.MustParse(fmt.Sprintf("%d", cfg.GPUsPerPod)),
			}
		}
		if cfg.Resources.Limits == nil {
			cfg.Resources.Limits = corev1.ResourceList{
				corev1.ResourceCPU:              resource.MustParse("16"),
				corev1.ResourceMemory:           resource.MustParse("16Gi"),
				corev1.ResourceEphemeralStorage: resource.MustParse("50Gi"),
				"nvidia.com/gpu":                resource.MustParse(fmt.Sprintf("%d", cfg.GPUsPerPod)),
			}
		}
	}

	return nil
}

func SetImageRegistry(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}
