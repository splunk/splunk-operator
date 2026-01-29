package graph

// Node represents a graph node.
type Node struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Label      string                 `json:"label,omitempty"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}

// Edge represents a graph edge.
type Edge struct {
	From       string                 `json:"from"`
	To         string                 `json:"to"`
	Type       string                 `json:"type"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}

// Graph is a lightweight knowledge graph for test results.
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// AddNode adds a node to the graph if it does not exist.
func (g *Graph) AddNode(node Node) {
	for _, existing := range g.Nodes {
		if existing.ID == node.ID {
			return
		}
	}
	g.Nodes = append(g.Nodes, node)
}

// AddEdge adds an edge to the graph.
func (g *Graph) AddEdge(edge Edge) {
	g.Edges = append(g.Edges, edge)
}
