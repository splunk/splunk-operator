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
	"context"
	"strings"
	"testing"
)

func TestResolveSQSEndpoint(t *testing.T) {
	ctx := context.TODO()

	tests := []struct {
		name           string
		region         string
		wantEndpoint   string
		wantErrContain string
	}{
		{
			name:         "valid us-east-1 region",
			region:       "us-east-1",
			wantEndpoint: "https://sqs.us-east-1.amazonaws.com",
		},
		{
			name:         "valid eu-west-1 region",
			region:       "eu-west-1",
			wantEndpoint: "https://sqs.eu-west-1.amazonaws.com",
		},
		{
			name:         "valid ap-southeast-1 region",
			region:       "ap-southeast-1",
			wantEndpoint: "https://sqs.ap-southeast-1.amazonaws.com",
		},
		{
			name:         "valid cn-north-1 region (China)",
			region:       "cn-north-1",
			wantEndpoint: "https://sqs.cn-north-1.amazonaws.com.cn",
		},
		{
			name:         "valid cn-northwest-1 region (China Ningxia)",
			region:       "cn-northwest-1",
			wantEndpoint: "https://sqs.cn-northwest-1.amazonaws.com.cn",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint, err := ResolveSQSEndpoint(ctx, tt.region)
			if tt.wantErrContain != "" {
				if err == nil {
					t.Errorf("ResolveSQSEndpoint() expected error containing %q, got nil", tt.wantErrContain)
				} else if !strings.Contains(err.Error(), tt.wantErrContain) {
					t.Errorf("ResolveSQSEndpoint() error = %v, want error containing %q", err, tt.wantErrContain)
				}
				return
			}
			if err != nil {
				t.Errorf("ResolveSQSEndpoint() unexpected error = %v", err)
				return
			}
			if endpoint != tt.wantEndpoint {
				t.Errorf("ResolveSQSEndpoint() = %v, want %v", endpoint, tt.wantEndpoint)
			}
		})
	}
}
