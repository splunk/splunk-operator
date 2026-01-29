package matrix

import (
	"fmt"
	"strings"

	"github.com/splunk/splunk-operator/e2e/framework/spec"
)

// Matrix defines test matrix configuration
type Matrix struct {
	Name         string                   `yaml:"name" json:"name"`
	Description  string                   `yaml:"description,omitempty" json:"description,omitempty"`
	Topologies   []string                 `yaml:"topologies" json:"topologies"`
	CloudProviders []string               `yaml:"cloud_providers,omitempty" json:"cloud_providers,omitempty"`
	SplunkVersions []string               `yaml:"splunk_versions,omitempty" json:"splunk_versions,omitempty"`
	OperatorVersions []string             `yaml:"operator_versions,omitempty" json:"operator_versions,omitempty"`
	Scenarios    []Scenario               `yaml:"scenarios" json:"scenarios"`
	Constraints  []Constraint             `yaml:"constraints,omitempty" json:"constraints,omitempty"`
	Tags         []string                 `yaml:"tags,omitempty" json:"tags,omitempty"`
}

// Scenario defines a test scenario template
type Scenario struct {
	Name        string                 `yaml:"name" json:"name"`
	Description string                 `yaml:"description,omitempty" json:"description,omitempty"`
	Tags        []string               `yaml:"tags" json:"tags"`
	Requires    []string               `yaml:"requires,omitempty" json:"requires,omitempty"`
	Steps       []spec.StepSpec        `yaml:"steps" json:"steps"`
	Params      map[string]interface{} `yaml:"params,omitempty" json:"params,omitempty"`
}

// Constraint defines constraints for test combinations
type Constraint struct {
	Type      string                 `yaml:"type" json:"type"` // "exclude", "include", "require"
	Condition map[string]interface{} `yaml:"condition" json:"condition"`
	Reason    string                 `yaml:"reason,omitempty" json:"reason,omitempty"`
}

// Combination represents a single test combination
type Combination struct {
	Topology        string
	CloudProvider   string
	SplunkVersion   string
	OperatorVersion string
	Scenario        Scenario
}

// Generator generates test specs from a matrix
type Generator struct {
	matrix *Matrix
}

// NewGenerator creates a new matrix generator
func NewGenerator(matrix *Matrix) *Generator {
	return &Generator{matrix: matrix}
}

// Generate generates all test specs from the matrix
func (g *Generator) Generate() ([]spec.TestSpec, error) {
	combinations := g.generateCombinations()
	filteredCombinations := g.applyConstraints(combinations)

	var specs []spec.TestSpec
	for _, combo := range filteredCombinations {
		testSpec := g.createTestSpec(combo)
		specs = append(specs, testSpec)
	}

	return specs, nil
}

// generateCombinations generates all possible combinations
func (g *Generator) generateCombinations() []Combination {
	var combinations []Combination

	// Default values if not specified
	cloudProviders := g.matrix.CloudProviders
	if len(cloudProviders) == 0 {
		cloudProviders = []string{"kind"}
	}

	splunkVersions := g.matrix.SplunkVersions
	if len(splunkVersions) == 0 {
		splunkVersions = []string{"latest"}
	}

	operatorVersions := g.matrix.OperatorVersions
	if len(operatorVersions) == 0 {
		operatorVersions = []string{"latest"}
	}

	// Generate cartesian product of all dimensions
	for _, topology := range g.matrix.Topologies {
		for _, cloud := range cloudProviders {
			for _, splunkVer := range splunkVersions {
				for _, operatorVer := range operatorVersions {
					for _, scenario := range g.matrix.Scenarios {
						combo := Combination{
							Topology:        topology,
							CloudProvider:   cloud,
							SplunkVersion:   splunkVer,
							OperatorVersion: operatorVer,
							Scenario:        scenario,
						}
						combinations = append(combinations, combo)
					}
				}
			}
		}
	}

	return combinations
}

// applyConstraints filters combinations based on constraints
func (g *Generator) applyConstraints(combinations []Combination) []Combination {
	var filtered []Combination

	for _, combo := range combinations {
		if g.shouldInclude(combo) {
			filtered = append(filtered, combo)
		}
	}

	return filtered
}

// shouldInclude checks if a combination should be included
func (g *Generator) shouldInclude(combo Combination) bool {
	for _, constraint := range g.matrix.Constraints {
		switch constraint.Type {
		case "exclude":
			if g.matchesCondition(combo, constraint.Condition) {
				return false
			}
		case "require":
			if !g.matchesCondition(combo, constraint.Condition) {
				return false
			}
		}
	}
	return true
}

// matchesCondition checks if a combination matches a condition
func (g *Generator) matchesCondition(combo Combination, condition map[string]interface{}) bool {
	for key, value := range condition {
		var comboValue string

		switch key {
		case "topology":
			comboValue = combo.Topology
		case "cloud_provider":
			comboValue = combo.CloudProvider
		case "splunk_version":
			comboValue = combo.SplunkVersion
		case "operator_version":
			comboValue = combo.OperatorVersion
		case "scenario":
			comboValue = combo.Scenario.Name
		case "scenario_tag":
			// Check if scenario has the tag
			for _, tag := range combo.Scenario.Tags {
				if tag == value {
					return true
				}
			}
			return false
		default:
			continue
		}

		// Handle string or slice of strings for value
		switch v := value.(type) {
		case string:
			if comboValue != v {
				return false
			}
		case []interface{}:
			found := false
			for _, item := range v {
				if str, ok := item.(string); ok && comboValue == str {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		case []string:
			found := false
			for _, item := range v {
				if comboValue == item {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}

	return true
}

// createTestSpec creates a test spec from a combination
func (g *Generator) createTestSpec(combo Combination) spec.TestSpec {
	// Generate unique test name
	testName := fmt.Sprintf("%s_%s_%s_%s_%s",
		g.matrix.Name,
		combo.Scenario.Name,
		combo.Topology,
		sanitizeName(combo.CloudProvider),
		sanitizeName(combo.SplunkVersion),
	)

	// Build description
	description := fmt.Sprintf("%s on %s topology with Splunk %s on %s",
		combo.Scenario.Description,
		combo.Topology,
		combo.SplunkVersion,
		combo.CloudProvider,
	)

	// Merge tags
	tags := append([]string{}, g.matrix.Tags...)
	tags = append(tags, combo.Scenario.Tags...)
	tags = append(tags, combo.Topology)
	tags = append(tags, "matrix-generated")

	// Merge requirements
	requires := append([]string{}, combo.Scenario.Requires...)

	// Process steps with variable substitution
	steps := make([]spec.StepSpec, len(combo.Scenario.Steps))
	for i, step := range combo.Scenario.Steps {
		processedStep := g.processStep(step, combo)
		steps[i] = processedStep
	}

	// Convert params to map[string]string
	topologyParams := make(map[string]string)
	if combo.Scenario.Params != nil {
		for k, v := range combo.Scenario.Params {
			topologyParams[k] = fmt.Sprint(v)
		}
	}

	return spec.TestSpec{
		Metadata: spec.Metadata{
			Name:        testName,
			Description: description,
			Component:   g.matrix.Name,
			Tags:        tags,
		},
		Topology: spec.Topology{
			Kind:   combo.Topology,
			Params: topologyParams,
		},
		Requires: requires,
		Steps:    steps,
	}
}

// processStep processes a step with variable substitution
func (g *Generator) processStep(step spec.StepSpec, combo Combination) spec.StepSpec {
	// Create a copy of the step
	processed := step

	// Variable substitution map
	vars := map[string]string{
		"${topology}":         combo.Topology,
		"${cloud_provider}":   combo.CloudProvider,
		"${splunk_version}":   combo.SplunkVersion,
		"${operator_version}": combo.OperatorVersion,
		"${scenario}":         combo.Scenario.Name,
	}

	// Process step name
	processed.Name = replaceVars(step.Name, vars)

	// Process step.With parameters
	if processed.With == nil {
		processed.With = make(map[string]interface{})
	}

	for key, value := range step.With {
		if str, ok := value.(string); ok {
			processed.With[key] = replaceVars(str, vars)
		}
	}

	// Add combination-specific params
	if _, ok := processed.With["splunk_image"]; !ok {
		if combo.SplunkVersion != "latest" {
			processed.With["splunk_image"] = fmt.Sprintf("splunk/splunk:%s", combo.SplunkVersion)
		}
	}

	return processed
}

// replaceVars replaces variables in a string
func replaceVars(input string, vars map[string]string) string {
	result := input
	for key, value := range vars {
		result = strings.ReplaceAll(result, key, value)
	}
	return result
}

// sanitizeName sanitizes a name for use in test identifiers
func sanitizeName(name string) string {
	// Replace dots and special characters with underscores
	result := strings.ReplaceAll(name, ".", "_")
	result = strings.ReplaceAll(result, "-", "_")
	result = strings.ReplaceAll(result, "/", "_")
	result = strings.ReplaceAll(result, ":", "_")
	result = strings.ToLower(result)
	return result
}

// GenerateReport generates a summary report of the matrix
func (g *Generator) GenerateReport() string {
	combinations := g.generateCombinations()
	filteredCombinations := g.applyConstraints(combinations)

	report := fmt.Sprintf("Matrix: %s\n", g.matrix.Name)
	report += fmt.Sprintf("Description: %s\n\n", g.matrix.Description)
	report += fmt.Sprintf("Dimensions:\n")
	report += fmt.Sprintf("  Topologies: %v\n", g.matrix.Topologies)
	report += fmt.Sprintf("  Cloud Providers: %v\n", g.matrix.CloudProviders)
	report += fmt.Sprintf("  Splunk Versions: %v\n", g.matrix.SplunkVersions)
	report += fmt.Sprintf("  Scenarios: %d\n", len(g.matrix.Scenarios))
	report += fmt.Sprintf("\nTotal Combinations: %d\n", len(combinations))
	report += fmt.Sprintf("After Constraints: %d\n", len(filteredCombinations))

	// Group by topology
	byTopology := make(map[string]int)
	for _, combo := range filteredCombinations {
		byTopology[combo.Topology]++
	}

	report += fmt.Sprintf("\nTests by Topology:\n")
	for topology, count := range byTopology {
		report += fmt.Sprintf("  %s: %d tests\n", topology, count)
	}

	return report
}
