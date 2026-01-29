package graph

import (
	"fmt"
	"sort"
	"strings"

	"github.com/splunk/splunk-operator/e2e/framework/results"
	"github.com/splunk/splunk-operator/e2e/framework/spec"
)

// PlantUMLGenerator generates PlantUML diagrams from graph and test data
type PlantUMLGenerator struct {
	graph *Graph
	specs []spec.TestSpec
	run   *results.RunResult
}

// NewPlantUMLGenerator creates a new PlantUML generator
func NewPlantUMLGenerator(g *Graph, specs []spec.TestSpec, run *results.RunResult) *PlantUMLGenerator {
	return &PlantUMLGenerator{
		graph: g,
		specs: specs,
		run:   run,
	}
}

// GenerateTopologyDiagram generates a component diagram showing the topology and resources
func (p *PlantUMLGenerator) GenerateTopologyDiagram() string {
	var sb strings.Builder

	sb.WriteString("@startuml\n")
	sb.WriteString("!define COMPONENT rectangle\n")
	sb.WriteString("skinparam componentStyle rectangle\n")
	sb.WriteString("skinparam shadowing false\n\n")

	// Find all topologies used
	topologies := make(map[string]*Node)
	for _, node := range p.graph.Nodes {
		if node.Type == "topology" {
			topologies[node.ID] = &node
		}
	}

	if len(topologies) == 0 {
		sb.WriteString("note \"No topologies found\" as N1\n")
		sb.WriteString("@enduml\n")
		return sb.String()
	}

	// Group tests by topology
	testsByTopology := make(map[string][]string)
	for _, edge := range p.graph.Edges {
		if edge.Type == "USES_TOPOLOGY" {
			testNode := p.findNode(edge.From)
			if testNode != nil {
				testsByTopology[edge.To] = append(testsByTopology[edge.To], testNode.Label)
			}
		}
	}

	sb.WriteString("title Topology Architecture Overview\n\n")

	// Generate topology components
	for topoID, topoNode := range topologies {
		sb.WriteString(fmt.Sprintf("package \"Topology: %s\" as %s {\n", topoNode.Label, p.sanitizeID(topoID)))

		// Extract topology parameters
		replicas := 1
		if r, ok := topoNode.Attributes["replicas"].(int); ok {
			replicas = r
		} else if r, ok := topoNode.Attributes["replicas"].(float64); ok {
			replicas = int(r)
		}

		kind := topoNode.Label

		// Generate resource components based on topology kind
		switch kind {
		case "standalone":
			sb.WriteString(fmt.Sprintf("  COMPONENT \"Standalone Instance\\n(replicas: %d)\" as standalone_%s #LightBlue\n", replicas, p.sanitizeID(topoID)))
		case "cluster-manager", "clustermanager":
			sb.WriteString(fmt.Sprintf("  COMPONENT \"Cluster Manager\" as cm_%s #LightGreen\n", p.sanitizeID(topoID)))
			indexers := 3
			if idx, ok := topoNode.Attributes["indexer_replicas"].(int); ok {
				indexers = idx
			}
			sb.WriteString(fmt.Sprintf("  COMPONENT \"Indexer Cluster\\n(%d indexers)\" as idx_%s #LightCoral\n", indexers, p.sanitizeID(topoID)))
			sb.WriteString(fmt.Sprintf("  cm_%s -down-> idx_%s : manages\n", p.sanitizeID(topoID), p.sanitizeID(topoID)))
		case "searchheadcluster", "search-head-cluster":
			sb.WriteString(fmt.Sprintf("  COMPONENT \"Search Head Cluster\\n(replicas: %d)\" as shc_%s #LightYellow\n", replicas, p.sanitizeID(topoID)))
			sb.WriteString(fmt.Sprintf("  COMPONENT \"Deployer\" as deployer_%s #LightGray\n", p.sanitizeID(topoID)))
			sb.WriteString(fmt.Sprintf("  deployer_%s -down-> shc_%s : deploys apps\n", p.sanitizeID(topoID), p.sanitizeID(topoID)))
		case "license-manager", "licensemanager":
			sb.WriteString(fmt.Sprintf("  COMPONENT \"License Manager\" as lm_%s #Lavender\n", p.sanitizeID(topoID)))
		case "monitoring-console", "monitoringconsole":
			sb.WriteString(fmt.Sprintf("  COMPONENT \"Monitoring Console\" as mc_%s #LightCyan\n", p.sanitizeID(topoID)))
		default:
			sb.WriteString(fmt.Sprintf("  COMPONENT \"%s\\n(replicas: %d)\" as generic_%s #LightGray\n", kind, replicas, p.sanitizeID(topoID)))
		}

		sb.WriteString("}\n\n")

		// Add test relationships
		if tests, ok := testsByTopology[topoID]; ok && len(tests) > 0 {
			sb.WriteString(fmt.Sprintf("note right of %s\n", p.sanitizeID(topoID)))
			sb.WriteString(fmt.Sprintf("  **Used by %d test(s)**\n", len(tests)))
			for i, testName := range tests {
				if i < 5 { // Limit to 5 tests to avoid clutter
					sb.WriteString(fmt.Sprintf("  - %s\n", testName))
				} else if i == 5 {
					sb.WriteString(fmt.Sprintf("  - ... and %d more\n", len(tests)-5))
					break
				}
			}
			sb.WriteString("end note\n\n")
		}
	}

	// Add image information
	images := make(map[string]*Node)
	for _, node := range p.graph.Nodes {
		if node.Type == "image" {
			images[node.ID] = &node
		}
	}

	if len(images) > 0 {
		sb.WriteString("legend right\n")
		sb.WriteString("  **Images Used**\n")
		sb.WriteString("  ==\n")
		for _, img := range images {
			imgType := "unknown"
			imgVersion := "unknown"
			if t, ok := img.Attributes["type"].(string); ok {
				imgType = t
			}
			if v, ok := img.Attributes["version"].(string); ok {
				imgVersion = v
			}
			sb.WriteString(fmt.Sprintf("  * %s: %s\n", imgType, imgVersion))
		}
		sb.WriteString("endlegend\n\n")
	}

	sb.WriteString("@enduml\n")
	return sb.String()
}

// GenerateTestSequenceDiagram generates a sequence diagram for a specific test
func (p *PlantUMLGenerator) GenerateTestSequenceDiagram(testName string) string {
	var sb strings.Builder

	// Find the test in results
	var testResult *results.TestResult
	for i := range p.run.Tests {
		if p.run.Tests[i].Name == testName {
			testResult = &p.run.Tests[i]
			break
		}
	}

	if testResult == nil {
		return fmt.Sprintf("@startuml\ntitle Test Not Found: %s\n@enduml\n", testName)
	}

	sb.WriteString("@startuml\n")
	sb.WriteString("skinparam sequenceMessageAlign center\n")
	sb.WriteString("skinparam responseMessageBelowArrow true\n\n")
	sb.WriteString(fmt.Sprintf("title Test Sequence: %s\n\n", testName))

	// Add status indicator
	statusColor := "#90EE90"
	statusSymbol := "✓"
	if testResult.Status != "passed" {
		statusColor = "#FFB6C6"
		statusSymbol = "✗"
	}

	sb.WriteString(fmt.Sprintf("participant \"Test Runner\" as Runner %s\n", statusColor))
	sb.WriteString("participant \"Kubernetes API\" as K8s\n")

	// Determine participants based on actions
	hasTopology := false
	hasSplunk := false
	for _, step := range testResult.Steps {
		if strings.HasPrefix(step.Action, "k8s_") {
			hasTopology = true
		}
		if strings.HasPrefix(step.Action, "splunk_") {
			hasSplunk = true
		}
	}

	if hasTopology {
		sb.WriteString("participant \"Topology\" as Topology\n")
	}
	if hasSplunk {
		sb.WriteString("participant \"Splunk Pod(s)\" as Splunk\n")
	}

	sb.WriteString("\n")

	// Add test metadata
	sb.WriteString("note over Runner\n")
	sb.WriteString(fmt.Sprintf("  **Status**: %s %s\n", testResult.Status, statusSymbol))
	sb.WriteString(fmt.Sprintf("  **Duration**: %.2fs\n", testResult.Duration.Seconds()))
	// Find first failed step for error message
	for _, step := range testResult.Steps {
		if step.Status != "passed" && step.Error != "" {
			sb.WriteString(fmt.Sprintf("  **Error**: %s\n", p.truncate(step.Error, 50)))
			break
		}
	}
	sb.WriteString("end note\n\n")

	// Generate sequence for each step
	for i, step := range testResult.Steps {
		stepNum := i + 1
		stepStatus := "✓"
		activationColor := ""
		if step.Status != "passed" {
			stepStatus = "✗"
			activationColor = " #FFB6C6"
		}

		duration := fmt.Sprintf("(%.1fs)", step.Duration.Seconds())

		// Map action to sequence
		switch {
		case step.Action == "k8s_create":
			sb.WriteString(fmt.Sprintf("Runner -> K8s%s: %d. k8s_create %s\n", activationColor, stepNum, duration))
			sb.WriteString(fmt.Sprintf("K8s --> Topology: Create resources\n"))
			sb.WriteString(fmt.Sprintf("K8s --> Runner: Created %s\n", stepStatus))

		case step.Action == "k8s_delete":
			sb.WriteString(fmt.Sprintf("Runner -> K8s%s: %d. k8s_delete %s\n", activationColor, stepNum, duration))
			sb.WriteString(fmt.Sprintf("K8s --> Topology: Delete resources\n"))
			sb.WriteString(fmt.Sprintf("K8s --> Runner: Deleted %s\n", stepStatus))

		case step.Action == "k8s_wait_for_pod":
			sb.WriteString(fmt.Sprintf("Runner -> K8s%s: %d. k8s_wait_for_pod %s\n", activationColor, stepNum, duration))
			sb.WriteString(fmt.Sprintf("K8s --> Topology: Poll pod status\n"))
			if step.Status == "passed" {
				sb.WriteString(fmt.Sprintf("K8s --> Runner: Pod ready %s\n", stepStatus))
			} else {
				sb.WriteString(fmt.Sprintf("K8s --> Runner: Timeout %s\n", stepStatus))
			}

		case step.Action == "k8s_exec":
			sb.WriteString(fmt.Sprintf("Runner -> K8s%s: %d. k8s_exec %s\n", activationColor, stepNum, duration))
			sb.WriteString(fmt.Sprintf("K8s --> Splunk: Execute command\n"))
			sb.WriteString(fmt.Sprintf("Splunk --> K8s: Command output\n"))
			sb.WriteString(fmt.Sprintf("K8s --> Runner: Result %s\n", stepStatus))

		case strings.HasPrefix(step.Action, "splunk_"):
			actionName := strings.TrimPrefix(step.Action, "splunk_")
			sb.WriteString(fmt.Sprintf("Runner -> Splunk%s: %d. %s %s\n", activationColor, stepNum, actionName, duration))
			sb.WriteString(fmt.Sprintf("Splunk --> Runner: Response %s\n", stepStatus))

		case strings.HasPrefix(step.Action, "license_"):
			actionName := strings.TrimPrefix(step.Action, "license_")
			sb.WriteString(fmt.Sprintf("Runner -> Splunk%s: %d. %s %s\n", activationColor, stepNum, actionName, duration))
			sb.WriteString(fmt.Sprintf("Splunk --> Runner: License result %s\n", stepStatus))

		case strings.HasPrefix(step.Action, "assert_"):
			actionName := strings.TrimPrefix(step.Action, "assert_")
			sb.WriteString(fmt.Sprintf("Runner -> Runner%s: %d. assert_%s %s %s\n", activationColor, stepNum, actionName, duration, stepStatus))

		default:
			sb.WriteString(fmt.Sprintf("Runner -> Runner%s: %d. %s %s %s\n", activationColor, stepNum, step.Action, duration, stepStatus))
		}

		// Add error note if step failed
		if step.Status != "passed" && step.Error != "" {
			sb.WriteString(fmt.Sprintf("note right of Runner #FFB6C6\n"))
			sb.WriteString(fmt.Sprintf("  **Step %d Failed**\n", stepNum))
			sb.WriteString(fmt.Sprintf("  %s\n", p.truncate(step.Error, 60)))
			sb.WriteString(fmt.Sprintf("end note\n"))
		}

		sb.WriteString("\n")
	}

	sb.WriteString("@enduml\n")
	return sb.String()
}

// GenerateRunSummaryDiagram generates an overview diagram for the entire test run
func (p *PlantUMLGenerator) GenerateRunSummaryDiagram() string {
	var sb strings.Builder

	sb.WriteString("@startuml\n")
	sb.WriteString("skinparam classFontSize 12\n")
	sb.WriteString("skinparam defaultTextAlignment center\n\n")

	sb.WriteString("title Test Run Summary\n\n")

	// Run summary box
	passed := 0
	failed := 0
	for _, test := range p.run.Tests {
		if test.Status == "passed" {
			passed++
		} else {
			failed++
		}
	}

	passRate := 0.0
	if len(p.run.Tests) > 0 {
		passRate = float64(passed) / float64(len(p.run.Tests)) * 100
	}

	sb.WriteString("rectangle \"Test Run\" #LightBlue {\n")
	sb.WriteString(fmt.Sprintf("  rectangle \"**Total**: %d tests\" as total\n", len(p.run.Tests)))
	sb.WriteString(fmt.Sprintf("  rectangle \"**Passed**: %d (%.1f%%)\" as pass #90EE90\n", passed, passRate))
	sb.WriteString(fmt.Sprintf("  rectangle \"**Failed**: %d\" as fail #FFB6C6\n", failed))
	sb.WriteString(fmt.Sprintf("  rectangle \"**Duration**: %.1fm\" as dur\n", p.run.Duration.Minutes()))
	sb.WriteString("}\n\n")

	// Group tests by status and topology
	testsByStatus := make(map[string][]string)
	for _, test := range p.run.Tests {
		status := string(test.Status)
		testsByStatus[status] = append(testsByStatus[status], test.Name)
	}

	// Show failed tests
	if len(testsByStatus["failed"]) > 0 {
		sb.WriteString("rectangle \"Failed Tests\" #FFB6C6 {\n")
		for i, testName := range testsByStatus["failed"] {
			if i < 10 { // Limit to 10 tests
				// Find duration
				var duration float64
				for _, test := range p.run.Tests {
					if test.Name == testName {
						duration = test.Duration.Seconds()
						break
					}
				}
				sb.WriteString(fmt.Sprintf("  rectangle \"%s\\n(%.1fs)\" as fail_%d\n", p.truncate(testName, 40), duration, i))
			} else if i == 10 {
				sb.WriteString(fmt.Sprintf("  rectangle \"... and %d more\" as fail_more\n", len(testsByStatus["failed"])-10))
				break
			}
		}
		sb.WriteString("}\n\n")
	}

	// Show topology distribution
	topologyCount := make(map[string]int)
	for _, test := range p.run.Tests {
		// Find topology for this test
		testID := fmt.Sprintf("test:%s", test.Name)
		for _, edge := range p.graph.Edges {
			if edge.From == testID && edge.Type == "USES_TOPOLOGY" {
				topoNode := p.findNode(edge.To)
				if topoNode != nil {
					topologyCount[topoNode.Label]++
				}
				break
			}
		}
	}

	if len(topologyCount) > 0 {
		sb.WriteString("rectangle \"Tests by Topology\" #LightYellow {\n")
		// Sort topologies by count
		type topoCount struct {
			name  string
			count int
		}
		var topoCounts []topoCount
		for name, count := range topologyCount {
			topoCounts = append(topoCounts, topoCount{name, count})
		}
		sort.Slice(topoCounts, func(i, j int) bool {
			return topoCounts[i].count > topoCounts[j].count
		})

		for i, tc := range topoCounts {
			if i < 8 { // Limit to 8 topologies
				sb.WriteString(fmt.Sprintf("  rectangle \"%s: %d tests\" as topo_%d\n", tc.name, tc.count, i))
			}
		}
		sb.WriteString("}\n\n")
	}

	sb.WriteString("@enduml\n")
	return sb.String()
}

// GenerateFailureAnalysisDiagram generates a diagram highlighting failure patterns
func (p *PlantUMLGenerator) GenerateFailureAnalysisDiagram() string {
	var sb strings.Builder

	// Collect failed tests
	var failedTests []results.TestResult
	for _, test := range p.run.Tests {
		if test.Status != "passed" {
			failedTests = append(failedTests, test)
		}
	}

	if len(failedTests) == 0 {
		sb.WriteString("@startuml\n")
		sb.WriteString("title Failure Analysis\n\n")
		sb.WriteString("rectangle \"No failures detected\" #90EE90\n")
		sb.WriteString("@enduml\n")
		return sb.String()
	}

	sb.WriteString("@startuml\n")
	sb.WriteString("skinparam rectangleFontSize 11\n\n")
	sb.WriteString(fmt.Sprintf("title Failure Analysis (%d failed tests)\n\n", len(failedTests)))

	// Group failures by error type
	errorTypes := make(map[string][]string)
	for _, test := range failedTests {
		errorKey := "unknown error"
		// Find first failed step to get error message
		for _, step := range test.Steps {
			if step.Status != "passed" && step.Error != "" {
				// Extract error type from error message
				if strings.Contains(step.Error, "timeout") {
					errorKey = "timeout"
				} else if strings.Contains(step.Error, "not found") {
					errorKey = "resource not found"
				} else if strings.Contains(step.Error, "connection refused") {
					errorKey = "connection refused"
				} else if strings.Contains(step.Error, "pod") {
					errorKey = "pod failure"
				} else {
					// Use first 30 chars of error
					errorKey = p.truncate(step.Error, 30)
				}
				break
			}
		}
		errorTypes[errorKey] = append(errorTypes[errorKey], test.Name)
	}

	// Sort error types by frequency
	type errCount struct {
		errType string
		count   int
		tests   []string
	}
	var errCounts []errCount
	for errType, tests := range errorTypes {
		errCounts = append(errCounts, errCount{errType, len(tests), tests})
	}
	sort.Slice(errCounts, func(i, j int) bool {
		return errCounts[i].count > errCounts[j].count
	})

	// Generate diagram
	for i, ec := range errCounts {
		sb.WriteString(fmt.Sprintf("rectangle \"Error: %s\\n(%d tests)\" as err_%d #FFB6C6 {\n", ec.errType, ec.count, i))
		for j, testName := range ec.tests {
			if j < 5 { // Limit to 5 tests per error type
				sb.WriteString(fmt.Sprintf("  rectangle \"%s\" as test_%d_%d\n", p.truncate(testName, 35), i, j))
			} else if j == 5 {
				sb.WriteString(fmt.Sprintf("  rectangle \"... %d more\" as more_%d\n", ec.count-5, i))
				break
			}
		}
		sb.WriteString("}\n\n")
	}

	// Add recommendations note
	sb.WriteString("note bottom\n")
	sb.WriteString("  **Common Failure Patterns**\n")
	sb.WriteString("  ==\n")
	for i, ec := range errCounts {
		if i < 3 {
			sb.WriteString(fmt.Sprintf("  * %s: %d occurrences\n", ec.errType, ec.count))
		}
	}
	sb.WriteString("end note\n\n")

	sb.WriteString("@enduml\n")
	return sb.String()
}

// Helper functions

func (p *PlantUMLGenerator) findNode(id string) *Node {
	for _, node := range p.graph.Nodes {
		if node.ID == id {
			return &node
		}
	}
	return nil
}

func (p *PlantUMLGenerator) sanitizeID(id string) string {
	// Replace invalid PlantUML characters
	id = strings.ReplaceAll(id, ":", "_")
	id = strings.ReplaceAll(id, "-", "_")
	id = strings.ReplaceAll(id, ".", "_")
	id = strings.ReplaceAll(id, " ", "_")
	return id
}

func (p *PlantUMLGenerator) truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
