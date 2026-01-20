package runner

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/splunk/splunk-operator/e2e/framework/artifacts"
	"github.com/splunk/splunk-operator/e2e/framework/config"
	"github.com/splunk/splunk-operator/e2e/framework/data"
	"github.com/splunk/splunk-operator/e2e/framework/graph"
	"github.com/splunk/splunk-operator/e2e/framework/k8s"
	"github.com/splunk/splunk-operator/e2e/framework/metrics"
	"github.com/splunk/splunk-operator/e2e/framework/results"
	"github.com/splunk/splunk-operator/e2e/framework/spec"
	"github.com/splunk/splunk-operator/e2e/framework/steps"
	"github.com/splunk/splunk-operator/e2e/framework/telemetry"
	"go.uber.org/zap"
)

// Runner executes E2E specs.
type Runner struct {
	cfg           *config.Config
	logger        *zap.Logger
	registry      *steps.Registry
	artifacts     *artifacts.Writer
	metrics       *metrics.Collector
	graph         *graph.Graph
	graphMu       sync.Mutex
	data          *data.Registry
	kube          *k8s.Client
	cluster       k8s.ClusterInfo
	operatorImage string
	logMu         sync.Mutex
	logCollected  map[string]string
	telemetry     *telemetry.Telemetry
	specs         []spec.TestSpec // Store specs for PlantUML generation
}

// NewRunner constructs a Runner.
func NewRunner(cfg *config.Config, logger *zap.Logger, registry *steps.Registry, dataRegistry *data.Registry, kube *k8s.Client, telemetryClient *telemetry.Telemetry) (*Runner, error) {
	writer, err := artifacts.NewWriter(cfg.ArtifactDir)
	if err != nil {
		return nil, err
	}
	clusterInfo, _ := kube.GetClusterInfo(context.Background())
	operatorImage := cfg.OperatorImage
	if detected, err := kube.GetDeploymentImage(context.Background(), cfg.OperatorNamespace, cfg.OperatorDeployment); err == nil && detected != "" {
		operatorImage = detected
	}
	return &Runner{
		cfg:           cfg,
		logger:        logger,
		registry:      registry,
		artifacts:     writer,
		metrics:       metrics.NewCollector(),
		graph:         &graph.Graph{},
		data:          dataRegistry,
		kube:          kube,
		cluster:       clusterInfo,
		operatorImage: operatorImage,
		logCollected:  make(map[string]string),
		telemetry:     telemetryClient,
	}, nil
}

// RunAll executes all specs and returns a run result.
func (r *Runner) RunAll(ctx context.Context, specs []spec.TestSpec) (*results.RunResult, error) {
	r.specs = specs // Store specs for PlantUML generation
	runCtx, runSpan := r.startRunSpan(ctx, specs)
	var result *results.RunResult
	var err error
	if strings.ToLower(r.cfg.TopologyMode) == "suite" {
		result, err = r.runByTopology(runCtx, specs)
	} else {
		result, err = r.runPerTest(runCtx, specs)
	}
	r.finishRunSpan(runSpan, result, err)
	if runSpan != nil {
		runSpan.End()
	}
	return result, err
}

func (r *Runner) runPerTest(ctx context.Context, specs []spec.TestSpec) (*results.RunResult, error) {
	start := time.Now().UTC()
	run := &results.RunResult{RunID: r.cfg.RunID, StartTime: start}

	sem := make(chan struct{}, r.cfg.Parallelism)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, testSpec := range specs {
		specCopy := testSpec
		if !specCopy.MatchesTags(r.cfg.IncludeTags, r.cfg.ExcludeTags) {
			result := r.skipResult(specCopy, "tag filtered")
			r.metrics.ObserveTest(string(result.Status), result.Duration)
			r.observeTestMetrics(specCopy, result)
			r.addGraphForTest(specCopy, result)
			run.Tests = append(run.Tests, result)
			continue
		}
		if !r.hasCapabilities(specCopy.Requires) {
			result := r.skipResult(specCopy, "missing capabilities")
			r.metrics.ObserveTest(string(result.Status), result.Duration)
			r.observeTestMetrics(specCopy, result)
			r.addGraphForTest(specCopy, result)
			run.Tests = append(run.Tests, result)
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			result := r.runSpec(ctx, specCopy)
			r.metrics.ObserveTest(string(result.Status), result.Duration)
			r.addGraphForTest(specCopy, result)
			mu.Lock()
			run.Tests = append(run.Tests, result)
			mu.Unlock()
		}()
	}

	wg.Wait()
	run.EndTime = time.Now().UTC()
	run.Duration = run.EndTime.Sub(run.StartTime)

	return run, nil
}

func (r *Runner) runSpec(ctx context.Context, testSpec spec.TestSpec) results.TestResult {
	exec := steps.NewContext(r.cfg.RunID, testSpec.Metadata.Name, r.logger, r.artifacts, r.data, r.cfg, r.kube, &testSpec)
	return r.runSpecWithExec(ctx, testSpec, exec)
}

func (r *Runner) runSpecWithExec(ctx context.Context, testSpec spec.TestSpec, exec *steps.Context) results.TestResult {
	result := results.TestResult{
		Name:        testSpec.Metadata.Name,
		Description: testSpec.Metadata.Description,
		Tags:        testSpec.Metadata.Tags,
		Requires:    testSpec.Requires,
		StartTime:   time.Now().UTC(),
		Metadata: map[string]string{
			"operator_image":    r.operatorImage,
			"splunk_image":      r.cfg.SplunkImage,
			"cluster_provider":  r.cfg.ClusterProvider,
			"k8s_version":       r.cluster.KubernetesVersion,
			"node_os":           r.cluster.NodeOSImage,
			"container_runtime": r.cluster.ContainerRuntime,
			"kubelet_version":   r.cluster.KubeletVersion,
		},
	}
	if r.logger != nil {
		r.logger.Info("test start", zap.String("test", testSpec.Metadata.Name), zap.String("topology", resolveTopology(testSpec, exec)))
	}
	defer func() {
		if r.logger != nil {
			r.logger.Info("test complete", zap.String("test", testSpec.Metadata.Name), zap.String("status", string(result.Status)), zap.Duration("duration", result.Duration))
		}
	}()

	timeout := r.cfg.DefaultTimeout
	if testSpec.Timeout != "" {
		if parsed, err := time.ParseDuration(testSpec.Timeout); err == nil {
			timeout = parsed
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ctx, span := r.startTestSpan(ctx, testSpec, exec)
	defer func() {
		r.finishTestSpan(span, testSpec, exec, &result)
		if span != nil {
			span.End()
		}
	}()

	for _, step := range testSpec.Steps {
		stepResult := r.runStep(ctx, exec, step)
		result.Steps = append(result.Steps, stepResult)
		if stepResult.Status == results.StatusFailed {
			result.Status = results.StatusFailed
			result.EndTime = time.Now().UTC()
			result.Duration = result.EndTime.Sub(result.StartTime)
			r.observeTestMetrics(testSpec, result)
			r.finalizeTest(ctx, exec, &result)
			return result
		}
	}

	for _, assertion := range testSpec.Assertions {
		stepSpec := spec.StepSpec{
			Name:   assertion.Name,
			Action: fmt.Sprintf("assert.%s", assertion.Type),
			With:   assertion.With,
		}
		stepResult := r.runStep(ctx, exec, stepSpec)
		result.Assertions = append(result.Assertions, results.AssertionResult{
			Name:     assertion.Name,
			Status:   stepResult.Status,
			Error:    stepResult.Error,
			Duration: stepResult.Duration,
		})
		if stepResult.Status == results.StatusFailed {
			result.Status = results.StatusFailed
			result.EndTime = time.Now().UTC()
			result.Duration = result.EndTime.Sub(result.StartTime)
			r.observeTestMetrics(testSpec, result)
			r.finalizeTest(ctx, exec, &result)
			return result
		}
	}

	result.Status = results.StatusPassed
	result.EndTime = time.Now().UTC()
	result.Duration = result.EndTime.Sub(result.StartTime)
	r.observeTestMetrics(testSpec, result)
	r.finalizeTest(ctx, exec, &result)
	return result
}

func (r *Runner) runStep(ctx context.Context, exec *steps.Context, step spec.StepSpec) results.StepResult {
	start := time.Now().UTC()
	if r.logger != nil {
		r.logger.Info("step start", zap.String("test", exec.TestName), zap.String("step", step.Name), zap.String("action", step.Action))
	}
	stepCtx, span := r.startStepSpan(ctx, exec, step)
	metadata, err := r.registry.Execute(stepCtx, exec, step)
	end := time.Now().UTC()

	stepResult := results.StepResult{
		Name:      step.Name,
		Action:    step.Action,
		StartTime: start,
		EndTime:   end,
		Duration:  end.Sub(start),
		Metadata:  metadata,
	}
	if err != nil {
		stepResult.Status = results.StatusFailed
		stepResult.Error = err.Error()
		if r.logger != nil {
			r.logger.Warn("step failed", zap.String("test", exec.TestName), zap.String("step", step.Name), zap.String("action", step.Action), zap.Duration("duration", stepResult.Duration), zap.Error(err))
		}
	} else {
		stepResult.Status = results.StatusPassed
		if r.logger != nil {
			// Warn if step took longer than 2 minutes
			if stepResult.Duration > 2*time.Minute {
				r.logger.Warn("step completed but took longer than 2 minutes", zap.String("test", exec.TestName), zap.String("step", step.Name), zap.String("action", step.Action), zap.Duration("duration", stepResult.Duration))
			} else {
				r.logger.Info("step complete", zap.String("test", exec.TestName), zap.String("step", step.Name), zap.String("action", step.Action), zap.Duration("duration", stepResult.Duration))
			}
		}
	}

	r.finishStepSpan(span, exec, step, stepResult, err)
	if span != nil {
		span.End()
	}

	r.metrics.ObserveStep(exec.TestName, step.Action, string(stepResult.Status), stepResult.Duration)
	r.recordStepTelemetry(exec, step, stepResult)
	return stepResult
}

func (r *Runner) skipResult(spec spec.TestSpec, reason string) results.TestResult {
	now := time.Now().UTC()
	return results.TestResult{
		Name:        spec.Metadata.Name,
		Description: spec.Metadata.Description,
		Tags:        spec.Metadata.Tags,
		Requires:    spec.Requires,
		Status:      results.StatusSkipped,
		StartTime:   now,
		EndTime:     now,
		Duration:    0,
		Metadata: map[string]string{
			"skip_reason": reason,
		},
	}
}

func (r *Runner) hasCapabilities(required []string) bool {
	if len(required) == 0 {
		return true
	}
	if len(r.cfg.Capabilities) == 0 {
		return false
	}
	available := make(map[string]bool, len(r.cfg.Capabilities))
	for _, cap := range r.cfg.Capabilities {
		available[strings.ToLower(cap)] = true
	}
	for _, req := range required {
		if !available[strings.ToLower(req)] {
			return false
		}
	}
	return true
}

func (r *Runner) addGraphForTest(spec spec.TestSpec, result results.TestResult) {
	if !r.cfg.GraphEnabled && !r.cfg.Neo4jEnabled {
		return
	}

	r.graphMu.Lock()
	defer r.graphMu.Unlock()

	runID := "run:" + r.cfg.RunID
	testID := "test:" + spec.Metadata.Name
	r.graph.AddNode(graph.Node{ID: runID, Type: "run", Label: r.cfg.RunID})

	// Add test node with comprehensive metadata
	testAttrs := map[string]interface{}{
		"status":      result.Status,
		"topology":    spec.Topology.Kind,
		"description": spec.Metadata.Description,
		"duration":    result.Duration.Seconds(),
	}
	if len(spec.Metadata.Tags) > 0 {
		testAttrs["tags"] = strings.Join(spec.Metadata.Tags, ",")
	}
	r.graph.AddNode(graph.Node{ID: testID, Type: "test", Label: spec.Metadata.Name, Attributes: testAttrs})
	r.graph.AddEdge(graph.Edge{From: runID, To: testID, Type: "HAS_TEST"})

	for _, dataset := range spec.Datasets {
		datasetID := "dataset:" + dataset.Name
		r.graph.AddNode(graph.Node{ID: datasetID, Type: "dataset", Label: dataset.Name})
		r.graph.AddEdge(graph.Edge{From: testID, To: datasetID, Type: "USES_DATASET"})
	}

	for _, step := range result.Steps {
		stepID := fmt.Sprintf("step:%s:%s", spec.Metadata.Name, step.Name)
		r.graph.AddNode(graph.Node{ID: stepID, Type: "step", Label: step.Name, Attributes: map[string]interface{}{"status": step.Status, "action": step.Action}})
		r.graph.AddEdge(graph.Edge{From: testID, To: stepID, Type: "HAS_STEP"})
	}

	for _, assertion := range result.Assertions {
		assertID := fmt.Sprintf("assert:%s:%s", spec.Metadata.Name, assertion.Name)
		r.graph.AddNode(graph.Node{ID: assertID, Type: "assertion", Label: assertion.Name, Attributes: map[string]interface{}{"status": assertion.Status}})
		r.graph.AddEdge(graph.Edge{From: testID, To: assertID, Type: "HAS_ASSERTION"})
	}

	// Add topology node
	if spec.Topology.Kind != "" {
		topologyID := "topology:" + spec.Topology.Kind
		topologyAttrs := map[string]interface{}{
			"kind": spec.Topology.Kind,
		}
		// Add topology params if present
		for key, value := range spec.Topology.Params {
			topologyAttrs[key] = value
		}
		r.graph.AddNode(graph.Node{ID: topologyID, Type: "topology", Label: spec.Topology.Kind, Attributes: topologyAttrs})
		r.graph.AddEdge(graph.Edge{From: testID, To: topologyID, Type: "USES_TOPOLOGY"})
	}

	// Add version and environment nodes with metadata
	imageID := "image:splunk:" + r.cfg.SplunkImage
	operatorID := "image:operator:" + r.operatorImage
	clusterID := "cluster:" + r.cfg.ClusterProvider
	k8sID := "k8s:" + r.cluster.KubernetesVersion

	r.graph.AddNode(graph.Node{ID: imageID, Type: "image", Label: r.cfg.SplunkImage, Attributes: map[string]interface{}{"type": "splunk", "version": r.cfg.SplunkImage}})
	r.graph.AddNode(graph.Node{ID: operatorID, Type: "image", Label: r.operatorImage, Attributes: map[string]interface{}{"type": "operator", "version": r.operatorImage}})
	clusterAttrs := map[string]interface{}{
		"provider": r.cfg.ClusterProvider,
	}
	if r.cluster.NodeOSImage != "" {
		clusterAttrs["node_os"] = r.cluster.NodeOSImage
	}
	if r.cluster.ContainerRuntime != "" {
		clusterAttrs["container_runtime"] = r.cluster.ContainerRuntime
	}
	r.graph.AddNode(graph.Node{ID: clusterID, Type: "cluster", Label: r.cfg.ClusterProvider, Attributes: clusterAttrs})
	if r.cluster.KubernetesVersion != "" {
		r.graph.AddNode(graph.Node{ID: k8sID, Type: "k8s", Label: r.cluster.KubernetesVersion, Attributes: map[string]interface{}{"version": r.cluster.KubernetesVersion}})
	}

	r.graph.AddEdge(graph.Edge{From: testID, To: imageID, Type: "USES_SPLUNK_IMAGE"})
	r.graph.AddEdge(graph.Edge{From: testID, To: operatorID, Type: "USES_OPERATOR_IMAGE"})
	r.graph.AddEdge(graph.Edge{From: testID, To: clusterID, Type: "RUNS_ON"})
	if r.cluster.KubernetesVersion != "" {
		r.graph.AddEdge(graph.Edge{From: clusterID, To: k8sID, Type: "HAS_K8S_VERSION"})
	}

	if result.Metadata != nil {
		if ns := result.Metadata["namespace"]; ns != "" {
			nsID := "namespace:" + ns
			r.graph.AddNode(graph.Node{ID: nsID, Type: "namespace", Label: ns})
			r.graph.AddEdge(graph.Edge{From: testID, To: nsID, Type: "RUNS_IN"})
		}
	}
	if result.Artifacts != nil {
		if logs := result.Artifacts["logs"]; logs != "" {
			logID := "artifact:logs:" + logs
			r.graph.AddNode(graph.Node{ID: logID, Type: "artifact", Label: "logs", Attributes: map[string]interface{}{"path": logs}})
			r.graph.AddEdge(graph.Edge{From: testID, To: logID, Type: "PRODUCED"})
		}
		if logs := result.Artifacts["operator_logs"]; logs != "" {
			logID := "artifact:operator_logs:" + logs
			r.graph.AddNode(graph.Node{ID: logID, Type: "artifact", Label: "operator_logs", Attributes: map[string]interface{}{"path": logs}})
			r.graph.AddEdge(graph.Edge{From: testID, To: logID, Type: "PRODUCED"})
		}
	}

	// Incrementally export to Neo4j after each test if enabled
	if r.cfg.Neo4jEnabled && r.cfg.Neo4jURI != "" {
		exportCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := r.exportGraphToNeo4j(exportCtx); err != nil {
			if r.logger != nil {
				r.logger.Warn("incremental neo4j export failed", zap.String("test", spec.Metadata.Name), zap.Error(err))
			}
		}
	}
}

// FlushArtifacts writes metrics and graph to disk.
func (r *Runner) FlushArtifacts(run *results.RunResult) error {
	if _, err := r.artifacts.WriteJSON("results.json", run); err != nil {
		return err
	}
	summary := summarize(run)
	if _, err := r.artifacts.WriteJSON("summary.json", summary); err != nil {
		return err
	}
	if r.cfg.GraphEnabled {
		if _, err := r.artifacts.WriteJSON("graph.json", r.graph); err != nil {
			return err
		}
	}
	if r.cfg.MetricsEnabled {
		if err := r.metrics.Write(r.cfg.MetricsPath); err != nil {
			return err
		}
	}
	// Neo4j export removed - now happens incrementally after each test in addGraphForTest()

	// Generate PlantUML diagrams
	if r.cfg.GraphEnabled && r.graph != nil {
		generator := graph.NewPlantUMLGenerator(r.graph, r.specs, run)

		// Generate topology diagram
		topologyDiagram := generator.GenerateTopologyDiagram()
		if _, err := r.artifacts.WriteText("topology.plantuml", topologyDiagram); err != nil {
			if r.logger != nil {
				r.logger.Warn("failed to write topology diagram", zap.Error(err))
			}
		}

		// Generate run summary diagram
		summaryDiagram := generator.GenerateRunSummaryDiagram()
		if _, err := r.artifacts.WriteText("run-summary.plantuml", summaryDiagram); err != nil {
			if r.logger != nil {
				r.logger.Warn("failed to write run summary diagram", zap.Error(err))
			}
		}

		// Generate failure analysis diagram
		failureDiagram := generator.GenerateFailureAnalysisDiagram()
		if _, err := r.artifacts.WriteText("failure-analysis.plantuml", failureDiagram); err != nil {
			if r.logger != nil {
				r.logger.Warn("failed to write failure analysis diagram", zap.Error(err))
			}
		}

		// Generate sequence diagrams for each test (limit to first 10 to avoid too many files)
		testCount := len(run.Tests)
		if testCount > 10 {
			testCount = 10
		}
		for i := 0; i < testCount; i++ {
			test := run.Tests[i]
			seqDiagram := generator.GenerateTestSequenceDiagram(test.Name)
			filename := fmt.Sprintf("test-sequence-%s.plantuml", sanitizeFilename(test.Name))
			if _, err := r.artifacts.WriteText(filename, seqDiagram); err != nil {
				if r.logger != nil {
					r.logger.Warn("failed to write test sequence diagram", zap.String("test", test.Name), zap.Error(err))
				}
			}
		}

		if r.logger != nil {
			r.logger.Info("PlantUML diagrams generated", zap.Int("topology_diagrams", 1), zap.Int("summary_diagrams", 1), zap.Int("test_sequences", testCount))
		}
	}

	return nil
}

// sanitizeFilename removes invalid characters from filenames
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, ":", "-")
	name = strings.ReplaceAll(name, "*", "-")
	name = strings.ReplaceAll(name, "?", "-")
	name = strings.ReplaceAll(name, "\"", "-")
	name = strings.ReplaceAll(name, "<", "-")
	name = strings.ReplaceAll(name, ">", "-")
	name = strings.ReplaceAll(name, "|", "-")
	return name
}

func (r *Runner) observeTestMetrics(spec spec.TestSpec, result results.TestResult) {
	topologyKind := resolveTopology(spec, nil)
	if topologyKind == "" {
		topologyKind = "unknown"
	}
	r.metrics.ObserveTestDetail(result.Name, string(result.Status), topologyKind, result.Duration)
	r.metrics.ObserveTestInfo(metrics.TestInfo{
		Test:              result.Name,
		Status:            string(result.Status),
		Topology:          topologyKind,
		OperatorImage:     r.operatorImage,
		SplunkImage:       r.cfg.SplunkImage,
		ClusterProvider:   r.cfg.ClusterProvider,
		KubernetesVersion: r.cluster.KubernetesVersion,
		NodeOSImage:       r.cluster.NodeOSImage,
		ContainerRuntime:  r.cluster.ContainerRuntime,
	})
	r.recordTestTelemetry(spec, result)
}

func (r *Runner) finalizeTest(ctx context.Context, exec *steps.Context, result *results.TestResult) {
	if ctx.Err() == context.DeadlineExceeded {
		if result.Metadata == nil {
			result.Metadata = make(map[string]string)
		}
		result.Metadata["timeout"] = "true"
		result.Metadata["timeout_error"] = ctx.Err().Error()
	}

	namespace := exec.Vars["namespace"]
	if namespace != "" {
		if result.Metadata == nil {
			result.Metadata = make(map[string]string)
		}
		result.Metadata["namespace"] = namespace
	}
	if namespace != "" {
		r.collectLogsForTest(ctx, namespace, result)
	}
	if namespace == "" || r.cfg.SkipTeardown {
		return
	}
	if exec.Vars["topology_shared"] == "true" {
		return
	}

	cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if err := r.kube.DeleteNamespace(cleanupCtx, namespace); err != nil {
		if result.Metadata == nil {
			result.Metadata = make(map[string]string)
		}
		result.Metadata["teardown_error"] = err.Error()
	}
}

type Summary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

func summarize(run *results.RunResult) Summary {
	summary := Summary{Total: len(run.Tests)}
	for _, test := range run.Tests {
		switch test.Status {
		case results.StatusPassed:
			summary.Passed++
		case results.StatusFailed:
			summary.Failed++
		case results.StatusSkipped:
			summary.Skipped++
		}
	}
	return summary
}
