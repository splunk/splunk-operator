/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import "testing"

func TestResolvePprofBindAddress(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		bindAddress string
		want        string
	}{
		{
			name:        "disabled",
			enabled:     false,
			bindAddress: defaultPprofBindAddress,
			want:        disabledPprofBindAddress,
		},
		{
			name:        "enabled",
			enabled:     true,
			bindAddress: "127.0.0.1:16060",
			want:        "127.0.0.1:16060",
		},
		{
			name:        "enabled but explicitly disabled",
			enabled:     true,
			bindAddress: disabledPprofBindAddress,
			want:        disabledPprofBindAddress,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolvePprofBindAddress(tt.enabled, tt.bindAddress); got != tt.want {
				t.Errorf("resolvePprofBindAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}
