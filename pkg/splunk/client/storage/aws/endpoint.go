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

// Package aws provides AWS S3 endpoint resolution.
package aws

import (
	"context"
	"fmt"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

var validRegion = regexp.MustCompile(`^[a-z][a-z0-9-]+$`)

// ResolveKMSEndpoint returns the regional KMS endpoint URL for the given AWS region.
func ResolveKMSEndpoint(ctx context.Context, region string) (string, error) {
	if !validRegion.MatchString(region) {
		return "", fmt.Errorf("invalid AWS region %q", region)
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return "", err
	}

	client := kms.NewFromConfig(cfg)
	params := kms.EndpointParameters{Region: &region}

	ep, err := client.Options().EndpointResolverV2.ResolveEndpoint(ctx, params)
	if err != nil {
		return "", err
	}
	return ep.URI.String(), nil
}

// ResolveS3Endpoint returns the regional S3 endpoint URL for the given AWS region.
func ResolveS3Endpoint(ctx context.Context, region string) (string, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return "", err
	}

	client := s3.NewFromConfig(cfg)
	params := s3.EndpointParameters{Region: &region}

	ep, err := client.Options().EndpointResolverV2.ResolveEndpoint(ctx, params)
	if err != nil {
		return "", err
	}
	return ep.URI.String(), nil
}
