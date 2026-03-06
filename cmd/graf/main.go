// Command graf builds a lightweight import/call graph.
// It reads a JSON input with paths and mode from stdin, walks files,
// parses imports and symbol definitions with regex, and outputs a graph
// of nodes (files) and edges (imports).
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// GrafInput is the input schema for the graf tool.
type GrafInput struct {
	Paths []string `json:"paths"`
	Mode  string   `json:"mode"` // imports, symbols, both
}

// GrafOutput is the output of the graf tool.
type GrafOutput struct {
	Nodes []common.GraphNode `json:"nodes"`
	Edges []common.GraphEdge `json:"edges"`
	Index map[string]string  `json:"index,omitempty"` // symbol -> file:line
}

// Regex patterns for import parsing.
var (
	// Go imports
	goMultiImport  = regexp.MustCompile(`(?s)import\s*\(([^)]+)\)`)
	goSingleImport = regexp.MustCompile(`import\s+"([^"]+)"`)

	// JS/TS imports
	jsImportFrom = regexp.MustCompile(`import\s+(?:.*?\s+from\s+)?['"]([^'"]+)['"]`)
	jsRequire    = regexp.MustCompile(`require\s*\(\s*['"]([^'"]+)['"]\s*\)`)

	// Python imports
	pyImport     = regexp.MustCompile(`^import\s+([\w.]+)`)
	pyFromImport = regexp.MustCompile(`^from\s+([\w.]+)\s+import`)

	// Symbol definitions (for symbols mode)
	goFunc   = regexp.MustCompile(`^func\s+(?:\([^)]*\)\s+)?(\w+)`)
	goType   = regexp.MustCompile(`^type\s+(\w+)\s+`)
	jsFunc   = regexp.MustCompile(`(?:^|\s)(?:export\s+)?(?:async\s+)?function\s+(\w+)`)
	jsClass  = regexp.MustCompile(`(?:^|\s)(?:export\s+)?class\s+(\w+)`)
	pyFunc   = regexp.MustCompile(`^def\s+(\w+)`)
	pyClass  = regexp.MustCompile(`^class\s+(\w+)`)
	rsFunc   = regexp.MustCompile(`^(?:pub\s+)?fn\s+(\w+)`)
	rsStruct = regexp.MustCompile(`^(?:pub\s+)?struct\s+(\w+)`)
)

func main() {
	var input GrafInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	if len(input.Paths) == 0 {
		input.Paths = []string{"."}
	}
	if input.Mode == "" {
		input.Mode = "imports"
	}

	wantImports := input.Mode == "imports" || input.Mode == "both"
	wantSymbols := input.Mode == "symbols" || input.Mode == "both"

	output := GrafOutput{
		Nodes: []common.GraphNode{},
		Edges: []common.GraphEdge{},
		Index: make(map[string]string),
	}

	nodeSet := make(map[string]bool)

	for _, root := range input.Paths {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		err = filepath.Walk(absRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // skip errors
			}
			if info.IsDir() {
				base := info.Name()
				// Skip hidden dirs and common non-source dirs
				if strings.HasPrefix(base, ".") || base == "node_modules" || base == "vendor" || base == "__pycache__" || base == "target" {
					return filepath.SkipDir
				}
				return nil
			}

			ext := filepath.Ext(path)
			lang := langFromExt(ext)
			if lang == "" {
				return nil
			}

			// Compute relative path for display
			relPath, err := filepath.Rel(absRoot, path)
			if err != nil {
				relPath = path
			}
			displayPath := filepath.Join(root, relPath)

			// Add file node
			if !nodeSet[displayPath] {
				nodeSet[displayPath] = true
				output.Nodes = append(output.Nodes, common.GraphNode{
					ID:   displayPath,
					Kind: "file",
					Name: filepath.Base(path),
					Path: displayPath,
					Props: map[string]string{
						"lang": lang,
					},
				})
			}

			// Read file content
			content, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			text := string(content)

			if wantImports {
				imports := extractImports(text, lang)
				for _, imp := range imports {
					output.Edges = append(output.Edges, common.GraphEdge{
						From: displayPath,
						To:   imp,
						Kind: "imports",
					})
				}
			}

			if wantSymbols {
				symbols := extractSymbols(text, lang)
				for _, sym := range symbols {
					output.Index[sym.name] = fmt.Sprintf("%s:%d", displayPath, sym.line)
					output.Nodes = append(output.Nodes, common.GraphNode{
						ID:   fmt.Sprintf("%s:%s", displayPath, sym.name),
						Kind: sym.kind,
						Name: sym.name,
						Path: displayPath,
						Props: map[string]string{
							"line": fmt.Sprintf("%d", sym.line),
						},
					})
				}
			}

			return nil
		})
		if err != nil {
			// non-fatal, continue
			continue
		}
	}

	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// langFromExt maps file extensions to language names.
func langFromExt(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".js", ".jsx", ".mjs":
		return "js"
	case ".ts", ".tsx":
		return "ts"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	default:
		return ""
	}
}

// extractImports extracts import targets from file content.
func extractImports(content, lang string) []string {
	var imports []string
	seen := make(map[string]bool)

	addImport := func(imp string) {
		imp = strings.TrimSpace(imp)
		if imp != "" && !seen[imp] {
			seen[imp] = true
			imports = append(imports, imp)
		}
	}

	switch lang {
	case "go":
		// Multi-line imports
		for _, match := range goMultiImport.FindAllStringSubmatch(content, -1) {
			block := match[1]
			for _, line := range strings.Split(block, "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "//") {
					continue
				}
				// Remove alias if present: alias "path"
				parts := strings.Fields(line)
				for _, p := range parts {
					p = strings.Trim(p, `"`)
					if p != "" && !strings.HasPrefix(p, "//") {
						addImport(p)
						break
					}
				}
			}
		}
		// Single imports
		for _, match := range goSingleImport.FindAllStringSubmatch(content, -1) {
			addImport(match[1])
		}

	case "js", "ts":
		for _, match := range jsImportFrom.FindAllStringSubmatch(content, -1) {
			addImport(match[1])
		}
		for _, match := range jsRequire.FindAllStringSubmatch(content, -1) {
			addImport(match[1])
		}

	case "python":
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if m := pyFromImport.FindStringSubmatch(line); m != nil {
				addImport(m[1])
			} else if m := pyImport.FindStringSubmatch(line); m != nil {
				addImport(m[1])
			}
		}

	case "rust":
		// Rust use statements
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "use ") {
				// use crate::foo::bar;
				imp := strings.TrimPrefix(line, "use ")
				imp = strings.TrimSuffix(imp, ";")
				imp = strings.TrimSpace(imp)
				// Trim braces for grouped imports
				if idx := strings.Index(imp, "::"); idx >= 0 {
					addImport(imp[:idx])
				} else {
					addImport(imp)
				}
			}
		}
	}

	return imports
}

// symbolDef is a discovered symbol definition.
type symbolDef struct {
	name string
	kind string // function, type, class
	line int
}

// extractSymbols extracts function/type definitions from file content.
func extractSymbols(content, lang string) []symbolDef {
	var symbols []symbolDef
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		lineNum := i + 1

		switch lang {
		case "go":
			if m := goFunc.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, symbolDef{name: m[1], kind: "function", line: lineNum})
			}
			if m := goType.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, symbolDef{name: m[1], kind: "type", line: lineNum})
			}

		case "js", "ts":
			if m := jsFunc.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, symbolDef{name: m[1], kind: "function", line: lineNum})
			}
			if m := jsClass.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, symbolDef{name: m[1], kind: "class", line: lineNum})
			}

		case "python":
			if m := pyFunc.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, symbolDef{name: m[1], kind: "function", line: lineNum})
			}
			if m := pyClass.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, symbolDef{name: m[1], kind: "class", line: lineNum})
			}

		case "rust":
			if m := rsFunc.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, symbolDef{name: m[1], kind: "function", line: lineNum})
			}
			if m := rsStruct.FindStringSubmatch(line); m != nil {
				symbols = append(symbols, symbolDef{name: m[1], kind: "type", line: lineNum})
			}
		}
	}

	return symbols
}
