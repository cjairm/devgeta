package main

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The shared formatter script (ADR-0063) is run for real against fake Mason
// binaries: the table of tools is what Claude and Pi both depend on, and a
// textual check of the script would not notice a tool called with the wrong
// arguments.

// golangci-lint v2 prints a "0 issues." summary on a clean run unless
// --show-stats=false is given, and knows --fast-only.
const fakeGolangciLintV2 = `#!/bin/sh
stats=1; file=""
for a in "$@"; do
  case "$a" in
    run|--fast-only) ;;
    --show-stats=false) stats=0 ;;
    -*) echo "Error: unknown flag: $a" >&2; exit 3 ;;
    *) file="$a" ;;
  esac
done
if grep -q BAD "$file"; then echo "$file:1:1: found BAD (fake)"; exit 1; fi
[ "$stats" = 1 ] && echo "0 issues."
exit 0
`

// golangci-lint v1 rejects --fast-only and spells it --fast; it prints nothing
// on a clean run.
const fakeGolangciLintV1 = `#!/bin/sh
file=""
for a in "$@"; do
  case "$a" in
    run|--fast) ;;
    -*) echo "Error: unknown flag: $a" >&2; exit 3 ;;
    *) file="$a" ;;
  esac
done
if grep -q BAD "$file"; then echo "$file:1:1: found BAD (fake v1)"; exit 1; fi
exit 0
`

type formatFileEnv struct {
	dataHome string
	masonBin string
	script   string
}

// newFormatFileEnv deploys the embedded script next to an empty fake Mason
// directory under a throwaway XDG_DATA_HOME. tweak, when set, edits the script
// text before it is written.
func newFormatFileEnv(t *testing.T, tweak func(string) string) *formatFileEnv {
	t.Helper()
	root := t.TempDir()
	env := &formatFileEnv{
		dataHome: filepath.Join(root, "data"),
		script:   filepath.Join(root, "format-file.sh"),
	}
	env.masonBin = filepath.Join(env.dataHome, "nvim", "mason", "bin")
	mustMkdirAll(t, env.masonBin)

	src, err := fs.ReadFile(ConfigsFS, "configs/devgeta/format-file.sh")
	if err != nil {
		t.Fatalf("read embedded format-file.sh: %v", err)
	}
	text := string(src)
	if tweak != nil {
		text = tweak(text)
	}
	if err := os.WriteFile(env.script, []byte(text), 0o755); err != nil {
		t.Fatal(err)
	}
	return env
}

func (e *formatFileEnv) tool(t *testing.T, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(e.masonBin, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// run returns stdout and the exit code. HOME points at an empty directory so
// the only Mason the script can find is the fake one under XDG_DATA_HOME.
func (e *formatFileEnv) run(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(e.script, args...)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"XDG_DATA_HOME=" + e.dataHome,
	}
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return string(out), exitErr.ExitCode()
		}
		t.Fatalf("run format-file.sh: %v", err)
	}
	return string(out), 0
}

func TestFormatFile(t *testing.T) {
	t.Run("formats the file in place and prints nothing", func(t *testing.T) {
		env := newFormatFileEnv(t, nil)
		// Chatter on stdout would corrupt the findings callers embed.
		env.tool(t, "gofumpt", "#!/bin/sh\necho 'formatting...'\nprintf 'formatted\\n' > \"$2\"\n")
		file := filepath.Join(t.TempDir(), "main.go")
		mustWriteFile(t, file, "package  main\n")

		out, code := env.run(t, file)

		if out != "" || code != 0 {
			t.Errorf("want no output and exit 0, got %q exit %d", out, code)
		}
		if got, _ := os.ReadFile(file); string(got) != "formatted\n" {
			t.Errorf("file was not formatted in place, got %q", got)
		}
	})

	// golangci-lint v2's "0 issues." summary used to reach the agent as a
	// finding to fix after every clean Go edit.
	t.Run("a clean Go file reports nothing under golangci-lint v2", func(t *testing.T) {
		env := newFormatFileEnv(t, nil)
		env.tool(t, "golangci-lint", fakeGolangciLintV2)
		file := filepath.Join(t.TempDir(), "main.go")
		mustWriteFile(t, file, "package main\n")

		if out, code := env.run(t, file); out != "" || code != 0 {
			t.Errorf("want no output and exit 0, got %q exit %d", out, code)
		}
	})

	t.Run("a Go finding is reported under golangci-lint v2", func(t *testing.T) {
		env := newFormatFileEnv(t, nil)
		env.tool(t, "golangci-lint", fakeGolangciLintV2)
		file := filepath.Join(t.TempDir(), "main.go")
		mustWriteFile(t, file, "BAD\n")

		out, _ := env.run(t, file)

		want := "[golangci-lint]\n" + file + ":1:1: found BAD (fake)\n\n"
		if out != want {
			t.Errorf("want %q, got %q", want, out)
		}
	})

	t.Run("falls back to --fast on golangci-lint v1", func(t *testing.T) {
		env := newFormatFileEnv(t, nil)
		env.tool(t, "golangci-lint", fakeGolangciLintV1)
		file := filepath.Join(t.TempDir(), "main.go")
		mustWriteFile(t, file, "BAD\n")

		out, _ := env.run(t, file)

		want := "[golangci-lint]\n" + file + ":1:1: found BAD (fake v1)\n\n"
		if out != want {
			t.Errorf("v1 fallback: want %q, got %q", want, out)
		}
		// The rejected first attempt must not leak into the findings.
		if strings.Contains(out, "unknown flag") {
			t.Errorf("the usage error reached the findings: %q", out)
		}
	})

	t.Run("Python findings come back under the flake8 label", func(t *testing.T) {
		env := newFormatFileEnv(t, nil)
		env.tool(t, "flake8", "#!/bin/sh\necho \"$1:1:1: F401 'os' imported but unused\"\nexit 1\n")
		file := filepath.Join(t.TempDir(), "x.py")
		mustWriteFile(t, file, "import os\n")

		out, code := env.run(t, file)

		want := "[flake8]\n" + file + ":1:1: F401 'os' imported but unused\n\n"
		if out != want || code != 0 {
			t.Errorf("want %q exit 0, got %q exit %d", want, out, code)
		}
	})

	t.Run("a linter that outruns its budget is silent", func(t *testing.T) {
		env := newFormatFileEnv(t, func(script string) string {
			shortened := strings.Replace(script, "LINT_TIMEOUT=15", "LINT_TIMEOUT=1", 1)
			if shortened == script {
				t.Fatal("LINT_TIMEOUT=15 not found in format-file.sh; update this test")
			}
			return shortened
		})
		env.tool(t, "flake8", "#!/bin/sh\necho half a result\nsleep 30\n")
		file := filepath.Join(t.TempDir(), "x.py")
		mustWriteFile(t, file, "import os\n")

		out, code := env.run(t, file)

		if out != "" || code != 0 {
			t.Errorf("a timed-out linter must print nothing and exit 0, got %q exit %d", out, code)
		}
	})

	t.Run("an extension with no rule prints nothing and leaves the file", func(t *testing.T) {
		env := newFormatFileEnv(t, nil)
		file := filepath.Join(t.TempDir(), "notes.xyz")
		mustWriteFile(t, file, "keep me\n")

		out, code := env.run(t, file)

		if out != "" || code != 0 {
			t.Errorf("want no output and exit 0, got %q exit %d", out, code)
		}
		if got, _ := os.ReadFile(file); string(got) != "keep me\n" {
			t.Errorf("file changed: %q", got)
		}
	})

	t.Run("a missing path, a directory and no argument all exit 0 silently", func(t *testing.T) {
		env := newFormatFileEnv(t, nil)
		env.tool(t, "flake8", "#!/bin/sh\necho finding\n")
		for name, args := range map[string][]string{
			"missing":   {filepath.Join(t.TempDir(), "gone.py")},
			"directory": {t.TempDir()},
			"none":      {},
		} {
			if out, code := env.run(t, args...); out != "" || code != 0 {
				t.Errorf("%s: want no output and exit 0, got %q exit %d", name, out, code)
			}
		}
	})

	t.Run("with no Mason directory it does nothing and exits 0", func(t *testing.T) {
		env := newFormatFileEnv(t, nil)
		if err := os.RemoveAll(filepath.Join(env.dataHome, "nvim")); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(t.TempDir(), "x.py")
		mustWriteFile(t, file, "import os\n")

		if out, code := env.run(t, file); out != "" || code != 0 {
			t.Errorf("want no output and exit 0, got %q exit %d", out, code)
		}
	})

	// Mason installs under Neovim's stdpath("data"), which honors XDG_DATA_HOME;
	// the old hook hard-coded $HOME/.local/share.
	t.Run("looks for Mason under XDG_DATA_HOME, not under HOME", func(t *testing.T) {
		env := newFormatFileEnv(t, nil)
		env.tool(t, "stylua", "#!/bin/sh\nprintf 'from xdg\\n' > \"$1\"\n")
		home := t.TempDir()
		decoy := filepath.Join(home, ".local", "share", "nvim", "mason", "bin")
		mustMkdirAll(t, decoy)
		if err := os.WriteFile(filepath.Join(decoy, "stylua"),
			[]byte("#!/bin/sh\nprintf 'from home\\n' > \"$1\"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(t.TempDir(), "x.lua")
		mustWriteFile(t, file, "local x=1\n")

		cmd := exec.Command(env.script, file)
		cmd.Env = []string{
			"PATH=" + os.Getenv("PATH"),
			"HOME=" + home,
			"XDG_DATA_HOME=" + env.dataHome,
		}
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(file); string(got) != "from xdg\n" {
			t.Errorf("used the wrong Mason directory, file is %q", got)
		}
	})
}
