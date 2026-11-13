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

package core

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

func TestBundleAndHelmSchemasExposeTheSamePlacementPolicy(t *testing.T) {
	var schemas []apiextv1.CustomResourceDefinition
	for _, path := range []string{
		"../../../../config/crd/bases/platform.splunk.com_postgresclusterclasses.yaml",
		"../../../../bundle/manifests/platform.splunk.com_postgresclusterclasses.yaml",
	} {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		var crd apiextv1.CustomResourceDefinition
		require.NoError(t, yaml.Unmarshal(data, &crd))
		schemas = append(schemas, crd)
	}
	for _, name := range []string{"storageClass", "affinity"} {
		properties := schemas[0].Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"].Properties["cnpg"].Properties
		expected, exists := properties[name]
		require.True(t, exists, name)
		properties = schemas[1].Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"].Properties["cnpg"].Properties
		actual, exists := properties[name]
		require.True(t, exists, "bundle CRD must not prune "+name)
		assert.Equal(t, expected, actual, name)
	}
}
