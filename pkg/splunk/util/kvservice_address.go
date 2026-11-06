// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/splunk/splunk-operator/pkg/splunk/common"
	k8svalidation "k8s.io/apimachinery/pkg/util/validation"
)

// KVServiceAddress identifies the managed backend and its client connection URL.
// The embedded URL preserves the supplied HTTP/HTTPS scheme and optional root slash.
type KVServiceAddress struct {
	*url.URL
	KVServiceName string
	Namespace     string
}

// ParseKVServiceAddress applies the same short Service DNS contract to client
// configuration, tenant discovery and Secret routing. Bare addresses use HTTP.
// Only the operator-generated Service name and port are supported; FQDNs, IPs,
// aliases, credentials, queries, fragments and non-root paths are rejected.
func ParseKVServiceAddress(address string) (*KVServiceAddress, error) {
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	endpoint, err := url.Parse(address)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") ||
		endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || strings.Contains(address, "#") ||
		(endpoint.Path != "" && endpoint.Path != "/") || endpoint.RawPath != "" {
		// Never include the supplied URL or a url.Parse error: either can contain credentials.
		return nil, fmt.Errorf("KVService address must be an HTTP or HTTPS endpoint without credentials, query, fragment, or non-root path")
	}
	service, namespace, found := strings.Cut(endpoint.Hostname(), ".")
	name, valid := KVServiceNameFromInstance(strings.TrimSuffix(service, "-service"))
	if !found || !valid || len(k8svalidation.IsDNS1123Label(name)) > 0 || len(k8svalidation.IsDNS1035Label(service)) > 0 ||
		len(k8svalidation.IsDNS1123Label(namespace)) > 0 || endpoint.Host != KVServiceBackendAddress(name, namespace) {
		return nil, fmt.Errorf("KVService address must use short Service DNS splunk-<kvservice-name>-kvservice-service.<namespace>:%d, optionally prefixed with http:// or https://; full DNS names, IP addresses, aliases, and other ports are not supported", common.KVServicePort)
	}
	return &KVServiceAddress{URL: endpoint, KVServiceName: name, Namespace: namespace}, nil
}
