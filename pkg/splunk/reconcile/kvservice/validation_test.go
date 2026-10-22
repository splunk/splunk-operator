// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
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

package kvservice

import (
	"context"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateKVServiceSpec(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	ctx := context.TODO()
	c := spltest.NewMockClient()
	cr := &enterpriseApi.KVService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	err := validateKVServiceSpec(ctx, c, cr)
	if err != nil {
		t.Errorf("validateKVServiceSpec error=%v", err)
	}
	if cr.Spec.Replicas != 1 {
		t.Errorf("replicas=%d; want 1", cr.Spec.Replicas)
	}
	if cr.Spec.Image != "kvstore/kvservice:latest" {
		t.Errorf("image=%s; want %s", cr.Spec.Image, "kvstore/kvservice:latest")
	}
	if cr.Spec.Volumes == nil {
		t.Errorf("volumes should be non-nil")
	}
	if len(cr.Spec.ImagePullSecrets) != 0 {
		t.Errorf("imagePullSecrets len=%d; want 0", len(cr.Spec.ImagePullSecrets))
	}

	invalid := &enterpriseApi.KVService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack2",
			Namespace: "test",
		},
	}
	invalid.Spec.LivenessInitialDelaySeconds = -1
	err = validateKVServiceSpec(ctx, c, invalid)
	if err == nil {
		t.Errorf("Expected error")
	}
}
