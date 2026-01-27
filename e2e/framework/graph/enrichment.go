package graph

import (
	"fmt"
	"strings"
	"time"
)

// ErrorPattern represents a categorized error pattern for knowledge retention
type ErrorPattern struct {
	Pattern     string   `json:"pattern"`
	Category    string   `json:"category"` // e.g., "ImagePullError", "OOMKilled", "NetworkTimeout"
	Severity    string   `json:"severity"` // "critical", "high", "medium", "low"
	Component   string   `json:"component"`
	Tags        []string `json:"tags"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
	Occurrences int      `json:"occurrences"`
}

// Resolution represents a documented solution for errors
type Resolution struct {
	ID          string    `json:"id"`
	ErrorPattern string   `json:"error_pattern"`
	Solution    string    `json:"solution"`
	Workaround  string    `json:"workaround,omitempty"`
	RootCause   string    `json:"root_cause"`
	RelatedDocs []string  `json:"related_docs,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Verified    bool      `json:"verified"`
	VerifiedBy  string    `json:"verified_by,omitempty"`
}

// TestFailureAnalysis contains enriched failure information
type TestFailureAnalysis struct {
	TestName       string         `json:"test_name"`
	FailureStep    string         `json:"failure_step"`
	ErrorMessage   string         `json:"error_message"`
	ErrorCategory  string         `json:"error_category"`
	StackTrace     string         `json:"stack_trace,omitempty"`
	PodEvents      []string       `json:"pod_events,omitempty"`
	PodLogs        []string       `json:"pod_logs,omitempty"`
	ResourceMetrics map[string]interface{} `json:"resource_metrics,omitempty"`
	KnownIssue     bool           `json:"known_issue"`
	RelatedFailures []string      `json:"related_failures,omitempty"`
}

// EnhancedGraph extends the basic graph with knowledge retention
type EnhancedGraph struct {
	*Graph
	ErrorPatterns     []ErrorPattern        `json:"error_patterns,omitempty"`
	Resolutions       []Resolution          `json:"resolutions,omitempty"`
	FailureAnalyses   []TestFailureAnalysis `json:"failure_analyses,omitempty"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}

// CategorizeError analyzes an error message and categorizes it
func CategorizeError(errorMsg string) string {
	errorLower := strings.ToLower(errorMsg)

	categories := map[string][]string{
		"ImagePullError": {"imagepullbackoff", "errimagepull", "image not found", "manifest unknown"},
		"OOMKilled": {"oomkilled", "out of memory", "memory limit exceeded"},
		"NetworkTimeout": {"timeout", "connection refused", "connection timed out", "dial tcp", "i/o timeout"},
		"PodInitError": {"init:error", "init:crashloopbackoff", "pod initialization error"},
		"ConfigError": {"configmap not found", "secret not found", "invalid configuration"},
		"ResourceQuota": {"exceeded quota", "resource quota", "insufficient resources"},
		"VolumeMount": {"volume mount", "persistentvolumeclaim", "storage", "volume not found"},
		"LicenseError": {"license", "expired license", "invalid license"},
		"CertificateError": {"certificate", "tls", "x509", "cert"},
		"APIError": {"api call failed", "forbidden", "unauthorized", "invalid api"},
		"ClusterError": {"cluster manager", "cluster master", "replication factor", "search factor"},
		"AppFrameworkError": {"app framework", "app install", "app download"},
		"OperatorError": {"operator", "reconcile", "controller"},
	}

	for category, patterns := range categories {
		for _, pattern := range patterns {
			if strings.Contains(errorLower, pattern) {
				return category
			}
		}
	}

	return "Unknown"
}

// DetermineSeverity determines severity based on error category and context
func DetermineSeverity(category string, testTags []string) string {
	// Critical errors that prevent deployment
	criticalCategories := map[string]bool{
		"OOMKilled": true,
		"ResourceQuota": true,
		"LicenseError": true,
	}

	// High severity errors that affect functionality
	highCategories := map[string]bool{
		"ImagePullError": true,
		"ClusterError": true,
		"OperatorError": true,
	}

	if criticalCategories[category] {
		return "critical"
	}
	if highCategories[category] {
		return "high"
	}

	// Check if smoke test - higher severity
	for _, tag := range testTags {
		if tag == "smoke" {
			return "high"
		}
	}

	return "medium"
}

// AddErrorPattern records an error pattern for knowledge building
func (eg *EnhancedGraph) AddErrorPattern(pattern ErrorPattern) {
	// Check if pattern already exists
	for i, existing := range eg.ErrorPatterns {
		if existing.Pattern == pattern.Pattern {
			eg.ErrorPatterns[i].LastSeen = time.Now().UTC()
			eg.ErrorPatterns[i].Occurrences++
			return
		}
	}

	// Add new pattern
	pattern.FirstSeen = time.Now().UTC()
	pattern.LastSeen = time.Now().UTC()
	pattern.Occurrences = 1
	eg.ErrorPatterns = append(eg.ErrorPatterns, pattern)

	// Add to graph
	patternID := fmt.Sprintf("error_pattern:%s", pattern.Category)
	eg.AddNode(Node{
		ID:    patternID,
		Type:  "error_pattern",
		Label: pattern.Category,
		Attributes: map[string]interface{}{
			"severity":    pattern.Severity,
			"component":   pattern.Component,
			"occurrences": pattern.Occurrences,
		},
	})
}

// AddResolution records a resolution for an error pattern
func (eg *EnhancedGraph) AddResolution(resolution Resolution) {
	resolution.UpdatedAt = time.Now().UTC()
	if resolution.CreatedAt.IsZero() {
		resolution.CreatedAt = resolution.UpdatedAt
	}

	eg.Resolutions = append(eg.Resolutions, resolution)

	// Add to graph
	resolutionID := fmt.Sprintf("resolution:%s", resolution.ID)
	errorPatternID := fmt.Sprintf("error_pattern:%s", resolution.ErrorPattern)

	eg.AddNode(Node{
		ID:    resolutionID,
		Type:  "resolution",
		Label: resolution.ID,
		Attributes: map[string]interface{}{
			"verified":   resolution.Verified,
			"created_at": resolution.CreatedAt,
		},
	})

	eg.AddEdge(Edge{
		From: errorPatternID,
		To:   resolutionID,
		Type: "HAS_RESOLUTION",
	})
}

// AddFailureAnalysis adds enriched failure information to the graph
func (eg *EnhancedGraph) AddFailureAnalysis(analysis TestFailureAnalysis) {
	eg.FailureAnalyses = append(eg.FailureAnalyses, analysis)

	// Add to graph
	testID := fmt.Sprintf("test:%s", analysis.TestName)
	analysisID := fmt.Sprintf("failure_analysis:%s:%s", analysis.TestName, analysis.FailureStep)
	errorPatternID := fmt.Sprintf("error_pattern:%s", analysis.ErrorCategory)

	eg.AddNode(Node{
		ID:    analysisID,
		Type:  "failure_analysis",
		Label: analysis.FailureStep,
		Attributes: map[string]interface{}{
			"error_category": analysis.ErrorCategory,
			"known_issue":    analysis.KnownIssue,
			"error_message":  analysis.ErrorMessage,
		},
	})

	eg.AddEdge(Edge{
		From: testID,
		To:   analysisID,
		Type: "HAS_FAILURE_ANALYSIS",
	})

	eg.AddEdge(Edge{
		From: analysisID,
		To:   errorPatternID,
		Type: "MATCHES_PATTERN",
	})

	// Link to related failures
	for _, relatedTest := range analysis.RelatedFailures {
		relatedID := fmt.Sprintf("test:%s", relatedTest)
		eg.AddEdge(Edge{
			From: testID,
			To:   relatedID,
			Type: "SIMILAR_FAILURE",
		})
	}
}

// AddConfigurationNode adds a CR configuration to the graph
func (eg *EnhancedGraph) AddConfigurationNode(testName, crKind, crName string, config map[string]interface{}) {
	testID := fmt.Sprintf("test:%s", testName)
	configID := fmt.Sprintf("config:%s:%s:%s", testName, crKind, crName)

	eg.AddNode(Node{
		ID:    configID,
		Type:  "configuration",
		Label: fmt.Sprintf("%s/%s", crKind, crName),
		Attributes: map[string]interface{}{
			"kind":   crKind,
			"name":   crName,
			"config": config,
		},
	})

	eg.AddEdge(Edge{
		From: testID,
		To:   configID,
		Type: "USES_CONFIGURATION",
	})
}

// AddTimingMetrics adds timing information to the graph
func (eg *EnhancedGraph) AddTimingMetrics(testName string, metrics map[string]time.Duration) {
	testID := fmt.Sprintf("test:%s", testName)
	metricsID := fmt.Sprintf("timing_metrics:%s", testName)

	// Convert durations to seconds for easier comparison
	metricsData := make(map[string]interface{})
	for key, duration := range metrics {
		metricsData[key] = duration.Seconds()
	}

	eg.AddNode(Node{
		ID:    metricsID,
		Type:  "timing_metrics",
		Label: "Timing Metrics",
		Attributes: metricsData,
	})

	eg.AddEdge(Edge{
		From: testID,
		To:   metricsID,
		Type: "HAS_TIMING_METRICS",
	})
}

// AddEnvironmentContext adds detailed environment information
func (eg *EnhancedGraph) AddEnvironmentContext(testName string, env map[string]string) {
	testID := fmt.Sprintf("test:%s", testName)
	envID := fmt.Sprintf("environment:%s", testName)

	envData := make(map[string]interface{})
	for k, v := range env {
		envData[k] = v
	}

	eg.AddNode(Node{
		ID:    envID,
		Type:  "environment",
		Label: "Environment Context",
		Attributes: envData,
	})

	eg.AddEdge(Edge{
		From: testID,
		To:   envID,
		Type: "HAS_ENVIRONMENT",
	})

	// Create edges to specific environment components
	if cloudProvider, ok := env["cloud_provider"]; ok {
		cloudID := fmt.Sprintf("cloud:%s", cloudProvider)
		eg.AddNode(Node{
			ID:    cloudID,
			Type:  "cloud_provider",
			Label: cloudProvider,
		})
		eg.AddEdge(Edge{
			From: envID,
			To:   cloudID,
			Type: "RUNS_ON_CLOUD",
		})
	}

	if region, ok := env["region"]; ok {
		regionID := fmt.Sprintf("region:%s", region)
		eg.AddNode(Node{
			ID:    regionID,
			Type:  "region",
			Label: region,
		})
		eg.AddEdge(Edge{
			From: envID,
			To:   regionID,
			Type: "IN_REGION",
		})
	}
}

// NewEnhancedGraph creates a new enhanced graph
func NewEnhancedGraph() *EnhancedGraph {
	return &EnhancedGraph{
		Graph:           &Graph{},
		ErrorPatterns:   []ErrorPattern{},
		Resolutions:     []Resolution{},
		FailureAnalyses: []TestFailureAnalysis{},
		Metadata:        make(map[string]interface{}),
	}
}
