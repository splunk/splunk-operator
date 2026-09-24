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

package aws_test

import (
	"context"
	"strings"
	"testing"

	storageaws "github.com/splunk/splunk-operator/pkg/splunk/client/storage/aws"
)

func TestResolveS3Endpoint(t *testing.T) {
	ctx := context.TODO()
	cases := []struct {
		name    string
		region  string
		want    string
		wantErr bool
	}{
		{"us-west-2", "us-west-2", "https://s3.us-west-2.amazonaws.com", false},
		{"us-east-1", "us-east-1", "https://s3.us-east-1.amazonaws.com", false},
		{"eu-central-1", "eu-central-1", "https://s3.eu-central-1.amazonaws.com", false},
		{"ap-southeast-1", "ap-southeast-1", "https://s3.ap-southeast-1.amazonaws.com", false},
		{"empty region", "", "", true},
		{"invalid chars", "invalid_region!", "", true},
		{"space in region", "gov cloud", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := storageaws.ResolveS3Endpoint(ctx, tc.region)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ResolveS3Endpoint(%q) = %q, want an error", tc.region, got)
				}
				if got != "" {
					t.Errorf("ResolveS3Endpoint(%q) returned endpoint %q on error, want empty string", tc.region, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ResolveS3Endpoint(%q) returned unexpected error: %v", tc.region, err)
				return
			}
			if got != tc.want {
				t.Errorf("ResolveS3Endpoint(%q) = %q, want %q", tc.region, got, tc.want)
			}
		})
	}
}

// TestResolveS3EndpointUsesRegionParam verifies the endpoint is derived from the
// region argument passed to the function, not from ambient AWS configuration.
func TestResolveS3EndpointUsesRegionParam(t *testing.T) {
	ctx := context.TODO()
	const region = "ca-central-1"
	got, err := storageaws.ResolveS3Endpoint(ctx, region)
	if err != nil {
		t.Fatalf("ResolveS3Endpoint(%q) returned unexpected error: %v", region, err)
	}
	if !strings.Contains(got, region) {
		t.Errorf("ResolveS3Endpoint(%q) = %q, expected it to contain the region", region, got)
	}
}

func TestResolveKMSEndpoint(t *testing.T) {
	ctx := context.TODO()
	cases := []struct {
		name    string
		region  string
		want    string
		wantErr bool
	}{
		{"us-west-2", "us-west-2", "https://kms.us-west-2.amazonaws.com", false},
		{"us-east-1", "us-east-1", "https://kms.us-east-1.amazonaws.com", false},
		{"eu-central-1", "eu-central-1", "https://kms.eu-central-1.amazonaws.com", false},
		{"empty region", "", "", true},
		{"invalid chars", "invalid_region!", "", true},
		{"space in region", "gov cloud", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := storageaws.ResolveKMSEndpoint(ctx, tc.region)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ResolveKMSEndpoint(%q) = %q, want an error", tc.region, got)
				}
				if got != "" {
					t.Errorf("ResolveKMSEndpoint(%q) returned endpoint %q on error, want empty string", tc.region, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ResolveKMSEndpoint(%q) returned unexpected error: %v", tc.region, err)
				return
			}
			if got != tc.want {
				t.Errorf("ResolveKMSEndpoint(%q) = %q, want %q", tc.region, got, tc.want)
			}
		})
	}
}

// TestResolveKMSEndpointUsesRegionParam verifies the endpoint is derived from the
// region argument passed to the function, not from ambient AWS configuration.
func TestResolveKMSEndpointUsesRegionParam(t *testing.T) {
	ctx := context.TODO()
	const region = "ca-central-1"
	got, err := storageaws.ResolveKMSEndpoint(ctx, region)
	if err != nil {
		t.Fatalf("ResolveKMSEndpoint(%q) returned unexpected error: %v", region, err)
	}
	if !strings.Contains(got, region) {
		t.Errorf("ResolveKMSEndpoint(%q) = %q, expected it to contain the region", region, got)
	}
}
