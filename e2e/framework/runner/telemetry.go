package runner

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/splunk/splunk-operator/e2e/framework/results"
	"github.com/splunk/splunk-operator/e2e/framework/spec"
	"github.com/splunk/splunk-operator/e2e/framework/steps"
	"go.opentelemetry.io/otel/trace"
)

func (r *Runner) startRunSpan(ctx context.Context, specs []spec.TestSpec) (context.Context, trace.Span) {
	if r.telemetry == nil || !r.telemetry.Enabled() {
		return ctx, nil
	}
	attrs := map[string]string{
		"e2e.run_id":        r.cfg.RunID,
		"e2e.topology_mode": r.cfg.TopologyMode,
		"e2e.parallelism":   fmt.Sprintf("%d", r.cfg.Parallelism),
		"e2e.spec_count":    fmt.Sprintf("%d", len(specs)),
		"cluster.provider":  r.cfg.ClusterProvider,
	}
	return r.telemetry.StartSpan(ctx, "e2e.run", attrs)
}

func (r *Runner) finishRunSpan(span trace.Span, run *results.RunResult, runErr error) {
	if span == nil || r.telemetry == nil || !r.telemetry.Enabled() {
		return
	}
	attrs := map[string]string{}
	status := "passed"
	if runErr != nil {
		status = "failed"
	}
	if run != nil {
		attrs["e2e.run_id"] = run.RunID
		attrs["e2e.duration_ms"] = fmt.Sprintf("%d", run.Duration.Milliseconds())
		total, passed, failed, skipped := summarizeRun(run)
		attrs["e2e.total"] = fmt.Sprintf("%d", total)
		attrs["e2e.passed"] = fmt.Sprintf("%d", passed)
		attrs["e2e.failed"] = fmt.Sprintf("%d", failed)
		attrs["e2e.skipped"] = fmt.Sprintf("%d", skipped)
		if runErr == nil && failed > 0 {
			status = "failed"
		}
		if runErr == nil && failed == 0 && passed == 0 && skipped > 0 {
			status = "skipped"
		}
	}
	if runErr != nil {
		r.telemetry.MarkSpan(span, status, runErr, attrs)
		return
	}
	if status == "failed" {
		r.telemetry.MarkSpan(span, status, errors.New("run failed"), attrs)
		return
	}
	r.telemetry.MarkSpan(span, status, nil, attrs)
}

func summarizeRun(run *results.RunResult) (total, passed, failed, skipped int) {
	if run == nil {
		return 0, 0, 0, 0
	}
	total = len(run.Tests)
	for _, test := range run.Tests {
		switch test.Status {
		case results.StatusPassed:
			passed++
		case results.StatusFailed:
			failed++
		case results.StatusSkipped:
			skipped++
		}
	}
	return total, passed, failed, skipped
}

func (r *Runner) startTestSpan(ctx context.Context, spec spec.TestSpec, exec *steps.Context) (context.Context, trace.Span) {
	if r.telemetry == nil || !r.telemetry.Enabled() {
		return ctx, nil
	}
	attrs := r.baseTestAttributes(spec, exec)
	spanName := "e2e.test"
	if spec.Metadata.Name != "" {
		spanName = "e2e.test:" + spec.Metadata.Name
	}
	return r.telemetry.StartSpan(ctx, spanName, attrs)
}

func (r *Runner) finishTestSpan(span trace.Span, spec spec.TestSpec, exec *steps.Context, result *results.TestResult) {
	if span == nil || r.telemetry == nil || !r.telemetry.Enabled() || result == nil {
		return
	}
	attrs := mergeAttrs(r.baseTestAttributes(spec, exec), testResultAttributes(result))
	err := testFailureError(*result)
	r.telemetry.MarkSpan(span, string(result.Status), err, attrs)
}

func (r *Runner) startStepSpan(ctx context.Context, exec *steps.Context, step spec.StepSpec) (context.Context, trace.Span) {
	if r.telemetry == nil || !r.telemetry.Enabled() {
		return ctx, nil
	}
	attrs := r.baseStepAttributes(exec, step)
	spanName := "e2e.step"
	if step.Action != "" {
		spanName = "e2e.step:" + step.Action
	} else if step.Name != "" {
		spanName = "e2e.step:" + step.Name
	}
	return r.telemetry.StartSpan(ctx, spanName, attrs)
}

func (r *Runner) finishStepSpan(span trace.Span, exec *steps.Context, step spec.StepSpec, result results.StepResult, stepErr error) {
	if span == nil || r.telemetry == nil || !r.telemetry.Enabled() {
		return
	}
	attrs := mergeAttrs(r.baseStepAttributes(exec, step), stepResultAttributes(result))
	r.telemetry.MarkSpan(span, string(result.Status), stepErr, attrs)
}

func (r *Runner) recordTestTelemetry(spec spec.TestSpec, result results.TestResult) {
	if r.telemetry == nil || !r.telemetry.Enabled() {
		return
	}
	attrs := r.testMetricAttributes(spec, result)
	r.telemetry.RecordTest(string(result.Status), result.Duration, attrs)
}

func (r *Runner) recordStepTelemetry(exec *steps.Context, step spec.StepSpec, result results.StepResult) {
	if r.telemetry == nil || !r.telemetry.Enabled() {
		return
	}
	attrs := r.stepMetricAttributes(exec, step)
	r.telemetry.RecordStep(string(result.Status), result.Duration, attrs)
}

func (r *Runner) baseTestAttributes(spec spec.TestSpec, exec *steps.Context) map[string]string {
	attrs := map[string]string{
		"e2e.run_id": r.cfg.RunID,
		"e2e.test":   spec.Metadata.Name,
	}
	if spec.Metadata.Owner != "" {
		attrs["e2e.owner"] = spec.Metadata.Owner
	}
	if spec.Metadata.Component != "" {
		attrs["e2e.component"] = spec.Metadata.Component
	}
	if len(spec.Metadata.Tags) > 0 {
		attrs["e2e.tags"] = strings.Join(spec.Metadata.Tags, ",")
	}
	if len(spec.Requires) > 0 {
		attrs["e2e.requires"] = strings.Join(spec.Requires, ",")
	}
	if len(spec.Datasets) > 0 {
		attrs["e2e.datasets"] = joinDatasetNames(spec.Datasets)
	}
	if topology := resolveTopology(spec, exec); topology != "" {
		attrs["e2e.topology"] = topology
	}
	if exec != nil {
		if namespace := strings.TrimSpace(exec.Vars["namespace"]); namespace != "" {
			attrs["k8s.namespace"] = namespace
		}
	}
	if r.operatorImage != "" {
		attrs["operator.image"] = r.operatorImage
	}
	if r.cfg.SplunkImage != "" {
		attrs["splunk.image"] = r.cfg.SplunkImage
	}
	if r.cfg.ClusterProvider != "" {
		attrs["cluster.provider"] = r.cfg.ClusterProvider
	}
	if r.cluster.KubernetesVersion != "" {
		attrs["k8s.version"] = r.cluster.KubernetesVersion
	}
	if r.cluster.NodeOSImage != "" {
		attrs["k8s.node_os"] = r.cluster.NodeOSImage
	}
	if r.cluster.ContainerRuntime != "" {
		attrs["container.runtime"] = r.cluster.ContainerRuntime
	}
	return attrs
}

func (r *Runner) baseStepAttributes(exec *steps.Context, step spec.StepSpec) map[string]string {
	attrs := map[string]string{
		"e2e.run_id": r.cfg.RunID,
	}
	if exec != nil {
		attrs["e2e.test"] = exec.TestName
		if exec.Spec != nil {
			if topology := resolveTopology(*exec.Spec, exec); topology != "" {
				attrs["e2e.topology"] = topology
			}
		}
		if namespace := strings.TrimSpace(exec.Vars["namespace"]); namespace != "" {
			attrs["k8s.namespace"] = namespace
		}
	}
	if step.Name != "" {
		attrs["e2e.step"] = step.Name
	}
	if step.Action != "" {
		attrs["e2e.action"] = step.Action
	}
	return attrs
}

func (r *Runner) testMetricAttributes(spec spec.TestSpec, result results.TestResult) map[string]string {
	attrs := map[string]string{
		"test":             result.Name,
		"topology":         resolveTopology(spec, nil),
		"operator_image":   r.operatorImage,
		"splunk_image":     r.cfg.SplunkImage,
		"cluster_provider": r.cfg.ClusterProvider,
	}
	if attrs["topology"] == "" {
		attrs["topology"] = "unknown"
	}
	return attrs
}

func (r *Runner) stepMetricAttributes(exec *steps.Context, step spec.StepSpec) map[string]string {
	attrs := map[string]string{
		"test":   "",
		"step":   step.Name,
		"action": step.Action,
	}
	if exec != nil {
		attrs["test"] = exec.TestName
		if exec.Spec != nil {
			attrs["topology"] = resolveTopology(*exec.Spec, exec)
		}
	}
	if attrs["topology"] == "" {
		attrs["topology"] = "unknown"
	}
	return attrs
}

func resolveTopology(spec spec.TestSpec, exec *steps.Context) string {
	topology := strings.TrimSpace(spec.Topology.Kind)
	if topology == "" && exec != nil {
		topology = strings.TrimSpace(exec.Vars["topology_kind"])
	}
	return topology
}

func joinDatasetNames(datasets []spec.DatasetRef) string {
	names := make([]string, 0, len(datasets))
	for _, dataset := range datasets {
		if dataset.Name != "" {
			names = append(names, dataset.Name)
		}
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

func testResultAttributes(result *results.TestResult) map[string]string {
	attrs := map[string]string{
		"e2e.status":      string(result.Status),
		"e2e.duration_ms": fmt.Sprintf("%d", result.Duration.Milliseconds()),
	}
	if result.Metadata != nil {
		if result.Metadata["timeout"] == "true" {
			attrs["e2e.timeout"] = "true"
		}
		if namespace := strings.TrimSpace(result.Metadata["namespace"]); namespace != "" {
			attrs["k8s.namespace"] = namespace
		}
	}
	return attrs
}

func stepResultAttributes(result results.StepResult) map[string]string {
	attrs := map[string]string{
		"e2e.status":      string(result.Status),
		"e2e.duration_ms": fmt.Sprintf("%d", result.Duration.Milliseconds()),
	}
	return attrs
}

func testFailureError(result results.TestResult) error {
	if result.Status != results.StatusFailed {
		return nil
	}
	if result.Metadata != nil {
		if msg := strings.TrimSpace(result.Metadata["timeout_error"]); msg != "" {
			return errors.New(msg)
		}
		if msg := strings.TrimSpace(result.Metadata["topology_error"]); msg != "" {
			return errors.New(msg)
		}
	}
	for _, step := range result.Steps {
		if step.Status == results.StatusFailed && step.Error != "" {
			return errors.New(step.Error)
		}
	}
	for _, assertion := range result.Assertions {
		if assertion.Status == results.StatusFailed && assertion.Error != "" {
			return errors.New(assertion.Error)
		}
	}
	return errors.New("test failed")
}

func mergeAttrs(values ...map[string]string) map[string]string {
	out := make(map[string]string)
	for _, attrs := range values {
		for key, value := range attrs {
			if value == "" {
				continue
			}
			out[key] = value
		}
	}
	return out
}
