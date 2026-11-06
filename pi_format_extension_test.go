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

// devgeta-format.js is Pi's side of ADR-0063. It is imported under node with a
// fake `pi` host whose exec runs a stub of the shared script, so the tests see
// exactly what Pi would hand back to the model.

const piFormatExtensionDriver = `
import { execFile } from "node:child_process";
const mod = await import(process.env.DG_EXT_URL);
let handler;
const pi = {
  on(name, fn) { if (name === "tool_result") handler = fn; },
  exec(command, args, opts) {
    return new Promise((resolve, reject) =>
      execFile(command, args, { timeout: opts.timeout }, (err, stdout, stderr) =>
        err && err.code === "ENOENT" ? reject(err) : resolve({ stdout, stderr, code: err ? 1 : 0 })));
  },
};
mod.default(pi);
const scenario = JSON.parse(process.env.DG_SCENARIO);
const result = await handler(scenario.event, { cwd: scenario.cwd, signal: undefined });
console.log(JSON.stringify({
  result: result ?? null,
  path: process.env.PATH,
  disableToolInstall: process.env.PI_LENS_DISABLE_TOOL_INSTALL ?? null,
  disableLspInstall: process.env.PI_LENS_DISABLE_LSP_INSTALL ?? null,
}));
`

type piFormatExtensionEnv struct {
	configHome string
	dataHome   string
	masonBin   string
	scriptPath string
	extension  string
	workdir    string
}

type piFormatOutcome struct {
	Result *struct {
		Content           []map[string]string `json:"content"`
		StructuredContent any                 `json:"structuredContent"`
	} `json:"result"`
	Path               string  `json:"path"`
	DisableToolInstall *string `json:"disableToolInstall"`
	DisableLspInstall  *string `json:"disableLspInstall"`
}

func newPiFormatExtensionEnv(t *testing.T) *piFormatExtensionEnv {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not found on PATH; skipping devgeta-format.js behavioral test")
	}
	root := t.TempDir()
	env := &piFormatExtensionEnv{
		configHome: filepath.Join(root, "config"),
		dataHome:   filepath.Join(root, "data"),
		extension:  filepath.Join(root, "devgeta-format.js"),
		workdir:    filepath.Join(root, "work"),
	}
	env.masonBin = filepath.Join(env.dataHome, "nvim", "mason", "bin")
	env.scriptPath = filepath.Join(env.configHome, "devgeta", "format-file.sh")
	mustMkdirAll(t, env.workdir)

	src, err := fs.ReadFile(ConfigsFS, "configs/pi/extensions/devgeta-format.js")
	if err != nil {
		t.Fatalf("read embedded devgeta-format.js: %v", err)
	}
	if err := os.WriteFile(env.extension, src, 0o644); err != nil {
		t.Fatal(err)
	}
	return env
}

// formatScript installs the stub shared script. It records each call in
// "calls" beside itself, then runs body (a shell snippet, $1 is the file).
func (e *piFormatExtensionEnv) formatScript(t *testing.T, body string) {
	t.Helper()
	stub := "#!/bin/sh\necho \"$1\" >> \"$(dirname \"$0\")/calls\"\n" + body + "\n"
	mustMkdirAll(t, filepath.Dir(e.scriptPath))
	if err := os.WriteFile(e.scriptPath, []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
}

func (e *piFormatExtensionEnv) scriptCalls() []string {
	data, _ := os.ReadFile(filepath.Join(filepath.Dir(e.scriptPath), "calls"))
	return strings.Fields(string(data))
}

func (e *piFormatExtensionEnv) run(
	t *testing.T, event map[string]any, extraEnv ...string,
) piFormatOutcome {
	t.Helper()
	scenario, err := json.Marshal(map[string]any{"event": event, "cwd": e.workdir})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", "--input-type=module")
	cmd.Stdin = strings.NewReader(piFormatExtensionDriver)
	cmd.Env = append([]string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"XDG_CONFIG_HOME=" + e.configHome,
		"XDG_DATA_HOME=" + e.dataHome,
		"DG_EXT_URL=file://" + e.extension,
		"DG_SCENARIO=" + string(scenario),
	}, extraEnv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("node driver failed: %v\n%s", err, stderr.String())
	}
	var outcome piFormatOutcome
	if err := json.Unmarshal(stdout.Bytes(), &outcome); err != nil {
		t.Fatalf("driver output is not JSON: %v\n%s", err, stdout.String())
	}
	return outcome
}

func editEvent(tool, path string) map[string]any {
	return map[string]any{
		"toolName": tool,
		"input":    map[string]any{"path": path},
		"isError":  false,
		"content":  []map[string]string{{"type": "text", "text": "Edited."}},
	}
}

// appended returns the text the extension added after the tool's own content.
func (o piFormatOutcome) appended(t *testing.T) string {
	t.Helper()
	if o.Result == nil {
		t.Fatal("expected the extension to return a result, got none")
	}
	content := o.Result.Content
	if len(content) != 2 || content[0]["text"] != "Edited." || content[1]["type"] != "text" {
		t.Fatalf("want the original content plus one text block, got %v", content)
	}
	return content[1]["text"]
}

func TestPiFormatExtension(t *testing.T) {
	t.Run("reports the changed lines and keeps the original content", func(t *testing.T) {
		env := newPiFormatExtensionEnv(t)
		env.formatScript(t, `printf 'a\nB\nc\n' > "$1"`)
		file := filepath.Join(env.workdir, "f.txt")
		mustWriteFile(t, file, "a\nb\nc\n")

		got := env.run(t, editEvent("edit", file)).appended(t)

		if want := "Formatted by devgeta: lines 2–2 changed. They now read:\nB"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("a write is formatted too", func(t *testing.T) {
		env := newPiFormatExtensionEnv(t)
		env.formatScript(t, `printf 'new\n' > "$1"`)
		file := filepath.Join(env.workdir, "f.txt")
		mustWriteFile(t, file, "old\n")

		got := env.run(t, editEvent("write", file)).appended(t)

		if !strings.HasPrefix(got, "Formatted by devgeta: lines 1–1 changed.") {
			t.Errorf("got %q", got)
		}
	})

	t.Run("a relative path resolves against the session directory", func(t *testing.T) {
		env := newPiFormatExtensionEnv(t)
		env.formatScript(t, `printf 'x\n' > "$1"`)
		mustWriteFile(t, filepath.Join(env.workdir, "sub", "f.txt"), "y\n")

		env.run(t, editEvent("edit", "sub/f.txt")).appended(t)

		want := filepath.Join(env.workdir, "sub", "f.txt")
		if calls := env.scriptCalls(); len(calls) != 1 || calls[0] != want {
			t.Errorf("script should get the absolute path %s, calls = %v", want, calls)
		}
	})

	t.Run("linter findings are appended under the path the model used", func(t *testing.T) {
		env := newPiFormatExtensionEnv(t)
		env.formatScript(t, `printf '[flake8]\nf.py:1:1: F401 unused\n\n'`)
		mustWriteFile(t, filepath.Join(env.workdir, "f.py"), "import os\n")

		got := env.run(t, editEvent("edit", "f.py")).appended(t)

		if want := "Linter findings for f.py — please fix:\n[flake8]\nf.py:1:1: F401 unused"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("a reformat and findings arrive in one block, reformat first", func(t *testing.T) {
		env := newPiFormatExtensionEnv(t)
		env.formatScript(t, "printf 'B\\n' > \"$1\"\nprintf '[x]\\nfinding\\n'")
		file := filepath.Join(env.workdir, "f.txt")
		mustWriteFile(t, file, "b\n")

		got := env.run(t, editEvent("edit", file)).appended(t)

		want := "Formatted by devgeta: lines 1–1 changed. They now read:\nB\n\n" +
			"Linter findings for " + file + " — please fix:\n[x]\nfinding"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("an edit that needs no change adds nothing", func(t *testing.T) {
		env := newPiFormatExtensionEnv(t)
		env.formatScript(t, "true")
		file := filepath.Join(env.workdir, "f.txt")
		mustWriteFile(t, file, "same\n")

		if out := env.run(t, editEvent("edit", file)); out.Result != nil {
			t.Errorf("expected no result, got %+v", out.Result)
		}
	})

	t.Run("a reformat too large to show tells the model to re-read the file", func(t *testing.T) {
		env := newPiFormatExtensionEnv(t)
		env.formatScript(t, `i=0; while [ $i -lt 100 ]; do echo "line $i"; i=$((i+1)); done > "$1"`)
		file := filepath.Join(env.workdir, "f.txt")
		mustWriteFile(t, file, "one\n")

		got := env.run(t, editEvent("edit", file)).appended(t)

		if !strings.Contains(got, "reformatted beyond what fits here; read the file before editing it.") {
			t.Errorf("missing the re-read instruction: %q", got)
		}
		if strings.Contains(got, "line 50") {
			t.Errorf("an over-cap reformat must not dump the lines: %q", got)
		}
	})

	t.Run("structuredContent survives, since replacing content would drop it", func(t *testing.T) {
		env := newPiFormatExtensionEnv(t)
		env.formatScript(t, `printf 'B\n' > "$1"`)
		file := filepath.Join(env.workdir, "f.txt")
		mustWriteFile(t, file, "b\n")
		event := editEvent("edit", file)
		event["structuredContent"] = map[string]any{"diff": "keep"}

		out := env.run(t, event)

		out.appended(t)
		if got, _ := json.Marshal(out.Result.StructuredContent); string(got) != `{"diff":"keep"}` {
			t.Errorf("structuredContent = %s", got)
		}
	})

	t.Run("a failed edit and other tools are left alone and never formatted", func(t *testing.T) {
		env := newPiFormatExtensionEnv(t)
		env.formatScript(t, `printf 'B\n' > "$1"`)
		file := filepath.Join(env.workdir, "f.txt")
		mustWriteFile(t, file, "b\n")

		failed := editEvent("edit", file)
		failed["isError"] = true
		bash := editEvent("bash", file)
		for name, event := range map[string]map[string]any{"failed edit": failed, "bash": bash} {
			if out := env.run(t, event); out.Result != nil {
				t.Errorf("%s: expected no result, got %+v", name, out.Result)
			}
		}
		if calls := env.scriptCalls(); len(calls) != 0 {
			t.Errorf("script ran for an event it should ignore: %v", calls)
		}
		if got, _ := os.ReadFile(file); string(got) != "b\n" {
			t.Errorf("file changed: %q", got)
		}
	})

	t.Run("a missing script or file never fails the edit", func(t *testing.T) {
		env := newPiFormatExtensionEnv(t)
		file := filepath.Join(env.workdir, "f.txt")
		mustWriteFile(t, file, "b\n")
		if out := env.run(t, editEvent("edit", file)); out.Result != nil {
			t.Errorf("missing script: expected no result, got %+v", out.Result)
		}

		env.formatScript(t, "true")
		gone := filepath.Join(env.workdir, "gone.txt")
		if out := env.run(t, editEvent("edit", gone)); out.Result != nil {
			t.Errorf("missing file: expected no result, got %+v", out.Result)
		}
	})

	t.Run("an event with no path is ignored", func(t *testing.T) {
		env := newPiFormatExtensionEnv(t)
		env.formatScript(t, "true")
		event := editEvent("edit", "")
		if out := env.run(t, event); out.Result != nil || len(env.scriptCalls()) != 0 {
			t.Errorf("want no result and no script call, got %+v %v", out.Result, env.scriptCalls())
		}
	})
}

func TestPiFormatExtensionEnvironment(t *testing.T) {
	event := func(env *piFormatExtensionEnv) map[string]any {
		return editEvent("edit", filepath.Join(env.workdir, "none.txt"))
	}

	t.Run("puts Mason first on PATH and turns pi-lens downloads off", func(t *testing.T) {
		env := newPiFormatExtensionEnv(t)
		mustMkdirAll(t, env.masonBin)

		out := env.run(t, event(env))

		if first := strings.Split(out.Path, string(os.PathListSeparator))[0]; first != env.masonBin {
			t.Errorf("Mason dir should lead PATH, first entry is %q", first)
		}
		for name, got := range map[string]*string{
			"PI_LENS_DISABLE_TOOL_INSTALL": out.DisableToolInstall,
			"PI_LENS_DISABLE_LSP_INSTALL":  out.DisableLspInstall,
		} {
			if got == nil || *got != "1" {
				t.Errorf("%s should be 1, got %v", name, got)
			}
		}
	})

	t.Run("does not add Mason twice", func(t *testing.T) {
		env := newPiFormatExtensionEnv(t)
		mustMkdirAll(t, env.masonBin)

		out := env.run(t, event(env), "PATH="+env.masonBin+string(os.PathListSeparator)+os.Getenv("PATH"))

		if n := strings.Count(out.Path, env.masonBin); n != 1 {
			t.Errorf("Mason dir appears %d times on PATH: %s", n, out.Path)
		}
	})

	t.Run("leaves PATH alone when there is no Mason directory", func(t *testing.T) {
		env := newPiFormatExtensionEnv(t)

		out := env.run(t, event(env))

		if strings.Contains(out.Path, env.masonBin) {
			t.Errorf("a missing Mason dir was put on PATH: %s", out.Path)
		}
	})

	t.Run("keeps pi-lens switches the user already set", func(t *testing.T) {
		env := newPiFormatExtensionEnv(t)

		out := env.run(t, event(env),
			"PI_LENS_DISABLE_TOOL_INSTALL=0", "PI_LENS_DISABLE_LSP_INSTALL=0")

		if out.DisableToolInstall == nil || *out.DisableToolInstall != "0" ||
			out.DisableLspInstall == nil || *out.DisableLspInstall != "0" {
			t.Errorf("user's switches were overwritten: %v %v", out.DisableToolInstall, out.DisableLspInstall)
		}
	})
}
