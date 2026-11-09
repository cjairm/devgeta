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

// The devgeta pack (ADR-0058 as amended, ADR-0068 §7) is the one pack that
// ships inside the binary. Its DEVGETA.md loads into every coder in every
// repository a user opens, so it has to make sense to a stranger.

const devgetaPackDir = "configs/agent-packs/devgeta"

func devgetaPackFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := fs.ReadFile(ConfigsFS, devgetaPackDir+"/"+name)
	if err != nil {
		t.Fatalf("the devgeta pack must ship %s: %v", name, err)
	}
	return data
}

func TestDevgetaPackShipsItsFiles(t *testing.T) {
	manifest := string(devgetaPackFile(t, "pack.yaml"))
	for _, want := range []string{"name: devgeta", "source: embedded"} {
		if !strings.Contains(manifest, want) {
			t.Errorf("pack.yaml must contain %q, got:\n%s", want, manifest)
		}
	}
	if strings.TrimSpace(string(devgetaPackFile(t, "DEVGETA.md"))) == "" {
		t.Error("DEVGETA.md must not be empty")
	}

	var pkg struct {
		Pi struct {
			Extensions []string `json:"extensions"`
		} `json:"pi"`
	}
	if err := json.Unmarshal(devgetaPackFile(t, "package.json"), &pkg); err != nil {
		t.Fatalf("package.json is not valid JSON: %v", err)
	}
	if len(pkg.Pi.Extensions) != 1 {
		t.Fatalf("expected exactly the instructions extension, got %v", pkg.Pi.Extensions)
	}
	devgetaPackFile(t, strings.TrimPrefix(pkg.Pi.Extensions[0], "./"))
}

// TestDevgetaInstructionsMakeSenseToAStranger is CLAUDE.md principle 8 as a
// check: nothing in DEVGETA.md may depend on this repository.
func TestDevgetaInstructionsMakeSenseToAStranger(t *testing.T) {
	text := strings.ToLower(string(devgetaPackFile(t, "DEVGETA.md")))
	for _, repoSpecific := range []string{
		"devgeta", "adr-", "docs/", "go test", "make lint", "agentsv2", "this repo", "claude.md", "release notes",
	} {
		if strings.Contains(text, repoSpecific) {
			t.Errorf("DEVGETA.md must not mention %q: it loads into repositories that have nothing to do with devgeta", repoSpecific)
		}
	}
}

const piInstructionsDriver = `
const mod = await import(process.env.DG_EXT_URL);
const handlers = {};
mod.default({ on(name, fn) { handlers[name] = fn; } });
const scenario = JSON.parse(process.env.DG_SCENARIO);
let result, threw = null;
try { result = await handlers.before_agent_start(scenario.event); } catch (e) { threw = String(e); }
console.log(JSON.stringify({ events: Object.keys(handlers), result: result ?? null, threw }));
`

type piInstructionsOutcome struct {
	Events []string `json:"events"`
	Result *struct {
		SystemPrompt string `json:"systemPrompt"`
	} `json:"result"`
	Threw *string `json:"threw"`
}

// piInstructionsPackage lays the devgeta pack out the way `pi install` sees
// it, and returns its root.
func piInstructionsPackage(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not found on PATH; skipping the devgeta-instructions.js behavioral test")
	}
	root := t.TempDir()
	for _, name := range []string{"package.json", "DEVGETA.md", "extensions/devgeta-instructions.js"} {
		dst := filepath.Join(root, name)
		mustMkdirAll(t, filepath.Dir(dst))
		if err := os.WriteFile(dst, devgetaPackFile(t, name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func runPiInstructions(t *testing.T, root, systemPrompt string) piInstructionsOutcome {
	t.Helper()
	scenario, err := json.Marshal(map[string]any{"event": map[string]any{
		"type": "before_agent_start", "messageType": "user", "prompt": "hi", "systemPrompt": systemPrompt,
	}})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", "--input-type=module")
	cmd.Stdin = strings.NewReader(piInstructionsDriver)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"DG_EXT_URL=file://" + filepath.Join(root, "extensions", "devgeta-instructions.js"),
		"DG_SCENARIO=" + string(scenario),
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("node driver failed: %v\n%s", err, stderr.String())
	}
	var out piInstructionsOutcome
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("driver output is not JSON: %v\n%s", err, stdout.String())
	}
	return out
}

func TestPiInstructionsExtensionAppendsDevgetaMdToTheSystemPrompt(t *testing.T) {
	root := piInstructionsPackage(t)
	text := strings.TrimSpace(string(devgetaPackFile(t, "DEVGETA.md")))

	out := runPiInstructions(t, root, "You are a coding agent.")

	if out.Threw != nil {
		t.Fatalf("the handler threw: %s", *out.Threw)
	}
	if len(out.Events) != 1 || out.Events[0] != "before_agent_start" {
		t.Errorf("expected a single before_agent_start handler, got %v", out.Events)
	}
	if out.Result == nil {
		t.Fatal("expected a systemPrompt result")
	}
	want := "You are a coding agent.\n\n" + text
	if out.Result.SystemPrompt != want {
		t.Errorf("system prompt:\n got %q\nwant %q", out.Result.SystemPrompt, want)
	}
}

func TestPiInstructionsExtensionDoesNotAppendTwice(t *testing.T) {
	root := piInstructionsPackage(t)
	text := strings.TrimSpace(string(devgetaPackFile(t, "DEVGETA.md")))

	out := runPiInstructions(t, root, "base\n\n"+text)

	if out.Result != nil {
		t.Errorf("a prompt that already holds the text must be left alone, got %q", out.Result.SystemPrompt)
	}
}

func TestPiInstructionsExtensionNeverBreaksASession(t *testing.T) {
	t.Run("DEVGETA.md is missing", func(t *testing.T) {
		root := piInstructionsPackage(t)
		if err := os.Remove(filepath.Join(root, "DEVGETA.md")); err != nil {
			t.Fatal(err)
		}
		out := runPiInstructions(t, root, "base")
		if out.Threw != nil || out.Result != nil {
			t.Errorf("a missing file means no change and no error, got threw=%v result=%v", out.Threw, out.Result)
		}
	})
	t.Run("DEVGETA.md is empty", func(t *testing.T) {
		root := piInstructionsPackage(t)
		if err := os.WriteFile(filepath.Join(root, "DEVGETA.md"), []byte("  \n"), 0o644); err != nil {
			t.Fatal(err)
		}
		out := runPiInstructions(t, root, "base")
		if out.Threw != nil || out.Result != nil {
			t.Errorf("an empty file adds nothing, got threw=%v result=%v", out.Threw, out.Result)
		}
	})
	t.Run("the event has no system prompt", func(t *testing.T) {
		root := piInstructionsPackage(t)
		scenario := `{"event":{"type":"before_agent_start"}}`
		cmd := exec.Command("node", "--input-type=module")
		cmd.Stdin = strings.NewReader(piInstructionsDriver)
		cmd.Env = []string{
			"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(),
			"DG_EXT_URL=file://" + filepath.Join(root, "extensions", "devgeta-instructions.js"),
			"DG_SCENARIO=" + scenario,
		}
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
		var out piInstructionsOutcome
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out.Threw != nil {
			t.Errorf("an unexpected event shape must not throw: %s", *out.Threw)
		}
	})
}

func TestPiInstructionsExtensionReadsTheFileAtRunTime(t *testing.T) {
	root := piInstructionsPackage(t)
	if err := os.WriteFile(filepath.Join(root, "DEVGETA.md"), []byte("# Edited by the user\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runPiInstructions(t, root, "base")

	if out.Result == nil || out.Result.SystemPrompt != "base\n\n# Edited by the user" {
		t.Errorf("the pack's own DEVGETA.md is what is loaded, got %+v", out.Result)
	}
}

// Claude Code and Pi both format on save through the one shared script, so the
// two cannot drift on which languages they cover (docs/guides/agent-sync.md,
// "Formatting on save"). OpenCode's formatter block is compared separately, by
// internal/apps/opencode/permissions_test.go.
func TestClaudeAndPiFormatThroughTheSharedScript(t *testing.T) {
	const shared = "format-file.sh"
	if _, err := fs.Stat(ConfigsFS, "configs/devgeta/"+shared); err != nil {
		t.Fatalf("the shared formatter script must ship: %v", err)
	}
	for _, path := range []string{"configs/claude/format.sh", "configs/pi/extensions/devgeta-format.js"} {
		data, err := fs.ReadFile(ConfigsFS, path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(data), shared) {
			t.Errorf("%s must format through the shared %s, not its own language list", path, shared)
		}
	}
}
