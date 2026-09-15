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

package common

import "testing"

func TestValidateProbeValues(t *testing.T) {
	tests := []struct {
		name    string
		values  [4]int32
		wantErr bool
	}{
		{name: "zero values are allowed", values: [4]int32{}},
		{name: "positive values are allowed", values: [4]int32{30, 5, 10, 3}},
		{name: "negative initial delay", values: [4]int32{-1, 1, 1, 1}, wantErr: true},
		{name: "negative timeout", values: [4]int32{1, -1, 1, 1}, wantErr: true},
		{name: "negative period", values: [4]int32{1, 1, -1, 1}, wantErr: true},
		{name: "negative failure threshold", values: [4]int32{1, 1, 1, -1}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProbeValues(tt.values[0], tt.values[1], tt.values[2], tt.values[3])
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateProbe() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}

func TestValidateProbe(t *testing.T) {
	err := ValidateProbe("Liveness", -1, 1, 1, 1)
	if err == nil || err.Error() != "invalid Liveness Probe config. Reason: negative values are not allowed. Configured values InitialDelaySeconds = -1, TimeoutSeconds = 1, PeriodSeconds = 1, FailureThreshold = 1" {
		t.Errorf("ValidateProbe() error = %v", err)
	}
}
