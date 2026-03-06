// Command forge is a tool builder that generates candidate implementations
// from a spec. It creates main.go, main_test.go, and manifest.json files,
// stores them in .clock/forge/<name>-<version>/, and shells out to shrd put.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// ForgeInput is the input schema for the forge tool.
type ForgeInput struct {
	Spec     json.RawMessage `json:"spec"`
	Template string          `json:"template"` // "go" (default)
}

// ToolSpec mirrors the spec tool's output for unmarshalling.
type ToolSpec struct {
	Tool        string                 `json:"tool"`
	Version     string                 `json:"version"`
	Goal        string                 `json:"goal"`
	Lang        string                 `json:"lang"`
	Inputs      map[string]interface{} `json:"inputs"`
	Outputs     map[string]interface{} `json:"outputs"`
	Permissions []string               `json:"permissions"`
	TestPlan    TestPlan               `json:"test_plan"`
	Examples    []Example              `json:"examples"`
	Entrypoint  string                 `json:"entrypoint"`
}

// TestPlan describes the test skeleton.
type TestPlan struct {
	UnitTests   []string `json:"unit_tests"`
	Conformance []string `json:"conformance"`
}

// Example is a sample input/output pair.
type Example struct {
	Input  interface{} `json:"input"`
	Output interface{} `json:"output"`
}

// ForgeOutput is the output schema for the forge tool.
type ForgeOutput struct {
	Artifact string   `json:"artifact"`
	Files    []string `json:"files"`
	Path     string   `json:"path"`
}

func main() {
	specFile := flag.String("spec", "", "path to spec JSON file (alternative to stdin)")
	flag.Parse()

	var spec ToolSpec

	if *specFile != "" {
		// Read spec from file
		data, err := os.ReadFile(*specFile)
		if err != nil {
			jsonutil.Fatal(fmt.Sprintf("read spec file: %v", err))
		}
		if err := json.Unmarshal(data, &spec); err != nil {
			jsonutil.Fatal(fmt.Sprintf("parse spec file: %v", err))
		}
	} else {
		// Read from stdin
		var input ForgeInput
		if err := jsonutil.ReadInput(&input); err != nil {
			jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
		}
		if input.Spec == nil {
			jsonutil.Fatal("spec is required (provide via stdin JSON or -spec flag)")
		}
		if err := json.Unmarshal(input.Spec, &spec); err != nil {
			jsonutil.Fatal(fmt.Sprintf("parse spec: %v", err))
		}
	}

	if spec.Tool == "" {
		jsonutil.Fatal("spec.tool is required")
	}
	if spec.Version == "" {
		spec.Version = "0.1.0"
	}
	if spec.Lang == "" {
		spec.Lang = "go"
	}

	// Create forge output directory
	forgeName := fmt.Sprintf("%s-%s", spec.Tool, spec.Version)
	forgeDir := filepath.Join(".clock", "forge", forgeName)
	if err := os.MkdirAll(forgeDir, 0o755); err != nil {
		jsonutil.Fatal(fmt.Sprintf("mkdir %s: %v", forgeDir, err))
	}

	// Also create the cmd directory
	cmdDir := filepath.Join(forgeDir, "cmd", spec.Tool)
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		jsonutil.Fatal(fmt.Sprintf("mkdir %s: %v", cmdDir, err))
	}

	// Generate main.go
	mainGoContent := generateMainGo(spec)
	mainGoPath := filepath.Join(cmdDir, "main.go")
	if err := os.WriteFile(mainGoPath, []byte(mainGoContent), 0o644); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write main.go: %v", err))
	}

	// Generate main_test.go
	testGoContent := generateTestGo(spec)
	testGoPath := filepath.Join(cmdDir, "main_test.go")
	if err := os.WriteFile(testGoPath, []byte(testGoContent), 0o644); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write main_test.go: %v", err))
	}

	// Generate manifest.json
	manifest := generateManifest(spec)
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("marshal manifest: %v", err))
	}
	manifestPath := filepath.Join(forgeDir, "manifest.json")
	if err := os.WriteFile(manifestPath, append(manifestBytes, '\n'), 0o644); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write manifest.json: %v", err))
	}

	// Compute artifact hash over all generated content
	hasher := sha256.New()
	hasher.Write([]byte(mainGoContent))
	hasher.Write([]byte(testGoContent))
	hasher.Write(manifestBytes)
	artifactHash := hex.EncodeToString(hasher.Sum(nil))
	artifactRef := "sha256:" + artifactHash

	// Shell out to shrd put to store the bundle
	storeBundle(forgeDir, artifactRef)

	// Relative file paths for output
	files := []string{
		filepath.Join("cmd", spec.Tool, "main.go"),
		filepath.Join("cmd", spec.Tool, "main_test.go"),
		"manifest.json",
	}

	output := ForgeOutput{
		Artifact: artifactRef,
		Files:    files,
		Path:     forgeDir + "/",
	}

	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// generateMainGo creates the main.go source file from the spec.
func generateMainGo(spec ToolSpec) string {
	// Build input struct fields from schema
	inputFields := schemaToStructFields(spec.Inputs, "Input")
	outputFields := schemaToStructFields(spec.Outputs, "Output")

	data := struct {
		Name         string
		Goal         string
		InputFields  string
		OutputFields string
		InputType    string
		OutputType   string
	}{
		Name:         spec.Tool,
		Goal:         spec.Goal,
		InputFields:  inputFields,
		OutputFields: outputFields,
		InputType:    capitalize(spec.Tool) + "Input",
		OutputType:   capitalize(spec.Tool) + "Output",
	}

	tmpl := template.Must(template.New("main").Parse(mainGoTemplate))
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		jsonutil.Fatal(fmt.Sprintf("execute template: %v", err))
	}
	return buf.String()
}

// generateTestGo creates the test file from the spec.
func generateTestGo(spec ToolSpec) string {
	var testCases []string

	// Add unit tests from spec
	for i, ut := range spec.TestPlan.UnitTests {
		testCases = append(testCases, fmt.Sprintf(`
func Test_%s_%d(t *testing.T) {
	// %s
	t.Skip("TODO: implement test")
}`, spec.Tool, i+1, ut))
	}

	// Add conformance tests
	for i, ct := range spec.TestPlan.Conformance {
		testCases = append(testCases, fmt.Sprintf(`
func TestConformance_%d(t *testing.T) {
	// %s
	t.Skip("TODO: implement test")
}`, i+1, ct))
	}

	// Add example-based tests
	for i, ex := range spec.Examples {
		inputJSON, _ := json.MarshalIndent(ex.Input, "\t", "  ")
		outputJSON, _ := json.MarshalIndent(ex.Output, "\t", "  ")
		testCases = append(testCases, fmt.Sprintf(`
func TestExample_%d(t *testing.T) {
	input := %s

	expectedOutput := %s

	// TODO: run the tool with input and compare to expectedOutput
	_ = input
	_ = expectedOutput
	t.Skip("TODO: implement example test")
}`, i+1, "`"+string(inputJSON)+"`", "`"+string(outputJSON)+"`"))
	}

	return fmt.Sprintf(`package main

import (
	"testing"
)
%s
`, strings.Join(testCases, "\n"))
}

// generateManifest creates a ToolManifest for the generated tool.
func generateManifest(spec ToolSpec) map[string]interface{} {
	riskClass := "low"
	for _, p := range spec.Permissions {
		if strings.Contains(p, "write") || strings.Contains(p, "run") {
			riskClass = "medium"
		}
		if strings.Contains(p, "net") {
			riskClass = "high"
		}
	}

	return map[string]interface{}{
		"name":           spec.Tool,
		"version":        spec.Version,
		"sha256":         "placeholder",
		"entrypoint":     spec.Entrypoint,
		"schema_in":      spec.Inputs,
		"schema_out":     spec.Outputs,
		"capabilities":   spec.Permissions,
		"risk_class":     riskClass,
		"tests_required": true,
		"owner":          "userland",
	}
}

// storeBundle shells out to shrd put with the bundle as a JSON artifact.
func storeBundle(forgeDir, artifactRef string) {
	// Read all files from the forge directory and create a bundle JSON
	bundle := map[string]interface{}{
		"artifact": artifactRef,
		"path":     forgeDir,
	}

	// Walk the forge directory and collect file contents
	files := map[string]string{}
	err := filepath.Walk(forgeDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(forgeDir, path)
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		files[rel] = string(data)
		return nil
	})
	if err != nil {
		// Non-fatal: we still output the result
		return
	}
	bundle["files"] = files

	bundleBytes, err := json.Marshal(bundle)
	if err != nil {
		return
	}

	// Try shrd put
	cmd := exec.Command("shrd", "put")
	cmd.Stdin = bytes.NewReader(bundleBytes)
	cmd.Stderr = os.Stderr
	_ = cmd.Run()

	// If shrd not found, try go run
	if cmd.ProcessState == nil || !cmd.ProcessState.Success() {
		cmd2 := exec.Command("go", "run", "./cmd/shrd", "put")
		cmd2.Stdin = bytes.NewReader(bundleBytes)
		cmd2.Stderr = os.Stderr
		_ = cmd2.Run()
	}
}

// schemaToStructFields converts JSON schema properties to Go struct field definitions.
func schemaToStructFields(schema map[string]interface{}, prefix string) string {
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		return fmt.Sprintf("\t// No properties defined for %s\n", prefix)
	}

	var fields []string
	for name, propI := range props {
		prop, ok := propI.(map[string]interface{})
		if !ok {
			continue
		}
		goType := jsonTypeToGo(prop)
		fieldName := capitalize(name)
		jsonTag := name
		desc := ""
		if d, ok := prop["description"].(string); ok {
			desc = " // " + d
		}
		fields = append(fields, fmt.Sprintf("\t%s %s `json:\"%s\"`%s", fieldName, goType, jsonTag, desc))
	}

	if len(fields) == 0 {
		return fmt.Sprintf("\t// No properties defined for %s\n", prefix)
	}
	return strings.Join(fields, "\n")
}

// jsonTypeToGo converts a JSON schema type to a Go type.
func jsonTypeToGo(prop map[string]interface{}) string {
	t, _ := prop["type"].(string)
	switch t {
	case "string":
		return "string"
	case "number":
		return "float64"
	case "integer":
		return "int"
	case "boolean":
		return "bool"
	case "array":
		items, ok := prop["items"].(map[string]interface{})
		if ok {
			return "[]" + jsonTypeToGo(items)
		}
		return "[]interface{}"
	case "object":
		return "map[string]interface{}"
	default:
		return "interface{}"
	}
}

// capitalize returns s with the first letter uppercased.
func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

const mainGoTemplate = `// Command {{.Name}} — {{.Goal}}
// Auto-generated by forge. Fill in the TODO sections.
package main

import (
	"fmt"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// {{.InputType}} is the input schema.
type {{.InputType}} struct {
{{.InputFields}}
}

// {{.OutputType}} is the output schema.
type {{.OutputType}} struct {
{{.OutputFields}}
}

func main() {
	var input {{.InputType}}
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	// TODO: implement {{.Goal}}
	output := process(input)

	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func process(input {{.InputType}}) {{.OutputType}} {
	var output {{.OutputType}}
	// TODO: implement processing logic
	_ = input
	return output
}
`
