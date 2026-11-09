package main

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// devgeta-quiet-bash.js is loaded by node against stub Pi packages, so the test
// pins what it registers and how it renders without a real Pi.
const quietBashDriver = `
const mod = await import(process.env.DG_EXT_URL);
let tool;
mod.default({ registerTool(t) { tool = t; } });
const stale = { kind: "text", text: "" };
const collapsed = tool.renderResult({}, { expanded: false }, {}, { lastComponent: stale });
const expanded = tool.renderResult({}, { expanded: true }, {}, { lastComponent: stale, state: 1 });
console.log(JSON.stringify({
  name: tool.name,
  executeIsBuiltIn: tool.execute === "builtin-execute",
  collapsed,
  expanded,
}));
`

func TestPiQuietBashExtension(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not found on PATH; skipping devgeta-quiet-bash.js test")
	}
	root := t.TempDir()
	stub := func(pkg, body string) {
		dir := filepath.Join(root, "node_modules", "@earendil-works", pkg)
		mustMkdirAll(t, dir)
		mustWriteFile(t, filepath.Join(dir, "package.json"),
			`{"name":"@earendil-works/`+pkg+`","type":"module","main":"index.js"}`)
		mustWriteFile(t, filepath.Join(dir, "index.js"), body)
	}
	stub("pi-coding-agent", `export function createBashToolDefinition() {
  return {
    name: "bash",
    execute: "builtin-execute",
    renderResult(result, options, theme, context) {
      return { builtIn: true, lastComponent: context.lastComponent ?? null, state: context.state };
    },
  };
}`)
	stub("pi-tui", `export class Text { constructor(text) { this.text = text; } }`)

	src, err := fs.ReadFile(ConfigsFS, "configs/pi/extensions/devgeta-quiet-bash.js")
	if err != nil {
		t.Fatalf("read embedded devgeta-quiet-bash.js: %v", err)
	}
	extension := filepath.Join(root, "devgeta-quiet-bash.mjs")
	mustWriteFile(t, extension, string(src))

	cmd := exec.Command("node", "--input-type=module")
	cmd.Stdin = strings.NewReader(quietBashDriver)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "DG_EXT_URL=file://" + extension}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node driver failed: %v\n%s", err, out)
	}

	want := `{"name":"bash","executeIsBuiltIn":true,` +
		`"collapsed":{"text":""},` +
		`"expanded":{"builtIn":true,"lastComponent":null,"state":1}}`
	if got := strings.TrimSpace(string(out)); got != want {
		t.Errorf("want %s\ngot  %s", want, got)
	}
}
