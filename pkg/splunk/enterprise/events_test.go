// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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

package enterprise

import (
	"context"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func init() {
}

func TestClusterManagerEventPublisher(t *testing.T) {

	builder := fake.NewClientBuilder()
	c := builder.Build()

	cm := enterpriseApi.ClusterMaster{}
	k8sevent, err := newK8EventPublisher(c, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	k8sevent.Normal(context.TODO(), "testing", "normal message")
	k8sevent.Warning(context.TODO(), "testing", "warning message")
}

func TestIndexerClusterEventPublisher(t *testing.T) {

	builder := fake.NewClientBuilder()
	c := builder.Build()

	cm := enterpriseApi.IndexerCluster{}
	k8sevent, err := newK8EventPublisher(c, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	k8sevent.Normal(context.TODO(), "testing", "normal message")
	k8sevent.Warning(context.TODO(), "testing", "warning message")
}

func TestMonitoringConsoleEventPublisher(t *testing.T) {

	builder := fake.NewClientBuilder()
	c := builder.Build()

	cm := enterpriseApi.MonitoringConsole{}
	k8sevent, err := newK8EventPublisher(c, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	k8sevent.Normal(context.TODO(), "testing", "normal message")
	k8sevent.Warning(context.TODO(), "testing", "warning message")
}

func TestSearchHeadClusterEventPublisher(t *testing.T) {

	builder := fake.NewClientBuilder()
	c := builder.Build()

	cm := enterpriseApi.SearchHeadCluster{}
	k8sevent, err := newK8EventPublisher(c, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	k8sevent.Normal(context.TODO(), "testing", "normal message")
	k8sevent.Warning(context.TODO(), "testing", "warning message")
}

func TestStandaloneEventPublisher(t *testing.T) {

	builder := fake.NewClientBuilder()
	c := builder.Build()

	cm := enterpriseApi.Standalone{}
	k8sevent, err := newK8EventPublisher(c, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	k8sevent.Normal(context.TODO(), "testing", "normal message")
	k8sevent.Warning(context.TODO(), "testing", "warning message")
}

func TestLicenseManagerEventPublisher(t *testing.T) {

	builder := fake.NewClientBuilder()
	c := builder.Build()

	cm := enterpriseApi.LicenseMaster{}
	k8sevent, err := newK8EventPublisher(c, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	k8sevent.Normal(context.TODO(), "testing", "normal message")
	k8sevent.Warning(context.TODO(), "testing", "warning message")
}
