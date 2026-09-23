/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package v3

import (
	"fmt"
	"strings"

	hubApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

// hubGroupVersion is the group-version every spoke in this package converts
// through.
var hubGroupVersion = hubApi.GroupVersion

// unrepresentableIndexerClusterFields returns the json paths set on the hub
// spec that this version cannot express.
func unrepresentableIndexerClusterFields(spec *hubApi.IndexerClusterSpec) []string {
	var fields []string
	if spec.QueueRef != nil {
		fields = append(fields, "spec.queueRef")
	}
	if spec.ObjectStorageRef != nil {
		fields = append(fields, "spec.objectStorageRef")
	}
	if spec.NoahClusterRef != nil {
		fields = append(fields, "spec.noahClusterRef")
	}
	return fields
}

// unrepresentableSearchHeadClusterFields returns the json paths set on the hub
// spec that this version cannot express.
func unrepresentableSearchHeadClusterFields(spec *hubApi.SearchHeadClusterSpec) []string {
	var fields []string
	if spec.NoahClusterRef != nil {
		fields = append(fields, "spec.noahClusterRef")
	}
	if len(spec.DeployerResourceSpec.Limits) > 0 || len(spec.DeployerResourceSpec.Requests) > 0 ||
		len(spec.DeployerResourceSpec.Claims) > 0 {
		fields = append(fields, "spec.deployerResourceSpec")
	}
	if spec.DeployerNodeAffinity != nil {
		fields = append(fields, "spec.deployerNodeAffinity")
	}
	return fields
}

// refuseIfUnrepresentable builds the error a refused downgrade returns, or nil
// when nothing unrepresentable is set. The API server surfaces the message to
// whoever issued the request, so it names the kind, the object, the offending
// fields and the version to use instead.
func refuseIfUnrepresentable(kind, namespace, name string, fields []string) error {
	if len(fields) == 0 {
		return nil
	}

	return fmt.Errorf("%s %s/%s cannot be represented in apiVersion %s: %s set but unsupported in %s. Use %s to read or modify this resource",
		kind, namespace, name, GroupVersion.String(), strings.Join(fields, ", "),
		GroupVersion.Version, hubGroupVersion.String())
}
