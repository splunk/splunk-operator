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

import "fmt"

// ValidateProbeValues validates runtime probe timing values shared by Splunk
// CRs. Zero values are allowed because the reconciler supplies Kubernetes
// defaults; explicitly negative values are invalid.
func ValidateProbeValues(initialDelaySeconds, timeoutSeconds, periodSeconds, failureThreshold int32) error {
	if initialDelaySeconds < 0 || timeoutSeconds < 0 || periodSeconds < 0 || failureThreshold < 0 {
		return fmt.Errorf("negative values are not allowed. Configured values InitialDelaySeconds = %d, TimeoutSeconds = %d, PeriodSeconds = %d, FailureThreshold = %d", initialDelaySeconds, timeoutSeconds, periodSeconds, failureThreshold)
	}
	return nil
}

// ValidateProbe validates one named runtime probe and preserves the error
// wording used by the reconcile paths.
func ValidateProbe(name string, initialDelaySeconds, timeoutSeconds, periodSeconds, failureThreshold int32) error {
	if err := ValidateProbeValues(initialDelaySeconds, timeoutSeconds, periodSeconds, failureThreshold); err != nil {
		return fmt.Errorf("invalid %s Probe config. Reason: %s", name, err)
	}
	return nil
}
