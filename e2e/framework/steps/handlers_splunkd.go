package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/splunk/splunk-operator/e2e/framework/data"
	"github.com/splunk/splunk-operator/e2e/framework/spec"
)

// RegisterSplunkdHandlers registers Splunkd steps and assertions.
func RegisterSplunkdHandlers(reg *Registry) {
	reg.Register("splunk.status.check", handleStatusCheck)
	reg.Register("splunk.index.create", handleCreateIndex)
	reg.Register("splunk.index.roll_hot", handleIndexRollHot)
	reg.Register("splunk.ingest.oneshot", handleIngestOneshot)
	reg.Register("splunk.search.sync", handleSearchSync)
	reg.Register("splunk.search.req", handleSearchReq)
	reg.Register("splunk.search.wait", handleSearchWait)
	reg.Register("splunk.search.results", handleSearchResults)
	reg.Register("assert.search.count", handleAssertSearchCount)
	reg.Register("assert.search.contains", handleAssertSearchContains)
	reg.Register("assert.search.field", handleAssertSearchField)
	reg.Register("assert.search.results.raw_contains", handleAssertSearchResultsRawContains)
	reg.Register("assert.splunk.index.exists", handleAssertIndexExists)
}

func handleStatusCheck(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec.Splunkd == nil {
		return nil, fmt.Errorf("splunkd client not initialized")
	}
	ensureSplunkdSecret(exec, step)
	if err := exec.Splunkd.CheckStatus(ctx); err != nil {
		return nil, fmt.Errorf("splunk status failed: %w", err)
	}
	return map[string]string{"status": "running"}, nil
}

func handleCreateIndex(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	indexName := expandVars(getString(step.With, "index", ""), exec.Vars)
	if indexName == "" {
		return nil, fmt.Errorf("index is required")
	}
	if exec.Splunkd == nil {
		return nil, fmt.Errorf("splunkd client not initialized")
	}
	ensureSplunkdSecret(exec, step)
	if err := exec.Splunkd.CreateIndex(ctx, indexName); err != nil {
		return nil, err
	}
	return map[string]string{"index": indexName}, nil
}

func handleIndexRollHot(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	indexName := expandVars(getString(step.With, "index", ""), exec.Vars)
	if indexName == "" {
		return nil, fmt.Errorf("index is required")
	}
	if exec.Splunkd == nil {
		return nil, fmt.Errorf("splunkd client not initialized")
	}
	ensureSplunkdSecret(exec, step)
	client := exec.Splunkd
	if pod := strings.TrimSpace(getString(step.With, "pod", "")); pod != "" {
		client = client.WithPod(expandVars(pod, exec.Vars))
	}
	path := fmt.Sprintf("/services/data/indexes/%s/roll-hot-buckets", url.PathEscape(indexName))
	if _, err := client.ManagementRequest(ctx, "POST", path, url.Values{"output_mode": []string{"json"}}, nil); err != nil {
		return nil, err
	}
	return map[string]string{"index": indexName}, nil
}

func handleIngestOneshot(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	if exec.Splunkd == nil {
		return nil, fmt.Errorf("splunkd client not initialized")
	}
	ensureSplunkdSecret(exec, step)
	client := exec.Splunkd
	if pod := strings.TrimSpace(getString(step.With, "pod", "")); pod != "" {
		client = client.WithPod(expandVars(pod, exec.Vars))
	}

	var dataset data.Dataset
	var datasetName string
	var localPath string
	var err error
	if name := getString(step.With, "dataset", ""); name != "" {
		datasetName = name
		found, ok := exec.DatasetRegistry.Get(datasetName)
		if !ok {
			return nil, fmt.Errorf("dataset not found: %s", datasetName)
		}
		dataset = found
		cacheDir := filepath.Join(exec.Artifacts.RunDir, "datasets")
		localPath, err = data.Fetch(ctx, dataset, cacheDir, baseObjectstoreConfig(exec))
		if err != nil {
			return nil, err
		}
	} else if path := getString(step.With, "path", ""); path != "" {
		localPath = expandVars(path, exec.Vars)
	} else if path := exec.Vars["last_generated_path"]; path != "" {
		localPath = path
	} else {
		return nil, fmt.Errorf("dataset or path is required")
	}

	remotePath := filepath.Join("/tmp", filepath.Base(localPath))
	if err := client.CopyFile(ctx, localPath, remotePath); err != nil {
		return nil, err
	}

	indexName := expandVars(getString(step.With, "index", ""), exec.Vars)
	if indexName == "" {
		indexName = dataset.Index
	}
	if err := client.IngestOneshot(ctx, remotePath, indexName); err != nil {
		return nil, err
	}

	metadata := map[string]string{"index": indexName, "remote_path": remotePath}
	if datasetName != "" {
		metadata["dataset"] = datasetName
	}
	return metadata, nil
}

func handleAssertIndexExists(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	indexName := expandVars(getString(step.With, "index", ""), exec.Vars)
	if indexName == "" {
		return nil, fmt.Errorf("index is required")
	}
	if exec.Splunkd == nil {
		return nil, fmt.Errorf("splunkd client not initialized")
	}
	ensureSplunkdSecret(exec, step)
	expected := getBool(step.With, "exists", true)
	expectedMaxData := getInt(step.With, "max_global_data_size_mb", -1)
	expectedMaxRaw := getInt(step.With, "max_global_raw_data_size_mb", -1)

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
		found, entry, err := getIndexEntry(ctx, exec, indexName)
		if err != nil {
			return nil, err
		}
		match := found == expected
		if match && expected {
			if expectedMaxData >= 0 && entry.Content.MaxGlobalDataSizeMB != expectedMaxData {
				match = false
			}
			if expectedMaxRaw >= 0 && entry.Content.MaxGlobalRawDataSizeMB != expectedMaxRaw {
				match = false
			}
		}
		if match {
			metadata := map[string]string{"index": indexName}
			if expectedMaxData >= 0 {
				metadata["max_global_data_size_mb"] = fmt.Sprintf("%d", entry.Content.MaxGlobalDataSizeMB)
			}
			if expectedMaxRaw >= 0 {
				metadata["max_global_raw_data_size_mb"] = fmt.Sprintf("%d", entry.Content.MaxGlobalRawDataSizeMB)
			}
			return metadata, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("index %s existence/config did not reach expected state within %s", indexName, timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

func handleSearchSync(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	query := expandVars(getString(step.With, "query", ""), exec.Vars)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if exec.Splunkd == nil {
		return nil, fmt.Errorf("splunkd client not initialized")
	}
	ensureSplunkdSecret(exec, step)

	output, err := exec.Splunkd.PerformSearchSync(ctx, query)
	if err != nil {
		return nil, err
	}

	artifactName := fmt.Sprintf("search-%s.json", sanitize(step.Name))
	path, err := exec.Artifacts.WriteText(artifactName, output)
	if err != nil {
		return nil, err
	}

	exec.Vars["last_search_output_path"] = path
	return map[string]string{"artifact": path}, nil
}

func handleSearchReq(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	query := expandVars(getString(step.With, "query", ""), exec.Vars)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if exec.Splunkd == nil {
		return nil, fmt.Errorf("splunkd client not initialized")
	}
	ensureSplunkdSecret(exec, step)
	sid, err := exec.Splunkd.PerformSearchReq(ctx, query)
	if err != nil {
		return nil, err
	}
	exec.Vars["last_search_sid"] = sid
	return map[string]string{"sid": sid}, nil
}

func handleSearchWait(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	sid := expandVars(getString(step.With, "sid", exec.Vars["last_search_sid"]), exec.Vars)
	if sid == "" {
		return nil, fmt.Errorf("sid is required")
	}
	if exec.Splunkd == nil {
		return nil, fmt.Errorf("splunkd client not initialized")
	}
	ensureSplunkdSecret(exec, step)

	timeout := 2 * time.Minute
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
		done, err := exec.Splunkd.GetSearchStatus(ctx, sid)
		if err != nil {
			return nil, err
		}
		if done {
			return map[string]string{"sid": sid, "status": "done"}, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("search did not complete within %s", timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

func handleSearchResults(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	sid := expandVars(getString(step.With, "sid", exec.Vars["last_search_sid"]), exec.Vars)
	if sid == "" {
		return nil, fmt.Errorf("sid is required")
	}
	if exec.Splunkd == nil {
		return nil, fmt.Errorf("splunkd client not initialized")
	}
	ensureSplunkdSecret(exec, step)
	output, err := exec.Splunkd.GetSearchResults(ctx, sid)
	if err != nil {
		return nil, err
	}
	artifactName := fmt.Sprintf("search-results-%s.json", sanitize(step.Name))
	path, err := exec.Artifacts.WriteText(artifactName, output)
	if err != nil {
		return nil, err
	}
	exec.Vars["last_search_results_path"] = path
	return map[string]string{"artifact": path, "sid": sid}, nil
}

func handleAssertSearchCount(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	expected := getInt(step.With, "count", -1)
	if expected < 0 {
		return nil, fmt.Errorf("count is required")
	}

	path := expandVars(getString(step.With, "path", exec.Vars["last_search_output_path"]), exec.Vars)
	if path == "" {
		return nil, fmt.Errorf("search output path is required")
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	count, err := extractCountFromSearchResult(string(payload))
	if err != nil {
		return nil, err
	}
	if count != expected {
		return nil, fmt.Errorf("expected count %d, got %d", expected, count)
	}
	return map[string]string{"count": fmt.Sprintf("%d", count)}, nil
}

func handleAssertSearchContains(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	value := getString(step.With, "value", "")
	if value == "" {
		return nil, fmt.Errorf("value is required")
	}

	path := expandVars(getString(step.With, "path", exec.Vars["last_search_output_path"]), exec.Vars)
	if path == "" {
		return nil, fmt.Errorf("search output path is required")
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(string(payload), value) {
		return nil, fmt.Errorf("expected search output to contain %q", value)
	}
	return map[string]string{"contains": value}, nil
}

func handleAssertSearchField(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	field := getString(step.With, "field", "")
	if field == "" {
		return nil, fmt.Errorf("field is required")
	}
	expected := expandVars(getString(step.With, "value", ""), exec.Vars)
	if expected == "" {
		return nil, fmt.Errorf("value is required")
	}

	path := expandVars(getString(step.With, "path", exec.Vars["last_search_output_path"]), exec.Vars)
	if path == "" {
		return nil, fmt.Errorf("search output path is required")
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	actual, err := extractFieldFromSearchResult(string(payload), field)
	if err != nil {
		return nil, err
	}
	if actual != expected {
		return nil, fmt.Errorf("expected %s=%s, got %s", field, expected, actual)
	}
	return map[string]string{field: actual}, nil
}

func handleAssertSearchResultsRawContains(ctx context.Context, exec *Context, step spec.StepSpec) (map[string]string, error) {
	value := expandVars(getString(step.With, "value", ""), exec.Vars)
	if value == "" {
		return nil, fmt.Errorf("value is required")
	}
	path := expandVars(getString(step.With, "path", exec.Vars["last_search_results_path"]), exec.Vars)
	if path == "" {
		return nil, fmt.Errorf("search results path is required")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	found, err := searchResultsContainRaw(string(payload), value)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("expected search results to contain %q in _raw", value)
	}
	return map[string]string{"raw_contains": value}, nil
}

type dataIndexesResponse struct {
	Entry []dataIndexEntry `json:"entry"`
}

type dataIndexEntry struct {
	Name    string           `json:"name"`
	Content dataIndexContent `json:"content"`
}

type dataIndexContent struct {
	MaxGlobalDataSizeMB    int `json:"maxGlobalDataSizeMB"`
	MaxGlobalRawDataSizeMB int `json:"maxGlobalRawDataSizeMB"`
}

func getIndexEntry(ctx context.Context, exec *Context, indexName string) (bool, dataIndexEntry, error) {
	payload, err := exec.Splunkd.ManagementRequest(ctx, "GET", "/services/data/indexes", url.Values{"output_mode": []string{"json"}}, nil)
	if err != nil {
		return false, dataIndexEntry{}, err
	}
	resp := dataIndexesResponse{}
	if err := json.Unmarshal(payload, &resp); err != nil {
		return false, dataIndexEntry{}, err
	}
	for _, entry := range resp.Entry {
		if entry.Name == indexName {
			return true, entry, nil
		}
	}
	return false, dataIndexEntry{}, nil
}

func extractCountFromSearchResult(payload string) (int, error) {
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &decoded); err == nil {
		return readCountFromMap(decoded)
	}

	lines := strings.Split(payload, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		count, err := readCountFromMap(entry)
		if err == nil {
			return count, nil
		}
	}
	return 0, fmt.Errorf("unable to extract count from search output")
}

func readCountFromMap(decoded map[string]interface{}) (int, error) {
	result, ok := decoded["result"].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("missing result object")
	}
	countValue, ok := result["count"]
	if !ok {
		return 0, fmt.Errorf("missing count")
	}
	switch typed := countValue.(type) {
	case string:
		var parsed int
		_, err := fmt.Sscanf(typed, "%d", &parsed)
		if err != nil {
			return 0, err
		}
		return parsed, nil
	case float64:
		return int(typed), nil
	default:
		return 0, fmt.Errorf("unsupported count type")
	}
}

func extractFieldFromSearchResult(payload, field string) (string, error) {
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &decoded); err == nil {
		if value, err := readFieldFromMap(decoded, field); err == nil {
			return value, nil
		}
	}

	lines := strings.Split(payload, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		value, err := readFieldFromMap(entry, field)
		if err == nil {
			return value, nil
		}
	}
	return "", fmt.Errorf("unable to extract %s from search output", field)
}

func readFieldFromMap(decoded map[string]interface{}, field string) (string, error) {
	result, ok := decoded["result"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("missing result object")
	}
	value, ok := result[field]
	if !ok {
		return "", fmt.Errorf("missing %s", field)
	}
	switch typed := value.(type) {
	case string:
		return typed, nil
	case float64:
		return fmt.Sprintf("%.0f", typed), nil
	default:
		return fmt.Sprintf("%v", typed), nil
	}
}

func searchResultsContainRaw(payload, expected string) (bool, error) {
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return strings.Contains(payload, expected), nil
	}
	results, ok := decoded["results"].([]interface{})
	if !ok {
		return strings.Contains(payload, expected), nil
	}
	for _, entry := range results {
		record, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		raw, _ := record["_raw"].(string)
		if strings.Contains(raw, expected) {
			return true, nil
		}
	}
	return false, nil
}

func ensureSplunkdSecret(exec *Context, step spec.StepSpec) {
	if exec == nil || exec.Splunkd == nil {
		return
	}
	secretName := strings.TrimSpace(getString(step.With, "secret_name", ""))
	if secretName == "" {
		secretName = strings.TrimSpace(exec.Vars["secret_name"])
	}
	if secretName != "" {
		exec.Splunkd.SecretName = secretName
	}
}

func sanitize(value string) string {
	clean := strings.ToLower(value)
	clean = strings.ReplaceAll(clean, " ", "-")
	clean = strings.ReplaceAll(clean, "/", "-")
	if clean == "" {
		return "search"
	}
	return clean
}
