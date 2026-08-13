// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package certmanager

import (
	"reflect"
	"testing"
)

func TestWildcardHeadlessSAN(t *testing.T) {
	got := WildcardHeadlessSAN("splunk-shc1-search-head-headless.ns.svc.cluster.local")
	want := "*.splunk-shc1-search-head-headless.ns.svc.cluster.local"
	if got != want {
		t.Errorf("WildcardHeadlessSAN() = %q, want %q", got, want)
	}
}

func TestDeriveDNSNames_ServiceOnly(t *testing.T) {
	got := DeriveDNSNames([]string{"svc.ns.svc.cluster.local"}, "", nil)
	want := []string{"svc.ns.svc.cluster.local"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DeriveDNSNames() = %v, want %v", got, want)
	}
}

func TestDeriveDNSNames_WithHeadlessWildcard(t *testing.T) {
	got := DeriveDNSNames(
		[]string{"svc.ns.svc.cluster.local"},
		"headless.ns.svc.cluster.local",
		nil,
	)
	want := []string{"svc.ns.svc.cluster.local", "*.headless.ns.svc.cluster.local"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DeriveDNSNames() = %v, want %v", got, want)
	}
}

func TestDeriveDNSNames_WithPodFQDNs(t *testing.T) {
	got := DeriveDNSNames(
		[]string{"svc.ns.svc.cluster.local"},
		"",
		[]string{"pod-0.headless.ns.svc.cluster.local"},
	)
	want := []string{"svc.ns.svc.cluster.local", "pod-0.headless.ns.svc.cluster.local"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DeriveDNSNames() = %v, want %v", got, want)
	}
}

func TestDeriveDNSNames_AllSources(t *testing.T) {
	got := DeriveDNSNames(
		[]string{"svc.ns.svc.cluster.local", "deployer.ns.svc.cluster.local"},
		"headless.ns.svc.cluster.local",
		[]string{"pod-0.headless.ns.svc.cluster.local"},
	)
	want := []string{
		"svc.ns.svc.cluster.local",
		"deployer.ns.svc.cluster.local",
		"*.headless.ns.svc.cluster.local",
		"pod-0.headless.ns.svc.cluster.local",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DeriveDNSNames() = %v, want %v", got, want)
	}
}

func TestDeriveDNSNames_Empty(t *testing.T) {
	got := DeriveDNSNames(nil, "", nil)
	if len(got) != 0 {
		t.Errorf("DeriveDNSNames() = %v, want empty", got)
	}
}
