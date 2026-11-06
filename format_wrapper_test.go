package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// configs/claude/format.sh adapts the shared script to Claude's hook protocol:
// payload in on stdin, JSON out on stdout. These tests run it for real against
// a stub shared script, so what is checked is Claude's observable output.

type formatWrapperEnv struct {
	home        string
	configHome  string
	wrapper     string
	callLogPath string
}

func newFormatWrapperEnv(t *testing.T, findings string) *formatWrapperEnv {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not found on PATH; skipping format.sh behavioral test")
	}
	root := t.TempDir()
	env := &formatWrapperEnv{
		home:       filepath.Join(root, "home"),
		configHome: filepath.Join(root, "config"),
		wrapper:    filepath.Join(root, "format.sh"),
	}
	mustMkdirAll(t, env.home)
	env.callLogPath = filepath.Join(env.configHome, "devgeta", "calls")

	src, err := fs.ReadFile(ConfigsFS, "configs/claude/format.sh")
	if err != nil {
		t.Fatalf("read embedded format.sh: %v", err)
	}
	if err := os.WriteFile(env.wrapper, src, 0o755); err != nil {
		t.Fatal(err)
	}

	// The stub records each call and prints the findings it was given.
	stub := "#!/bin/sh\necho \"$1\" >> \"$(dirname \"$0\")/calls\"\ncat \"$(dirname \"$0\")/findings\"\n"
	mustWriteFile(t, filepath.Join(env.configHome, "devgeta", "findings"), findings)
	if err := os.WriteFile(filepath.Join(env.configHome, "devgeta", "format-file.sh"),
		[]byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	return env
}

func (e *formatWrapperEnv) calls() []string {
	data, err := os.ReadFile(e.callLogPath)
	if err != nil {
		return nil
	}
	return strings.Fields(string(data))
}

func (e *formatWrapperEnv) run(t *testing.T, payload string) (string, int) {
	t.Helper()
	cmd := exec.Command(e.wrapper)
	cmd.Stdin = strings.NewReader(payload)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + e.home,
		"XDG_CONFIG_HOME=" + e.configHome,
		"XDG_DATA_HOME=" + filepath.Join(e.home, ".local", "share"),
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		return stdout.String(), exitErr.ExitCode()
	}
	if err != nil {
		t.Fatalf("run format.sh: %v", err)
	}
	return stdout.String(), 0
}

func hookPayload(t *testing.T, file string) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"tool_input": map[string]string{"file_path": file}})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFormatWrapper(t *testing.T) {
	t.Run("findings become additionalContext with the original prefix", func(t *testing.T) {
		env := newFormatWrapperEnv(t, "[flake8]\nx.py:1:1: F401 unused\n\n")
		file := filepath.Join(t.TempDir(), "x.py")
		mustWriteFile(t, file, "import os\n")

		out, code := env.run(t, hookPayload(t, file))

		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		var got struct {
			HookSpecificOutput struct {
				HookEventName     string `json:"hookEventName"`
				AdditionalContext string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("stdout is not valid JSON: %v\n%s", err, out)
		}
		if got.HookSpecificOutput.HookEventName != "PostToolUse" {
			t.Errorf("hookEventName = %q", got.HookSpecificOutput.HookEventName)
		}
		want := "Linter findings for " + file + " — please fix:\n[flake8]\nx.py:1:1: F401 unused"
		if strings.TrimSpace(got.HookSpecificOutput.AdditionalContext) != want {
			t.Errorf("additionalContext = %q, want %q", got.HookSpecificOutput.AdditionalContext, want)
		}
	})

	t.Run("no findings means no output at all", func(t *testing.T) {
		env := newFormatWrapperEnv(t, "")
		file := filepath.Join(t.TempDir(), "x.py")
		mustWriteFile(t, file, "x = 1\n")

		out, code := env.run(t, hookPayload(t, file))

		if out != "" || code != 0 {
			t.Errorf("want no output and exit 0, got %q exit %d", out, code)
		}
		if calls := env.calls(); len(calls) != 1 || calls[0] != file {
			t.Errorf("shared script should run once on the edited file, calls = %v", calls)
		}
	})

	t.Run("a file that does not exist never reaches the script", func(t *testing.T) {
		env := newFormatWrapperEnv(t, "[x]\nfinding\n")
		out, code := env.run(t, hookPayload(t, filepath.Join(t.TempDir(), "gone.py")))
		if out != "" || code != 0 || len(env.calls()) != 0 {
			t.Errorf("want silence, got %q exit %d calls %v", out, code, env.calls())
		}
	})

	t.Run("a missing shared script exits 0 silently", func(t *testing.T) {
		env := newFormatWrapperEnv(t, "[x]\nfinding\n")
		if err := os.Remove(filepath.Join(env.configHome, "devgeta", "format-file.sh")); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(t.TempDir(), "x.py")
		mustWriteFile(t, file, "x = 1\n")

		if out, code := env.run(t, hookPayload(t, file)); out != "" || code != 0 {
			t.Errorf("want silence, got %q exit %d", out, code)
		}
	})

	t.Run("an empty or malformed payload exits 0 silently", func(t *testing.T) {
		env := newFormatWrapperEnv(t, "[x]\nfinding\n")
		for _, payload := range []string{"", "not json", `{"tool_input":{}}`} {
			if out, code := env.run(t, payload); out != "" || code != 0 {
				t.Errorf("payload %q: want silence, got %q exit %d", payload, out, code)
			}
		}
		if len(env.calls()) != 0 {
			t.Errorf("script ran for an unusable payload: %v", env.calls())
		}
	})

	// The grandfathered exception (TestEmbeddedConfigsNameNoPersonalCheckoutPaths)
	// lives only in Claude's wrapper, so Pi never inherits it.
	t.Run("the grandfathered private-repo HTML skip never calls the script", func(t *testing.T) {
		env := newFormatWrapperEnv(t, "[x]\nfinding\n")
		skipped := filepath.Join(env.home, "lever", "hire2", "page.html")
		mustWriteFile(t, skipped, "<p>x</p>\n")

		if out, code := env.run(t, hookPayload(t, skipped)); out != "" || code != 0 {
			t.Errorf("want silence, got %q exit %d", out, code)
		}
		if len(env.calls()) != 0 {
			t.Errorf("script ran for the skipped path: %v", env.calls())
		}
	})

	t.Run("other HTML files still go through the script", func(t *testing.T) {
		env := newFormatWrapperEnv(t, "")
		other := filepath.Join(env.home, "lever", "other", "page.html")
		mustWriteFile(t, other, "<p>x</p>\n")

		env.run(t, hookPayload(t, other))

		if calls := env.calls(); len(calls) != 1 || calls[0] != other {
			t.Errorf("script should run once on %s, calls = %v", other, calls)
		}
	})
}
