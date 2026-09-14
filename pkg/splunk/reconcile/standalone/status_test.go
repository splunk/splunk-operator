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

package standalone

import (
	"context"
	"os"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

func TestUpdateCRStatus(t *testing.T) {
	os.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")

	sch := pkgruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))

	builder := newFakeClientBuilder(sch).
		WithStatusSubresource(&enterpriseApi.LicenseManager{}).
		WithStatusSubresource(&enterpriseApi.ClusterManager{}).
		WithStatusSubresource(&enterpriseApi.Standalone{}).
		WithStatusSubresource(&enterpriseApi.MonitoringConsole{}).
		WithStatusSubresource(&enterpriseApi.IndexerCluster{}).
		WithStatusSubresource(&enterpriseApi.Queue{}).
		WithStatusSubresource(&enterpriseApi.ObjectStorage{}).
		WithStatusSubresource(&enterpriseApi.IngestorCluster{}).
		WithStatusSubresource(&enterpriseApi.SearchHeadCluster{})
	c := builder.Build()
	ctx := context.TODO()

	// create standalone custom resource
	standalone := &enterpriseApi.Standalone{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Standalone",
			APIVersion: "enterprise.splunk.com/v3",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: enterpriseApi.StandaloneSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
				},
				Volumes: []corev1.Volume{},
			},
		},
		Status: enterpriseApi.StandaloneStatus{
			ReadyReplicas: 2,
		},
	}

	// When the CR is not even existing, error handling will keep retrying to update the CR, but fails at the end.
	updateCRStatus(ctx, c, standalone, nil)

	// Creating a standalone, and updating the CR will cover the happy path
	// simulate create standalone instance before reconciliation
	err := c.Create(ctx, standalone)
	if err != nil {
		t.Errorf("standalone CR creation failed.")
	}

	// call reconciliation
	_, err = ApplyStandalone(ctx, c, standalone)
	if err != nil {
		t.Errorf("Apply standalone failed.")
	}
	standalone.Status.ReadyReplicas = 3
	updateCRStatus(ctx, c, standalone, &err)
}
