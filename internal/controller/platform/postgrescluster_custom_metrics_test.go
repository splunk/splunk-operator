/*
Copyright 2026.

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

package controller

import (
	"testing"

	monitoring "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNewCustomMetricsModelDispatch(t *testing.T) {
	scheme := runtime.NewScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	model, err := newCustomMetricsModel(cnpgProvisioner, c, scheme, monitoring.Target{})
	require.NoError(t, err)
	assert.NotNil(t, model)

	model, err = newCustomMetricsModel("example.invalid/provider", c, scheme, monitoring.Target{})
	assert.Nil(t, model)
	assert.EqualError(t, err, `custom metrics: unsupported provisioner "example.invalid/provider"`)
}
