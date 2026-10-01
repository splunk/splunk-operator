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

package k8sops

import (
	"context"
	"testing"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestGetClusterMasterList(t *testing.T) {
	ctx := context.TODO()
	cm := enterpriseApiV3.ClusterMaster{}
	listOpts := []client.ListOption{client.InNamespace("test")}
	mockClient := spltest.NewMockClient()

	_, err := GetClusterMasterList(ctx, mockClient, &cm, listOpts)
	if err == nil {
		t.Errorf("GetClusterMasterList() should have returned error as we haven't added cluster master to the list yet")
	}

	cmList := &enterpriseApiV3.ClusterMasterList{}
	cmList.Items = append(cmList.Items, cm)
	mockClient.ListObj = cmList

	list, err := GetClusterMasterList(ctx, mockClient, &cm, listOpts)
	if err != nil {
		t.Errorf("GetClusterMasterList() should not have returned error=%v", err)
	}

	if len(list.Items) != 1 {
		t.Errorf("Got wrong number of ClusterMaster objects. Expected=%d, Got=%d", 1, len(list.Items))
	}
}
