// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
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

package spark

import (
	"os"
	"testing"
)

func TestGetSparkStatefulsetName(t *testing.T) {
	got := GetSparkStatefulsetName(SparkWorker, "s1")
	want := "splunk-s1-spark-worker"
	if got != want {
		t.Errorf("GetSparkStatefulsetName(\"%s\",\"%s\") = %s; want %s", SparkWorker.ToString(), "s1", got, want)
	}
}

func TestGetSparkDeploymentName(t *testing.T) {
	got := GetSparkDeploymentName(SparkMaster, "s1")
	want := "splunk-s1-spark-master"
	if got != want {
		t.Errorf("GetSplunkDeploymentName(\"%s\",\"%s\") = %s; want %s", SparkMaster.ToString(), "s1", got, want)
	}
}

func TestGetSparkServiceName(t *testing.T) {
	test := func(want string, instanceType InstanceType, identifier string, isHeadless bool) {
		got := GetSparkServiceName(instanceType, identifier, isHeadless)
		if got != want {
			t.Errorf("GetSparkServiceName(\"%s\",\"%s\",%t) = %s; want %s",
				instanceType.ToString(), identifier, isHeadless, got, want)
		}
	}

	test("splunk-s1-spark-master-headless", SparkMaster, "s1", true)
	test("splunk-s2-spark-master-service", SparkMaster, "s2", false)
}

func TestGetSparkImage(t *testing.T) {
	var specImage string

	test := func(want string) {
		got := GetSparkImage(specImage)
		if got != want {
			t.Errorf("GetSparkImage() = %s; want %s", got, want)
		}
	}

	test("splunk/spark")

	os.Setenv("RELATED_IMAGE_SPLUNK_SPARK", "splunk-test/spark")
	test("splunk-test/spark")

	specImage = "splunk/spark-test"
	test("splunk/spark-test")
}
