// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
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
	"io"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SHCDefaultsRestartClassification describes whether an inline defaults
// change can be applied through phased Search Head replacement.
type SHCDefaultsRestartClassification struct {
	RequiresSimultaneousRestart bool
	Setting                     string
}

type inlineSHCDefaults struct {
	settings map[string]string
}

type inlineDefaultsDocument struct {
	Splunk struct {
		Conf yaml.Node `yaml:"conf"`
	} `yaml:"splunk"`
}

type inlineDefaultsConfEntry struct {
	Key   string    `yaml:"key"`
	Value yaml.Node `yaml:"value"`
}

type inlineDefaultsConfFile struct {
	Content map[string]yaml.Node `yaml:"content"`
}

// ClassifySHCDefaultsRestart compares two inline defaults documents. Splunk
// permits captain_is_adhoc_searchhead and shcluster_label to change through a
// rolling restart. A change to any other [shclustering] setting requires an
// approximately simultaneous restart and must not enter a phased rollout.
// Live-editable cluster settings are intentionally not exempted here: inline
// defaults write member-local server.conf, while the supported SHC API owns
// replicated cluster configuration.
func ClassifySHCDefaultsRestart(
	defaults string,
	previousDefaults string,
) (SHCDefaultsRestartClassification, error) {
	if defaults == previousDefaults {
		return SHCDefaultsRestartClassification{}, nil
	}

	current, err := parseInlineSHCDefaults(defaults)
	if err != nil {
		return SHCDefaultsRestartClassification{},
			fmt.Errorf("cannot classify current inline defaults: %w", err)
	}
	previous, err := parseInlineSHCDefaults(previousDefaults)
	if err != nil {
		return SHCDefaultsRestartClassification{},
			fmt.Errorf("cannot classify previous inline defaults: %w", err)
	}

	changed := make(map[string]struct{}, len(current.settings)+len(previous.settings))
	for name, value := range current.settings {
		if oldValue, ok := previous.settings[name]; !ok || oldValue != value {
			changed[name] = struct{}{}
		}
	}
	for name := range previous.settings {
		if _, ok := current.settings[name]; !ok {
			changed[name] = struct{}{}
		}
	}
	changedNames := make([]string, 0, len(changed))
	for name := range changed {
		changedNames = append(changedNames, name)
	}
	sort.Strings(changedNames)

	for _, name := range changedNames {
		if name == "captain_is_adhoc_searchhead" || name == "shcluster_label" {
			continue
		}
		return SHCDefaultsRestartClassification{
			RequiresSimultaneousRestart: true,
			Setting:                     name,
		}, nil
	}
	return SHCDefaultsRestartClassification{}, nil
}

func parseInlineSHCDefaults(defaults string) (inlineSHCDefaults, error) {
	result := inlineSHCDefaults{settings: map[string]string{}}
	if strings.TrimSpace(defaults) == "" {
		return result, nil
	}

	decoder := yaml.NewDecoder(strings.NewReader(defaults))
	var document inlineDefaultsDocument
	if err := decoder.Decode(&document); err != nil {
		return result, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return result, fmt.Errorf("multiple YAML documents are not supported")
		}
		return result, err
	}

	conf := &document.Splunk.Conf
	if conf.Kind == 0 {
		return result, nil
	}
	var server yaml.Node
	switch conf.Kind {
	case yaml.MappingNode:
		var files map[string]yaml.Node
		if err := conf.Decode(&files); err != nil {
			return result,
				fmt.Errorf("splunk.conf must be a mapping of configuration files: %w", err)
		}
		var found bool
		server, found = files["server"]
		if !found {
			return result, nil
		}
	case yaml.SequenceNode:
		var entries []inlineDefaultsConfEntry
		if err := conf.Decode(&entries); err != nil {
			return result,
				fmt.Errorf("splunk.conf key/value sequence is invalid: %w", err)
		}
		found := false
		for index := range entries {
			if entries[index].Key == "" || entries[index].Value.Kind == 0 {
				return result,
					fmt.Errorf(
						"splunk.conf[%d] must contain key and value fields",
						index,
					)
			}
			if entries[index].Key != "server" {
				continue
			}
			if found {
				return result,
					fmt.Errorf("duplicate splunk.conf entry %q", "server")
			}
			found = true
			server = entries[index].Value
		}
		if !found {
			return result, nil
		}
	default:
		return result,
			fmt.Errorf("splunk.conf must be a mapping or a key/value sequence")
	}

	var file inlineDefaultsConfFile
	if err := server.Decode(&file); err != nil {
		return result, fmt.Errorf("splunk.conf.server must be a mapping: %w", err)
	}
	stanza, found := file.Content["shclustering"]
	if !found {
		return result, nil
	}
	stanzaNode, err := unwrapYAMLNode(&stanza)
	if err != nil {
		return result, err
	}
	if stanzaNode.Kind != yaml.MappingNode {
		return result,
			fmt.Errorf("splunk.conf.server.content.shclustering must be a mapping")
	}
	var settings map[string]yaml.Node
	if err := stanzaNode.Decode(&settings); err != nil {
		return result,
			fmt.Errorf("cannot decode splunk.conf.server.content.shclustering: %w", err)
	}
	for name, value := range settings {
		valueNode, err := unwrapYAMLNode(&value)
		if err != nil {
			return result, err
		}
		if valueNode.Kind != yaml.ScalarNode {
			return result,
				fmt.Errorf("[shclustering] setting %q must have a scalar value", name)
		}
		result.settings[name] = valueNode.Tag + "\x00" + valueNode.Value
	}
	return result, nil
}

func unwrapYAMLNode(node *yaml.Node) (*yaml.Node, error) {
	if node == nil {
		return nil, fmt.Errorf("unexpected empty YAML node")
	}
	for node.Kind == yaml.DocumentNode || node.Kind == yaml.AliasNode {
		if node.Kind == yaml.DocumentNode {
			if len(node.Content) != 1 {
				return nil, fmt.Errorf("YAML document must contain one root value")
			}
			node = node.Content[0]
			continue
		}
		if node.Alias == nil {
			return nil, fmt.Errorf("invalid YAML alias")
		}
		node = node.Alias
	}
	return node, nil
}
