/*
Copyright 2026.

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
package connectioninfo

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"slices"
	"strconv"
)

const (
	// AnnotationSourceRestoreFence invalidates a delayed commit after blue restoration.
	AnnotationSourceRestoreFence = "platform.splunk.com/endpoint-source-restore-fence"
	// These annotations make the canonical cluster ConfigMap the observable
	// endpoint commit record. Database ConfigMaps also use the intent markers to
	// reject an older publication after a newer one has been written.
	AnnotationActiveEnvironment        = "platform.splunk.com/active-environment"
	AnnotationEndpointAttemptID        = "platform.splunk.com/endpoint-attempt-id"
	AnnotationEndpointIntentGeneration = "platform.splunk.com/endpoint-intent-generation"
	AnnotationEndpointFingerprint      = "platform.splunk.com/endpoint-fingerprint"
)

var endpointKeys = []string{
	KeyClusterRWEndpoint,
	KeyClusterROEndpoint,
	KeyClusterREndpoint,
	KeyDefaultClusterPort,
	KeyPoolerRWEndpoint,
	KeyPoolerROEndpoint,
}

// EndpointKeys lists the routing keys in a stable order. The database identity
// and credential keys are intentionally excluded from endpoint comparisons.
func EndpointKeys() []string {
	return slices.Clone(endpointKeys)
}

// RoutingData builds the shared routing payload without database-specific keys.
func RoutingData(endpoints Endpoints) (map[string]string, error) {
	data, _, err := BuildConfigMapData(endpoints)
	return data, err
}

// FingerprintEndpointData hashes key presence and values in a stable order.
// Missing optional keys and present-but-empty keys have different fingerprints.
func FingerprintEndpointData(data map[string]string) string {
	h := sha256.New()
	for _, key := range endpointKeys {
		writePart(h, key)
		value, present := data[key]
		if present {
			_, _ = h.Write([]byte{1})
			writePart(h, value)
		} else {
			_, _ = h.Write([]byte{0})
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// EndpointDataMatches compares the complete routing payload while allowing
// database-specific identity keys in actual ConfigMap data.
func EndpointDataMatches(actual, expected map[string]string) bool {
	for _, key := range endpointKeys {
		actualValue, actualPresent := actual[key]
		expectedValue, expectedPresent := expected[key]
		if actualPresent != expectedPresent || actualValue != expectedValue {
			return false
		}
	}
	return true
}

func writePart(h hash.Hash, value string) {
	_, _ = h.Write([]byte(strconv.Itoa(len(value))))
	_, _ = h.Write([]byte{':'})
	_, _ = h.Write([]byte(value))
}
