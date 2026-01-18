package spec

import "strings"

// TestSpec describes a single E2E test case.
type TestSpec struct {
	APIVersion string        `json:"apiVersion" yaml:"apiVersion"`
	Kind       string        `json:"kind" yaml:"kind"`
	Metadata   Metadata      `json:"metadata" yaml:"metadata"`
	Topology   Topology      `json:"topology" yaml:"topology"`
	Datasets   []DatasetRef  `json:"datasets,omitempty" yaml:"datasets,omitempty"`
	Steps      []StepSpec    `json:"steps" yaml:"steps"`
	Assertions []AssertSpec  `json:"assertions,omitempty" yaml:"assertions,omitempty"`
	Requires   []string      `json:"requires,omitempty" yaml:"requires,omitempty"`
	Timeout    string        `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Variants   []VariantSpec `json:"variants,omitempty" yaml:"variants,omitempty"`
}

// Metadata captures human-readable test metadata.
type Metadata struct {
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Owner       string   `json:"owner,omitempty" yaml:"owner,omitempty"`
	Component   string   `json:"component,omitempty" yaml:"component,omitempty"`
	Tags        []string `json:"tags,omitempty" yaml:"tags,omitempty"`
}

// Topology defines the Splunk deployment layout.
type Topology struct {
	Kind   string            `json:"kind" yaml:"kind"`
	Params map[string]string `json:"params,omitempty" yaml:"params,omitempty"`
}

// DatasetRef refers to a dataset in the registry.
type DatasetRef struct {
	Name  string            `json:"name" yaml:"name"`
	Index string            `json:"index,omitempty" yaml:"index,omitempty"`
	With  map[string]string `json:"with,omitempty" yaml:"with,omitempty"`
}

// StepSpec defines a test step.
type StepSpec struct {
	Name   string                 `json:"name" yaml:"name"`
	Action string                 `json:"action" yaml:"action"`
	With   map[string]interface{} `json:"with,omitempty" yaml:"with,omitempty"`
}

// AssertSpec defines a test assertion.
type AssertSpec struct {
	Name string                 `json:"name" yaml:"name"`
	Type string                 `json:"type" yaml:"type"`
	With map[string]interface{} `json:"with,omitempty" yaml:"with,omitempty"`
}

// VariantSpec defines a test variant derived from a base spec.
type VariantSpec struct {
	Name          string            `json:"name,omitempty" yaml:"name,omitempty"`
	NameSuffix    string            `json:"name_suffix,omitempty" yaml:"name_suffix,omitempty"`
	Tags          []string          `json:"tags,omitempty" yaml:"tags,omitempty"`
	Params        map[string]string `json:"params,omitempty" yaml:"params,omitempty"`
	StepOverrides []StepOverride    `json:"step_overrides,omitempty" yaml:"step_overrides,omitempty"`
}

// StepOverride updates a single step in a variant.
type StepOverride struct {
	Name    string                 `json:"name" yaml:"name"`
	Action  string                 `json:"action,omitempty" yaml:"action,omitempty"`
	With    map[string]interface{} `json:"with,omitempty" yaml:"with,omitempty"`
	Replace bool                   `json:"replace,omitempty" yaml:"replace,omitempty"`
}

// MatchesTags returns true if the test is allowed by include/exclude tags.
func (s TestSpec) MatchesTags(include []string, exclude []string) bool {
	if len(include) == 0 && len(exclude) == 0 {
		return true
	}
	for _, tag := range exclude {
		for _, existing := range s.Metadata.Tags {
			if strings.EqualFold(tag, existing) {
				return false
			}
		}
	}
	if len(include) == 0 {
		return true
	}
	for _, tag := range include {
		for _, existing := range s.Metadata.Tags {
			if strings.EqualFold(tag, existing) {
				return true
			}
		}
	}
	return false
}
