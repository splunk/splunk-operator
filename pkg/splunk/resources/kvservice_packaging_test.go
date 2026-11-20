// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

package resources

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	admissionv1 "k8s.io/api/admissionregistration/v1"
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

func TestKVServiceOLMAdmissionRegistration(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "bundle/manifests/splunk-operator.clusterserviceversion.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var csv struct {
		Spec struct {
			WebhookDefinitions []struct {
				Type                    string
				DeploymentName          string
				WebhookPath             string
				ContainerPort           int32
				TargetPort              int32
				AdmissionReviewVersions []string
				FailurePolicy           *admissionv1.FailurePolicyType
				Rules                   []admissionv1.RuleWithOperations
			}
		}
	}
	if err := yaml.Unmarshal(data, &csv); err != nil {
		t.Fatal(err)
	}
	for _, webhook := range csv.Spec.WebhookDefinitions {
		for _, rule := range webhook.Rules {
			if !includesRBAC(rule.APIGroups, "enterprise.splunk.com") || !includesRBAC(rule.Resources, "kvservices") {
				continue
			}
			if webhook.Type != "ValidatingAdmissionWebhook" || webhook.DeploymentName != "splunk-operator-controller-manager" || webhook.WebhookPath != "/validate" || webhook.ContainerPort != 443 || webhook.TargetPort != 9443 {
				t.Errorf("KVService admission is not routed to the operator's TLS validation server: %+v", webhook)
			}
			if webhook.FailurePolicy == nil || *webhook.FailurePolicy != admissionv1.Ignore {
				t.Error("optional OLM validation must not block writes when its server is disabled or unavailable")
			}
			if !reflect.DeepEqual(webhook.AdmissionReviewVersions, []string{"v1"}) || !reflect.DeepEqual(rule.APIVersions, []string{"v4"}) || !reflect.DeepEqual(rule.Resources, []string{"kvservices"}) || rule.Scope == nil || *rule.Scope != admissionv1.NamespacedScope {
				t.Errorf("OLM KVService validation must be limited to namespaced v4 KVService writes: %+v", rule)
			}
			if !reflect.DeepEqual(rule.Operations, []admissionv1.OperationType{admissionv1.Create, admissionv1.Update}) {
				t.Errorf("OLM KVService validation must cover CREATE and UPDATE: %v", rule.Operations)
			}
			return
		}
	}
	t.Fatal("OLM CSV has no KVService admission webhook")
}
