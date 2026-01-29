package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/e2e/framework/objectstore"
	"github.com/splunk/splunk-operator/e2e/framework/spec"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RegisterAppFrameworkHandlers registers app framework steps.
func RegisterAppFrameworkHandlers(reg *Registry) {
	reg.Register("appframework.spec.build", handleAppFrameworkSpecBuild)
	reg.Register("appframework.apply", handleAppFrameworkApply)
	reg.Register("appframework.phase.wait", handleAppFrameworkWaitPhase)
	reg.Register("appframework.status.wait", handleAppFrameworkWaitStatus)
	reg.Register("appframework.repo.assert", handleAppFrameworkAssertRepoState)
	reg.Register("appframework.deployment.assert", handleAppFrameworkAssertDeployment)
	reg.Register("appframework.bundle.assert", handleAppFrameworkAssertBundlePush)
	reg.Register("appframework.manual_poll.trigger", handleAppFrameworkManualPollTrigger)
	reg.Register("appframework.apps.assert", handleAppFrameworkAssertApps)
}

func handleAppFrameworkSpecBuild(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	spec, metadata, err := buildAppFrameworkSpec(exec, step.With)
	if err != nil {
		return nil, err
	}
	artifactName := fmt.Sprintf("appframework-%s.json", sanitize(exec.TestName))
	path, err := exec.Artifacts.WriteJSON(artifactName, spec)
	if err != nil {
		return nil, err
	}
	exec.Vars["last_appframework_spec_path"] = path
	exec.Vars["last_appframework_volume"] = spec.Defaults.VolName
	if metadata == nil {
		metadata = map[string]string{}
	}
	metadata["path"] = path
	return metadata, nil
}

func handleAppFrameworkApply(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	specPath := expandVars(getString(step.With, "spec_path", exec.Vars["last_appframework_spec_path"]), exec.Vars)
	var appSpec enterpriseApi.AppFrameworkSpec
	metadata := map[string]string{}
	if specPath != "" {
		payload, err := readAppFrameworkSpec(specPath)
		if err != nil {
			return nil, err
		}
		appSpec = payload
		metadata["spec_path"] = specPath
	} else {
		parsed, meta, err := buildAppFrameworkSpec(exec, step.With)
		if err != nil {
			return nil, err
		}
		appSpec = parsed
		for key, value := range meta {
			metadata[key] = value
		}
	}

	targetKind := expandVars(strings.TrimSpace(getString(step.With, "target_kind", "")), exec.Vars)
	if targetKind == "" {
		targetKind = guessAppFrameworkTargetKind(exec)
	}
	targetName := expandVars(strings.TrimSpace(getString(step.With, "target_name", "")), exec.Vars)
	if targetName == "" {
		targetName = guessAppFrameworkTargetName(exec, targetKind)
	}
	if targetKind == "" || targetName == "" {
		return nil, fmt.Errorf("target_kind and target_name are required")
	}
	replace := getBool(step.With, "replace", true)
	if err := applyAppFrameworkSpec(ctx, exec, targetKind, targetName, appSpec, replace); err != nil {
		return nil, err
	}
	metadata["target_kind"] = targetKind
	metadata["target_name"] = targetName
	return metadata, nil
}

func buildAppFrameworkSpec(exec *Context, params map[string]interface{}) (enterpriseApi.AppFrameworkSpec, map[string]string, error) {
	var spec enterpriseApi.AppFrameworkSpec
	metadata := map[string]string{}
	base := baseObjectstoreConfig(exec)

	providerRaw := strings.TrimSpace(getString(params, "provider", base.Provider))
	providerRaw = expandWithFallback(providerRaw, base.Provider, exec.Vars)
	providerKind := objectstore.NormalizeProvider(providerRaw)
	if providerKind == "" {
		return spec, nil, fmt.Errorf("appframework provider is required")
	}
	providerValue := normalizeAppFrameworkProvider(providerRaw, providerKind)
	storageType := strings.TrimSpace(getString(params, "storage_type", ""))
	storageType = expandWithFallback(storageType, "", exec.Vars)
	if storageType == "" {
		storageType = defaultStorageType(providerKind)
	}

	bucket := expandWithFallback(strings.TrimSpace(getString(params, "bucket", base.Bucket)), base.Bucket, exec.Vars)
	prefix := expandWithFallback(strings.TrimSpace(getString(params, "prefix", base.Prefix)), base.Prefix, exec.Vars)
	location := expandWithFallback(strings.TrimSpace(getString(params, "location", prefix)), prefix, exec.Vars)
	if location == "" {
		return spec, nil, fmt.Errorf("appframework app source location is required")
	}
	volumePath := expandWithFallback(strings.TrimSpace(getString(params, "volume_path", "")), "", exec.Vars)
	if volumePath == "" {
		if bucket == "" {
			return spec, nil, fmt.Errorf("appframework bucket is required")
		}
		volumePath = bucket
	}

	region := expandWithFallback(strings.TrimSpace(getString(params, "region", base.Region)), base.Region, exec.Vars)
	endpoint := expandWithFallback(strings.TrimSpace(getString(params, "endpoint", base.Endpoint)), base.Endpoint, exec.Vars)
	azureAccount := expandWithFallback(strings.TrimSpace(getString(params, "azure_account", base.AzureAccount)), base.AzureAccount, exec.Vars)
	if endpoint == "" {
		endpoint = defaultEndpoint(providerKind, region, azureAccount)
	}
	if endpoint == "" {
		return spec, nil, fmt.Errorf("appframework endpoint is required")
	}

	volumeName := expandWithFallback(strings.TrimSpace(getString(params, "volume_name", "")), "", exec.Vars)
	if volumeName == "" {
		volumeName = "appframework-vol"
	}
	secretRef := strings.TrimSpace(getString(params, "secret_ref", ""))
	secretRef = expandVars(secretRef, exec.Vars)

	volumes, err := parseVolumeSpecs(params, exec, providerValue, storageType, endpoint, region, volumePath, volumeName, secretRef)
	if err != nil {
		return spec, nil, err
	}

	defaultVolName := volumeName
	if len(volumes) > 0 {
		defaultVolName = volumes[0].Name
	}
	defaultScope := strings.TrimSpace(getString(params, "scope", enterpriseApi.ScopeLocal))
	defaults := enterpriseApi.AppSourceDefaultSpec{
		VolName: defaultVolName,
		Scope:   defaultScope,
	}
	if rawDefaults, ok := params["defaults"]; ok && rawDefaults != nil {
		if mapped, ok := toStringMap(rawDefaults); ok {
			if value := strings.TrimSpace(getString(mapped, "volume_name", "")); value != "" {
				defaults.VolName = expandVars(value, exec.Vars)
			}
			if value := strings.TrimSpace(getString(mapped, "scope", "")); value != "" {
				defaults.Scope = expandVars(value, exec.Vars)
			}
		}
	}

	appSources, err := parseAppSources(params, defaults, location, exec.Vars)
	if err != nil {
		return spec, nil, err
	}

	spec = enterpriseApi.AppFrameworkSpec{
		Defaults:                  defaults,
		AppsRepoPollInterval:      int64(getInt(params, "poll_interval", 0)),
		SchedulerYieldInterval:    uint64(getInt(params, "scheduler_yield_interval", 0)),
		PhaseMaxRetries:           uint32(getInt(params, "phase_max_retries", 0)),
		VolList:                   volumes,
		AppSources:                appSources,
		MaxConcurrentAppDownloads: uint64(getInt(params, "max_concurrent_downloads", 0)),
	}

	metadata["provider"] = providerValue
	metadata["storage_type"] = storageType
	metadata["bucket"] = bucket
	metadata["prefix"] = prefix
	metadata["endpoint"] = endpoint
	metadata["volume_name"] = defaultVolName
	metadata["app_source_location"] = location
	return spec, metadata, nil
}

func parseVolumeSpecs(params map[string]interface{}, exec *Context, providerValue, storageType, endpoint, region, volumePath, volumeName, secretRef string) ([]enterpriseApi.VolumeSpec, error) {
	if rawVolumes, ok := params["volumes"]; ok && rawVolumes != nil {
		items, ok := rawVolumes.([]interface{})
		if !ok {
			return nil, fmt.Errorf("volumes must be a list")
		}
		out := make([]enterpriseApi.VolumeSpec, 0, len(items))
		for _, item := range items {
			mapped, ok := toStringMap(item)
			if !ok {
				return nil, fmt.Errorf("volume entry must be a map")
			}
			volName := expandWithFallback(strings.TrimSpace(getString(mapped, "name", volumeName)), volumeName, exec.Vars)
			volEndpoint := expandWithFallback(strings.TrimSpace(getString(mapped, "endpoint", endpoint)), endpoint, exec.Vars)
			volPath := expandWithFallback(strings.TrimSpace(getString(mapped, "path", volumePath)), volumePath, exec.Vars)
			volProvider := expandWithFallback(strings.TrimSpace(getString(mapped, "provider", providerValue)), providerValue, exec.Vars)
			volType := expandWithFallback(strings.TrimSpace(getString(mapped, "storage_type", storageType)), storageType, exec.Vars)
			volRegion := expandWithFallback(strings.TrimSpace(getString(mapped, "region", region)), region, exec.Vars)
			volSecret := strings.TrimSpace(getString(mapped, "secret_ref", secretRef))
			volSecret = expandVars(volSecret, exec.Vars)
			if volName == "" || volEndpoint == "" || volPath == "" {
				return nil, fmt.Errorf("volume requires name, endpoint, and path")
			}
			out = append(out, enterpriseApi.VolumeSpec{
				Name:      volName,
				Endpoint:  volEndpoint,
				Path:      volPath,
				SecretRef: volSecret,
				Type:      volType,
				Provider:  volProvider,
				Region:    volRegion,
			})
		}
		return out, nil
	}
	if volumeName == "" || endpoint == "" || volumePath == "" {
		return nil, fmt.Errorf("volume_name, endpoint, and volume_path are required")
	}
	return []enterpriseApi.VolumeSpec{{
		Name:      expandVars(volumeName, exec.Vars),
		Endpoint:  expandVars(endpoint, exec.Vars),
		Path:      expandVars(volumePath, exec.Vars),
		SecretRef: expandVars(secretRef, exec.Vars),
		Type:      expandVars(storageType, exec.Vars),
		Provider:  expandVars(providerValue, exec.Vars),
		Region:    expandVars(region, exec.Vars),
	}}, nil
}

func parseAppSources(params map[string]interface{}, defaults enterpriseApi.AppSourceDefaultSpec, defaultLocation string, vars map[string]string) ([]enterpriseApi.AppSourceSpec, error) {
	if rawSources, ok := params["app_sources"]; ok && rawSources != nil {
		items, ok := rawSources.([]interface{})
		if !ok {
			return nil, fmt.Errorf("app_sources must be a list")
		}
		out := make([]enterpriseApi.AppSourceSpec, 0, len(items))
		for _, item := range items {
			mapped, ok := toStringMap(item)
			if !ok {
				return nil, fmt.Errorf("app_sources entry must be a map")
			}
			name := strings.TrimSpace(getString(mapped, "name", ""))
			name = expandVars(name, vars)
			if name == "" {
				return nil, fmt.Errorf("app source name is required")
			}
			location := strings.TrimSpace(getString(mapped, "location", defaultLocation))
			location = expandVars(location, vars)
			if location == "" {
				return nil, fmt.Errorf("app source location is required")
			}
			scope := strings.TrimSpace(getString(mapped, "scope", defaults.Scope))
			scope = expandVars(scope, vars)
			volName := strings.TrimSpace(getString(mapped, "volume_name", defaults.VolName))
			volName = expandVars(volName, vars)
			appSource := enterpriseApi.AppSourceSpec{
				Name:     name,
				Location: location,
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: volName,
					Scope:   scope,
				},
			}
			out = append(out, appSource)
		}
		return out, nil
	}

	name := strings.TrimSpace(getString(params, "app_source_name", ""))
	name = expandVars(name, vars)
	if name == "" {
		name = "appsource"
	}
	location := strings.TrimSpace(getString(params, "location", defaultLocation))
	location = expandVars(location, vars)
	if location == "" {
		return nil, fmt.Errorf("app source location is required")
	}
	appSource := enterpriseApi.AppSourceSpec{
		Name:     name,
		Location: location,
		AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
			VolName: defaults.VolName,
			Scope:   defaults.Scope,
		},
	}
	return []enterpriseApi.AppSourceSpec{appSource}, nil
}

func applyAppFrameworkSpec(ctx context.Context, exec *Context, kind, name string, spec enterpriseApi.AppFrameworkSpec, replace bool) error {
	namespace := strings.TrimSpace(exec.Vars["namespace"])
	if namespace == "" {
		return fmt.Errorf("namespace not set")
	}
	switch strings.ToLower(kind) {
	case "standalone":
		target := &enterpriseApi.Standalone{}
		if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, target); err != nil {
			return err
		}
		target.Spec.AppFrameworkConfig = mergeAppFramework(target.Spec.AppFrameworkConfig, spec, replace)
		return exec.Kube.Client.Update(ctx, target)
	case "clustermanager", "cluster_manager":
		target := &enterpriseApi.ClusterManager{}
		if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, target); err != nil {
			return err
		}
		target.Spec.AppFrameworkConfig = mergeAppFramework(target.Spec.AppFrameworkConfig, spec, replace)
		return exec.Kube.Client.Update(ctx, target)
	case "cluster_master", "clustermaster":
		target := &enterpriseApiV3.ClusterMaster{}
		if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, target); err != nil {
			return err
		}
		target.Spec.AppFrameworkConfig = mergeAppFramework(target.Spec.AppFrameworkConfig, spec, replace)
		return exec.Kube.Client.Update(ctx, target)
	case "searchheadcluster", "search_head_cluster":
		target := &enterpriseApi.SearchHeadCluster{}
		if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, target); err != nil {
			return err
		}
		target.Spec.AppFrameworkConfig = mergeAppFramework(target.Spec.AppFrameworkConfig, spec, replace)
		return exec.Kube.Client.Update(ctx, target)
	case "monitoringconsole", "monitoring_console":
		target := &enterpriseApi.MonitoringConsole{}
		if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, target); err != nil {
			return err
		}
		target.Spec.AppFrameworkConfig = mergeAppFramework(target.Spec.AppFrameworkConfig, spec, replace)
		return exec.Kube.Client.Update(ctx, target)
	case "licensemanager", "license_manager":
		target := &enterpriseApi.LicenseManager{}
		if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, target); err != nil {
			return err
		}
		target.Spec.AppFrameworkConfig = mergeAppFramework(target.Spec.AppFrameworkConfig, spec, replace)
		return exec.Kube.Client.Update(ctx, target)
	case "licensemaster", "license_master":
		target := &enterpriseApiV3.LicenseMaster{}
		if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, target); err != nil {
			return err
		}
		target.Spec.AppFrameworkConfig = mergeAppFramework(target.Spec.AppFrameworkConfig, spec, replace)
		return exec.Kube.Client.Update(ctx, target)
	default:
		return fmt.Errorf("unsupported target kind: %s", kind)
	}
}

func mergeAppFramework(existing, updated enterpriseApi.AppFrameworkSpec, replace bool) enterpriseApi.AppFrameworkSpec {
	if replace {
		return updated
	}
	out := existing
	if updated.Defaults.VolName != "" {
		out.Defaults.VolName = updated.Defaults.VolName
	}
	if updated.Defaults.Scope != "" {
		out.Defaults.Scope = updated.Defaults.Scope
	}
	if updated.AppsRepoPollInterval != 0 {
		out.AppsRepoPollInterval = updated.AppsRepoPollInterval
	}
	if updated.SchedulerYieldInterval != 0 {
		out.SchedulerYieldInterval = updated.SchedulerYieldInterval
	}
	if updated.PhaseMaxRetries != 0 {
		out.PhaseMaxRetries = updated.PhaseMaxRetries
	}
	if updated.MaxConcurrentAppDownloads != 0 {
		out.MaxConcurrentAppDownloads = updated.MaxConcurrentAppDownloads
	}
	if len(updated.VolList) > 0 {
		out.VolList = mergeVolumes(out.VolList, updated.VolList)
	}
	if len(updated.AppSources) > 0 {
		out.AppSources = mergeAppSources(out.AppSources, updated.AppSources)
	}
	return out
}

func mergeVolumes(existing, updated []enterpriseApi.VolumeSpec) []enterpriseApi.VolumeSpec {
	out := make([]enterpriseApi.VolumeSpec, 0, len(existing)+len(updated))
	index := make(map[string]int, len(existing))
	for i, vol := range existing {
		out = append(out, vol)
		index[vol.Name] = i
	}
	for _, vol := range updated {
		if idx, ok := index[vol.Name]; ok {
			out[idx] = vol
			continue
		}
		out = append(out, vol)
	}
	return out
}

func mergeAppSources(existing, updated []enterpriseApi.AppSourceSpec) []enterpriseApi.AppSourceSpec {
	out := make([]enterpriseApi.AppSourceSpec, 0, len(existing)+len(updated))
	index := make(map[string]int, len(existing))
	for i, src := range existing {
		out = append(out, src)
		index[src.Name] = i
	}
	for _, src := range updated {
		if idx, ok := index[src.Name]; ok {
			out[idx] = src
			continue
		}
		out = append(out, src)
	}
	return out
}

func handleAppFrameworkWaitPhase(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	targetKind := expandVars(strings.TrimSpace(getString(step.With, "target_kind", "")), exec.Vars)
	if targetKind == "" {
		targetKind = guessAppFrameworkTargetKind(exec)
	}
	targetName := expandVars(strings.TrimSpace(getString(step.With, "target_name", "")), exec.Vars)
	if targetName == "" {
		targetName = guessAppFrameworkTargetName(exec, targetKind)
	}
	if targetKind == "" || targetName == "" {
		return nil, fmt.Errorf("target_kind and target_name are required")
	}
	appSource := expandVars(strings.TrimSpace(getString(step.With, "app_source", "")), exec.Vars)
	if appSource == "" {
		return nil, fmt.Errorf("app_source is required")
	}
	apps, err := getStringList(step.With, "apps")
	if err != nil {
		return nil, err
	}
	apps = expandStringSlice(apps, exec.Vars)
	if len(apps) == 0 {
		return nil, fmt.Errorf("apps are required")
	}
	phaseRaw := strings.TrimSpace(getString(step.With, "phase", ""))
	if phaseRaw == "" {
		return nil, fmt.Errorf("phase is required")
	}
	phase, err := parseAppPhase(phaseRaw)
	if err != nil {
		return nil, err
	}
	statusRaw := strings.TrimSpace(getString(step.With, "status", ""))
	var expectedStatus *enterpriseApi.AppPhaseStatusType
	if statusRaw != "" {
		parsed, err := parsePhaseStatus(statusRaw)
		if err != nil {
			return nil, err
		}
		expectedStatus = &parsed
	}
	timeout := exec.Config.DefaultTimeout
	if raw := getString(step.With, "timeout", ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			timeout = parsed
		}
	}
	interval := 5 * time.Second
	if raw := getString(step.With, "interval", ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			interval = parsed
		}
	}
	deadline := time.Now().Add(timeout)
	for {
		allReady := true
		for _, app := range apps {
			info, err := getAppDeploymentInfo(ctx, exec, targetKind, targetName, appSource, app)
			if err != nil {
				return nil, err
			}
			if expectedStatus != nil {
				if info.PhaseInfo.Status != *expectedStatus {
					allReady = false
				}
				continue
			}
			if phase == enterpriseApi.PhaseDownload || phase == enterpriseApi.PhasePodCopy {
				if info.PhaseInfo.Phase == phase {
					allReady = false
				}
				continue
			}
			if info.PhaseInfo.Phase != phase || info.PhaseInfo.Status != enterpriseApi.AppPkgInstallComplete {
				allReady = false
			}
		}
		if allReady {
			return map[string]string{"target_kind": targetKind, "target_name": targetName, "phase": string(phase)}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("app phase did not reach expected state within %s", timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

func handleAppFrameworkWaitStatus(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	targetKind := expandVars(strings.TrimSpace(getString(step.With, "target_kind", "")), exec.Vars)
	if targetKind == "" {
		targetKind = guessAppFrameworkTargetKind(exec)
	}
	targetName := expandVars(strings.TrimSpace(getString(step.With, "target_name", "")), exec.Vars)
	if targetName == "" {
		targetName = guessAppFrameworkTargetName(exec, targetKind)
	}
	if targetKind == "" || targetName == "" {
		return nil, fmt.Errorf("target_kind and target_name are required")
	}
	appSource := expandVars(strings.TrimSpace(getString(step.With, "app_source", "")), exec.Vars)
	if appSource == "" {
		return nil, fmt.Errorf("app_source is required")
	}
	apps, err := getStringList(step.With, "apps")
	if err != nil {
		return nil, err
	}
	apps = expandStringSlice(apps, exec.Vars)
	if len(apps) == 0 {
		return nil, fmt.Errorf("apps are required")
	}
	statusRaw := strings.TrimSpace(getString(step.With, "status", ""))
	minRaw := strings.TrimSpace(getString(step.With, "min", ""))
	maxRaw := strings.TrimSpace(getString(step.With, "max", ""))
	var expected *enterpriseApi.AppPhaseStatusType
	if statusRaw != "" {
		parsed, err := parsePhaseStatus(statusRaw)
		if err != nil {
			return nil, err
		}
		expected = &parsed
	}
	var minStatus, maxStatus *enterpriseApi.AppPhaseStatusType
	if minRaw != "" {
		parsed, err := parsePhaseStatus(minRaw)
		if err != nil {
			return nil, err
		}
		minStatus = &parsed
	}
	if maxRaw != "" {
		parsed, err := parsePhaseStatus(maxRaw)
		if err != nil {
			return nil, err
		}
		maxStatus = &parsed
	}
	if expected == nil && (minStatus == nil || maxStatus == nil) {
		return nil, fmt.Errorf("status or min/max are required")
	}
	timeout := exec.Config.DefaultTimeout
	if raw := getString(step.With, "timeout", ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			timeout = parsed
		}
	}
	interval := 5 * time.Second
	if raw := getString(step.With, "interval", ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			interval = parsed
		}
	}
	deadline := time.Now().Add(timeout)
	for {
		allReady := true
		for _, app := range apps {
			info, err := getAppDeploymentInfo(ctx, exec, targetKind, targetName, appSource, app)
			if err != nil {
				return nil, err
			}
			if expected != nil {
				if info.PhaseInfo.Status != *expected {
					allReady = false
				}
				continue
			}
			status := info.PhaseInfo.Status
			if status < *minStatus || status > *maxStatus {
				allReady = false
			}
		}
		if allReady {
			return map[string]string{"target_kind": targetKind, "target_name": targetName}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("app status did not reach expected range within %s", timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

func handleAppFrameworkAssertRepoState(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	targetKind := expandVars(strings.TrimSpace(getString(step.With, "target_kind", "")), exec.Vars)
	if targetKind == "" {
		targetKind = guessAppFrameworkTargetKind(exec)
	}
	targetName := expandVars(strings.TrimSpace(getString(step.With, "target_name", "")), exec.Vars)
	if targetName == "" {
		targetName = guessAppFrameworkTargetName(exec, targetKind)
	}
	if targetKind == "" || targetName == "" {
		return nil, fmt.Errorf("target_kind and target_name are required")
	}
	appSource := expandVars(strings.TrimSpace(getString(step.With, "app_source", "")), exec.Vars)
	if appSource == "" {
		return nil, fmt.Errorf("app_source is required")
	}
	apps, err := getStringList(step.With, "apps")
	if err != nil {
		return nil, err
	}
	apps = expandStringSlice(apps, exec.Vars)
	if len(apps) == 0 {
		return nil, fmt.Errorf("apps are required")
	}
	stateRaw := strings.TrimSpace(getString(step.With, "state", ""))
	if stateRaw == "" {
		return nil, fmt.Errorf("state is required")
	}
	state, err := parseRepoState(stateRaw)
	if err != nil {
		return nil, err
	}
	for _, app := range apps {
		info, err := getAppDeploymentInfo(ctx, exec, targetKind, targetName, appSource, app)
		if err != nil {
			return nil, err
		}
		if info.RepoState != state {
			return nil, fmt.Errorf("repo state mismatch for app %s expected=%d actual=%d", app, state, info.RepoState)
		}
	}
	return map[string]string{"target_kind": targetKind, "target_name": targetName}, nil
}

func handleAppFrameworkAssertDeployment(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	targetKind := expandVars(strings.TrimSpace(getString(step.With, "target_kind", "")), exec.Vars)
	if targetKind == "" {
		targetKind = guessAppFrameworkTargetKind(exec)
	}
	targetName := expandVars(strings.TrimSpace(getString(step.With, "target_name", "")), exec.Vars)
	if targetName == "" {
		targetName = guessAppFrameworkTargetName(exec, targetKind)
	}
	if targetKind == "" || targetName == "" {
		return nil, fmt.Errorf("target_kind and target_name are required")
	}
	expected := getBool(step.With, "in_progress", true)
	appContext, err := getAppContext(ctx, exec, targetKind, targetName)
	if err != nil {
		return nil, err
	}
	if appContext.IsDeploymentInProgress != expected {
		return nil, fmt.Errorf("deployment in progress mismatch expected=%t actual=%t", expected, appContext.IsDeploymentInProgress)
	}
	return map[string]string{"target_kind": targetKind, "target_name": targetName}, nil
}

func handleAppFrameworkAssertBundlePush(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	targetKind := expandVars(strings.TrimSpace(getString(step.With, "target_kind", "")), exec.Vars)
	if targetKind == "" {
		targetKind = guessAppFrameworkTargetKind(exec)
	}
	targetName := expandVars(strings.TrimSpace(getString(step.With, "target_name", "")), exec.Vars)
	if targetName == "" {
		targetName = guessAppFrameworkTargetName(exec, targetKind)
	}
	if targetKind == "" || targetName == "" {
		return nil, fmt.Errorf("target_kind and target_name are required")
	}
	stageRaw := strings.TrimSpace(getString(step.With, "stage", "complete"))
	stage, err := parseBundlePushStage(stageRaw)
	if err != nil {
		return nil, err
	}
	appContext, err := getAppContext(ctx, exec, targetKind, targetName)
	if err != nil {
		return nil, err
	}
	if appContext.BundlePushStatus.BundlePushStage != stage {
		return nil, fmt.Errorf("bundle push stage mismatch expected=%d actual=%d", stage, appContext.BundlePushStatus.BundlePushStage)
	}
	return map[string]string{"target_kind": targetKind, "target_name": targetName}, nil
}

func handleAppFrameworkManualPollTrigger(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Kube == nil {
		return nil, fmt.Errorf("kube client not available")
	}
	namespace := expandVars(strings.TrimSpace(getString(step.With, "namespace", exec.Vars["namespace"])), exec.Vars)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	keys, err := getStringList(step.With, "keys")
	if err != nil {
		return nil, err
	}
	keys = expandStringSlice(keys, exec.Vars)
	if len(keys) == 0 {
		return nil, fmt.Errorf("keys are required")
	}
	configName := fmt.Sprintf("splunk-%s-manual-app-update", namespace)
	config := &corev1.ConfigMap{}
	if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Name: configName, Namespace: namespace}, config); err != nil {
		return nil, err
	}
	if config.Data == nil {
		return nil, fmt.Errorf("configmap %s has no data", configName)
	}
	for _, key := range keys {
		value := config.Data[key]
		if value == "" {
			return nil, fmt.Errorf("configmap %s missing key %s", configName, key)
		}
		config.Data[key] = strings.Replace(value, "status: off", "status: on", 1)
	}
	if err := exec.Kube.Client.Update(ctx, config); err != nil {
		return nil, err
	}
	waitOff := getBool(step.With, "wait_off", false)
	if !waitOff {
		return map[string]string{"configmap": configName}, nil
	}
	timeout := exec.Config.DefaultTimeout
	if raw := getString(step.With, "timeout", ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			timeout = parsed
		}
	}
	interval := 5 * time.Second
	if raw := getString(step.With, "interval", ""); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			interval = parsed
		}
	}
	deadline := time.Now().Add(timeout)
	for {
		config := &corev1.ConfigMap{}
		if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Name: configName, Namespace: namespace}, config); err != nil {
			return nil, err
		}
		allOff := true
		for _, key := range keys {
			if !strings.Contains(config.Data[key], "status: off") {
				allOff = false
				break
			}
		}
		if allOff {
			return map[string]string{"configmap": configName}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("manual poll did not reset to off within %s", timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

func handleAppFrameworkAssertApps(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec == nil || exec.Splunkd == nil {
		return nil, fmt.Errorf("splunkd client not initialized")
	}
	ensureSplunkdSecret(exec, step)
	pods, err := getStringList(step.With, "pods")
	if err != nil {
		return nil, err
	}
	if len(pods) == 0 {
		if pod := strings.TrimSpace(getString(step.With, "pod", "")); pod != "" {
			pods = []string{pod}
		}
	}
	pods = expandStringSlice(pods, exec.Vars)
	if len(pods) == 0 {
		return nil, fmt.Errorf("pod or pods are required")
	}
	apps, err := getStringList(step.With, "apps")
	if err != nil {
		return nil, err
	}
	apps = expandStringSlice(apps, exec.Vars)
	if len(apps) == 0 {
		return nil, fmt.Errorf("apps are required")
	}
	expectedEnabled := getBool(step.With, "enabled", true)
	expectedVersion := strings.TrimSpace(getString(step.With, "version", ""))
	for _, pod := range pods {
		client := exec.Splunkd.WithPod(pod)
		for _, app := range apps {
			info, err := client.GetAppInfo(ctx, app)
			if err != nil {
				return nil, err
			}
			if info.Disabled == expectedEnabled {
				return nil, fmt.Errorf("app state mismatch for %s on pod %s", app, pod)
			}
			if expectedVersion != "" && info.Version != "" && info.Version != expectedVersion {
				return nil, fmt.Errorf("app version mismatch for %s on pod %s expected=%s actual=%s", app, pod, expectedVersion, info.Version)
			}
		}
	}
	return map[string]string{"pods": strings.Join(pods, ",")}, nil
}

func readAppFrameworkSpec(path string) (enterpriseApi.AppFrameworkSpec, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return enterpriseApi.AppFrameworkSpec{}, err
	}
	spec := enterpriseApi.AppFrameworkSpec{}
	if err := json.Unmarshal(payload, &spec); err != nil {
		return enterpriseApi.AppFrameworkSpec{}, err
	}
	return spec, nil
}

func guessAppFrameworkTargetKind(exec *Context) string {
	if exec == nil {
		return ""
	}
	switch exec.Vars["topology_kind"] {
	case "s1":
		return "standalone"
	case "c3", "m4", "m1":
		if strings.EqualFold(exec.Vars["cluster_manager_kind"], "master") {
			return "cluster_master"
		}
		return "cluster_manager"
	default:
		return ""
	}
}

func guessAppFrameworkTargetName(exec *Context, kind string) string {
	if exec == nil {
		return ""
	}
	switch strings.ToLower(kind) {
	case "standalone":
		return exec.Vars["standalone_name"]
	case "clustermanager", "cluster_manager", "cluster_master", "clustermaster":
		if exec.Vars["cluster_manager_name"] != "" {
			return exec.Vars["cluster_manager_name"]
		}
		return exec.Vars["base_name"]
	case "searchheadcluster", "search_head_cluster":
		return exec.Vars["search_head_cluster_name"]
	case "monitoringconsole", "monitoring_console":
		return exec.Vars["monitoring_console_name"]
	case "licensemanager", "license_manager":
		return exec.Vars["license_manager_name"]
	case "licensemaster", "license_master":
		return exec.Vars["license_master_name"]
	case "ingestorcluster", "ingestor_cluster":
		return exec.Vars["ingestor_cluster_name"]
	default:
		return ""
	}
}

func normalizeAppFrameworkProvider(raw, kind string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch kind {
	case "s3":
		if value == "minio" {
			return "minio"
		}
		return "aws"
	case "gcs":
		return "gcp"
	case "azure":
		return "azure"
	default:
		return value
	}
}

func defaultStorageType(kind string) string {
	switch kind {
	case "s3":
		return "s3"
	case "gcs":
		return "gcs"
	case "azure":
		return "blob"
	default:
		return ""
	}
}

func defaultEndpoint(kind, region, azureAccount string) string {
	switch kind {
	case "s3":
		region = strings.TrimSpace(region)
		if region == "" {
			region = "us-west-2"
		}
		return fmt.Sprintf("https://s3-%s.amazonaws.com", region)
	case "gcs":
		return "https://storage.googleapis.com"
	case "azure":
		if strings.TrimSpace(azureAccount) == "" {
			return ""
		}
		return fmt.Sprintf("https://%s.blob.core.windows.net", strings.TrimSpace(azureAccount))
	default:
		return ""
	}
}

func getAppContext(ctx context.Context, exec *Context, kind, name string) (enterpriseApi.AppDeploymentContext, error) {
	if exec == nil || exec.Kube == nil {
		return enterpriseApi.AppDeploymentContext{}, fmt.Errorf("kube client not available")
	}
	namespace := strings.TrimSpace(exec.Vars["namespace"])
	if namespace == "" {
		return enterpriseApi.AppDeploymentContext{}, fmt.Errorf("namespace not set")
	}
	switch strings.ToLower(kind) {
	case "standalone":
		cr := &enterpriseApi.Standalone{}
		if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, cr); err != nil {
			return enterpriseApi.AppDeploymentContext{}, err
		}
		return cr.Status.AppContext, nil
	case "clustermanager", "cluster_manager":
		cr := &enterpriseApi.ClusterManager{}
		if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, cr); err != nil {
			return enterpriseApi.AppDeploymentContext{}, err
		}
		return cr.Status.AppContext, nil
	case "cluster_master", "clustermaster":
		cr := &enterpriseApiV3.ClusterMaster{}
		if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, cr); err != nil {
			return enterpriseApi.AppDeploymentContext{}, err
		}
		return cr.Status.AppContext, nil
	case "searchheadcluster", "search_head_cluster":
		cr := &enterpriseApi.SearchHeadCluster{}
		if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, cr); err != nil {
			return enterpriseApi.AppDeploymentContext{}, err
		}
		return cr.Status.AppContext, nil
	case "monitoringconsole", "monitoring_console":
		cr := &enterpriseApi.MonitoringConsole{}
		if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, cr); err != nil {
			return enterpriseApi.AppDeploymentContext{}, err
		}
		return cr.Status.AppContext, nil
	case "licensemanager", "license_manager":
		cr := &enterpriseApi.LicenseManager{}
		if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, cr); err != nil {
			return enterpriseApi.AppDeploymentContext{}, err
		}
		return cr.Status.AppContext, nil
	case "licensemaster", "license_master":
		cr := &enterpriseApiV3.LicenseMaster{}
		if err := exec.Kube.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, cr); err != nil {
			return enterpriseApi.AppDeploymentContext{}, err
		}
		return cr.Status.AppContext, nil
	default:
		return enterpriseApi.AppDeploymentContext{}, fmt.Errorf("unsupported target kind: %s", kind)
	}
}

func getAppDeploymentInfo(ctx context.Context, exec *Context, kind, name, appSource, appName string) (enterpriseApi.AppDeploymentInfo, error) {
	appContext, err := getAppContext(ctx, exec, kind, name)
	if err != nil {
		return enterpriseApi.AppDeploymentInfo{}, err
	}
	source := appContext.AppsSrcDeployStatus[appSource]
	for _, info := range source.AppDeploymentInfoList {
		if strings.Contains(appName, info.AppName) || strings.Contains(info.AppName, appName) {
			return info, nil
		}
	}
	return enterpriseApi.AppDeploymentInfo{}, fmt.Errorf("app deployment info not found for %s", appName)
}

func parseAppPhase(value string) (enterpriseApi.AppPhaseType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "download":
		return enterpriseApi.PhaseDownload, nil
	case "podcopy", "pod_copy":
		return enterpriseApi.PhasePodCopy, nil
	case "install":
		return enterpriseApi.PhaseInstall, nil
	default:
		return "", fmt.Errorf("unsupported phase: %s", value)
	}
}

func parsePhaseStatus(value string) (enterpriseApi.AppPhaseStatusType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "download_pending":
		return enterpriseApi.AppPkgDownloadPending, nil
	case "download_in_progress":
		return enterpriseApi.AppPkgDownloadInProgress, nil
	case "download_complete":
		return enterpriseApi.AppPkgDownloadComplete, nil
	case "pod_copy_pending":
		return enterpriseApi.AppPkgPodCopyPending, nil
	case "pod_copy_in_progress":
		return enterpriseApi.AppPkgPodCopyInProgress, nil
	case "pod_copy_complete":
		return enterpriseApi.AppPkgPodCopyComplete, nil
	case "install_pending":
		return enterpriseApi.AppPkgInstallPending, nil
	case "install_in_progress":
		return enterpriseApi.AppPkgInstallInProgress, nil
	case "install_complete":
		return enterpriseApi.AppPkgInstallComplete, nil
	}
	if raw := strings.TrimSpace(value); raw != "" {
		var parsed int
		if _, err := fmt.Sscanf(raw, "%d", &parsed); err == nil {
			return enterpriseApi.AppPhaseStatusType(parsed), nil
		}
	}
	return 0, fmt.Errorf("unsupported phase status: %s", value)
}

func parseRepoState(value string) (enterpriseApi.AppRepoState, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "active":
		return enterpriseApi.RepoStateActive, nil
	case "deleted":
		return enterpriseApi.RepoStateDeleted, nil
	case "passive":
		return enterpriseApi.RepoStatePassive, nil
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err == nil {
		return enterpriseApi.AppRepoState(parsed), nil
	}
	return 0, fmt.Errorf("unsupported repo state: %s", value)
}

func parseBundlePushStage(value string) (enterpriseApi.BundlePushStageType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "pending":
		return enterpriseApi.BundlePushPending, nil
	case "in_progress":
		return enterpriseApi.BundlePushInProgress, nil
	case "complete":
		return enterpriseApi.BundlePushComplete, nil
	case "uninitialized":
		return enterpriseApi.BundlePushUninitialized, nil
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err == nil {
		return enterpriseApi.BundlePushStageType(parsed), nil
	}
	return 0, fmt.Errorf("unsupported bundle push stage: %s", value)
}

func toStringMap(value interface{}) (map[string]interface{}, bool) {
	switch typed := value.(type) {
	case map[string]interface{}:
		return typed, true
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(typed))
		for key, val := range typed {
			out[fmt.Sprintf("%v", key)] = val
		}
		return out, true
	default:
		return nil, false
	}
}

func expandWithFallback(value, fallback string, vars map[string]string) string {
	if value == "" {
		return fallback
	}
	expanded := expandVars(value, vars)
	if strings.TrimSpace(expanded) == "" {
		return fallback
	}
	return expanded
}
