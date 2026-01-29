package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/splunk/splunk-operator/e2e/framework/graph"
	"go.uber.org/zap"
)

const neo4jBatchSize = 200

func (r *Runner) exportGraphToNeo4j(ctx context.Context) error {
	if r.logger != nil {
		r.logger.Info("neo4j export starting", zap.String("uri", r.cfg.Neo4jURI), zap.Bool("graph_nil", r.graph == nil))
	}
	if r.cfg.Neo4jURI == "" {
		return fmt.Errorf("neo4j uri is required")
	}
	if r.graph == nil {
		if r.logger != nil {
			r.logger.Warn("neo4j export skipped: graph is nil")
		}
		return nil
	}
	if r.logger != nil {
		r.logger.Info("neo4j export graph stats", zap.Int("nodes", len(r.graph.Nodes)), zap.Int("edges", len(r.graph.Edges)))
	}

	auth := neo4j.NoAuth()
	if r.cfg.Neo4jUser != "" || r.cfg.Neo4jPassword != "" {
		auth = neo4j.BasicAuth(r.cfg.Neo4jUser, r.cfg.Neo4jPassword, "")
	}
	driver, err := neo4j.NewDriverWithContext(r.cfg.Neo4jURI, auth)
	if err != nil {
		return err
	}
	defer func() {
		if err := driver.Close(ctx); err != nil && r.logger != nil {
			r.logger.Warn("neo4j close failed", zap.Error(err))
		}
	}()

	if err := driver.VerifyConnectivity(ctx); err != nil {
		return err
	}

	session := driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: r.cfg.Neo4jDatabase,
		AccessMode:   neo4j.AccessModeWrite,
	})
	defer session.Close(ctx)

	if err := r.ensureNeo4jSchema(ctx, session); err != nil {
		return err
	}
	if err := r.writeNeo4jNodes(ctx, session, r.graph.Nodes); err != nil {
		return err
	}
	if err := r.writeNeo4jEdges(ctx, session, r.graph.Edges); err != nil {
		return err
	}

	if r.logger != nil {
		r.logger.Info("neo4j export complete", zap.Int("nodes", len(r.graph.Nodes)), zap.Int("edges", len(r.graph.Edges)))
	}
	return nil
}

func (r *Runner) ensureNeo4jSchema(ctx context.Context, session neo4j.SessionWithContext) error {
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, "CREATE CONSTRAINT e2e_node_id IF NOT EXISTS FOR (n:E2E) REQUIRE n.id IS UNIQUE", nil)
		return nil, err
	})
	return err
}

func (r *Runner) writeNeo4jNodes(ctx context.Context, session neo4j.SessionWithContext, nodes []graph.Node) error {
	for i := 0; i < len(nodes); i += neo4jBatchSize {
		end := i + neo4jBatchSize
		if end > len(nodes) {
			end = len(nodes)
		}
		rows := make([]map[string]any, 0, end-i)
		for _, node := range nodes[i:end] {
			rows = append(rows, map[string]any{
				"id":     node.ID,
				"type":   node.Type,
				"label":  node.Label,
				"status": attributeValue(node.Attributes, "status"),
				"action": attributeValue(node.Attributes, "action"),
				"path":   attributeValue(node.Attributes, "path"),
				"attrs":  encodeAttributes(node.Attributes),
			})
		}
		_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
			_, err := tx.Run(ctx, `
UNWIND $rows AS row
MERGE (n:E2E {id: row.id})
SET n.type = row.type,
    n.label = row.label,
    n.status = row.status,
    n.action = row.action,
    n.path = row.path,
    n.attrs = row.attrs`, map[string]any{"rows": rows})
			return nil, err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) writeNeo4jEdges(ctx context.Context, session neo4j.SessionWithContext, edges []graph.Edge) error {
	edgesByType := make(map[string][]map[string]any)
	for _, edge := range edges {
		relType := sanitizeRelType(edge.Type)
		edgesByType[relType] = append(edgesByType[relType], map[string]any{
			"from":  edge.From,
			"to":    edge.To,
			"type":  edge.Type,
			"attrs": encodeAttributes(edge.Attributes),
		})
	}

	for relType, rows := range edgesByType {
		for i := 0; i < len(rows); i += neo4jBatchSize {
			end := i + neo4jBatchSize
			if end > len(rows) {
				end = len(rows)
			}
			chunk := rows[i:end]
			query := fmt.Sprintf(`
UNWIND $rows AS row
MATCH (from:E2E {id: row.from})
MATCH (to:E2E {id: row.to})
MERGE (from)-[r:%s]->(to)
SET r.type = row.type,
    r.attrs = row.attrs`, relType)
			_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
				_, err := tx.Run(ctx, query, map[string]any{"rows": chunk})
				return nil, err
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func sanitizeRelType(value string) string {
	clean := strings.TrimSpace(strings.ToUpper(value))
	if clean == "" {
		return "RELATED_TO"
	}
	for _, r := range clean {
		if r >= 'A' && r <= 'Z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '_' {
			continue
		}
		return "RELATED_TO"
	}
	return clean
}

func encodeAttributes(attrs map[string]interface{}) string {
	if len(attrs) == 0 {
		return ""
	}
	payload, err := json.Marshal(attrs)
	if err != nil {
		return ""
	}
	return string(payload)
}

func attributeValue(attrs map[string]interface{}, key string) string {
	if attrs == nil {
		return ""
	}
	value, ok := attrs[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprint(value)
	}
}
