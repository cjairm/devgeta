package languages

import (
	"fmt"
	"strings"

	"github.com/cjairm/devgeta/pkg/constants"
)

// containsIgnoreCase checks if a string exists in a slice (case-insensitive)
func containsIgnoreCase(target string, items []string) bool {
	for _, item := range items {
		if strings.EqualFold(target, item) {
			return true
		}
	}
	return false
}

// toDisplayName converts a lowercase constant to a display name
// Examples: "node" -> "Node", "php" -> "PHP", "go" -> "Go"
func toDisplayName(name string) string {
	// Special cases for acronyms that should be fully uppercase
	upperAcronyms := map[string]bool{
		"php": true,
	}
	if upperAcronyms[name] {
		return strings.ToUpper(name)
	}
	if len(name) == 0 {
		return name
	}
	return strings.ToUpper(string(name[0])) + name[1:]
}

// formatSpec creates a specification string for tracking
// Format: "name@version" when useVersion is true, "name" otherwise
func formatSpec(name, version string, useVersion bool) string {
	if useVersion && version != "" {
		return fmt.Sprintf("%s@%s", name, version)
	}
	return name
}

// filterSlice removes items from source that exist in exclude list (case-insensitive)
func filterSlice(source, exclude []string) []string {
	filtered := []string{}
	for _, item := range source {
		if !containsIgnoreCase(item, exclude) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// GetLanguageConfigs returns all available language configurations
// Organized alphabetically for maintainability
func GetLanguageConfigs() []LanguageConfig {
	return []LanguageConfig{
		// Mise-managed languages (alphabetically ordered)
		{DisplayName: toDisplayName(constants.Bun), Name: constants.Bun, Version: "latest", UseMise: true},
		{DisplayName: toDisplayName(constants.Deno), Name: constants.Deno, Version: "latest", UseMise: true},
		{DisplayName: toDisplayName(constants.Elixir), Name: constants.Elixir, Version: "latest", UseMise: true},
		{DisplayName: toDisplayName(constants.Erlang), Name: constants.Erlang, Version: "latest", UseMise: true},
		{DisplayName: toDisplayName(constants.Go), Name: constants.Go, Version: "latest", UseMise: true},
		{DisplayName: toDisplayName(constants.Java), Name: constants.Java, Version: "latest", UseMise: true},
		{DisplayName: toDisplayName(constants.Node), Name: constants.Node, Version: "lts", UseMise: true},
		{DisplayName: toDisplayName(constants.Python), Name: constants.Python, Version: "latest", UseMise: true},
		{DisplayName: toDisplayName(constants.Ruby), Name: constants.Ruby, Version: "latest", UseMise: true},
		{DisplayName: toDisplayName(constants.Rust), Name: constants.Rust, Version: "latest", UseMise: true},

		// Native package manager languages
		{DisplayName: toDisplayName(constants.PHP), Name: constants.PHP, Version: "", UseMise: false},
	}
}

// getVersionCommand returns the command and args to check if a language is installed
func getVersionCommand(langName string) (string, []string) {
	switch langName {
	case constants.Bun:
		return constants.Bun, []string{"--version"}
	case constants.Deno:
		return constants.Deno, []string{"--version"}
	case constants.Elixir:
		return constants.Elixir, []string{"--version"}
	case constants.Erlang:
		return "erl", []string{"-eval", "erlang:display(erlang:system_info(otp_release)), halt().", "-noshell"}
	case constants.Go:
		return constants.Go, []string{"version"}
	case constants.Java:
		return "java", []string{"--version"}
	case constants.Node:
		return constants.Node, []string{"--version"}
	case constants.PHP:
		return constants.PHP, []string{"--version"}
	case constants.Python:
		// Try python3 first (more common on modern systems)
		return "python3", []string{"--version"}
	case constants.Ruby:
		return constants.Ruby, []string{"--version"}
	case constants.Rust:
		return "rustc", []string{"--version"}
	default:
		return langName, []string{"--version"}
	}
}
