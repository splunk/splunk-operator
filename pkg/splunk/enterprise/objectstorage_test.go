/*
Copyright 2025.
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

package enterprise

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func init() {
	GetReadinessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../" + readinessScriptLocation)
		return fileLocation
	}
	GetLivenessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../" + livenessScriptLocation)
		return fileLocation
	}
	GetStartupScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../../" + startupScriptLocation)
		return fileLocation
	}
}

func TestApplyObjectStorage(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()

	scheme := runtime.NewScheme()
	_ = enterpriseApi.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Object definitions
	os := &enterpriseApi.ObjectStorage{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ObjectStorage",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "os",
			Namespace: "test",
		},
		Spec: enterpriseApi.ObjectStorageSpec{
			Provider: "s3",
			S3: enterpriseApi.S3Spec{
				Endpoint: "https://s3.us-west-2.amazonaws.com",
				Path:     "s3://bucket/key",
			},
		},
	}
	c.Create(ctx, os)

	// ApplyObjectStorage
	result, err := ApplyObjectStorage(ctx, c, os)
	assert.NoError(t, err)
	assert.True(t, result.Requeue)
	assert.NotEqual(t, enterpriseApi.PhaseError, os.Status.Phase)
	assert.Equal(t, enterpriseApi.PhaseReady, os.Status.Phase)
}
