package databases

import (
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
// Examples: "redis" -> "Redis", "postgresql" -> "PostgreSQL", "mysql" -> "MySQL"
func toDisplayName(name string) string {
	// Special cases for names that should have specific capitalization
	specialCases := map[string]string{
		constants.MySQL:      "MySQL",
		constants.PostgreSQL: "PostgreSQL",
		constants.SQLite:     "SQLite",
		constants.MongoDB:    "MongoDB",
	}
	if displayName, ok := specialCases[name]; ok {
		return displayName
	}
	if len(name) == 0 {
		return name
	}
	return strings.ToUpper(string(name[0])) + name[1:]
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

// GetDatabaseConfigs returns all available database configurations
// Organized alphabetically for maintainability
func GetDatabaseConfigs() []DatabaseConfig {
	return []DatabaseConfig{
		// All databases use native package managers (alphabetically ordered)
		{DisplayName: toDisplayName(constants.MongoDB), Name: constants.MongoDB},
		{DisplayName: toDisplayName(constants.MySQL), Name: constants.MySQL},
		{DisplayName: toDisplayName(constants.PostgreSQL), Name: constants.PostgreSQL},
		{DisplayName: toDisplayName(constants.Redis), Name: constants.Redis},
		{DisplayName: toDisplayName(constants.SQLite), Name: constants.SQLite},
	}
}

// getVersionCommand returns the command and args to check if a database is installed
func getVersionCommand(dbName string) (string, []string) {
	switch dbName {
	case constants.MongoDB:
		return "mongod", []string{"--version"}
	case constants.MySQL:
		return constants.MySQL, []string{"--version"}
	case constants.PostgreSQL:
		return "psql", []string{"--version"}
	case constants.Redis:
		return "redis-server", []string{"--version"}
	case constants.SQLite:
		return "sqlite3", []string{"--version"}
	default:
		return dbName, []string{"--version"}
	}
}
