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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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
