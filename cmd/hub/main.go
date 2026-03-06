// Command hub is a file-based message bus for agents (pub/sub).
// Messages are stored as JSONL in .clock/hub/<channel>.jsonl.
// Subcommands: pub, sub, channels, create, drain.
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pedrommaiaa/clock/internal/jsonutil"
)

const defaultHubDir = ".clock/hub"

// hubMessage is a message in a channel.
type hubMessage struct {
	ID      string          `json:"id"`
	TS      string          `json:"ts"`
	Channel string          `json:"channel"`
	From    string          `json:"from"`
	Data    json.RawMessage `json:"data"`
}

// pubInput is the input for publishing a message.
type pubInput struct {
	From string          `json:"from"`
	Data json.RawMessage `json:"data"`
}

func main() {
	if len(os.Args) < 2 {
		jsonutil.Fatal("usage: hub <pub|sub|channels|create|drain> [args]")
	}

	hubDir := os.Getenv("CLOCK_HUB_DIR")
	if hubDir == "" {
		hubDir = defaultHubDir
	}

	if err := os.MkdirAll(hubDir, 0o755); err != nil {
		jsonutil.Fatal(fmt.Sprintf("mkdir %s: %v", hubDir, err))
	}

	cmd := os.Args[1]
	switch cmd {
	case "pub":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: hub pub <channel>")
		}
		doPub(hubDir, os.Args[2])
	case "sub":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: hub sub <channel> [-after <msg_id>]")
		}
		var afterID string
		if len(os.Args) >= 5 && os.Args[3] == "-after" {
			afterID = os.Args[4]
		}
		doSub(hubDir, os.Args[2], afterID)
	case "channels":
		doChannels(hubDir)
	case "create":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: hub create <channel>")
		}
		doCreate(hubDir, os.Args[2])
	case "drain":
		if len(os.Args) < 3 {
			jsonutil.Fatal("usage: hub drain <channel>")
		}
		doDrain(hubDir, os.Args[2])
	default:
		jsonutil.Fatal(fmt.Sprintf("unknown subcommand %q; use pub, sub, channels, create, drain", cmd))
	}
}

// generateMsgID creates a unique message ID: <unix_ms>-<random_hex>.
func generateMsgID() string {
	ts := time.Now().UnixMilli()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d-%d", ts, time.Now().UnixNano()%100000)
	}
	return fmt.Sprintf("%d-%s", ts, hex.EncodeToString(b))
}

// channelPath returns the file path for a channel.
func channelPath(hubDir, channel string) string {
	return filepath.Join(hubDir, channel+".jsonl")
}

// lockPath returns the lock file path for a channel.
func lockPath(hubDir, channel string) string {
	return filepath.Join(hubDir, channel+".lock")
}

// acquireLock acquires an exclusive flock on a channel lock file.
func acquireLock(hubDir, channel string) (*os.File, error) {
	lp := lockPath(hubDir, channel)
	f, err := os.OpenFile(lp, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("flock: %w", err)
	}
	return f, nil
}

// releaseLock releases and closes the lock file.
func releaseLock(f *os.File) {
	if f != nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
}

// doPub reads a message from stdin and appends it to the channel.
func doPub(hubDir, channel string) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read stdin: %v", err))
	}
	if len(data) == 0 {
		jsonutil.Fatal("empty input")
	}

	// Parse the input to extract from and data fields
	var input pubInput
	if err := json.Unmarshal(data, &input); err != nil {
		// If it's not a pubInput, treat the whole thing as data
		input.Data = json.RawMessage(data)
		input.From = "unknown"
	}

	lk, err := acquireLock(hubDir, channel)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("lock: %v", err))
	}
	defer releaseLock(lk)

	msg := hubMessage{
		ID:      generateMsgID(),
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		Channel: channel,
		From:    input.From,
		Data:    input.Data,
	}

	// Marshal the message
	line, err := json.Marshal(msg)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("marshal message: %v", err))
	}

	// Append to channel file
	cp := channelPath(hubDir, channel)
	f, err := os.OpenFile(cp, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("open channel %s: %v", channel, err))
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		jsonutil.Fatal(fmt.Sprintf("write message: %v", err))
	}
	if err := f.Close(); err != nil {
		jsonutil.Fatal(fmt.Sprintf("close channel: %v", err))
	}

	// Output the published message
	result := map[string]interface{}{
		"ok": true,
		"id": msg.ID,
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// doSub reads all messages from a channel and outputs them as JSONL.
// If afterID is set, only messages after that ID are output.
func doSub(hubDir, channel, afterID string) {
	lk, err := acquireLock(hubDir, channel)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("lock: %v", err))
	}
	defer releaseLock(lk)

	cp := channelPath(hubDir, channel)
	f, err := os.Open(cp)
	if err != nil {
		if os.IsNotExist(err) {
			// Channel doesn't exist yet — no messages
			return
		}
		jsonutil.Fatal(fmt.Sprintf("open channel %s: %v", channel, err))
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	enc := json.NewEncoder(os.Stdout)
	pastAfter := afterID == ""

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		if !pastAfter {
			// Check if this message has the afterID
			var msg hubMessage
			if err := json.Unmarshal(line, &msg); err != nil {
				continue
			}
			if msg.ID == afterID {
				pastAfter = true
			}
			continue
		}

		// Output the raw JSONL line
		var msg hubMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		if err := enc.Encode(msg); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write jsonl: %v", err))
		}
	}

	if err := scanner.Err(); err != nil {
		jsonutil.Fatal(fmt.Sprintf("read channel: %v", err))
	}
}

// doChannels lists all available channels.
func doChannels(hubDir string) {
	entries, err := os.ReadDir(hubDir)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("read hub dir: %v", err))
	}

	var channels []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".jsonl") {
			ch := strings.TrimSuffix(name, ".jsonl")
			channels = append(channels, ch)
		}
	}

	if channels == nil {
		channels = []string{}
	}

	result := map[string]interface{}{
		"channels": channels,
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// doCreate creates a new channel file.
func doCreate(hubDir, channel string) {
	cp := channelPath(hubDir, channel)

	// Check if already exists
	if _, err := os.Stat(cp); err == nil {
		result := map[string]interface{}{
			"ok":      true,
			"channel": channel,
			"created": false,
		}
		if err := jsonutil.WriteOutput(result); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
		}
		return
	}

	// Create empty channel file
	f, err := os.Create(cp)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("create channel %s: %v", channel, err))
	}
	f.Close()

	result := map[string]interface{}{
		"ok":      true,
		"channel": channel,
		"created": true,
	}
	if err := jsonutil.WriteOutput(result); err != nil {
		jsonutil.Fatal(fmt.Sprintf("write output: %v", err))
	}
}

// doDrain reads and deletes all messages from a channel atomically.
func doDrain(hubDir, channel string) {
	lk, err := acquireLock(hubDir, channel)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("lock: %v", err))
	}
	defer releaseLock(lk)

	cp := channelPath(hubDir, channel)
	tmpPath := cp + ".drain"

	// Rename to temp for atomic read+delete
	if err := os.Rename(cp, tmpPath); err != nil {
		if os.IsNotExist(err) {
			// Channel doesn't exist or is empty
			fmt.Println("[]")
			return
		}
		jsonutil.Fatal(fmt.Sprintf("rename channel: %v", err))
	}

	// Read all messages from the temp file
	f, err := os.Open(tmpPath)
	if err != nil {
		// Restore the original file on failure
		os.Rename(tmpPath, cp)
		jsonutil.Fatal(fmt.Sprintf("open drain file: %v", err))
	}

	var messages []hubMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg hubMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		messages = append(messages, msg)
	}
	f.Close()

	if err := scanner.Err(); err != nil {
		// Restore on failure
		os.Rename(tmpPath, cp)
		jsonutil.Fatal(fmt.Sprintf("read drain file: %v", err))
	}

	// Delete the temp file
	os.Remove(tmpPath)

	// Re-create an empty channel file
	emptyF, err := os.Create(cp)
	if err != nil {
		jsonutil.Fatal(fmt.Sprintf("recreate channel: %v", err))
	}
	emptyF.Close()

	// Output drained messages
	enc := json.NewEncoder(os.Stdout)
	for _, msg := range messages {
		if err := enc.Encode(msg); err != nil {
			jsonutil.Fatal(fmt.Sprintf("write jsonl: %v", err))
		}
	}
}
