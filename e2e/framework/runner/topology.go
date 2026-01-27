package runner

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/splunk/splunk-operator/e2e/framework/results"
	"github.com/splunk/splunk-operator/e2e/framework/spec"
	"github.com/splunk/splunk-operator/e2e/framework/steps"
	"github.com/splunk/splunk-operator/e2e/framework/topology"
	"go.uber.org/zap"
)

type topologyGroup struct {
	key    string
	kind   string
	params map[string]string
	specs  []spec.TestSpec
}

func (r *Runner) runByTopology(ctx context.Context, specs []spec.TestSpec) (*results.RunResult, error) {
	start := time.Now().UTC()
	run := &results.RunResult{RunID: r.cfg.RunID, StartTime: start}

	groups, skipped := r.buildTopologyGroups(specs)
	if len(skipped) > 0 {
		run.Tests = append(run.Tests, skipped...)
	}

	sem := make(chan struct{}, r.cfg.Parallelism)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, group := range groups {
		groupCopy := group
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			results := r.runTopologyGroup(ctx, groupCopy)
			mu.Lock()
			run.Tests = append(run.Tests, results...)
			mu.Unlock()
		}()
	}

	wg.Wait()
	run.EndTime = time.Now().UTC()
	run.Duration = run.EndTime.Sub(run.StartTime)
	return run, nil
}

func (r *Runner) buildTopologyGroups(specs []spec.TestSpec) ([]topologyGroup, []results.TestResult) {
	groupsByKey := make(map[string]*topologyGroup)
	order := make([]string, 0)
	skipped := make([]results.TestResult, 0)

	for _, testSpec := range specs {
		specCopy := testSpec
		if !specCopy.MatchesTags(r.cfg.IncludeTags, r.cfg.ExcludeTags) {
			result := r.skipResult(specCopy, "tag filtered")
			r.metrics.ObserveTest(string(result.Status), result.Duration)
			r.observeTestMetrics(specCopy, result)
			r.addGraphForTest(specCopy, result)
			skipped = append(skipped, result)
			continue
		}
		if !r.hasCapabilities(specCopy.Requires) {
			result := r.skipResult(specCopy, "missing capabilities")
			r.metrics.ObserveTest(string(result.Status), result.Duration)
			r.observeTestMetrics(specCopy, result)
			r.addGraphForTest(specCopy, result)
			skipped = append(skipped, result)
			continue
		}

		params := collectTopologyParams(specCopy)
		kind := strings.ToLower(strings.TrimSpace(specCopy.Topology.Kind))
		if kind == "" {
			kind = strings.ToLower(strings.TrimSpace(params["kind"]))
		}
		key := topologyKey(kind, params)
		group, ok := groupsByKey[key]
		if !ok {
			order = append(order, key)
			group = &topologyGroup{
				key:    key,
				kind:   kind,
				params: params,
				specs:  []spec.TestSpec{},
			}
			groupsByKey[key] = group
		}
		group.specs = append(group.specs, specCopy)
	}

	groups := make([]topologyGroup, 0, len(groupsByKey))
	for _, key := range order {
		if group := groupsByKey[key]; group != nil {
			groups = append(groups, *group)
		}
	}
	return groups, skipped
}

func (r *Runner) runTopologyGroup(ctx context.Context, group topologyGroup) []results.TestResult {
	if group.kind == "" {
		out := make([]results.TestResult, 0, len(group.specs))
		for _, testSpec := range group.specs {
			result := r.runSpec(ctx, testSpec)
			r.metrics.ObserveTest(string(result.Status), result.Duration)
			r.addGraphForTest(testSpec, result)
			out = append(out, result)
		}
		return out
	}

	namespace := strings.TrimSpace(group.params["namespace"])
	if namespace != "" {
		namespace = os.ExpandEnv(namespace)
	}
	if namespace == "" {
		namespace = fmt.Sprintf("%s-%s", r.cfg.NamespacePrefix, topology.RandomDNSName(5))
	}
	baseName := strings.TrimSpace(group.params["name"])
	if baseName != "" {
		baseName = os.ExpandEnv(baseName)
	}
	if baseName == "" {
		baseName = namespace
	}
	if r.logger != nil {
		r.logger.Info("topology group start", zap.String("kind", group.kind), zap.String("namespace", namespace), zap.String("base_name", baseName), zap.Int("tests", len(group.specs)))
	}
	if err := r.kube.EnsureNamespace(ctx, namespace); err != nil {
		return r.failTopologyGroup(group, err, namespace)
	}

	opts := topology.Options{
		Kind:                 group.kind,
		Namespace:            namespace,
		BaseName:             baseName,
		SplunkImage:          r.cfg.SplunkImage,
		ServiceAccount:       strings.TrimSpace(group.params["service_account"]),
		LicenseManagerRef:    strings.TrimSpace(group.params["license_manager_ref"]),
		LicenseMasterRef:     strings.TrimSpace(group.params["license_master_ref"]),
		MonitoringConsoleRef: strings.TrimSpace(group.params["monitoring_console_ref"]),
		ClusterManagerKind:   strings.TrimSpace(group.params["cluster_manager_kind"]),
		IndexerReplicas:      int32Param(group.params, "indexer_replicas", int32(defaultIndexerReplicas(group.kind))),
		SHCReplicas:          int32Param(group.params, "shc_replicas", int32(defaultSHCReplicas(group.kind))),
		WithSHC:              boolParam(group.params, "with_shc", true),
		SiteCount:            intParam(group.params, "site_count", defaultSiteCount(group.kind)),
	}
	if opts.SiteCount == defaultSiteCount(group.kind) {
		opts.SiteCount = intParam(group.params, "sites", opts.SiteCount)
	}

	if r.logger != nil {
		r.logger.Info("topology deploy", zap.String("kind", opts.Kind), zap.String("namespace", opts.Namespace), zap.String("base_name", opts.BaseName))
	}
	session, err := topology.Deploy(ctx, r.kube, opts)
	if err != nil {
		return r.failTopologyGroup(group, err, namespace)
	}
	if r.logger != nil {
		r.logger.Info("topology deploy complete", zap.String("kind", opts.Kind), zap.String("namespace", opts.Namespace), zap.String("base_name", opts.BaseName))
	}

	timeout := r.cfg.DefaultTimeout
	if override := strings.TrimSpace(group.params["timeout"]); override != "" {
		if parsed, err := time.ParseDuration(override); err == nil {
			timeout = parsed
		}
	}
	if r.logger != nil {
		r.logger.Info("topology wait ready", zap.String("kind", opts.Kind), zap.String("namespace", opts.Namespace), zap.Duration("timeout", timeout))
	}
	if err := topology.WaitReady(ctx, r.kube, session, timeout); err != nil {
		return r.failTopologyGroup(group, err, namespace)
	}
	if r.logger != nil {
		r.logger.Info("topology ready", zap.String("kind", opts.Kind), zap.String("namespace", opts.Namespace))
	}

	out := make([]results.TestResult, 0, len(group.specs))
	for _, testSpec := range group.specs {
		exec := steps.NewContext(r.cfg.RunID, testSpec.Metadata.Name, r.logger, r.artifacts, r.data, r.cfg, r.kube, &testSpec)
		steps.ApplyTopologySession(exec, session)
		exec.Vars["topology_shared"] = "true"
		exec.Vars["topology_waited"] = "true"
		result := r.runSpecWithExec(ctx, testSpec, exec)
		r.metrics.ObserveTest(string(result.Status), result.Duration)
		r.addGraphForTest(testSpec, result)
		out = append(out, result)
	}

	if strings.ToLower(r.cfg.LogCollection) == "always" {
		_, _ = r.ensureNamespaceLogs(context.Background(), namespace)
		if r.cfg.OperatorNamespace != "" && r.cfg.OperatorNamespace != namespace {
			_, _ = r.ensureNamespaceLogs(context.Background(), r.cfg.OperatorNamespace)
		}
	}

	if !r.cfg.SkipTeardown {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		if err := r.kube.DeleteNamespace(cleanupCtx, namespace); err != nil {
			r.logger.Warn("namespace teardown failed", zap.String("namespace", namespace), zap.Error(err))
		}
	}

	return out
}

func (r *Runner) failTopologyGroup(group topologyGroup, err error, namespace string) []results.TestResult {
	if r.logger != nil {
		r.logger.Error("topology group failed", zap.String("kind", group.kind), zap.String("namespace", namespace), zap.Error(err))
	}
	out := make([]results.TestResult, 0, len(group.specs))
	for _, testSpec := range group.specs {
		now := time.Now().UTC()
		result := results.TestResult{
			Name:        testSpec.Metadata.Name,
			Description: testSpec.Metadata.Description,
			Tags:        testSpec.Metadata.Tags,
			Status:      results.StatusFailed,
			StartTime:   now,
			EndTime:     now,
			Duration:    0,
			Requires:    testSpec.Requires,
			Metadata: map[string]string{
				"topology_error": err.Error(),
			},
		}
		if namespace != "" {
			result.Metadata["namespace"] = namespace
		}
		r.metrics.ObserveTest(string(result.Status), result.Duration)
		r.observeTestMetrics(testSpec, result)
		r.addGraphForTest(testSpec, result)
		out = append(out, result)
	}

	if namespace != "" && strings.ToLower(r.cfg.LogCollection) != "never" {
		_, _ = r.ensureNamespaceLogs(context.Background(), namespace)
		if r.cfg.OperatorNamespace != "" && r.cfg.OperatorNamespace != namespace {
			_, _ = r.ensureNamespaceLogs(context.Background(), r.cfg.OperatorNamespace)
		}
	}
	return out
}

func collectTopologyParams(testSpec spec.TestSpec) map[string]string {
	params := make(map[string]string)
	for key, value := range testSpec.Topology.Params {
		if strings.TrimSpace(value) != "" {
			params[key] = value
		}
	}
	for _, step := range testSpec.Steps {
		if step.Action != "topology.deploy" {
			continue
		}
		for key, value := range step.With {
			if _, exists := params[key]; exists {
				continue
			}
			if value == nil {
				continue
			}
			params[key] = fmt.Sprintf("%v", value)
		}
	}
	return params
}

func topologyKey(kind string, params map[string]string) string {
	parts := []string{strings.ToLower(strings.TrimSpace(kind))}
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, params[key]))
	}
	return strings.Join(parts, "|")
}

func intParam(params map[string]string, key string, fallback int) int {
	raw := strings.TrimSpace(params[key])
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

// int32Param safely parses a parameter as int32 with bounds checking.
// Returns fallback if the value is empty, invalid, or out of int32 range.
func int32Param(params map[string]string, key string, fallback int32) int32 {
	raw := strings.TrimSpace(params[key])
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return fallback
	}
	return int32(value)
}

func boolParam(params map[string]string, key string, fallback bool) bool {
	raw := strings.TrimSpace(params[key])
	if raw == "" {
		return fallback
	}
	switch strings.ToLower(raw) {
	case "true", "1", "yes", "y":
		return true
	case "false", "0", "no", "n":
		return false
	default:
		return fallback
	}
}

func defaultIndexerReplicas(kind string) int {
	switch strings.ToLower(kind) {
	case "m4":
		return 1
	case "c3":
		return 3
	default:
		return 1
	}
}

func defaultSHCReplicas(kind string) int {
	switch strings.ToLower(kind) {
	case "m4":
		return 3
	case "c3":
		return 3
	default:
		return 1
	}
}

func defaultSiteCount(kind string) int {
	switch strings.ToLower(kind) {
	case "m4", "m1":
		return 3
	}
	return 0
}
