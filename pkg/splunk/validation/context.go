/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package validation

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ValidationContext provides context for validation operations that may need
// to access Kubernetes resources
type ValidationContext struct {
	// Client is the Kubernetes client for resource lookups
	Client client.Client

	// Namespace is the namespace of the object being validated
	Namespace string

	// Ctx is the context for API calls
	Ctx context.Context
}

// NewValidationContext creates a new ValidationContext
func NewValidationContext(c client.Client, namespace string) *ValidationContext {
	return &ValidationContext{
		Client:    c,
		Namespace: namespace,
		Ctx:       context.Background(),
	}
}

// SecretExists checks if a secret with the given name exists in the namespace
func (vc *ValidationContext) SecretExists(name string) (bool, error) {
	if vc.Client == nil {
		// If no client is available, skip existence check
		return true, nil
	}

	secret := &corev1.Secret{}
	err := vc.Client.Get(vc.Ctx, types.NamespacedName{
		Name:      name,
		Namespace: vc.Namespace,
	}, secret)

	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Secret not found
			return false, nil
		}
		return false, err
	}

	return true, nil
}
