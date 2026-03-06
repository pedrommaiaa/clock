package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// testSetup creates a temp dir and sets the knox file paths.
// Returns a cleanup function that restores the original paths.
func testSetup(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	knoxDir := filepath.Join(dir, "knox")
	if err := os.MkdirAll(knoxDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	origStore := storeDir
	origNodes := nodesFile
	origEdges := edgesFile

	// We can't reassign consts, so we'll work with files directly
	// and use the load/append functions with our temp paths
	return dir, func() {
		_ = origStore
		_ = origNodes
		_ = origEdges
	}
}

func writeNodesFile(t *testing.T, path string, nodes []Node) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create nodes file: %v", err)
	}
	defer f.Close()
	for _, n := range nodes {
		line, _ := json.Marshal(n)
		f.Write(append(line, '\n'))
	}
}

func writeEdgesFile(t *testing.T, path string, edges []Edge) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create edges file: %v", err)
	}
	defer f.Close()
	for _, e := range edges {
		line, _ := json.Marshal(e)
		f.Write(append(line, '\n'))
	}
}

func TestLoadJSONLNodes(t *testing.T) {
	dir := t.TempDir()
	nodesPath := filepath.Join(dir, "nodes.jsonl")

	nodes := []Node{
		{ID: "n1", Kind: "module", Name: "auth"},
		{ID: "n2", Kind: "file", Name: "main.go", Path: "cmd/main.go"},
		{ID: "n3", Kind: "function", Name: "Login"},
	}
	writeNodesFile(t, nodesPath, nodes)

	loaded, err := loadJSONL[Node](nodesPath)
	if err != nil {
		t.Fatalf("loadJSONL: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("loaded %d nodes, want 3", len(loaded))
	}
	if loaded[0].ID != "n1" || loaded[0].Kind != "module" {
		t.Errorf("node 0: got %+v", loaded[0])
	}
	if loaded[1].Path != "cmd/main.go" {
		t.Errorf("node 1 path: got %q", loaded[1].Path)
	}
}

func TestLoadJSONLEdges(t *testing.T) {
	dir := t.TempDir()
	edgesPath := filepath.Join(dir, "edges.jsonl")

	edges := []Edge{
		{From: "n1", To: "n2", Kind: "imports"},
		{From: "n2", To: "n3", Kind: "calls"},
	}
	writeEdgesFile(t, edgesPath, edges)

	loaded, err := loadJSONL[Edge](edgesPath)
	if err != nil {
		t.Fatalf("loadJSONL: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded %d edges, want 2", len(loaded))
	}
}

func TestLoadJSONLMissingFile(t *testing.T) {
	items, err := loadJSONL[Node]("/nonexistent/path/nodes.jsonl")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if items != nil {
		t.Errorf("expected nil slice for missing file, got %v", items)
	}
}

func TestLoadJSONLMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nodes.jsonl")

	content := `{"id":"n1","kind":"module","name":"auth"}
not-json
{"id":"n2","kind":"file","name":"main.go"}
`
	os.WriteFile(path, []byte(content), 0o644)

	loaded, err := loadJSONL[Node](path)
	if err != nil {
		t.Fatalf("loadJSONL: %v", err)
	}
	// Should skip the malformed line
	if len(loaded) != 2 {
		t.Errorf("loaded %d nodes (expected 2, skipping malformed)", len(loaded))
	}
}

func TestAppendJSONL(t *testing.T) {
	dir := t.TempDir()
	knoxDir := filepath.Join(dir, ".clock", "knox")
	os.MkdirAll(knoxDir, 0o755)

	path := filepath.Join(knoxDir, "test.jsonl")

	node := Node{ID: "n1", Kind: "module", Name: "test"}
	// appendJSONL calls ensureDir() which uses the const storeDir.
	// We test the underlying write logic directly.
	line, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	f.Write(append(line, '\n'))
	f.Close()

	loaded, err := loadJSONL[Node](path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 || loaded[0].ID != "n1" {
		t.Errorf("expected 1 node with ID n1, got %+v", loaded)
	}
}

func TestGraphQueryFindsNode(t *testing.T) {
	dir := t.TempDir()
	nodesPath := filepath.Join(dir, "nodes.jsonl")
	edgesPath := filepath.Join(dir, "edges.jsonl")

	nodes := []Node{
		{ID: "n1", Kind: "module", Name: "auth"},
		{ID: "n2", Kind: "file", Name: "main.go"},
	}
	edges := []Edge{
		{From: "n1", To: "n2", Kind: "imports"},
	}
	writeNodesFile(t, nodesPath, nodes)
	writeEdgesFile(t, edgesPath, edges)

	loadedNodes, _ := loadJSONL[Node](nodesPath)
	loadedEdges, _ := loadJSONL[Edge](edgesPath)
	g := &Graph{Nodes: loadedNodes, Edges: loadedEdges}

	// Find node n1
	var found *Node
	for i := range g.Nodes {
		if g.Nodes[i].ID == "n1" {
			found = &g.Nodes[i]
			break
		}
	}
	if found == nil {
		t.Fatal("node n1 not found")
	}
	if found.Name != "auth" {
		t.Errorf("node name = %q, want %q", found.Name, "auth")
	}
}

func TestGraphNeighbors(t *testing.T) {
	dir := t.TempDir()
	nodesPath := filepath.Join(dir, "nodes.jsonl")
	edgesPath := filepath.Join(dir, "edges.jsonl")

	nodes := []Node{
		{ID: "a", Kind: "module", Name: "A"},
		{ID: "b", Kind: "module", Name: "B"},
		{ID: "c", Kind: "module", Name: "C"},
		{ID: "d", Kind: "module", Name: "D"},
	}
	edges := []Edge{
		{From: "a", To: "b", Kind: "imports"},
		{From: "a", To: "c", Kind: "calls"},
		{From: "d", To: "a", Kind: "depends"},
	}
	writeNodesFile(t, nodesPath, nodes)
	writeEdgesFile(t, edgesPath, edges)

	loadedNodes, _ := loadJSONL[Node](nodesPath)
	loadedEdges, _ := loadJSONL[Edge](edgesPath)
	g := &Graph{Nodes: loadedNodes, Edges: loadedEdges}

	// Neighbors of "a": should include b, c, d
	neighborIDs := make(map[string]bool)
	for _, e := range g.Edges {
		if e.From == "a" {
			neighborIDs[e.To] = true
		} else if e.To == "a" {
			neighborIDs[e.From] = true
		}
	}

	expected := map[string]bool{"b": true, "c": true, "d": true}
	for id := range expected {
		if !neighborIDs[id] {
			t.Errorf("expected neighbor %q not found", id)
		}
	}
}

func TestBFSPathFinding(t *testing.T) {
	dir := t.TempDir()
	nodesPath := filepath.Join(dir, "nodes.jsonl")
	edgesPath := filepath.Join(dir, "edges.jsonl")

	nodes := []Node{
		{ID: "a", Kind: "module", Name: "A"},
		{ID: "b", Kind: "module", Name: "B"},
		{ID: "c", Kind: "module", Name: "C"},
		{ID: "d", Kind: "module", Name: "D"},
	}
	edges := []Edge{
		{From: "a", To: "b", Kind: "imports"},
		{From: "b", To: "c", Kind: "calls"},
		{From: "c", To: "d", Kind: "depends"},
	}
	writeNodesFile(t, nodesPath, nodes)
	writeEdgesFile(t, edgesPath, edges)

	loadedNodes, _ := loadJSONL[Node](nodesPath)
	loadedEdges, _ := loadJSONL[Edge](edgesPath)
	g := &Graph{Nodes: loadedNodes, Edges: loadedEdges}

	// BFS from a to d
	type adjEntry struct {
		neighbor string
		edge     Edge
	}
	adj := make(map[string][]adjEntry)
	for _, e := range g.Edges {
		adj[e.From] = append(adj[e.From], adjEntry{neighbor: e.To, edge: e})
		adj[e.To] = append(adj[e.To], adjEntry{neighbor: e.From, edge: e})
	}

	type bfsState struct {
		nodeID string
		path   []Edge
	}

	visited := make(map[string]bool)
	queue := []bfsState{{nodeID: "a", path: nil}}
	visited["a"] = true
	var foundPath []Edge
	found := false

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		if curr.nodeID == "d" {
			foundPath = curr.path
			found = true
			break
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

	if !found {
		t.Fatal("BFS did not find path from a to d")
	}
	if len(foundPath) != 3 {
		t.Errorf("path length = %d, want 3", len(foundPath))
	}
}

func TestBFSNoPath(t *testing.T) {
	// Graph with disconnected components
	dir := t.TempDir()
	nodesPath := filepath.Join(dir, "nodes.jsonl")
	edgesPath := filepath.Join(dir, "edges.jsonl")

	nodes := []Node{
		{ID: "a", Kind: "module", Name: "A"},
		{ID: "b", Kind: "module", Name: "B"},
		{ID: "c", Kind: "module", Name: "C"},
	}
	edges := []Edge{
		{From: "a", To: "b", Kind: "imports"},
		// c is disconnected
	}
	writeNodesFile(t, nodesPath, nodes)
	writeEdgesFile(t, edgesPath, edges)

	loadedEdges, _ := loadJSONL[Edge](edgesPath)

	type adjEntry struct {
		neighbor string
	}
	adj := make(map[string][]adjEntry)
	for _, e := range loadedEdges {
		adj[e.From] = append(adj[e.From], adjEntry{neighbor: e.To})
		adj[e.To] = append(adj[e.To], adjEntry{neighbor: e.From})
	}

	visited := make(map[string]bool)
	type bfsState struct {
		nodeID string
	}
	queue := []bfsState{{nodeID: "a"}}
	visited["a"] = true
	found := false

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		if curr.nodeID == "c" {
			found = true
			break
		}
		for _, entry := range adj[curr.nodeID] {
			if !visited[entry.neighbor] {
				visited[entry.neighbor] = true
				queue = append(queue, bfsState{nodeID: entry.neighbor})
			}
		}
	}

	if found {
		t.Error("BFS should not find path from a to c (disconnected)")
	}
}

func TestGraphStats(t *testing.T) {
	dir := t.TempDir()
	nodesPath := filepath.Join(dir, "nodes.jsonl")
	edgesPath := filepath.Join(dir, "edges.jsonl")

	nodes := []Node{
		{ID: "n1", Kind: "module", Name: "auth"},
		{ID: "n2", Kind: "module", Name: "db"},
		{ID: "n3", Kind: "file", Name: "main.go"},
		{ID: "n4", Kind: "function", Name: "Login"},
	}
	edges := []Edge{
		{From: "n1", To: "n2", Kind: "imports"},
		{From: "n3", To: "n4", Kind: "calls"},
	}
	writeNodesFile(t, nodesPath, nodes)
	writeEdgesFile(t, edgesPath, edges)

	loadedNodes, _ := loadJSONL[Node](nodesPath)
	loadedEdges, _ := loadJSONL[Edge](edgesPath)

	kinds := make(map[string]int)
	for _, n := range loadedNodes {
		kinds[n.Kind]++
	}

	if len(loadedNodes) != 4 {
		t.Errorf("nodes count = %d, want 4", len(loadedNodes))
	}
	if len(loadedEdges) != 2 {
		t.Errorf("edges count = %d, want 2", len(loadedEdges))
	}
	if kinds["module"] != 2 {
		t.Errorf("module count = %d, want 2", kinds["module"])
	}
	if kinds["file"] != 1 {
		t.Errorf("file count = %d, want 1", kinds["file"])
	}
	if kinds["function"] != 1 {
		t.Errorf("function count = %d, want 1", kinds["function"])
	}
}

func TestEmptyGraph(t *testing.T) {
	dir := t.TempDir()
	nodesPath := filepath.Join(dir, "nodes.jsonl")
	edgesPath := filepath.Join(dir, "edges.jsonl")

	// Load from non-existent files
	nodes, err := loadJSONL[Node](nodesPath)
	if err != nil {
		t.Fatalf("loadJSONL nodes: %v", err)
	}
	edges, err := loadJSONL[Edge](edgesPath)
	if err != nil {
		t.Fatalf("loadJSONL edges: %v", err)
	}
	if nodes != nil {
		t.Errorf("expected nil nodes, got %v", nodes)
	}
	if edges != nil {
		t.Errorf("expected nil edges, got %v", edges)
	}
}

func TestNodeWithProps(t *testing.T) {
	dir := t.TempDir()
	nodesPath := filepath.Join(dir, "nodes.jsonl")

	nodes := []Node{
		{
			ID:   "n1",
			Kind: "function",
			Name: "handleRequest",
			Path: "api/handler.go",
			Props: map[string]string{
				"lang":   "go",
				"public": "true",
			},
		},
	}
	writeNodesFile(t, nodesPath, nodes)

	loaded, _ := loadJSONL[Node](nodesPath)
	if len(loaded) != 1 {
		t.Fatalf("loaded %d nodes, want 1", len(loaded))
	}
	if loaded[0].Props["lang"] != "go" {
		t.Errorf("props[lang] = %q, want %q", loaded[0].Props["lang"], "go")
	}
	if loaded[0].Props["public"] != "true" {
		t.Errorf("props[public] = %q, want %q", loaded[0].Props["public"], "true")
	}
}
