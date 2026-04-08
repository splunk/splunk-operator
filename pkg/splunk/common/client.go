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

package common

import "sigs.k8s.io/controller-runtime/pkg/client"

// APIReaderProvider exposes a direct API reader alongside a controller client.
type APIReaderProvider interface {
	GetAPIReader() client.Reader
}

// APIAwareClient keeps the standard controller client behavior while exposing a
// direct API reader for code paths that need a live read before a write.
type APIAwareClient struct {
	client.Client
	apiReader client.Reader
}

// NewAPIAwareClient returns a client wrapper that exposes the provided API reader.
func NewAPIAwareClient(baseClient client.Client, apiReader client.Reader) client.Client {
	if baseClient == nil || apiReader == nil {
		return baseClient
	}

	return &APIAwareClient{
		Client:    baseClient,
		apiReader: apiReader,
	}
}

// GetAPIReader returns the live API reader associated with the wrapped client.
func (c *APIAwareClient) GetAPIReader() client.Reader {
	if c.apiReader != nil {
		return c.apiReader
	}

	return c.Client
}

// ResolveAPIReader returns a live API reader when one is available, otherwise
// it falls back to the provided client.
func ResolveAPIReader(baseClient client.Client) client.Reader {
	if provider, ok := baseClient.(APIReaderProvider); ok {
		if apiReader := provider.GetAPIReader(); apiReader != nil {
			return apiReader
		}
	}

	return baseClient
}
