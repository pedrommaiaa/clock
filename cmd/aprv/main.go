// Command aprv is a human approval gateway using file-based approvals.
// Subcommands: request, list, ok, deny, check, defer.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

const baseDir = ".clock/approvals"

// ApprovalRequest is the input for the request subcommand.
type ApprovalRequest struct {
	ID      string   `json:"id"`
	Diff    string   `json:"diff"`
	Risk    float64  `json:"risk"`
	Reasons []string `json:"reasons"`
}

// ApprovalEntry is the stored approval record.
type ApprovalEntry struct {
	ID        string   `json:"id"`
	Diff      string   `json:"diff,omitempty"`
	Risk      float64  `json:"risk"`
	Reasons   []string `json:"reasons,omitempty"`
	Status    string   `json:"status"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at,omitempty"`
	Reason    string   `json:"reason,omitempty"` // denial reason
}

// StatusResponse is the output for status queries.
type StatusResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func main() {
	if len(os.Args) < 2 {
		jsonutil.Fatal("usage: aprv <request|list|ok|deny|check|defer> [args...]")
	}

	ensureDirs()

	cmd := os.Args[1]
	switch cmd {
	case "request":
		doRequest()
	case "list":
		doList()
	case "ok":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: aprv ok <id>")
		}
		doOK(os.Args[2])
	case "deny":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: aprv deny <id>")
		}
		reason := ""
		if len(os.Args) >= 4 {
			reason = strings.Join(os.Args[3:], " ")
		}
		doDeny(os.Args[2], reason)
	case "check":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: aprv check <id>")
		}
		doCheck(os.Args[2])
	case "defer":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: aprv defer <id>")
		}
		doDefer(os.Args[2])
	default:
		jsonutil.Fatal(fmt.Sprintf("unknown subcommand: %s", cmd))
	}
}

func ensureDirs() {
	dirs := []string{
		filepath.Join(baseDir, "inbox"),
		filepath.Join(baseDir, "approved"),
		filepath.Join(baseDir, "denied"),
		filepath.Join(baseDir, "deferred"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			jsonutil.Fatal(fmt.Sprintf("mkdir %s: %v", d, err))
		}
	}
}

func doRequest() {
	var req ApprovalRequest
	if err := jsonutil.ReadInput(&req); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read input: %v", err))
	}
	if req.ID == "" {
		jsonutil.Fatal("id is required")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	entry := ApprovalEntry{
		ID:        req.ID,
		Diff:      req.Diff,
		Risk:      req.Risk,
		Reasons:   req.Reasons,
		Status:    "pending",
		CreatedAt: now,
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("marshal: %v", err))
	}

	path := filepath.Join(baseDir, "inbox", req.ID+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write %s: %v", path, err))
	}

	if err := jsonutil.WriteOutput(StatusResponse{ID: req.ID, Status: "pending"}); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func doList() {
	inboxDir := filepath.Join(baseDir, "inbox")
	entries, err := os.ReadDir(inboxDir)
	if err != nil {
		// Empty list is fine if dir doesn't exist yet
		if os.IsNotExist(err) {
			return
		}
		jsonutil.Fatal(fmt.Sprintf("read inbox: %v", err))
	}

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(inboxDir, e.Name()))
		if err != nil {
			continue
		}
		var entry ApprovalEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		jsonutil.WriteJSONL(entry)
	}
}

func doOK(id string) {
	moveApproval(id, "inbox", "approved", "approved")
}

func doDeny(id, reason string) {
	src := filepath.Join(baseDir, "inbox", id+".json")
	data, err := os.ReadFile(src)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read %s: %v", src, err))
	}

	var entry ApprovalEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		jsonutil.Fatal(fmt.Sprintf("unmarshal: %v", err))
	}

	entry.Status = "denied"
	entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if reason != "" {
		entry.Reason = reason
	}

	out, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("marshal: %v", err))
	}

	dst := filepath.Join(baseDir, "denied", id+".json")
	if err := os.WriteFile(dst, out, 0644); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write %s: %v", dst, err))
	}

	if err := os.Remove(src); err != nil {
		jsonutil.Fatal(fmt.Sprintf("remove %s: %v", src, err))
	}

	if err := jsonutil.WriteOutput(StatusResponse{ID: id, Status: "denied"}); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func doCheck(id string) {
	// Search across all directories
	dirs := []struct {
		dir    string
		status string
	}{
		{filepath.Join(baseDir, "inbox"), "pending"},
		{filepath.Join(baseDir, "approved"), "approved"},
		{filepath.Join(baseDir, "denied"), "denied"},
		{filepath.Join(baseDir, "deferred"), "deferred"},
	}

	for _, d := range dirs {
		path := filepath.Join(d.dir, id+".json")
		if _, err := os.Stat(path); err == nil {
			if err := jsonutil.WriteOutput(StatusResponse{ID: id, Status: d.status}); err != nil {
				jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
			}
			return
		}
	}

	if err := jsonutil.WriteOutput(StatusResponse{ID: id, Status: "not_found"}); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

func doDefer(id string) {
	moveApproval(id, "inbox", "deferred", "deferred")
}

func moveApproval(id, fromDir, toDir, newStatus string) {
	src := filepath.Join(baseDir, fromDir, id+".json")
	data, err := os.ReadFile(src)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read %s: %v", src, err))
	}

	var entry ApprovalEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		jsonutil.Fatal(fmt.Sprintf("unmarshal: %v", err))
	}

	entry.Status = newStatus
	entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	out, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("marshal: %v", err))
	}

	dst := filepath.Join(baseDir, toDir, id+".json")
	if err := os.WriteFile(dst, out, 0644); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write %s: %v", dst, err))
	}

	if err := os.Remove(src); err != nil {
		jsonutil.Fatal(fmt.Sprintf("remove %s: %v", src, err))
	}

	if err := jsonutil.WriteOutput(StatusResponse{ID: id, Status: newStatus}); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}
