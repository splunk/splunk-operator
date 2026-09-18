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

package upgrade

import (
	"context"
	"fmt"
	"regexp"
	"sort"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// getCurrentImage gets the image of the statefulset, returns the image, and error if something goes wrong.
func getCurrentImage(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, instanceType splcommon.InstanceType) (string, error) {
	namespacedName := types.NamespacedName{
		Namespace: cr.GetNamespace(),
		Name:      splutil.GetSplunkStatefulsetName(instanceType, cr.GetName()),
	}
	statefulSet := &appsv1.StatefulSet{}
	err := c.Get(ctx, namespacedName, statefulSet)
	if err != nil {
		return "", err
	}

	if len(statefulSet.Spec.Template.Spec.Containers) > 0 {
		return statefulSet.Spec.Template.Spec.Containers[0].Image, nil
	}
	return "", fmt.Errorf("unable to get image from statefulset of type %s", instanceType.ToString())
}

func getIndexerClusterSortedSiteList(ctx context.Context, c splcommon.ControllerClient, ref corev1.ObjectReference, indexerList enterpriseApi.IndexerClusterList) (enterpriseApi.IndexerClusterList, error) {
	namespaceList := enterpriseApi.IndexerClusterList{}
	for _, v := range indexerList.Items {
		if v.Spec.ClusterManagerRef == ref {
			namespaceList.Items = append(namespaceList.Items, v)
		}
	}

	sort.SliceStable(namespaceList.Items, func(i, j int) bool {
		return getSiteName(ctx, c, &namespaceList.Items[i]) < getSiteName(ctx, c, &namespaceList.Items[j])
	})

	return namespaceList, nil
}

func getSiteName(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.IndexerCluster) string {
	defaults := cr.Spec.Defaults
	// site name starts with site:
	pattern := `site:\s+(\w+)`

	// Compile the regular expression pattern
	re := regexp.MustCompile(pattern)

	// Find the first match in the input string
	match := re.FindStringSubmatch(defaults)

	var extractedValue string
	if len(match) > 1 {
		// Extracted value is stored in the second element of the match array
		extractedValue := match[1]
		return extractedValue
	}

	return extractedValue
}
