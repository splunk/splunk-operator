// Copyright (c) 2018-2019 Splunk Inc. All rights reserved.
// Use of this source code is governed by an Apache 2 style
// license that can be found in the LICENSE file.

package resources

// SplunkServiceType is used to represent the type of Kubernetes service (service or headless).
type SplunkServiceType string

const SERVICE SplunkServiceType = "service"
const HEADLESS_SERVICE SplunkServiceType = "headless"

func (s SplunkServiceType) ToString() string {
	return string(s)
}
