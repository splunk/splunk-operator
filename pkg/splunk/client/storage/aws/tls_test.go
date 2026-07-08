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

package aws

import (
	"crypto/tls"
	"net/http"
	"testing"
)

func TestTLSVersionString(t *testing.T) {
	cases := []struct {
		version uint16
		want    string
	}{
		{tls.VersionTLS10, "TLS 1.0"},
		{tls.VersionTLS11, "TLS 1.1"},
		{tls.VersionTLS12, "TLS 1.2"},
		{tls.VersionTLS13, "TLS 1.3"},
		{14, "Unknown"},
	}
	for _, tc := range cases {
		tr := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tc.version}}
		if got := getTLSVersion(tr); got != tc.want {
			t.Errorf("getTLSVersion(0x%x) = %q, want %q", tc.version, got, tc.want)
		}
	}
}
