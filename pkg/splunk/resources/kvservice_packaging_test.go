// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

package resources

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

// OLM installs the bundled CRD, not the controller-gen source CRD. Check the
// complete schema so required fields, CEL rules and defaults cannot drift.
func TestKVServiceBundledSchemaMatchesSource(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	readCRD := func(path string) apiextensionsv1.CustomResourceDefinition {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		var crd apiextensionsv1.CustomResourceDefinition
		if err := yaml.Unmarshal(data, &crd); err != nil {
			t.Fatal(err)
		}
		return crd
	}
	source := readCRD("config/crd/bases/enterprise.splunk.com_kvservices.yaml")
	bundled := readCRD("bundle/manifests/enterprise.splunk.com_kvservices.yaml")
	if !reflect.DeepEqual(source.Spec, bundled.Spec) {
		t.Fatal("bundled KVService CRD differs from controller-gen output; regenerate the bundle")
	}
}
