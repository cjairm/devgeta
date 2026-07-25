package languages

import (
	"testing"

	"github.com/cjairm/devgeta/pkg/constants"
)

// Tests for containsIgnoreCase
func TestContainsIgnoreCase_ExactMatch(t *testing.T) {
	items := []string{"Node", "Python", "Go"}
	if !containsIgnoreCase("Node", items) {
		t.Error("Expected to find exact match 'Node'")
	}
}

func TestContainsIgnoreCase_CaseInsensitive(t *testing.T) {
	items := []string{"Node", "Python", "Go"}
	if !containsIgnoreCase("node", items) {
		t.Error("Expected to find 'node' (case-insensitive)")
	}
	if !containsIgnoreCase("PYTHON", items) {
		t.Error("Expected to find 'PYTHON' (case-insensitive)")
	}
}

func TestContainsIgnoreCase_NotFound(t *testing.T) {
	items := []string{"Node", "Python", "Go"}
	if containsIgnoreCase("Ruby", items) {
		t.Error("Expected not to find 'Ruby'")
	}
}

func TestContainsIgnoreCase_EmptySlice(t *testing.T) {
	items := []string{}
	if containsIgnoreCase("Node", items) {
		t.Error("Expected not to find in empty slice")
	}
}

func TestContainsIgnoreCase_EmptyString(t *testing.T) {
	items := []string{"Node", "Python"}
	if containsIgnoreCase("", items) {
		t.Error("Expected not to find empty string")
	}
}

func TestContainsIgnoreCase_SpecialCharacters(t *testing.T) {
	items := []string{"C++", "C#"}
	if !containsIgnoreCase("c++", items) {
		t.Error("Expected to find 'c++' (case-insensitive)")
	}
}

func TestContainsIgnoreCase_Whitespace(t *testing.T) {
	items := []string{"Visual Basic", "R Studio"}
	if !containsIgnoreCase("VISUAL BASIC", items) {
		t.Error("Expected to find 'VISUAL BASIC' with whitespace")
	}
}

func TestContainsIgnoreCase_Unicode(t *testing.T) {
	items := []string{"Café", "Naïve"}
	if !containsIgnoreCase("café", items) {
		t.Error("Expected to find 'café' (unicode)")
	}
}

// Tests for toDisplayName
func TestToDisplayName_Node(t *testing.T) {
	result := toDisplayName("node")
	expected := "Node"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestToDisplayName_PHP(t *testing.T) {
	result := toDisplayName("php")
	expected := "PHP"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestToDisplayName_Python(t *testing.T) {
	result := toDisplayName("python")
	expected := "Python"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestToDisplayName_Go(t *testing.T) {
	result := toDisplayName("go")
	expected := "Go"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestToDisplayName_Rust(t *testing.T) {
	result := toDisplayName("rust")
	expected := "Rust"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestToDisplayName_EmptyString(t *testing.T) {
	result := toDisplayName("")
	expected := ""
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestToDisplayName_AlreadyCapitalized(t *testing.T) {
	result := toDisplayName("Node")
	expected := "Node"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestToDisplayName_SingleCharacter(t *testing.T) {
	result := toDisplayName("r")
	expected := "R"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

// Tests for formatSpec
func TestFormatSpec_WithVersion(t *testing.T) {
	result := formatSpec("node", "lts", true)
	expected := "node@lts"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestFormatSpec_WithoutVersion(t *testing.T) {
	result := formatSpec("php", "", false)
	expected := "php"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestFormatSpec_UseVersionFalse(t *testing.T) {
	result := formatSpec("python", "latest", false)
	expected := "python"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestFormatSpec_EmptyVersion(t *testing.T) {
	result := formatSpec("go", "", true)
	expected := "go"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestFormatSpec_NumericVersion(t *testing.T) {
	result := formatSpec("rust", "1.70.0", true)
	expected := "rust@1.70.0"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestFormatSpec_LatestVersion(t *testing.T) {
	result := formatSpec("go", "latest", true)
	expected := "go@latest"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

// Tests for filterSlice
func TestFilterSlice_RemovesMatches(t *testing.T) {
	source := []string{"All", "None", "Done", "Node", "Python", "Go"}
	exclude := []string{"Node", "Python"}
	result := filterSlice(source, exclude)
	expected := []string{"All", "None", "Done", "Go"}

	if len(result) != len(expected) {
		t.Errorf("Expected length %d, got %d", len(expected), len(result))
	}
	for i, val := range result {
		if val != expected[i] {
			t.Errorf("Expected '%s' at index %d, got '%s'", expected[i], i, val)
		}
	}
}

func TestFilterSlice_CaseInsensitive(t *testing.T) {
	source := []string{"Node", "Python", "Go"}
	exclude := []string{"node", "PYTHON"}
	result := filterSlice(source, exclude)

	if len(result) != 1 || result[0] != "Go" {
		t.Errorf("Expected ['Go'], got %v", result)
	}
}

func TestFilterSlice_EmptyExclude(t *testing.T) {
	source := []string{"Node", "Python", "Go"}
	exclude := []string{}
	result := filterSlice(source, exclude)

	if len(result) != len(source) {
		t.Errorf("Expected same length as source, got %d", len(result))
	}
}

func TestFilterSlice_EmptySource(t *testing.T) {
	source := []string{}
	exclude := []string{"Node"}
	result := filterSlice(source, exclude)

	if len(result) != 0 {
		t.Errorf("Expected empty result, got %v", result)
	}
}

func TestFilterSlice_NoMatches(t *testing.T) {
	source := []string{"Node", "Python", "Go"}
	exclude := []string{"Ruby", "Java"}
	result := filterSlice(source, exclude)

	if len(result) != len(source) {
		t.Errorf("Expected same length as source, got %d", len(result))
	}
}

func TestFilterSlice_AllMatch(t *testing.T) {
	source := []string{"Node", "Python"}
	exclude := []string{"Node", "Python"}
	result := filterSlice(source, exclude)

	if len(result) != 0 {
		t.Errorf("Expected empty result, got %v", result)
	}
}

// Tests for getVersionCommand
func TestGetVersionCommand_Bun(t *testing.T) {
	cmd, args := getVersionCommand(constants.Bun)
	if cmd != "bun" {
		t.Errorf("Expected command 'bun', got '%s'", cmd)
	}
	if len(args) != 1 || args[0] != "--version" {
		t.Errorf("Expected args ['--version'], got %v", args)
	}
}

func TestGetVersionCommand_Deno(t *testing.T) {
	cmd, args := getVersionCommand(constants.Deno)
	if cmd != "deno" {
		t.Errorf("Expected command 'deno', got '%s'", cmd)
	}
	if len(args) != 1 || args[0] != "--version" {
		t.Errorf("Expected args ['--version'], got %v", args)
	}
}

func TestGetVersionCommand_Elixir(t *testing.T) {
	cmd, args := getVersionCommand(constants.Elixir)
	if cmd != "elixir" {
		t.Errorf("Expected command 'elixir', got '%s'", cmd)
	}
	if len(args) != 1 || args[0] != "--version" {
		t.Errorf("Expected args ['--version'], got %v", args)
	}
}

func TestGetVersionCommand_Erlang(t *testing.T) {
	cmd, args := getVersionCommand(constants.Erlang)
	if cmd != "erl" {
		t.Errorf("Expected command 'erl', got '%s'", cmd)
	}
	if len(args) != 3 {
		t.Errorf("Expected 3 args for erlang, got %d", len(args))
	}
}

func TestGetVersionCommand_Go(t *testing.T) {
	cmd, args := getVersionCommand(constants.Go)
	if cmd != "go" {
		t.Errorf("Expected command 'go', got '%s'", cmd)
	}
	if len(args) != 1 || args[0] != "version" {
		t.Errorf("Expected args ['version'], got %v", args)
	}
}

func TestGetVersionCommand_Java(t *testing.T) {
	cmd, args := getVersionCommand(constants.Java)
	if cmd != "java" {
		t.Errorf("Expected command 'java', got '%s'", cmd)
	}
	if len(args) != 1 || args[0] != "--version" {
		t.Errorf("Expected args ['--version'], got %v", args)
	}
}

func TestGetVersionCommand_Node(t *testing.T) {
	cmd, args := getVersionCommand(constants.Node)
	if cmd != "node" {
		t.Errorf("Expected command 'node', got '%s'", cmd)
	}
	if len(args) != 1 || args[0] != "--version" {
		t.Errorf("Expected args ['--version'], got %v", args)
	}
}

func TestGetVersionCommand_PHP(t *testing.T) {
	cmd, args := getVersionCommand(constants.PHP)
	if cmd != "php" {
		t.Errorf("Expected command 'php', got '%s'", cmd)
	}
	if len(args) != 1 || args[0] != "--version" {
		t.Errorf("Expected args ['--version'], got %v", args)
	}
}

func TestGetVersionCommand_Python(t *testing.T) {
	cmd, args := getVersionCommand(constants.Python)
	if cmd != "python3" {
		t.Errorf("Expected command 'python3', got '%s'", cmd)
	}
	if len(args) != 1 || args[0] != "--version" {
		t.Errorf("Expected args ['--version'], got %v", args)
	}
}

func TestGetVersionCommand_Ruby(t *testing.T) {
	cmd, args := getVersionCommand(constants.Ruby)
	if cmd != "ruby" {
		t.Errorf("Expected command 'ruby', got '%s'", cmd)
	}
	if len(args) != 1 || args[0] != "--version" {
		t.Errorf("Expected args ['--version'], got %v", args)
	}
}

func TestGetVersionCommand_Rust(t *testing.T) {
	cmd, args := getVersionCommand(constants.Rust)
	if cmd != "rustc" {
		t.Errorf("Expected command 'rustc', got '%s'", cmd)
	}
	if len(args) != 1 || args[0] != "--version" {
		t.Errorf("Expected args ['--version'], got %v", args)
	}
}

func TestGetVersionCommand_Unknown(t *testing.T) {
	cmd, args := getVersionCommand("unknown")
	if cmd != "unknown" {
		t.Errorf("Expected command 'unknown', got '%s'", cmd)
	}
	if len(args) != 1 || args[0] != "--version" {
		t.Errorf("Expected args ['--version'], got %v", args)
	}
}
