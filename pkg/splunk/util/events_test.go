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

package util

import (
	"context"
	"testing"

	"github.com/pkg/errors"
	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func init() {
}

func TestClusterManagerEventPublisher(t *testing.T) {

	builder := fake.NewClientBuilder()
	c := builder.Build()

	cm := enterpriseApi.ClusterManager{}
	k8sevent, err := NewK8EventPublisher(c, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	ctx := context.TODO()
	k8sevent.Normal(ctx, "testing", "normal message")
	k8sevent.Warning(ctx, "testing", "warning message")

	cmaster := enterpriseApiV3.ClusterMaster{}
	k8sevent.instance = &cmaster
	k8sevent.Normal(ctx, "", "")
}

func TestIndexerClusterEventPublisher(t *testing.T) {

	builder := fake.NewClientBuilder()
	c := builder.Build()

	cm := enterpriseApi.IndexerCluster{}
	k8sevent, err := NewK8EventPublisher(c, &cm)
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
	k8sevent, err := NewK8EventPublisher(c, &cm)
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
	k8sevent, err := NewK8EventPublisher(c, &cm)
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
	k8sevent, err := NewK8EventPublisher(c, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	k8sevent.Normal(context.TODO(), "testing", "normal message")
	k8sevent.Warning(context.TODO(), "testing", "warning message")

	// Negative testing
	ctx := context.TODO()
	k8sevent.client = nil
	k8sevent.publishEvent(ctx, "", "", "")

	mockClient := spltest.NewMockClient()
	mockClient.InduceErrorKind[splcommon.MockClientInduceErrorCreate] = errors.New(splcommon.Rerr)
	k8sevent.client = mockClient
	k8sevent.publishEvent(ctx, "", "", "")

	k8sevent.instance = "randomString"
	k8sevent.publishEvent(ctx, "", "", "")
}

func TestLicenseManagerEventPublisher(t *testing.T) {

	builder := fake.NewClientBuilder()
	c := builder.Build()

	lmanager := enterpriseApi.LicenseManager{}
	k8sevent, err := NewK8EventPublisher(c, &lmanager)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	ctx := context.TODO()
	k8sevent.Normal(ctx, "testing", "normal message")
	k8sevent.Warning(ctx, "testing", "warning message")

	lmaster := enterpriseApiV3.LicenseMaster{}
	k8sevent.instance = &lmaster
	k8sevent.Normal(ctx, "", "")
}
