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

func TestApplyBusConfiguration(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	ctx := context.TODO()

	scheme := runtime.NewScheme()
	_ = enterpriseApi.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Object definitions
	busConfig := &enterpriseApi.BusConfiguration{
		TypeMeta: metav1.TypeMeta{
			Kind:       "BusConfiguration",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "busConfig",
			Namespace: "test",
		},
		Spec: enterpriseApi.BusConfigurationSpec{
			Type: "sqs_smartbus",
			SQS: enterpriseApi.SQSSpec{
				QueueName:                 "test-queue",
				AuthRegion:                "us-west-2",
				Endpoint:                  "https://sqs.us-west-2.amazonaws.com",
				LargeMessageStorePath:     "s3://ingestion/smartbus-test",
				LargeMessageStoreEndpoint: "https://s3.us-west-2.amazonaws.com",
				DeadLetterQueueName:       "sqs-dlq-test",
			},
		},
	}
	c.Create(ctx, busConfig)

	// ApplyBusConfiguration
	result, err := ApplyBusConfiguration(ctx, c, busConfig)
	assert.NoError(t, err)
	assert.True(t, result.Requeue)
	assert.NotEqual(t, enterpriseApi.PhaseError, busConfig.Status.Phase)
	assert.Equal(t, enterpriseApi.PhaseReady, busConfig.Status.Phase)
}

func TestValidateBusConfigurationInputs(t *testing.T) {
	busConfig := enterpriseApi.BusConfiguration{
		TypeMeta: metav1.TypeMeta{
			Kind:       "BusConfiguration",
			APIVersion: "enterprise.splunk.com/v4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "busConfig",
		},
		Spec: enterpriseApi.BusConfigurationSpec{
			Type: "othertype",
			SQS:  enterpriseApi.SQSSpec{},
		},
	}

	err := validateBusConfigurationInputs(&busConfig)
	assert.NotNil(t, err)
	assert.Equal(t, "only sqs_smartbus type is supported in bus configuration", err.Error())

	busConfig.Spec.Type = "sqs_smartbus"

	err = validateBusConfigurationInputs(&busConfig)
	assert.NotNil(t, err)
	assert.Equal(t, "bus configuration sqs queueName, authRegion, deadLetterQueueName cannot be empty", err.Error())

	busConfig.Spec.SQS.AuthRegion = "us-west-2"

	err = validateBusConfigurationInputs(&busConfig)
	assert.NotNil(t, err)
	assert.Equal(t, "bus configuration sqs queueName, deadLetterQueueName cannot be empty", err.Error())

	busConfig.Spec.SQS.QueueName = "test-queue"
	busConfig.Spec.SQS.DeadLetterQueueName = "dlq-test"
	busConfig.Spec.SQS.AuthRegion = ""

	err = validateBusConfigurationInputs(&busConfig)
	assert.NotNil(t, err)
	assert.Equal(t, "bus configuration sqs authRegion cannot be empty", err.Error())

	busConfig.Spec.SQS.AuthRegion = "us-west-2"

	err = validateBusConfigurationInputs(&busConfig)
	assert.NotNil(t, err)
	assert.Equal(t, "bus configuration sqs endpoint, largeMessageStoreEndpoint must start with https://", err.Error())

	busConfig.Spec.SQS.Endpoint = "https://sqs.us-west-2.amazonaws.com"
	busConfig.Spec.SQS.LargeMessageStoreEndpoint = "https://s3.us-west-2.amazonaws.com"

	err = validateBusConfigurationInputs(&busConfig)
	assert.NotNil(t, err)
	assert.Equal(t, "bus configuration sqs largeMessageStorePath must start with s3://", err.Error())

	busConfig.Spec.SQS.LargeMessageStorePath = "ingestion/smartbus-test"

	err = validateBusConfigurationInputs(&busConfig)
	assert.NotNil(t, err)
	assert.Equal(t, "bus configuration sqs largeMessageStorePath must start with s3://", err.Error())

	busConfig.Spec.SQS.LargeMessageStorePath = "s3://ingestion/smartbus-test"

	err = validateBusConfigurationInputs(&busConfig)
	assert.Nil(t, err)
}
