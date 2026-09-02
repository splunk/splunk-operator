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

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetSmartstoreVolumesConfig(t *testing.T) {
	cr := &enterpriseApi.Standalone{ObjectMeta: metav1.ObjectMeta{Name: "standalone", Namespace: "test"}}
	smartstore := &enterpriseApi.SmartStoreSpec{VolList: []enterpriseApi.VolumeSpec{{Name: "s3-volume", Endpoint: "https://s3.example.com", Path: "bucket", Region: "eu-west-1"}}}

	config, err := GetSmartstoreVolumesConfig(context.Background(), spltest.NewMockClient(), cr, smartstore, map[string]string{})

	require.NoError(t, err)
	assert.Contains(t, config, "[volume:s3-volume]")
	assert.Contains(t, config, "path = s3://bucket")
	assert.Contains(t, config, "remote.s3.endpoint = https://s3.example.com")
}

func TestApplySmartstoreConfigMap(t *testing.T) {
	ctx := context.Background()
	client := spltest.NewMockClient()
	cr := &enterpriseApi.Standalone{
		TypeMeta:   metav1.TypeMeta{Kind: "Standalone", APIVersion: enterpriseApi.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Name: "standalone", Namespace: "test"},
	}

	configMap, changed, err := ApplySmartstoreConfigMap(ctx, client, cr, &enterpriseApi.SmartStoreSpec{})

	require.NoError(t, err)
	assert.False(t, changed)
	require.NotNil(t, configMap)
	assert.Contains(t, configMap.Data["indexes.conf"], "[default]")
}

func TestAreRemoteVolumeKeysChanged(t *testing.T) {
	ctx := context.Background()
	client := spltest.NewMockClient()
	cr := &enterpriseApi.Standalone{ObjectMeta: metav1.ObjectMeta{Name: "standalone", Namespace: "test"}}
	smartstore := &enterpriseApi.SmartStoreSpec{}
	var err error

	assert.False(t, AreRemoteVolumeKeysChanged(ctx, client, cr, splcommon.SplunkStandalone, smartstore, map[string]string{}, &err))
	assert.NoError(t, err)
}

func TestGetSmartstoreRemoteVolumeSecrets(t *testing.T) {
	ctx := context.Background()
	client := spltest.NewMockClient()
	cr := &enterpriseApi.Standalone{ObjectMeta: metav1.ObjectMeta{Name: "standalone", Namespace: "test"}}
	client.AddObject(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "s3-secret", Namespace: "test", ResourceVersion: "1"},
		Data:       map[string][]byte{s3AccessKey: []byte("access"), s3SecretKey: []byte("secret")},
	})

	accessKey, secretKey, resourceVersion, err := GetSmartstoreRemoteVolumeSecrets(ctx, enterpriseApi.VolumeSpec{SecretRef: "s3-secret"}, client, cr, &enterpriseApi.SmartStoreSpec{})

	require.NoError(t, err)
	assert.Equal(t, "access", accessKey)
	assert.Equal(t, "secret", secretKey)
	assert.Equal(t, "1", resourceVersion)
}
