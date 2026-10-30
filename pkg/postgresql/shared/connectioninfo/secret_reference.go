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
	"fmt"
	"strings"
)

// FormatSecretReference encodes a validated Secret name and data key.
func FormatSecretReference(name, key string) string { return name + "/" + key }

// ParseSecretReference decodes the name/key contract published in connection metadata.
func ParseSecretReference(value string) (name, key string, err error) {
	name, key, ok := strings.Cut(value, "/")
	if !ok || name == "" || key == "" || strings.Contains(key, "/") {
		return "", "", fmt.Errorf("invalid Secret reference %q: expected secret-name/key", value)
	}
	return name, key, nil
}
