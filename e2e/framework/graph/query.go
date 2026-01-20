package graph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// QueryInterface provides high-level query methods for support teams
type QueryInterface struct {
	driver neo4j.DriverWithContext
	database string
}

// NewQueryInterface creates a new query interface
func NewQueryInterface(uri, user, password, database string) (*QueryInterface, error) {
	auth := neo4j.NoAuth()
	if user != "" || password != "" {
		auth = neo4j.BasicAuth(user, password, "")
	}

	driver, err := neo4j.NewDriverWithContext(uri, auth)
	if err != nil {
		return nil, err
	}

	if database == "" {
		database = "neo4j"
	}

	return &QueryInterface{
		driver:   driver,
		database: database,
	}, nil
}

// Close closes the driver connection
func (qi *QueryInterface) Close(ctx context.Context) error {
	return qi.driver.Close(ctx)
}

// TestFailureInfo contains information about a test failure
type TestFailureInfo struct {
	TestName       string    `json:"test_name"`
	Status         string    `json:"status"`
	Timestamp      time.Time `json:"timestamp"`
	ErrorCategory  string    `json:"error_category,omitempty"`
	OperatorImage  string    `json:"operator_image"`
	SplunkImage    string    `json:"splunk_image"`
	ClusterProvider string   `json:"cluster_provider"`
	K8sVersion     string    `json:"k8s_version"`
	Topology       string    `json:"topology,omitempty"`
}

// FindSimilarFailures finds tests that failed with similar errors
func (qi *QueryInterface) FindSimilarFailures(ctx context.Context, errorCategory string, limit int) ([]TestFailureInfo, error) {
	session := qi.driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: qi.database,
		AccessMode:   neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	query := `
		MATCH (t:E2E {type: 'test', status: 'failed'})-[:HAS_FAILURE_ANALYSIS]->(fa:E2E {type: 'failure_analysis'})
		WHERE fa.error_category = $category
		OPTIONAL MATCH (t)-[:USES_OPERATOR_IMAGE]->(op:E2E {type: 'image'})
		OPTIONAL MATCH (t)-[:USES_SPLUNK_IMAGE]->(sp:E2E {type: 'image'})
		OPTIONAL MATCH (t)-[:RUNS_ON]->(cl:E2E {type: 'cluster'})
		RETURN t.label AS test_name,
		       t.status AS status,
		       fa.error_category AS error_category,
		       op.label AS operator_image,
		       sp.label AS splunk_image,
		       cl.label AS cluster_provider
		LIMIT $limit
	`

	result, err := session.Run(ctx, query, map[string]any{
		"category": errorCategory,
		"limit":    limit,
	})
	if err != nil {
		return nil, err
	}

	var failures []TestFailureInfo
	for result.Next(ctx) {
		record := result.Record()
		failure := TestFailureInfo{
			TestName:        getStringValue(record, "test_name"),
			Status:          getStringValue(record, "status"),
			ErrorCategory:   getStringValue(record, "error_category"),
			OperatorImage:   getStringValue(record, "operator_image"),
			SplunkImage:     getStringValue(record, "splunk_image"),
			ClusterProvider: getStringValue(record, "cluster_provider"),
		}
		failures = append(failures, failure)
	}

	return failures, result.Err()
}

// FindResolutionsForError finds documented resolutions for an error category
func (qi *QueryInterface) FindResolutionsForError(ctx context.Context, errorCategory string) ([]map[string]interface{}, error) {
	session := qi.driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: qi.database,
		AccessMode:   neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	query := `
		MATCH (ep:E2E {type: 'error_pattern', label: $category})-[:HAS_RESOLUTION]->(r:E2E {type: 'resolution'})
		WHERE r.verified = true
		RETURN r
		ORDER BY r.created_at DESC
	`

	result, err := session.Run(ctx, query, map[string]any{
		"category": errorCategory,
	})
	if err != nil {
		return nil, err
	}

	var resolutions []map[string]interface{}
	for result.Next(ctx) {
		record := result.Record()
		if val, ok := record.Get("r"); ok {
			if node, ok := val.(neo4j.Node); ok {
				resolutions = append(resolutions, node.Props)
			}
		}
	}

	return resolutions, result.Err()
}

// FindUntestedCombinations finds combinations of versions/providers that haven't been tested
func (qi *QueryInterface) FindUntestedCombinations(ctx context.Context) ([]map[string]string, error) {
	session := qi.driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: qi.database,
		AccessMode:   neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	query := `
		MATCH (sp:E2E {type: 'image'}), (cl:E2E {type: 'cluster'})
		WHERE sp.id STARTS WITH 'image:splunk:' AND cl.id STARTS WITH 'cluster:'
		WITH sp, cl
		WHERE NOT exists {
			MATCH (t:E2E {type: 'test'})-[:USES_SPLUNK_IMAGE]->(sp)
			MATCH (t)-[:RUNS_ON]->(cl)
		}
		RETURN sp.label AS splunk_image, cl.label AS cluster_provider
		LIMIT 50
	`

	result, err := session.Run(ctx, query, nil)
	if err != nil {
		return nil, err
	}

	var combinations []map[string]string
	for result.Next(ctx) {
		record := result.Record()
		combination := map[string]string{
			"splunk_image":     getStringValue(record, "splunk_image"),
			"cluster_provider": getStringValue(record, "cluster_provider"),
		}
		combinations = append(combinations, combination)
	}

	return combinations, result.Err()
}

// GetTestSuccessRate calculates success rate by topology/cloud/version
func (qi *QueryInterface) GetTestSuccessRate(ctx context.Context, filters map[string]string) (map[string]interface{}, error) {
	session := qi.driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: qi.database,
		AccessMode:   neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	// Build dynamic query based on filters
	whereClause := []string{}
	params := make(map[string]any)

	if topology, ok := filters["topology"]; ok {
		whereClause = append(whereClause, "t.topology = $topology")
		params["topology"] = topology
	}

	if cluster, ok := filters["cluster"]; ok {
		whereClause = append(whereClause, "cl.label = $cluster")
		params["cluster"] = cluster
	}

	where := ""
	if len(whereClause) > 0 {
		where = "WHERE " + strings.Join(whereClause, " AND ")
	}

	query := fmt.Sprintf(`
		MATCH (t:E2E {type: 'test'})
		OPTIONAL MATCH (t)-[:RUNS_ON]->(cl:E2E {type: 'cluster'})
		%s
		WITH t.status AS status, count(*) AS count
		RETURN status, count
	`, where)

	result, err := session.Run(ctx, query, params)
	if err != nil {
		return nil, err
	}

	stats := map[string]interface{}{
		"total":   0,
		"passed":  0,
		"failed":  0,
		"skipped": 0,
	}

	for result.Next(ctx) {
		record := result.Record()
		status := getStringValue(record, "status")
		count := getInt64Value(record, "count")

		stats[status] = count
		stats["total"] = stats["total"].(int) + int(count)
	}

	if total := stats["total"].(int); total > 0 {
		passedCount := int64(0)
		if passed, ok := stats["passed"].(int64); ok {
			passedCount = passed
		}
		stats["success_rate"] = float64(passedCount) / float64(total) * 100
	}

	return stats, result.Err()
}

// FindFlakyTests identifies tests with inconsistent pass/fail patterns
func (qi *QueryInterface) FindFlakyTests(ctx context.Context, threshold float64) ([]map[string]interface{}, error) {
	session := qi.driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: qi.database,
		AccessMode:   neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	query := `
		MATCH (t:E2E {type: 'test'})
		WITH t.label AS test_name,
		     sum(CASE WHEN t.status = 'passed' THEN 1 ELSE 0 END) AS passed,
		     sum(CASE WHEN t.status = 'failed' THEN 1 ELSE 0 END) AS failed,
		     count(*) AS total
		WHERE total > 3 AND passed > 0 AND failed > 0
		WITH test_name, passed, failed, total,
		     toFloat(passed) / toFloat(total) AS pass_rate
		WHERE pass_rate > $threshold AND pass_rate < (1 - $threshold)
		RETURN test_name, passed, failed, total, pass_rate
		ORDER BY pass_rate ASC
	`

	result, err := session.Run(ctx, query, map[string]any{
		"threshold": threshold,
	})
	if err != nil {
		return nil, err
	}

	var flakyTests []map[string]interface{}
	for result.Next(ctx) {
		record := result.Record()
		test := map[string]interface{}{
			"test_name": getStringValue(record, "test_name"),
			"passed":    getInt64Value(record, "passed"),
			"failed":    getInt64Value(record, "failed"),
			"total":     getInt64Value(record, "total"),
			"pass_rate": getFloat64Value(record, "pass_rate"),
		}
		flakyTests = append(flakyTests, test)
	}

	return flakyTests, result.Err()
}

// GetAverageTimings gets average deployment times for topologies
func (qi *QueryInterface) GetAverageTimings(ctx context.Context, topology string) (map[string]float64, error) {
	session := qi.driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: qi.database,
		AccessMode:   neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	query := `
		MATCH (t:E2E {type: 'test'})-[:HAS_TIMING_METRICS]->(tm:E2E {type: 'timing_metrics'})
		WHERE t.topology = $topology AND t.status = 'passed'
		WITH keys(tm) AS metrics, tm
		UNWIND metrics AS metric_name
		WITH metric_name, avg(toFloat(tm[metric_name])) AS avg_time
		WHERE metric_name <> 'id' AND metric_name <> 'type' AND metric_name <> 'label'
		RETURN metric_name, avg_time
	`

	result, err := session.Run(ctx, query, map[string]any{
		"topology": topology,
	})
	if err != nil {
		return nil, err
	}

	timings := make(map[string]float64)
	for result.Next(ctx) {
		record := result.Record()
		metricName := getStringValue(record, "metric_name")
		avgTime := getFloat64Value(record, "avg_time")
		timings[metricName] = avgTime
	}

	return timings, result.Err()
}

// FindTestsByErrorPattern finds all tests that match a specific error pattern
func (qi *QueryInterface) FindTestsByErrorPattern(ctx context.Context, pattern string) ([]TestFailureInfo, error) {
	session := qi.driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: qi.database,
		AccessMode:   neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	query := `
		MATCH (t:E2E {type: 'test'})-[:HAS_FAILURE_ANALYSIS]->(fa:E2E {type: 'failure_analysis'})
		WHERE fa.error_message =~ $pattern
		OPTIONAL MATCH (t)-[:USES_OPERATOR_IMAGE]->(op:E2E {type: 'image'})
		OPTIONAL MATCH (t)-[:USES_SPLUNK_IMAGE]->(sp:E2E {type: 'image'})
		OPTIONAL MATCH (t)-[:RUNS_ON]->(cl:E2E {type: 'cluster'})
		RETURN t.label AS test_name,
		       t.status AS status,
		       fa.error_category AS error_category,
		       fa.error_message AS error_message,
		       op.label AS operator_image,
		       sp.label AS splunk_image,
		       cl.label AS cluster_provider
		LIMIT 100
	`

	result, err := session.Run(ctx, query, map[string]any{
		"pattern": pattern,
	})
	if err != nil {
		return nil, err
	}

	var failures []TestFailureInfo
	for result.Next(ctx) {
		record := result.Record()
		failure := TestFailureInfo{
			TestName:        getStringValue(record, "test_name"),
			Status:          getStringValue(record, "status"),
			ErrorCategory:   getStringValue(record, "error_category"),
			OperatorImage:   getStringValue(record, "operator_image"),
			SplunkImage:     getStringValue(record, "splunk_image"),
			ClusterProvider: getStringValue(record, "cluster_provider"),
		}
		failures = append(failures, failure)
	}

	return failures, result.Err()
}

// Helper functions to safely extract values from records
func getStringValue(record *neo4j.Record, key string) string {
	if val, ok := record.Get(key); ok && val != nil {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func getInt64Value(record *neo4j.Record, key string) int64 {
	if val, ok := record.Get(key); ok && val != nil {
		if num, ok := val.(int64); ok {
			return num
		}
	}
	return 0
}

func getFloat64Value(record *neo4j.Record, key string) float64 {
	if val, ok := record.Get(key); ok && val != nil {
		if num, ok := val.(float64); ok {
			return num
		}
		if num, ok := val.(int64); ok {
			return float64(num)
		}
	}
	return 0.0
}
