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

package monitoring

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// ConfigMap content is excluded because the cluster observes that mutable dependency.
func ContributionRevision(databaseName string, exists bool, selectors []QuerySelector) string {
	var value strings.Builder
	appendRevisionPart(&value, databaseName)
	appendRevisionPart(&value, strconv.FormatBool(exists))
	for _, selector := range selectors {
		appendRevisionPart(&value, selector.ConfigMapName)
		appendRevisionPart(&value, selector.ConfigMapKey)
	}
	sum := sha256.Sum256([]byte(value.String()))
	return hex.EncodeToString(sum[:])
}

func appendRevisionPart(value *strings.Builder, part string) {
	value.WriteString(strconv.Itoa(len(part)))
	value.WriteByte(':')
	value.WriteString(part)
	value.WriteByte(';')
}
