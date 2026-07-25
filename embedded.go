package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cjairm/devgeta/pkg/logger"
)

//go:embed all:configs
var ConfigsFS embed.FS

// ExtractEmbeddedConfigs extracts the embedded configs/ directory to destDir
// preserving the directory structure. Uses fs.WalkDir pattern per research.md R1.
func ExtractEmbeddedConfigs(destDir string) error {
	logger.L().Infow("Extracting embedded configs", "destination", destDir)

	// Create destination directory if it doesn't exist
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Walk the embedded filesystem
	err := fs.WalkDir(ConfigsFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("error walking embedded filesystem at %s: %w", path, err)
		}

		// Skip the root "configs" directory itself
		if path == "." || path == "configs" {
			return nil
		}

		// Strip "configs/" prefix from the path
		relPath := path
		if len(path) > 8 && path[:8] == "configs/" {
			relPath = path[8:]
		}

		// Construct destination path
		dstPath := filepath.Join(destDir, relPath)

		if d.IsDir() {
			// Create directory
			if err := os.MkdirAll(dstPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dstPath, err)
			}
			logger.L().Debugw("Created directory", "path", dstPath)
		} else {
			// Read file from embed.FS
			data, err := fs.ReadFile(ConfigsFS, path)
			if err != nil {
				return fmt.Errorf("failed to read embedded file %s: %w", path, err)
			}

			// Write file to destination
			if err := os.WriteFile(dstPath, data, 0644); err != nil {
				return fmt.Errorf("failed to write file %s: %w", dstPath, err)
			}
			logger.L().Debugw("Extracted file", "path", dstPath, "size", len(data))
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to extract embedded configs: %w", err)
	}

	logger.L().Infow("Successfully extracted embedded configs", "destination", destDir)
	return nil
}
