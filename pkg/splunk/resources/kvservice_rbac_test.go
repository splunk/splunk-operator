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
		"namespaces":                        {"get", "list", "watch"},
		"horizontalpodautoscalers":          {"create", "delete", "get", "list", "update", "watch"},
		"events":                            {"create", "patch"},
		"deployments":                       {"create", "delete", "get", "list", "patch", "update", "watch"},
		"kvservices":                        {"create", "delete", "get", "list", "patch", "update", "watch"},
		"kvservices/status":                 {"get", "patch", "update"},
		"kvservices/finalizers":             {"update"},
		"postgresclusterclasses":            {"get", "list", "watch"},
		"postgresclusters":                  {"create", "delete", "get", "list", "patch", "update", "watch"},
		"postgresdatabases":                 {"create", "delete", "get", "list", "patch", "update", "watch"},
		"postgresclusters/status":           {"get", "patch", "update"},
		"postgresdatabases/status":          {"get", "list", "patch", "update", "watch"},
		"postgresclusters/finalizers":       {"update"},
		"postgresdatabases/finalizers":      {"update"},
		"networking.k8s.io:networkpolicies": {"get", "list", "watch"},
	}
	for _, resource := range []string{"backups", "clusters", "databases", "poolers", "publications", "scheduledbackups", "subscriptions"} {
		required["postgresql.cnpg.io:"+resource] = []string{"create", "delete", "get", "list", "patch", "update", "watch"}
	}
	for _, resource := range []string{"backups/status", "clusters/status", "poolers/status", "publications/status", "scheduledbackups/status", "subscriptions/status"} {
		required["postgresql.cnpg.io:"+resource] = []string{"get"}
	}
	check := func(t *testing.T, rules []rbacv1.PolicyRule) {
		t.Helper()
		for resource, verbs := range required {
			group := "enterprise.splunk.com"
			if qualifiedGroup, resourceName, ok := strings.Cut(resource, ":"); ok {
				group, resource = qualifiedGroup, resourceName
			} else if resource == "deployments" {
				group = "apps"
			} else if resource == "horizontalpodautoscalers" {
				group = "autoscaling"
			} else if resource == "events" {
				group = "events.k8s.io"
			} else if resource == "namespaces" {
				group = ""
			} else if strings.HasPrefix(resource, "postgres") {
				group = "platform.splunk.com"
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
	t.Run("canonical", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(root, "config/rbac/role.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		var role rbacv1.ClusterRole
		if err := yaml.Unmarshal(data, &role); err != nil {
			t.Fatal(err)
		}
		check(t, role.Rules)
	})
	// Helm permissions and feature gates are checked on rendered charts by
	// helm-chart/splunk-operator/tests/*rbac_test.yaml.
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
		t.Error("operator service account has no clusterPermissions")
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
