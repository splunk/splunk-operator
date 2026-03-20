// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package testenv

import (
	"context"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UpdateResourceLimits updates the resource limits for a given CR object
func UpdateResourceLimits(ctx context.Context, deployment *Deployment, obj client.Object, instanceName string, cpuLimit string, memoryLimit string) error {
	err := deployment.GetInstance(ctx, instanceName, obj)
	if err != nil {
		return err
	}

	limits := corev1.ResourceList{}
	if cpuLimit != "" {
		limits["cpu"] = resource.MustParse(cpuLimit)
	}
	if memoryLimit != "" {
		limits["memory"] = resource.MustParse(memoryLimit)
	}

	return deployment.UpdateCR(ctx, obj)
}

// GetAndDeleteCR is a helper function that gets a CR instance and then deletes it
func GetAndDeleteCR(ctx context.Context, deployment *Deployment, obj client.Object, instanceName string) error {
	err := deployment.GetInstance(ctx, instanceName, obj)
	Expect(err).To(Succeed(), "Unable to get instance", "Instance Name", instanceName)

	err = deployment.DeleteCR(ctx, obj)
	Expect(err).To(Succeed(), "Unable to delete instance", "Instance Name", instanceName)

	return nil
}

// UpdateCRWithRetry attempts to update a CR with retry logic
func UpdateCRWithRetry(ctx context.Context, deployment *Deployment, obj client.Object, maxRetries int) error {
	var err error
	for i := 0; i < maxRetries; i++ {
		err = deployment.UpdateCR(ctx, obj)
		if err == nil {
			return nil
		}
	}
	return err
}

// GetInstanceWithExpect is a wrapper around GetInstance that includes Gomega expectations
func GetInstanceWithExpect(ctx context.Context, deployment *Deployment, obj client.Object, instanceName string, errorMsg string) {
	err := deployment.GetInstance(ctx, instanceName, obj)
	Expect(err).To(Succeed(), errorMsg, "Instance Name", instanceName)
}

// UpdateCRWithExpect is a wrapper around UpdateCR that includes Gomega expectations
func UpdateCRWithExpect(ctx context.Context, deployment *Deployment, obj client.Object, errorMsg string) {
	err := deployment.UpdateCR(ctx, obj)
	Expect(err).To(Succeed(), errorMsg)
}

// DeleteCRWithExpect is a wrapper around DeleteCR that includes Gomega expectations
func DeleteCRWithExpect(ctx context.Context, deployment *Deployment, obj client.Object, errorMsg string) {
	err := deployment.DeleteCR(ctx, obj)
	Expect(err).To(Succeed(), errorMsg)
}
