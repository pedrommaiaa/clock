// Command knox is a file-based knowledge graph store.
// It stores nodes and edges as JSONL in .clock/knox/ and supports
// querying, searching, BFS pathfinding, and graph statistics.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// Node is a knowledge graph node.
type Node struct {
	ID    string            `json:"id"`
	Kind  string            `json:"kind"` // module, api, file, function, fact
	Name  string            `json:"name"`
	Path  string            `json:"path,omitempty"`
	Props map[string]string `json:"props,omitempty"`
}

// Edge is a knowledge graph edge.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"` // imports, calls, depends, breaks, fixes
}

// QueryResult is the output of the query subcommand.
type QueryResult struct {
	Node      *Node  `json:"node"`
	Edges     []Edge `json:"edges"`
	Neighbors []Node `json:"neighbors"`
}

// Stats is the output of the stats subcommand.
type Stats struct {
	Nodes int            `json:"nodes"`
	Edges int            `json:"edges"`
	Kinds map[string]int `json:"kinds"`
}

// Graph holds the full in-memory graph.
type Graph struct {
	Nodes []Node
	Edges []Edge
}

// DumpResult is the output of the dump subcommand.
type DumpResult struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// PathResult is the output of the path subcommand.
type PathResult struct {
	Found bool   `json:"found"`
	Path  []Edge `json:"path"`
}

const (
	storeDir  = ".clock/knox"
	nodesFile = ".clock/knox/nodes.jsonl"
	edgesFile = ".clock/knox/edges.jsonl"
)

func main() {
	if len(os.Args) < 2 {
		jsonutil.Fatal("usage: knox <subcommand> [args]\nsubcommands: add-node, add-edge, query, search, neighbors, path, stats, dump")
	}

	sub := os.Args[1]
	switch sub {
	case "add-node":
		cmdAddNode()
	case "add-edge":
		cmdAddEdge()
	case "query":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: knox query <id>")
		}
		cmdQuery(os.Args[2])
	case "search":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: knox search <term>")
		}
		cmdSearch(os.Args[2])
	case "neighbors":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: knox neighbors <id>")
		}
		cmdNeighbors(os.Args[2])
	case "path":
		if len(os.Args) < 4 {
			jsonutil.Fatal("usage: knox path <from> <to>")
		}
		cmdPath(os.Args[2], os.Args[3])
	case "stats":
		cmdStats()
	case "dump":
		cmdDump()
	default:
		jsonutil.Fatal(fmt.Sprintf("unknown subcommand: %s", sub))
	}
}

func ensureDir() {
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		jsonutil.Fatal(fmt.Sprintf("mkdir %s: %v", storeDir, err))
	}
}

func appendJSONL(path string, v interface{}) error {
	ensureDir()
	line, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

func loadNodes() ([]Node, error) {
	return loadJSONL[Node](nodesFile)
}

func loadEdges() ([]Edge, error) {
	return loadJSONL[Edge](edgesFile)
}

func loadJSONL[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var items []T
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var item T
		if err := json.Unmarshal(line, &item); err != nil {
			continue // skip malformed lines
		}
		items = append(items, item)
	}
	if err := scanner.Err(); err != nil {
		return items, err
	}
	return items, nil
}

func loadGraph() (*Graph, error) {
	nodes, err := loadNodes()
	if err != nil {
		return nil, fmt.Errorf("load nodes: %w", err)
	}
	edges, err := loadEdges()
	if err != nil {
		return nil, fmt.Errorf("load edges: %w", err)
	}
	return &Graph{Nodes: nodes, Edges: edges}, nil
}

func cmdAddNode() {
	var node Node
	if err := jsonutil.ReadInput(&node); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}
	if node.ID == "" {
		jsonutil.Fatal("node id is required")
	}
	if node.Kind == "" {
		jsonutil.Fatal("node kind is required")
	}
	if node.Name == "" {
		jsonutil.Fatal("node name is required")
	}
	if err := appendJSONL(nodesFile, node); err != nil {
		jsonutil.Fatal(fmt.Sprintf("append node: %v", err))
	}
	if err := jsonutil.WriteOutput(node); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func cmdAddEdge() {
	var edge Edge
	if err := jsonutil.ReadInput(&edge); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}
	if edge.From == "" || edge.To == "" {
		jsonutil.Fatal("edge from and to are required")
	}
	if edge.Kind == "" {
		jsonutil.Fatal("edge kind is required")
	}
	if err := appendJSONL(edgesFile, edge); err != nil {
		jsonutil.Fatal(fmt.Sprintf("append edge: %v", err))
	}
	if err := jsonutil.WriteOutput(edge); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func cmdQuery(id string) {
	g, err := loadGraph()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load graph: %v", err))
	}

	result := QueryResult{
		Edges:     []Edge{},
		Neighbors: []Node{},
	}

	// Find the node
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			result.Node = &g.Nodes[i]
			break
		}
	}

	if result.Node == nil {
		jsonutil.Fatal(fmt.Sprintf("node not found: %s", id))
	}

	// Find connected edges and neighbor IDs
	neighborIDs := make(map[string]bool)
	for _, e := range g.Edges {
		if e.From == id || e.To == id {
			result.Edges = append(result.Edges, e)
			if e.From == id {
				neighborIDs[e.To] = true
			} else {
				neighborIDs[e.From] = true
			}
		}
	}

	// Collect neighbor nodes
	for _, n := range g.Nodes {
		if neighborIDs[n.ID] {
			result.Neighbors = append(result.Neighbors, n)
		}
	}

	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func cmdSearch(term string) {
	nodes, err := loadNodes()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load nodes: %v", err))
	}

	termLower := strings.ToLower(term)
	for _, n := range nodes {
		if strings.Contains(strings.ToLower(n.Name), termLower) ||
			strings.Contains(strings.ToLower(n.Kind), termLower) ||
			strings.Contains(strings.ToLower(n.ID), termLower) ||
			strings.Contains(strings.ToLower(n.Path), termLower) {
			if err := jsonutil.WriteJSONL(n); err != nil {
				jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
			}
		}
	}
}

func cmdNeighbors(id string) {
	g, err := loadGraph()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load graph: %v", err))
	}

	neighborIDs := make(map[string]bool)
	for _, e := range g.Edges {
		if e.From == id {
			neighborIDs[e.To] = true
		} else if e.To == id {
			neighborIDs[e.From] = true
		}
	}

	for _, n := range g.Nodes {
		if neighborIDs[n.ID] {
			if err := jsonutil.WriteJSONL(n); err != nil {
				jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
			}
		}
	}
}

func cmdPath(from, to string) {
	g, err := loadGraph()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load graph: %v", err))
	}

	// Build adjacency list (undirected for traversal)
	type adjEntry struct {
		neighbor string
		edge     Edge
	}
	adj := make(map[string][]adjEntry)
	for _, e := range g.Edges {
		adj[e.From] = append(adj[e.From], adjEntry{neighbor: e.To, edge: e})
		adj[e.To] = append(adj[e.To], adjEntry{neighbor: e.From, edge: e})
	}

	// BFS
	type bfsState struct {
		nodeID string
		path   []Edge
	}

	visited := make(map[string]bool)
	queue := []bfsState{{nodeID: from, path: nil}}
	visited[from] = true

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if curr.nodeID == to {
			result := PathResult{Found: true, Path: curr.path}
			if result.Path == nil {
				result.Path = []Edge{}
			}
			if err := jsonutil.WriteOutput(result); err != nil {
				jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
			}
			return
		}

		for _, entry := range adj[curr.nodeID] {
			if !visited[entry.neighbor] {
				visited[entry.neighbor] = true
				newPath := make([]Edge, len(curr.path)+1)
				copy(newPath, curr.path)
				newPath[len(curr.path)] = entry.edge
				queue = append(queue, bfsState{nodeID: entry.neighbor, path: newPath})
			}
		}
	}

	// No path found
	result := PathResult{Found: false, Path: []Edge{}}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func cmdStats() {
	g, err := loadGraph()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load graph: %v", err))
	}

	kinds := make(map[string]int)
	for _, n := range g.Nodes {
		kinds[n.Kind]++
	}

	stats := Stats{
		Nodes: len(g.Nodes),
		Edges: len(g.Edges),
		Kinds: kinds,
	}
	if err := jsonutil.WriteOutput(stats); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func cmdDump() {
	g, err := loadGraph()
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("load graph: %v", err))
	}

	result := DumpResult{
		Nodes: g.Nodes,
		Edges: g.Edges,
	}
	if result.Nodes == nil {
		result.Nodes = []Node{}
	}
	if result.Edges == nil {
		result.Edges = []Edge{}
	}

	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

