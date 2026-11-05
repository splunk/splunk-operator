// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package testenv

import "testing"

func TestNewOperatorSuppliesMetricsNamespace(t *testing.T) {
	operator := newOperator("operator", "test-ns", "operator-sa", "operator:test", "splunk:test")
	for _, env := range operator.Spec.Template.Spec.Containers[0].Env {
		if env.Name != "POD_NAMESPACE" {
			continue
		}
		if env.Value != "" || env.ValueFrom == nil || env.ValueFrom.FieldRef == nil || env.ValueFrom.FieldRef.FieldPath != "metadata.namespace" {
			t.Errorf("POD_NAMESPACE must come from the Pod namespace, got %#v", env)
		}
		return
	}
	t.Error("operator Deployment lacks POD_NAMESPACE required for metrics startup")
}
