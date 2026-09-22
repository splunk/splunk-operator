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

package licensemanager

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splstorage "github.com/splunk/splunk-operator/pkg/splunk/client/storage"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RemoteDataClientManager struct {
	client              splcommon.ControllerClient
	cr                  splcommon.MetaObject
	appFrameworkRef     *enterpriseApi.AppFrameworkSpec
	vol                 *enterpriseApi.VolumeSpec
	location            string
	initFn              splcommon.GetInitFunc
	getRemoteDataClient func(context.Context, splcommon.ControllerClient, splcommon.MetaObject, *enterpriseApi.AppFrameworkSpec, *enterpriseApi.VolumeSpec, string, splcommon.GetInitFunc) (splstorage.SplunkRemoteDataClient, error)
}

func (m *RemoteDataClientManager) GetAppsList(ctx context.Context) (splcommon.RemoteDataListResponse, error) {
	c, err := m.getRemoteDataClient(ctx, m.client, m.cr, m.appFrameworkRef, m.vol, m.location, m.initFn)
	if err != nil {
		return splcommon.RemoteDataListResponse{}, err
	}
	return c.Client.GetAppsList(ctx)
}

func loadFixture(t *testing.T, filename string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "fixtures", filename))
	if err != nil {
		t.Errorf("Failed to load fixture %s: %v", filename, err)
		return ""
	}
	var compactJSON bytes.Buffer
	if err := json.Compact(&compactJSON, data); err != nil {
		t.Errorf("Failed to compact JSON from fixture %s: %v", filename, err)
		return ""
	}
	return compactJSON.String()
}

func configTester(t *testing.T, method string, f func() (interface{}, error), want string) {
	t.Helper()
	result, err := f()
	require.NoError(t, err, method)
	got, err := json.Marshal(result)
	require.NoError(t, err, method)
	require.JSONEq(t, want, string(got), method)
}

func splunkDeletionTester(t *testing.T, cr splcommon.MetaObject, delete func(splcommon.MetaObject, splcommon.ControllerClient) (bool, error)) {
	t.Helper()
	pvcList := corev1.PersistentVolumeClaimList{Items: []corev1.PersistentVolumeClaim{{ObjectMeta: metav1.ObjectMeta{Name: "splunk-pvc-stack1-var", Namespace: "test"}}}}
	c := spltest.NewMockClient()
	c.ListObj = &pvcList
	deleted, err := delete(cr, c)
	if deleted != (cr.GetObjectMeta().GetDeletionTimestamp() != nil) || err != nil {
		t.Errorf("k8sops.CheckForDeletion() returned %t, %v; want expected deletion, nil", deleted, err)
	}
}
