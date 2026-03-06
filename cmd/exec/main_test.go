package main

import (
	"testing"
)

func TestScrubEnv(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		want []string
	}{
		{
			name: "removes API_KEY",
			env:  []string{"API_KEY=secret", "HOME=/home/user"},
			want: []string{"HOME=/home/user"},
		},
		{
			name: "removes TOKEN vars",
			env:  []string{"GITHUB_TOKEN=abc", "PATH=/usr/bin"},
			want: []string{"PATH=/usr/bin"},
		},
		{
			name: "removes multiple sensitive",
			env: []string{
				"PATH=/usr/bin",
				"AWS_SECRET_ACCESS_KEY=x",
				"OPENAI_API_KEY=sk-abc",
				"HOME=/home",
				"ANTHROPIC_API_KEY=ak-def",
				"GH_TOKEN=ghp_123",
			},
			want: []string{"PATH=/usr/bin", "HOME=/home"},
		},
		{
			name: "removes PASSWORD vars",
			env:  []string{"DB_PASSWORD=hunter2", "LANG=en_US"},
			want: []string{"LANG=en_US"},
		},
		{
			name: "case insensitive matching",
			env:  []string{"my_api_key_here=x", "SHELL=/bin/bash"},
			want: []string{"SHELL=/bin/bash"},
		},
		{
			name: "keeps safe vars",
			env:  []string{"HOME=/home", "PATH=/usr/bin", "LANG=en_US"},
			want: []string{"HOME=/home", "PATH=/usr/bin", "LANG=en_US"},
		},
		{
			name: "empty input",
			env:  []string{},
			want: nil,
		},
		{
			name: "removes PRIVATE_KEY",
			env:  []string{"SSH_PRIVATE_KEY=data", "EDITOR=vim"},
			want: []string{"EDITOR=vim"},
		},
		{
			name: "removes CREDENTIALS",
			env:  []string{"AWS_CREDENTIALS=creds", "TERM=xterm"},
			want: []string{"TERM=xterm"},
		},
		{
			name: "removes SECRET",
			env:  []string{"MY_SECRET_THING=x", "USER=pedro"},
			want: []string{"USER=pedro"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scrubEnv(tt.env)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d\ngot:  %v\nwant: %v", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("env[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "short string not truncated",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exact length not truncated",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "long string truncated",
			input:  "hello world this is long",
			maxLen: 11,
			want:   "hello world\n[truncated]",
		},
		{
			name:   "empty string",
			input:  "",
			maxLen: 10,
			want:   "",
		},
		{
			name:   "maxLen zero",
			input:  "hi",
			maxLen: 0,
			want:   "\n[truncated]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestScrubEnvPreservesOrder(t *testing.T) {
	env := []string{
		"A=1",
		"GITHUB_TOKEN=x",
		"B=2",
		"API_KEY=y",
		"C=3",
	}
	got := scrubEnv(env)
	want := []string{"A=1", "B=2", "C=3"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestScrubEnvHandlesEqualsInValue(t *testing.T) {
	env := []string{
		"MY_VAR=key=value=extra",
		"API_KEY=abc=def",
	}
	got := scrubEnv(env)
	if len(got) != 1 {
		t.Fatalf("expected 1 env var, got %d: %v", len(got), got)
	}
	if got[0] != "MY_VAR=key=value=extra" {
		t.Errorf("got %q, want %q", got[0], "MY_VAR=key=value=extra")
	}
}
