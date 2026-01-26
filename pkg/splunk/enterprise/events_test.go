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
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"k8s.io/client-go/tools/record"
)

func TestClusterManagerEventPublisher(t *testing.T) {
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
}

func TestIndexerClusterEventPublisher(t *testing.T) {
	recorder := record.NewFakeRecorder(10)

	cm := enterpriseApi.IndexerCluster{}
	k8sevent, err := newK8EventPublisher(recorder, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	k8sevent.Normal(context.TODO(), "testing", "normal message")
	k8sevent.Warning(context.TODO(), "testing", "warning message")
}

func TestMonitoringConsoleEventPublisher(t *testing.T) {
	recorder := record.NewFakeRecorder(10)

	cm := enterpriseApi.MonitoringConsole{}
	k8sevent, err := newK8EventPublisher(recorder, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	k8sevent.Normal(context.TODO(), "testing", "normal message")
	k8sevent.Warning(context.TODO(), "testing", "warning message")
}

func TestSearchHeadClusterEventPublisher(t *testing.T) {
	recorder := record.NewFakeRecorder(10)

	cm := enterpriseApi.SearchHeadCluster{}
	k8sevent, err := newK8EventPublisher(recorder, &cm)
	if err != nil {
		t.Errorf("Unexpected error while creating new event publisher %v", err)
	}

	k8sevent.Normal(context.TODO(), "testing", "normal message")
	k8sevent.Warning(context.TODO(), "testing", "warning message")
}

func TestStandaloneEventPublisher(t *testing.T) {
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
}

func TestLicenseManagerEventPublisher(t *testing.T) {
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

}

func TestGetEventPublisher(t *testing.T) {
	recorder := record.NewFakeRecorder(10)
	cm := &enterpriseApi.ClusterManager{}

	// Test 1: GetEventPublisher with recorder in context
	ctx := context.WithValue(context.TODO(), splcommon.EventRecorderKey, recorder)
	eventPublisher := GetEventPublisher(ctx, cm)
	if eventPublisher == nil {
		t.Error("Expected non-nil event publisher")
	}

	// Test 2: GetEventPublisher with existing publisher in context
	ctx = context.WithValue(context.TODO(), splcommon.EventPublisherKey, eventPublisher)
	eventPublisher2 := GetEventPublisher(ctx, cm)
	if eventPublisher2 != eventPublisher {
		t.Error("Expected to get same event publisher from context")
	}

	// Test 3: GetEventPublisher with no recorder in context
	ctx = context.TODO()
	eventPublisher3 := GetEventPublisher(ctx, cm)
	if eventPublisher3 == nil {
		t.Error("Expected non-nil event publisher even without recorder")
	}

	// Test 4: Verify publisher works (no panic)
	eventPublisher.Normal(context.TODO(), "TestReason", "Test message")
	eventPublisher.Warning(context.TODO(), "TestReason", "Test warning")
}
