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

package indexercluster

import (
	"strings"
)

// imageUpdatedTo9 reports whether an image changed from an 8.x version to a 9.x version.
func imageUpdatedTo9(previousImage string, currentImage string) bool {
	if !strings.Contains(previousImage, ":") || !strings.Contains(currentImage, ":") {
		return false
	}
	previousVersion := strings.Split(previousImage, ":")[1]
	currentVersion := strings.Split(currentImage, ":")[1]
	return strings.HasPrefix(previousVersion, "8") && strings.HasPrefix(currentVersion, "9")
}
