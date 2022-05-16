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
	"fmt"
	"os"
	"testing"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

func TestGetSplunkDeploymentName(t *testing.T) {
	got := GetSplunkDeploymentName(SplunkClusterManager, "t1")
	want := fmt.Sprintf(splcommon.TestClusterManager, "t1")
	if got != want {
		t.Errorf("GetSplunkDeploymentName(\"%s\",\"%s\") = %s; want %s", SplunkIndexer.ToString(), "t1", got, want)
	}
}

func TestGetSplunkStatefulsetName(t *testing.T) {
	got := GetSplunkStatefulsetName(SplunkIndexer, "t2")
	want := "splunk-t2-indexer"
	if got != want {
		t.Errorf("GetSplunkStatefulsetName(\"%s\",\"%s\") = %s; want %s", SplunkIndexer.ToString(), "t2", got, want)
	}
}

func TestGetSplunkStatefulsetPodName(t *testing.T) {
	got := GetSplunkStatefulsetPodName(SplunkSearchHead, "t3", 2)
	want := "splunk-t3-search-head-2"
	if got != want {
		t.Errorf("GetSplunkStatefulsetPodName(\"%s\",\"%s\",%d) = %s; want %s", SplunkIndexer.ToString(), "t3", 2, got, want)
	}
}

func TestGetSplunkServiceName(t *testing.T) {
	test := func(want string, instanceType InstanceType, identifier string, isHeadless bool) {
		got := GetSplunkServiceName(instanceType, identifier, isHeadless)
		if got != want {
			t.Errorf("GetSplunkServiceName(\"%s\",\"%s\",%t) = %s; want %s",
				instanceType.ToString(), identifier, isHeadless, got, want)
		}
	}

	test("splunk-t1-deployer-headless", SplunkDeployer, "t1", true)
	test("splunk-t2-search-head-service", SplunkSearchHead, "t2", false)
	test("splunk-t3-license-manager-service", SplunkLicenseManager, "t3", false)

	LicenseManagerRefName := "stack1"
	test("splunk-stack1-license-manager-service", SplunkLicenseManager, LicenseManagerRefName, false)
}

func TestGetSplunkDefaultsName(t *testing.T) {
	got := GetSplunkDefaultsName("t1", SplunkSearchHead)
	want := "splunk-t1-search-head-defaults"
	if got != want {
		t.Errorf("GetSplunkDefaultsName(\"%s\",\"%s\") = %s; want %s", "t1", SplunkSearchHead, got, want)
	}
}

func TestGetSplunkMonitoringconsoleConfigMapName(t *testing.T) {
	got := GetSplunkMonitoringconsoleConfigMapName("t1", SplunkMonitoringConsole)
	want := "splunk-t1-monitoring-console"
	if got != want {
		t.Errorf("GetSplunkMonitoringconsoleConfigMapName(\"%s\",\"%s\") = %s; want %s", "t1", SplunkMonitoringConsole, got, want)
	}
}

func TestGetSplunkStatefulsetUrls(t *testing.T) {
	test := func(want string, namespace string, instanceType InstanceType, identifier string, replicas int32, hostnameOnly bool) {
		got := GetSplunkStatefulsetUrls(namespace, instanceType, identifier, replicas, hostnameOnly)
		if got != want {
			t.Errorf("GetSplunkStatefulsetUrls(\"%s\",\"%s\",\"%s\",%d,%t) = %s; want %s",
				namespace, instanceType.ToString(), identifier, replicas, hostnameOnly, got, want)
		}
	}

	test("splunk-t1-search-head-0,splunk-t1-search-head-1,splunk-t1-search-head-2",
		"splunktest", SplunkSearchHead, "t1", 3, true)
	test("splunk-t2-indexer-0.splunk-t2-indexer-headless.splunktest.svc.cluster.local,splunk-t2-indexer-1.splunk-t2-indexer-headless.splunktest.svc.cluster.local,splunk-t2-indexer-2.splunk-t2-indexer-headless.splunktest.svc.cluster.local,splunk-t2-indexer-3.splunk-t2-indexer-headless.splunktest.svc.cluster.local",
		"splunktest", SplunkIndexer, "t2", 4, false)
}

func TestGetSplunkImage(t *testing.T) {
	var specImage string

	test := func(want string) {
		got := GetSplunkImage(specImage)
		if got != want {
			t.Errorf("GetSplunkImage() = %s; want %s", got, want)
		}
	}

	test("splunk/splunk")

	os.Setenv("RELATED_IMAGE_SPLUNK_ENTERPRISE", "splunk-test/splunk")
	test("splunk-test/splunk")
	os.Setenv("RELATED_IMAGE_SPLUNK_ENTERPRISE", "splunk/splunk")

	specImage = "splunk/splunk-test"
	test("splunk/splunk-test")
}
