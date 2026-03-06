// Command lmux is an LLM multiplexer/router that distributes requests
// across multiple providers using configurable strategies (fallback, race, round-robin).
// It supports the canonical Clock input/output format with provider-prefixed model names.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// CanonicalInput is the ssh.txt canonical input format.
type CanonicalInput struct {
	// Canonical format fields
	Model    string          `json:"model,omitempty"`    // e.g. "anth:sonnet"
	Messages []Message       `json:"messages,omitempty"` // chat messages
	Opts     *RequestOpts    `json:"opts,omitempty"`     // generation options
	Tools    []ToolDef       `json:"tools,omitempty"`    // tool definitions
	Timeout  int             `json:"timeout,omitempty"`  // timeout in seconds
	Fallback []string        `json:"fallback,omitempty"` // fallback model list e.g. ["oai:gpt4o","oll:llama3"]

	// Legacy format fields (backward compatible)
	Bundle    *LegacyBundle   `json:"bundle,omitempty"`
	Strategy  string          `json:"strategy,omitempty"`  // fallback, race, round-robin
	Providers []Provider      `json:"providers,omitempty"` // legacy provider list
}

// Message is a chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// RequestOpts holds generation options.
type RequestOpts struct {
	Temp   float64 `json:"temp,omitempty"`
	TopP   float64 `json:"top_p,omitempty"`
	MaxOut int     `json:"max_out,omitempty"`
	JSON   bool    `json:"json,omitempty"`
}

// ToolDef is a tool definition.
type ToolDef struct {
	Name   string      `json:"name"`
	Schema interface{} `json:"schema,omitempty"`
}

// LegacyBundle is the old PackBundle format for backward compatibility.
type LegacyBundle struct {
	System   string      `json:"system"`
	Messages []Message   `json:"messages"`
	Tools    []ToolDef   `json:"tools,omitempty"`
	Policy   interface{} `json:"policy,omitempty"`
}

// Provider is a configured LLM provider (legacy format).
type Provider struct {
	Name      string  `json:"name"`
	Model     string  `json:"model"`
	APIKeyEnv string  `json:"api_key_env,omitempty"`
	Priority  int     `json:"priority,omitempty"`
	Weight    float64 `json:"weight,omitempty"`
}

// CanonicalOutput is the ssh.txt canonical output format.
type CanonicalOutput struct {
	OK    bool            `json:"ok"`
	Model string          `json:"model"`
	Text  string          `json:"text"`
	JSON  json.RawMessage `json:"json,omitempty"`
	Usage *UsageInfo      `json:"usage,omitempty"`
	Meta  *MetaInfo       `json:"meta,omitempty"`
}

// UsageInfo holds token usage and cost.
type UsageInfo struct {
	In   int     `json:"in"`
	Out  int     `json:"out"`
	Cost float64 `json:"cost,omitempty"`
}

// MetaInfo holds request metadata.
type MetaInfo struct {
	Ms    int64  `json:"ms"`
	ReqID string `json:"req_id,omitempty"`
}

// LegacyOutput is the old output format (for backward compat with strategy+providers).
type LegacyOutput struct {
	Response     json.RawMessage `json:"response"`
	ProviderUsed string          `json:"provider_used"`
	LatencyMs    int64           `json:"latency_ms"`
	Attempts     int             `json:"attempts"`
}

// RoundRobinState tracks round-robin request distribution.
type RoundRobinState struct {
	Counter int `json:"counter"`
}

// providerInfo is parsed from a model prefix string.
type providerInfo struct {
	Prefix   string // e.g. "anth"
	Provider string // e.g. "anthropic"
	Model    string // e.g. "sonnet"
	Raw      string // original e.g. "anth:sonnet"
}

const stateFile = ".clock/lmux_state.json"

// prefixToProvider maps short prefixes to provider names.
var prefixToProvider = map[string]string{
	"anth": "anthropic",
	"oai":  "openai",
	"oll":  "ollama",
	"vllm": "vllm",
	"lcpp": "lcpp",
}

func main() {
	var input CanonicalInput
	if err := jsonutil.ReadInput(&input); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}

	// Determine if this is canonical format or legacy format
	if len(input.Providers) > 0 && input.Bundle != nil {
		// Legacy format: use strategy-based routing
		handleLegacy(input)
		return
	}

	// Canonical format: parse model prefix
	if input.Model == "" {
		jsonutil.Fatal("model field is required")
	}

	handleCanonical(input)
}

// parseModel parses a provider-prefixed model string like "anth:sonnet".
func parseModel(model string) (providerInfo, error) {
	parts := strings.SplitN(model, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return providerInfo{}, fmt.Errorf("invalid model format %q: expected prefix:model (e.g. anth:sonnet)", model)
	}

	provider, ok := prefixToProvider[parts[0]]
	if !ok {
		return providerInfo{}, fmt.Errorf("unknown provider prefix %q: valid prefixes are anth, oai, oll, vllm, lcpp", parts[0])
	}

	return providerInfo{
		Prefix:   parts[0],
		Provider: provider,
		Model:    parts[1],
		Raw:      model,
	}, nil
}

// handleCanonical processes the canonical input format.
func handleCanonical(input CanonicalInput) {
	// Build the ordered list of models to try: primary + fallbacks
	models := []string{input.Model}
	models = append(models, input.Fallback...)

	var lastErr error
	for _, modelStr := range models {
		info, err := parseModel(modelStr)
		if err != nil {
			lastErr = err
			continue
		}

		result, err := callCanonical(info, input)
		if err != nil {
			lastErr = err
			continue
		}

		if err := jsonutil.WriteOutput(result); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
		}
		return
	}

	jsonutil.Fatal(fmt.Sprintf("all models failed; last error: %v", lastErr))
}

// callCanonical sends a canonical request to the llm binary.
func callCanonical(info providerInfo, input CanonicalInput) (*CanonicalOutput, error) {
	// Build the provider-specific request
	llmReq := struct {
		Provider string     `json:"provider"`
		Model    string     `json:"model"`
		Messages []Message  `json:"messages"`
		Opts     *RequestOpts `json:"opts,omitempty"`
		Tools    []ToolDef  `json:"tools,omitempty"`
		Timeout  int        `json:"timeout,omitempty"`
	}{
		Provider: info.Provider,
		Model:    info.Model,
		Messages: input.Messages,
		Opts:     input.Opts,
		Tools:    input.Tools,
		Timeout:  input.Timeout,
	}

	inputData, err := json.Marshal(llmReq)
	if err != nil {
		return nil, fmt.Errorf("marshal llm input: %w", err)
	}

	start := time.Now()

	cmd := exec.Command("llm")
	cmd.Stdin = bytes.NewReader(inputData)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = err.Error()
		}
		return nil, fmt.Errorf("provider %s (%s) failed: %s", info.Provider, info.Model, errMsg)
	}

	// Parse the raw response to extract fields
	var rawResp map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &rawResp); err != nil {
		return nil, fmt.Errorf("parse llm output for %s: %w", info.Provider, err)
	}

	result := &CanonicalOutput{
		OK:    true,
		Model: info.Raw,
		Meta: &MetaInfo{
			Ms: elapsed,
		},
	}

	// Extract text content
	if textRaw, ok := rawResp["text"]; ok {
		json.Unmarshal(textRaw, &result.Text)
	} else if answerRaw, ok := rawResp["answer"]; ok {
		json.Unmarshal(answerRaw, &result.Text)
	} else if contentRaw, ok := rawResp["content"]; ok {
		json.Unmarshal(contentRaw, &result.Text)
	}

	// Extract JSON content (could be an action envelope or structured output)
	if jsonRaw, ok := rawResp["json"]; ok {
		result.JSON = jsonRaw
	} else if kindRaw, ok := rawResp["kind"]; ok {
		// The whole response is an action envelope
		result.JSON = json.RawMessage(stdout.Bytes())
		_ = kindRaw
	}

	// Extract usage info
	if usageRaw, ok := rawResp["usage"]; ok {
		var usage UsageInfo
		if err := json.Unmarshal(usageRaw, &usage); err == nil {
			result.Usage = &usage
		}
	}

	// Extract request ID
	if metaRaw, ok := rawResp["meta"]; ok {
		var meta MetaInfo
		if err := json.Unmarshal(metaRaw, &meta); err == nil {
			if result.Meta != nil {
				result.Meta.ReqID = meta.ReqID
			}
		}
	}
	if reqIDRaw, ok := rawResp["req_id"]; ok {
		var reqID string
		if err := json.Unmarshal(reqIDRaw, &reqID); err == nil && result.Meta != nil {
			result.Meta.ReqID = reqID
		}
	}

	return result, nil
}

// handleLegacy processes the legacy format with strategy and providers.
func handleLegacy(input CanonicalInput) {
	if input.Strategy == "" {
		input.Strategy = "fallback"
	}

	var output LegacyOutput
	var err error

	switch input.Strategy {
	case "fallback":
		output, err = strategyFallback(input)
	case "race":
		output, err = strategyRace(input)
	case "round-robin":
		output, err = strategyRoundRobin(input)
	default:
		jsonutil.Fatal(fmt.Sprintf("unknown strategy: %q", input.Strategy))
	}

	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("lmux: %v", err))
	}

	if err := jsonutil.WriteOutput(output); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// callProviderLegacy shells out to the llm binary with legacy provider config and bundle.
func callProviderLegacy(p Provider, bundle *LegacyBundle) (json.RawMessage, int64, error) {
	llmInput := struct {
		Provider  string       `json:"provider"`
		Model     string       `json:"model"`
		Bundle    *LegacyBundle `json:"bundle"`
		APIKeyEnv string       `json:"api_key_env,omitempty"`
	}{
		Provider:  p.Name,
		Model:     p.Model,
		Bundle:    bundle,
		APIKeyEnv: p.APIKeyEnv,
	}

	inputData, err := json.Marshal(llmInput)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal llm input: %w", err)
	}

	start := time.Now()

	cmd := exec.Command("llm")
	cmd.Stdin = bytes.NewReader(inputData)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = err.Error()
		}
		return nil, elapsed, fmt.Errorf("provider %s failed: %s", p.Name, errMsg)
	}

	return json.RawMessage(stdout.Bytes()), elapsed, nil
}

// strategyFallback tries providers in priority order, using the first that succeeds.
func strategyFallback(input CanonicalInput) (LegacyOutput, error) {
	providers := sortByPriority(input.Providers)

	var lastErr error
	for i, p := range providers {
		response, latency, err := callProviderLegacy(p, input.Bundle)
		if err != nil {
			lastErr = err
			continue
		}
		return LegacyOutput{
			Response:     response,
			ProviderUsed: p.Name,
			LatencyMs:    latency,
			Attempts:     i + 1,
		}, nil
	}

	return LegacyOutput{}, fmt.Errorf("all %d providers failed; last error: %v", len(providers), lastErr)
}

// strategyRace sends to all providers concurrently, using the first response.
func strategyRace(input CanonicalInput) (LegacyOutput, error) {
	type raceResult struct {
		response json.RawMessage
		provider string
		latency  int64
		err      error
	}

	results := make(chan raceResult, len(input.Providers))
	var wg sync.WaitGroup

	for _, p := range input.Providers {
		wg.Add(1)
		go func(prov Provider) {
			defer wg.Done()
			response, latency, err := callProviderLegacy(prov, input.Bundle)
			results <- raceResult{
				response: response,
				provider: prov.Name,
				latency:  latency,
				err:      err,
			}
		}(p)
	}

	// Close channel when all goroutines finish
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results, return first success
	var errors []error
	for r := range results {
		if r.err != nil {
			errors = append(errors, r.err)
			continue
		}
		return LegacyOutput{
			Response:     r.response,
			ProviderUsed: r.provider,
			LatencyMs:    r.latency,
			Attempts:     len(input.Providers),
		}, nil
	}

	return LegacyOutput{}, fmt.Errorf("all %d providers failed in race", len(input.Providers))
}

// strategyRoundRobin distributes across providers based on request count.
func strategyRoundRobin(input CanonicalInput) (LegacyOutput, error) {
	state := loadState()
	idx := state.Counter % len(input.Providers)

	// Save incremented counter
	state.Counter++
	saveState(state)

	// Try the selected provider first, then fall back to others
	order := make([]Provider, 0, len(input.Providers))
	order = append(order, input.Providers[idx])
	for i, p := range input.Providers {
		if i != idx {
			order = append(order, p)
		}
	}

	var lastErr error
	for attempt, p := range order {
		response, latency, err := callProviderLegacy(p, input.Bundle)
		if err != nil {
			lastErr = err
			continue
		}
		return LegacyOutput{
			Response:     response,
			ProviderUsed: p.Name,
			LatencyMs:    latency,
			Attempts:     attempt + 1,
		}, nil
	}

	return LegacyOutput{}, fmt.Errorf("all providers failed in round-robin; last: %v", lastErr)
}

// sortByPriority returns providers sorted by priority (ascending).
func sortByPriority(providers []Provider) []Provider {
	sorted := make([]Provider, len(providers))
	copy(sorted, providers)
	// Simple insertion sort (small N)
	for i := 1; i < len(sorted); i++ {
		key := sorted[i]
		j := i - 1
		for j >= 0 && sorted[j].Priority > key.Priority {
			sorted[j+1] = sorted[j]
			j--
		}
		sorted[j+1] = key
	}
	return sorted
}

// loadState reads the round-robin state from disk.
func loadState() RoundRobinState {
	var state RoundRobinState
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return state
	}
	json.Unmarshal(data, &state)
	return state
}

// saveState writes the round-robin state to disk.
func saveState(state RoundRobinState) {
	if err := os.MkdirAll(filepath.Dir(stateFile), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	os.WriteFile(stateFile, data, 0o644)
}
