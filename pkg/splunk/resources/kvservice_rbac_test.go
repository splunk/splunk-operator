// Copyright (c) 2026 Splunk Inc. All rights reserved.
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

package resources

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

func TestKVServicePackagedOperatorPermissions(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	required := map[string][]string{
		"deployments":           {"create", "delete", "get", "list", "patch", "update", "watch"},
		"kvservices":            {"create", "delete", "get", "list", "patch", "update", "watch"},
		"kvservices/status":     {"get", "patch", "update"},
		"kvservices/finalizers": {"update"},
	}
	check := func(t *testing.T, rules []rbacv1.PolicyRule) {
		t.Helper()
		for resource, verbs := range required {
			group := "enterprise.splunk.com"
			if resource == "deployments" {
				group = "apps"
			}
			for _, verb := range verbs {
				allowed := false
				for _, rule := range rules {
					if includesRBAC(rule.APIGroups, group) && includesRBAC(rule.Resources, resource) && includesRBAC(rule.Verbs, verb) {
						allowed = true
						break
					}
				}
				if !allowed {
					t.Errorf("operator cannot %s %s/%s", verb, group, resource)
				}
			}
		}
	}
	for _, name := range []string{"role", "clusterrole"} {
		t.Run("helm-"+name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, "helm-chart/splunk-operator/templates/rbac", name+".yaml"))
			if err != nil {
				t.Fatal(err)
			}
			// The only template expressions are the surrounding condition and metadata name.
			var static []string
			for _, line := range strings.Split(string(data), "\n") {
				if !strings.Contains(line, "{{") {
					static = append(static, line)
				}
			}
			var role rbacv1.Role
			if err := yaml.Unmarshal([]byte(strings.Join(static, "\n")), &role); err != nil {
				t.Fatal(err)
			}
			check(t, role.Rules)
		})
	}
	t.Run("olm", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(root, "bundle/manifests/splunk-operator.clusterserviceversion.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		var csv struct {
			Spec struct {
				Install struct {
					Spec struct {
						ClusterPermissions []struct {
							ServiceAccountName string
							Rules              []rbacv1.PolicyRule
						}
					}
				}
			}
		}
		if err := yaml.Unmarshal(data, &csv); err != nil {
			t.Fatal(err)
		}
		for _, entry := range csv.Spec.Install.Spec.ClusterPermissions {
			if entry.ServiceAccountName == "splunk-operator-controller-manager" {
				check(t, entry.Rules)
				return
			}
		}
		t.Fatal("operator service account has no clusterPermissions")
	})
}

func includesRBAC(values []string, wanted string) bool {
	for _, v := range values {
		if v == wanted || v == "*" {
			return true
		}
	}
	return false
}
