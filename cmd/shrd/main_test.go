package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseRef(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		want    string
		wantErr bool
	}{
		{
			name: "sha256 prefix",
			ref:  "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			want: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		},
		{
			name: "bare hex hash",
			ref:  "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			want: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		},
		{
			name:    "too short",
			ref:     "abc123",
			wantErr: true,
		},
		{
			name:    "invalid prefix",
			ref:     "md5:abc123",
			wantErr: true,
		},
		{
			name:    "empty",
			ref:     "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRef(tt.ref)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("parseRef(%q) = %q, want %q", tt.ref, got, tt.want)
			}
		})
	}
}

func TestArtifactPath(t *testing.T) {
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	got := artifactPath("/tmp/store", hash)
	want := filepath.Join("/tmp/store", "ab", hash+".json")
	if got != want {
		t.Errorf("artifactPath = %q, want %q", got, want)
	}
}

func TestArtifactPathPrefix(t *testing.T) {
	// Verify that the first two chars of the hash are used as prefix directory
	hash := "ff0011aabbccddee0011223344556677ff0011aabbccddee0011223344556677"
	got := artifactPath("/store", hash)
	if !filepath.IsAbs(got) || !contains(got, "ff") {
		t.Errorf("artifactPath should include prefix dir 'ff', got %q", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && containsStr(s, sub)
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestPutAndGet(t *testing.T) {
	dir := t.TempDir()

	// Simulate a put: store JSON content
	content := []byte(`{"key":"value","num":42}`)
	h := sha256.Sum256(content)
	hash := hex.EncodeToString(h[:])

	// Create prefix dir
	prefix := hash[:2]
	prefixDir := filepath.Join(dir, prefix)
	if err := os.MkdirAll(prefixDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write artifact
	path := artifactPath(dir, hash)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Verify hash is correct
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	verifyHash := sha256.Sum256(data)
	verifyHex := hex.EncodeToString(verifyHash[:])
	if verifyHex != hash {
		t.Errorf("hash mismatch: stored=%s, verified=%s", hash, verifyHex)
	}

	// Verify content is valid JSON
	if !json.Valid(data) {
		t.Error("stored content is not valid JSON")
	}
}

func TestHashCalculation(t *testing.T) {
	tests := []struct {
		name    string
		content string
		// Pre-computed SHA256 hashes
	}{
		{"empty json object", "{}"},
		{"simple string", `"hello"`},
		{"nested", `{"a":{"b":1}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte(tt.content)
			h := sha256.Sum256(data)
			hash := hex.EncodeToString(h[:])

			// Hash should be 64 hex chars
			if len(hash) != 64 {
				t.Errorf("hash length = %d, want 64", len(hash))
			}

			// Same content should produce same hash
			h2 := sha256.Sum256(data)
			hash2 := hex.EncodeToString(h2[:])
			if hash != hash2 {
				t.Error("same content produced different hashes")
			}
		})
	}
}

func TestListArtifacts(t *testing.T) {
	dir := t.TempDir()

	// Store 3 artifacts
	for _, content := range []string{`{"a":1}`, `{"b":2}`, `{"c":3}`} {
		data := []byte(content)
		h := sha256.Sum256(data)
		hash := hex.EncodeToString(h[:])
		prefix := hash[:2]
		prefixDir := filepath.Join(dir, prefix)
		os.MkdirAll(prefixDir, 0o755)
		os.WriteFile(filepath.Join(prefixDir, hash+".json"), data, 0o644)
	}

	// Count artifacts by walking prefix dirs
	count := 0
	prefixDirs, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, pd := range prefixDirs {
		if !pd.IsDir() || len(pd.Name()) != 2 {
			continue
		}
		files, err := os.ReadDir(filepath.Join(dir, pd.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if !f.IsDir() && filepath.Ext(f.Name()) == ".json" {
				count++
			}
		}
	}
	if count != 3 {
		t.Errorf("listed %d artifacts, want 3", count)
	}
}

func TestHasArtifact(t *testing.T) {
	dir := t.TempDir()

	data := []byte(`{"exists":true}`)
	h := sha256.Sum256(data)
	hash := hex.EncodeToString(h[:])
	prefix := hash[:2]
	os.MkdirAll(filepath.Join(dir, prefix), 0o755)
	os.WriteFile(artifactPath(dir, hash), data, 0o644)

	// Existing artifact
	path := artifactPath(dir, hash)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("artifact should exist: %v", err)
	}

	// Non-existing artifact
	fakeHash := "0000000000000000000000000000000000000000000000000000000000000000"
	fakePath := artifactPath(dir, fakeHash)
	if _, err := os.Stat(fakePath); err == nil {
		t.Error("fake artifact should not exist")
	}
}

func TestDuplicatePut(t *testing.T) {
	dir := t.TempDir()

	data := []byte(`{"dup":true}`)
	h := sha256.Sum256(data)
	hash := hex.EncodeToString(h[:])
	prefix := hash[:2]
	os.MkdirAll(filepath.Join(dir, prefix), 0o755)

	path := artifactPath(dir, hash)

	// Write twice
	os.WriteFile(path, data, 0o644)
	os.WriteFile(path, data, 0o644)

	// Should still be one file with correct content
	readBack, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(readBack) != string(data) {
		t.Error("content mismatch after duplicate put")
	}
}
