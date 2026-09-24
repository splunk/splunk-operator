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

// Package aws provides AWS SQS endpoint resolution.
package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// ResolveSQSEndpoint returns the regional SQS endpoint URL for the given AWS region.
func ResolveSQSEndpoint(ctx context.Context, region string) (string, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return "", err
	}

	client := sqs.NewFromConfig(cfg)
	params := sqs.EndpointParameters{Region: &region}

	ep, err := client.Options().EndpointResolverV2.ResolveEndpoint(ctx, params)
	if err != nil {
		return "", err
	}
	return ep.URI.String(), nil
}
