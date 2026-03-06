// Command act is an action grammar enforcer and dispatcher.
// It reads an ActionEnvelope JSON from stdin, validates the kind and fields,
// normalizes the output, and writes the dispatched payload to stdout.
package main

import (
	"fmt"

	"github.com/pedrommaiaa/clock/internal/common"
	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

// validKinds are the accepted values for ActionEnvelope.Kind.
var validKinds = map[string]bool{
	"srch":  true,
	"slce":  true,
	"patch": true,
	"run":   true,
	"done":  true,
	"tool":  true,
}

// knownTools are the valid tool names when kind=tool.
var knownTools = map[string]bool{
	"srch": true,
	"slce": true,
	"pack": true,
	"map":  true,
	"ctrt": true,
	"flow": true,
	"doss": true,
	"vrfy": true,
	"exec": true,
	"graf": true,
}

// ActOutput is the normalized output from the act tool.
type ActOutput struct {
	Kind    string      `json:"kind"`
	Payload interface{} `json:"payload"`
}

// ActError is the error output from the act tool.
type ActError struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

func main() {
	var envelope common.ActionEnvelope
	if err := jsonutil.ReadInput(&envelope); err != nil {
		writeError(fmt.Sprintf("read input: %v", err))
		return
	}

	result, err := validate(envelope)
	if err != nil {
		writeError(err.Error())
		return
	}

	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// validate checks the ActionEnvelope and returns a normalized ActOutput.
func validate(env common.ActionEnvelope) (ActOutput, error) {
	if env.Kind == "" {
		return ActOutput{}, fmt.Errorf("kind is required")
	}

	if !validKinds[env.Kind] {
		return ActOutput{}, fmt.Errorf("invalid kind %q; must be one of: srch, slce, patch, run, done, tool", env.Kind)
	}

	switch env.Kind {
	case "tool":
		return handleTool(env)
	case "patch":
		return handlePatch(env)
	case "done":
		return handleDone(env)
	case "run":
		return handleRun(env)
	default:
		// Direct kind (srch, slce) - treat as tool dispatch
		return ActOutput{
			Kind:    env.Kind,
			Payload: env.Args,
		}, nil
	}
}

// handleTool validates and normalizes a tool action.
func handleTool(env common.ActionEnvelope) (ActOutput, error) {
	if env.Name == "" {
		return ActOutput{}, fmt.Errorf("kind=tool requires a non-empty name")
	}

	if !knownTools[env.Name] {
		return ActOutput{}, fmt.Errorf("unknown tool %q; known tools: srch, slce, pack, map, ctrt, flow, doss, vrfy, exec, graf", env.Name)
	}

	return ActOutput{
		Kind:    env.Name,
		Payload: env.Args,
	}, nil
}

// handlePatch validates and normalizes a patch action.
func handlePatch(env common.ActionEnvelope) (ActOutput, error) {
	if env.Diff == "" {
		return ActOutput{}, fmt.Errorf("kind=patch requires a non-empty diff")
	}

	return ActOutput{
		Kind: "patch",
		Payload: map[string]string{
			"diff": env.Diff,
		},
	}, nil
}

// handleDone validates and normalizes a done action.
func handleDone(env common.ActionEnvelope) (ActOutput, error) {
	if env.Answer == "" {
		return ActOutput{}, fmt.Errorf("kind=done requires a non-empty answer")
	}

	return ActOutput{
		Kind: "done",
		Payload: map[string]string{
			"answer": env.Answer,
			"why":    env.Why,
		},
	}, nil
}

// handleRun validates and normalizes a run action.
func handleRun(env common.ActionEnvelope) (ActOutput, error) {
	// Args should contain the command to run
	// Try to extract cmd from args
	var cmd interface{}
	if args, ok := env.Args.(map[string]interface{}); ok {
		if c, exists := args["cmd"]; exists {
			cmd = c
		} else {
			cmd = env.Args
		}
	} else {
		cmd = env.Args
	}

	return ActOutput{
		Kind: "run",
		Payload: map[string]interface{}{
			"cmd": cmd,
		},
	}, nil
}

// writeError writes an error response to stdout.
func writeError(msg string) {
	result := ActError{
		OK:    false,
		Error: msg,
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write error: %v", err))
	}
}
