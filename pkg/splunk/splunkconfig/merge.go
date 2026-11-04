// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package splunkconfig

import (
	"fmt"
	"maps"

	"github.com/splunk/splunk-operator/pkg/splunk/common"
)

// MergeConfEntries combines stanzas targeting the same directory and file.
// Inputs are copied so merging cannot mutate features reused by another consumer.
// Conflicting field values are rejected rather than silently overwritten.
func MergeConfEntries(entries ...common.ConfFileEntry) ([]common.ConfFileEntry, error) {
	var merged []common.ConfFileEntry
	targets := make(map[confTarget]int)
	for _, entry := range entries {
		target := confTarget{directory: entry.Value.Directory, confFileName: entry.ConfFileName}
		index, found := targets[target]
		if !found {
			index = len(merged)
			targets[target] = index
			merged = append(merged, common.ConfFileEntry{
				ConfFileName: entry.ConfFileName,
				Value:        common.ConfFileValue{Directory: entry.Value.Directory, Stanzas: make(common.ConfFileStanzas)},
			})
		}
		stanzas := merged[index].Value.Stanzas
		for name, fields := range entry.Value.Stanzas {
			existing, found := stanzas[name]
			if !found || existing == nil {
				stanzas[name] = maps.Clone(fields)
				continue
			}
			for field, value := range fields {
				if previous, found := existing[field]; found && previous != value {
					return nil, fmt.Errorf("conflicting field %s in [%s] for %s.conf in %q", field, name, entry.ConfFileName, entry.Value.Directory)
				}
				existing[field] = value
			}
		}
	}
	return merged, nil
}
