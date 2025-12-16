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

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"k8s.io/client-go/tools/record"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func init() {
}

func TestClusterManagerEventPublisher(t *testing.T) {

	builder := fake.NewClientBuilder()
	c := builder.Build()
	recorder := record.NewFakeRecorder(10)

	cm := enterpriseApi.ClusterManager{}
	k8sevent, err := newK8EventPublisher(recorder, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	ctx := context.TODO()
	k8sevent.Normal(ctx, "testing", "normal message")
	k8sevent.Warning(ctx, "testing", "warning message")

	cmaster := enterpriseApiV3.ClusterMaster{}
	k8sevent.instance = &cmaster
	k8sevent.Normal(ctx, "", "")

	// Use client to avoid unused variable warning
	_ = c
}

func TestIndexerClusterEventPublisher(t *testing.T) {

	builder := fake.NewClientBuilder()
	c := builder.Build()
	recorder := record.NewFakeRecorder(10)

	cm := enterpriseApi.IndexerCluster{}
	k8sevent, err := newK8EventPublisher(recorder, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	k8sevent.Normal(context.TODO(), "testing", "normal message")
	k8sevent.Warning(context.TODO(), "testing", "warning message")

	// Use client to avoid unused variable warning
	_ = c
}

func TestMonitoringConsoleEventPublisher(t *testing.T) {

	builder := fake.NewClientBuilder()
	c := builder.Build()
	recorder := record.NewFakeRecorder(10)

	cm := enterpriseApi.MonitoringConsole{}
	k8sevent, err := newK8EventPublisher(recorder, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	k8sevent.Normal(context.TODO(), "testing", "normal message")
	k8sevent.Warning(context.TODO(), "testing", "warning message")

	// Use client to avoid unused variable warning
	_ = c
}

func TestSearchHeadClusterEventPublisher(t *testing.T) {

	builder := fake.NewClientBuilder()
	c := builder.Build()
	recorder := record.NewFakeRecorder(10)

	cm := enterpriseApi.SearchHeadCluster{}
	k8sevent, err := newK8EventPublisher(recorder, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	k8sevent.Normal(context.TODO(), "testing", "normal message")
	k8sevent.Warning(context.TODO(), "testing", "warning message")

	// Use client to avoid unused variable warning
	_ = c
}

func TestStandaloneEventPublisher(t *testing.T) {

	builder := fake.NewClientBuilder()
	c := builder.Build()
	recorder := record.NewFakeRecorder(10)

	cm := enterpriseApi.Standalone{}
	k8sevent, err := newK8EventPublisher(recorder, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	k8sevent.Normal(context.TODO(), "testing", "normal message")
	k8sevent.Warning(context.TODO(), "testing", "warning message")

	// Negative testing
	ctx := context.TODO()
	k8sevent.recorder = nil
	k8sevent.publishEvent(ctx, "", "", "")

	// Test with different instance type (this should work with EventRecorder)
	k8sevent.recorder = recorder
	k8sevent.instance = &cm
	k8sevent.publishEvent(ctx, "Normal", "TestReason", "Test message")

	// Use client to avoid unused variable warning
	_ = c
}

func TestLicenseManagerEventPublisher(t *testing.T) {

	builder := fake.NewClientBuilder()
	c := builder.Build()
	recorder := record.NewFakeRecorder(10)

	lmanager := enterpriseApi.LicenseManager{}
	k8sevent, err := newK8EventPublisher(recorder, &lmanager)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	ctx := context.TODO()
	k8sevent.Normal(ctx, "testing", "normal message")
	k8sevent.Warning(ctx, "testing", "warning message")

	lmaster := enterpriseApiV3.LicenseMaster{}
	k8sevent.instance = &lmaster
	k8sevent.Normal(ctx, "", "")

	// Use client to avoid unused variable warning
	_ = c
}
