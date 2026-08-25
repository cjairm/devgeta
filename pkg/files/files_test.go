package files_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/logger"
)

var fileContent = "Hello, World!"

func createTempDir(t *testing.T) string {
	tempDir, err := os.MkdirTemp("", "testdir-")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}
	return tempDir
}

func createTempSourceFile(t *testing.T, tempDir string, fileName string) string {
	srcFilePath := filepath.Join(tempDir, fileName)
	err := os.WriteFile(srcFilePath, []byte(fileContent), 0o644)
	if err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}
	return srcFilePath
}

func createTempSourceDir(t *testing.T, tempDir string) string {
	srcDir := filepath.Join(tempDir, "source")
	err := os.Mkdir(srcDir, 0o755)
	if err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}
	createTempSourceFile(t, srcDir, "file1.txt")

	subDir := filepath.Join(srcDir, "subdir")
	err = os.Mkdir(subDir, 0o755)
	if err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}
	createTempSourceFile(t, subDir, "file2.txt")

	return srcDir
}

func TestCopyFile(t *testing.T) {
	tempDir := createTempDir(t)
	defer os.RemoveAll(tempDir)
	logger.Init(false)

	srcFilePath := createTempSourceFile(t, tempDir, "source.txt")
	dstFilePath := filepath.Join(tempDir, "destination.txt")

	// Test successful copy
	if err := files.CopyFile(srcFilePath, dstFilePath); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}
	t.Log("File copied successfully")

	// Verify the content of the destination file
	dstContent, err := os.ReadFile(dstFilePath)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}
	expectedContent := "Hello, World!"
	if string(dstContent) != expectedContent {
		t.Errorf("Expected content %q, got %q", expectedContent, dstContent)
	}
	t.Log("Content copied succesfuly")

	// Verify the destination file was written with FilePermission (0644), not
	// AllPermissions (0777) — CopyFile must never make a deployed config
	// group- or world-writable.
	dstInfo, err := os.Stat(dstFilePath)
	if err != nil {
		t.Fatalf("Failed to stat destination file: %v", err)
	}
	if dstInfo.Mode().Perm() != 0o644 {
		t.Errorf("Expected destination file permissions 0644, got %o", dstInfo.Mode().Perm())
	}

	// Test copying from a non-existent source file
	nonExistentSrc := filepath.Join(tempDir, "nonexistent.txt")
	err = files.CopyFile(nonExistentSrc, dstFilePath)
	if err == nil {
		t.Fatal("Expected an error when copying from a nonexistent file, got nil")
	}
	t.Log("Correctly handled copying from a nonexistent file")

	// Test copying to a directory instead of a file
	dstDirPath := filepath.Join(tempDir, "destinationDir")
	if err := os.Mkdir(dstDirPath, 0o755); err != nil {
		t.Fatalf("Failed to create destination directory: %v", err)
	}
	err = files.CopyFile(srcFilePath, dstDirPath)
	if err == nil {
		t.Fatal("Expected an error when copying to a directory, got nil")
	}
	t.Log("Correctly handled copying to a directory")
}

func TestCopyDir(t *testing.T) {
	tempDir := createTempDir(t)
	defer os.RemoveAll(tempDir)

	// Create a source directory with files
	srcDir := createTempSourceDir(t, tempDir)
	dstDir := filepath.Join(tempDir, "destination")

	// Test successful copy
	if err := files.CopyDir(srcDir, dstDir); err != nil {
		t.Fatalf("CopyDir failed: %v", err)
	}
	t.Log("Successfully copied files from source to destination.")

	// Verify the content of the destination directory
	dstFile1 := filepath.Join(dstDir, "file1.txt")
	dstFile2 := filepath.Join(dstDir, "subdir", "file2.txt")

	if _, err := os.Stat(dstFile1); os.IsNotExist(err) {
		t.Errorf("Expected file %q to exist, but it does not", dstFile1)
	}

	if _, err := os.Stat(dstFile2); os.IsNotExist(err) {
		t.Errorf("Expected file %q to exist, but it does not", dstFile2)
	}

	// Verify permissions: copied files get FilePermission (0644), copied
	// directories get DirPermission (0755) — never AllPermissions (0777).
	dstFile1Info, err := os.Stat(dstFile1)
	if err != nil {
		t.Fatalf("Failed to stat %q: %v", dstFile1, err)
	}
	if dstFile1Info.Mode().Perm() != 0o644 {
		t.Errorf("Expected %q permissions 0644, got %o", dstFile1, dstFile1Info.Mode().Perm())
	}

	dstSubDirInfo, err := os.Stat(filepath.Join(dstDir, "subdir"))
	if err != nil {
		t.Fatalf("Failed to stat destination subdir: %v", err)
	}
	if dstSubDirInfo.Mode().Perm() != 0o755 {
		t.Errorf(
			"Expected destination subdir permissions 0755, got %o",
			dstSubDirInfo.Mode().Perm(),
		)
	}

	// Test copying an empty directory
	emptySrcDir := filepath.Join(srcDir, "empty")
	if err := os.Mkdir(emptySrcDir, 0o755); err != nil {
		t.Fatalf("Failed to create empty source directory: %v", err)
	}
	emptyDstDir := filepath.Join(dstDir, "empty")
	if err := files.CopyDir(srcDir, dstDir); err != nil {
		t.Fatalf("CopyDir failed for empty directory: %v", err)
	}
	if _, err := os.Stat(emptyDstDir); os.IsNotExist(err) {
		t.Errorf("Expected empty destination directory %q to exist, but it does not", emptyDstDir)
	}

	// Test copying from a non-existent source directory
	nonExistentSrcDir := filepath.Join(tempDir, "nonexistent")
	err = files.CopyDir(nonExistentSrcDir, dstDir)
	if err == nil {
		t.Fatal("Expected an error when copying from a nonexistent directory, got nil")
	}
	t.Log("Correctly received an error when copying from a nonexistent directory.")

	// Test copying to an existing directory
	existingFile := filepath.Join(dstDir, "file1.txt")
	if err := os.WriteFile(existingFile, []byte("Existing file"), 0o644); err != nil {
		t.Fatalf("Failed to create existing file in destination: %v", err)
	}
	if err := files.CopyDir(srcDir, dstDir); err != nil {
		t.Fatalf("CopyDir failed when copying to an existing directory: %v", err)
	}
	t.Log("Successfully copied files to an existing directory.")

	// Verify that the existing file is still there
	if _, err := os.Stat(existingFile); os.IsNotExist(err) {
		t.Errorf("Expected existing file %q to still exist, but it does not", existingFile)
	}

	// Test handling read-only files
	readOnlyFile := filepath.Join(dstDir, "readonly.txt")
	if err := os.WriteFile(readOnlyFile, []byte("Read-only file"), 0o444); err != nil {
		t.Fatalf("Failed to create read-only file: %v", err)
	}
	if err := files.CopyDir(srcDir, dstDir); err != nil {
		t.Fatalf("CopyDir failed when copying with read-only file: %v", err)
	}
	t.Log("Successfully copied files with a read-only file present.")

	// Verify that the read-only file is still there
	if _, err := os.Stat(readOnlyFile); os.IsNotExist(err) {
		t.Errorf("Expected read-only file %q to still exist, but it does not", readOnlyFile)
	}
}

func TestIsDirEmpty(t *testing.T) {
	tempDir := createTempDir(t)
	defer os.RemoveAll(tempDir)

	// Test empty directory
	emptyDir := filepath.Join(tempDir, "empty")
	if err := os.Mkdir(emptyDir, 0o755); err != nil {
		t.Fatalf("Failed to create empty directory: %v", err)
	}

	if !files.IsDirEmpty(emptyDir) {
		t.Errorf("Expected empty directory %q to be detected as empty", emptyDir)
	}
	t.Log("Correctly detected empty directory")

	// Test directory with files
	nonEmptyDir := filepath.Join(tempDir, "nonempty")
	if err := os.Mkdir(nonEmptyDir, 0o755); err != nil {
		t.Fatalf("Failed to create non-empty directory: %v", err)
	}
	createTempSourceFile(t, nonEmptyDir, "file.txt")

	if files.IsDirEmpty(nonEmptyDir) {
		t.Errorf("Expected non-empty directory %q to be detected as non-empty", nonEmptyDir)
	}
	t.Log("Correctly detected non-empty directory")

	// Test non-existent directory (should return true as it's treated as empty)
	nonExistentDir := filepath.Join(tempDir, "nonexistent")
	if !files.IsDirEmpty(nonExistentDir) {
		t.Errorf("Expected non-existent directory to be treated as empty")
	}
	t.Log("Correctly treated non-existent directory as empty")

	// Test directory with subdirectories
	dirWithSubdir := filepath.Join(tempDir, "withsubdir")
	if err := os.Mkdir(dirWithSubdir, 0o755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}
	subDir := filepath.Join(dirWithSubdir, "subdir")
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}

	if files.IsDirEmpty(dirWithSubdir) {
		t.Errorf("Expected directory with subdirectories to be detected as non-empty")
	}
	t.Log("Correctly detected directory with subdirectories as non-empty")
}

func TestSoftCopyFile(t *testing.T) {
	tempDir := createTempDir(t)
	defer os.RemoveAll(tempDir)
	logger.Init(false)

	srcFilePath := createTempSourceFile(t, tempDir, "source.txt")
	dstFilePath := filepath.Join(tempDir, "destination.txt")

	t.Run("successful copy when destination does not exist", func(t *testing.T) {
		// Test successful soft copy when destination doesn't exist
		if err := files.SoftCopyFile(srcFilePath, dstFilePath); err != nil {
			t.Fatalf("SoftCopyFile failed: %v", err)
		}

		// Verify the content of the destination file
		dstContent, err := os.ReadFile(dstFilePath)
		if err != nil {
			t.Fatalf("Failed to read destination file: %v", err)
		}
		if string(dstContent) != fileContent {
			t.Errorf("Expected content %q, got %q", fileContent, dstContent)
		}
		t.Log("File copied successfully when destination did not exist")
	})

	t.Run("skip copy when destination already exists", func(t *testing.T) {
		// Modify the destination file to verify it's not overwritten
		modifiedContent := "Modified content"
		if err := os.WriteFile(dstFilePath, []byte(modifiedContent), 0o644); err != nil {
			t.Fatalf("Failed to modify destination file: %v", err)
		}

		// Attempt soft copy again
		if err := files.SoftCopyFile(srcFilePath, dstFilePath); err != nil {
			t.Fatalf("SoftCopyFile failed: %v", err)
		}

		// Verify the destination file was NOT overwritten
		finalContent, err := os.ReadFile(dstFilePath)
		if err != nil {
			t.Fatalf("Failed to read destination file: %v", err)
		}
		if string(finalContent) != modifiedContent {
			t.Errorf(
				"SoftCopyFile overwrote existing file. Expected %q, got %q",
				modifiedContent,
				finalContent,
			)
		}
		t.Log("Correctly skipped copying when destination already existed")
	})

	t.Run("error when source does not exist", func(t *testing.T) {
		nonExistentSrc := filepath.Join(tempDir, "nonexistent.txt")
		newDst := filepath.Join(tempDir, "newdest.txt")

		err := files.SoftCopyFile(nonExistentSrc, newDst)
		if err == nil {
			t.Fatal("Expected an error when copying from a nonexistent file, got nil")
		}
		t.Logf("Correctly handled nonexistent source file: %v", err)
	})
}

func TestSoftCopyDir(t *testing.T) {
	tempDir := createTempDir(t)
	defer os.RemoveAll(tempDir)
	logger.Init(false)

	srcDir := createTempSourceDir(t, tempDir)
	dstDir := filepath.Join(tempDir, "destination")

	t.Run("successful copy when destination does not exist", func(t *testing.T) {
		// Test successful soft copy when destination doesn't exist
		if err := files.SoftCopyDir(srcDir, dstDir); err != nil {
			t.Fatalf("SoftCopyDir failed: %v", err)
		}

		// Verify the content of the destination directory
		dstFile1 := filepath.Join(dstDir, "file1.txt")
		dstFile2 := filepath.Join(dstDir, "subdir", "file2.txt")

		if _, err := os.Stat(dstFile1); os.IsNotExist(err) {
			t.Errorf("Expected file %q to exist, but it does not", dstFile1)
		}

		if _, err := os.Stat(dstFile2); os.IsNotExist(err) {
			t.Errorf("Expected file %q to exist, but it does not", dstFile2)
		}
		t.Log("Directory copied successfully when destination did not exist")
	})

	t.Run("skip copy when destination exists with content", func(t *testing.T) {
		// Create a marker file in destination to verify it's not overwritten
		markerFile := filepath.Join(dstDir, "marker.txt")
		markerContent := "This file should not be removed"
		if err := os.WriteFile(markerFile, []byte(markerContent), 0o644); err != nil {
			t.Fatalf("Failed to create marker file: %v", err)
		}

		// Attempt soft copy again
		if err := files.SoftCopyDir(srcDir, dstDir); err != nil {
			t.Fatalf("SoftCopyDir failed: %v", err)
		}

		// Verify the marker file still exists
		if _, err := os.Stat(markerFile); os.IsNotExist(err) {
			t.Error("SoftCopyDir removed existing files when it should have skipped")
		}

		// Verify marker file content is unchanged
		finalContent, err := os.ReadFile(markerFile)
		if err != nil {
			t.Fatalf("Failed to read marker file: %v", err)
		}
		if string(finalContent) != markerContent {
			t.Errorf(
				"Marker file content changed. Expected %q, got %q",
				markerContent,
				finalContent,
			)
		}
		t.Log("Correctly skipped copying when destination already existed with content")
	})

	t.Run("copy when destination exists but is empty", func(t *testing.T) {
		emptyDstDir := filepath.Join(tempDir, "empty_destination")
		if err := os.Mkdir(emptyDstDir, 0o755); err != nil {
			t.Fatalf("Failed to create empty destination directory: %v", err)
		}

		// Attempt soft copy to empty directory
		if err := files.SoftCopyDir(srcDir, emptyDstDir); err != nil {
			t.Fatalf("SoftCopyDir failed on empty destination: %v", err)
		}

		// Verify files were copied
		dstFile1 := filepath.Join(emptyDstDir, "file1.txt")
		if _, err := os.Stat(dstFile1); os.IsNotExist(err) {
			t.Errorf("Expected file %q to exist in empty destination, but it does not", dstFile1)
		}
		t.Log("Correctly copied to empty destination directory")
	})

	t.Run("error when source does not exist", func(t *testing.T) {
		nonExistentSrc := filepath.Join(tempDir, "nonexistent_dir")
		newDst := filepath.Join(tempDir, "newdest_dir")

		err := files.SoftCopyDir(nonExistentSrc, newDst)
		if err == nil {
			t.Fatal("Expected an error when copying from a nonexistent directory, got nil")
		}
		t.Logf("Correctly handled nonexistent source directory: %v", err)
	})
}

func TestGenerateFromTemplate(t *testing.T) {
	tempDir := createTempDir(t)
	defer os.RemoveAll(tempDir)
	logger.Init(false)

	t.Run("successful template generation with map data", func(t *testing.T) {
		// Create a template file
		templatePath := filepath.Join(tempDir, "config.tmpl")
		templateContent := `# Configuration file
export HOME="{{.Home}}"
export USER="{{.User}}"
export DEBUG={{.Debug}}`

		if err := os.WriteFile(templatePath, []byte(templateContent), 0o644); err != nil {
			t.Fatalf("Failed to create template file: %v", err)
		}
		// Prepare template data
		data := map[string]any{
			"Home":  "/home/testuser",
			"User":  "testuser",
			"Debug": true,
		}
		// Generate output file
		outputPath := filepath.Join(tempDir, "config.sh")
		if err := files.GenerateFromTemplate(templatePath, outputPath, data); err != nil {
			t.Fatalf("GenerateFromTemplate failed: %v", err)
		}

		// Verify output file was created
		if _, err := os.Stat(outputPath); os.IsNotExist(err) {
			t.Fatalf("Expected output file %q to exist, but it does not", outputPath)
		}

		// Verify content
		outputContent, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("Failed to read output file: %v", err)
		}

		expectedContent := `# Configuration file
export HOME="/home/testuser"
export USER="testuser"
export DEBUG=true`

		if string(outputContent) != expectedContent {
			t.Errorf("Content mismatch.\nExpected:\n%s\n\nGot:\n%s", expectedContent, outputContent)
		}

		// Verify file permissions
		info, err := os.Stat(outputPath)
		if err != nil {
			t.Fatalf("Failed to stat output file: %v", err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("Expected file permissions 0644, got %o", info.Mode().Perm())
		}

		t.Log("Successfully generated file from template with map data")
	})

	t.Run("successful template generation with struct data", func(t *testing.T) {
		// Create a template file
		templatePath := filepath.Join(tempDir, "shell.tmpl")
		templateContent := `#!/bin/bash
# Shell configuration for {{.Name}}
export PATH="{{.BinPath}}:$PATH"
export EDITOR="{{.Editor}}"`

		if err := os.WriteFile(templatePath, []byte(templateContent), 0o644); err != nil {
			t.Fatalf("Failed to create template file: %v", err)
		}

		// Prepare template data as struct
		type ShellConfig struct {
			Name    string
			BinPath string
			Editor  string
		}

		data := ShellConfig{
			Name:    "devgeta",
			BinPath: "/usr/local/bin",
			Editor:  "nvim",
		}

		// Generate output file
		outputPath := filepath.Join(tempDir, "shell_config.sh")
		if err := files.GenerateFromTemplate(templatePath, outputPath, data); err != nil {
			t.Fatalf("GenerateFromTemplate failed: %v", err)
		}

		// Verify content
		outputContent, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("Failed to read output file: %v", err)
		}

		expectedContent := `#!/bin/bash
# Shell configuration for devgeta
export PATH="/usr/local/bin:$PATH"
export EDITOR="nvim"`

		if string(outputContent) != expectedContent {
			t.Errorf("Content mismatch.\nExpected:\n%s\n\nGot:\n%s", expectedContent, outputContent)
		}

		t.Log("Successfully generated file from template with struct data")
	})

	t.Run("template with conditionals", func(t *testing.T) {
		// Create a template file with conditional logic
		templatePath := filepath.Join(tempDir, "conditional.tmpl")
		templateContent := `# Config
{{if .EnableDebug}}export DEBUG=1{{end}}
{{if .EnableVerbose}}export VERBOSE=1{{end}}`

		if err := os.WriteFile(templatePath, []byte(templateContent), 0o644); err != nil {
			t.Fatalf("Failed to create template file: %v", err)
		}

		// Test with debug enabled, verbose disabled
		data := map[string]bool{
			"EnableDebug":   true,
			"EnableVerbose": false,
		}

		outputPath := filepath.Join(tempDir, "conditional_output.sh")
		if err := files.GenerateFromTemplate(templatePath, outputPath, data); err != nil {
			t.Fatalf("GenerateFromTemplate failed: %v", err)
		}

		// Verify content
		outputContent, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("Failed to read output file: %v", err)
		}

		expectedContent := `# Config
export DEBUG=1
`

		if string(outputContent) != expectedContent {
			t.Errorf("Content mismatch.\nExpected:\n%s\n\nGot:\n%s", expectedContent, outputContent)
		}

		t.Log("Successfully generated file with conditional logic")
	})

	t.Run("template with range/loops", func(t *testing.T) {
		// Create a template file with loops
		templatePath := filepath.Join(tempDir, "loop.tmpl")
		templateContent := `# Packages to install
{{range .Packages}}install {{.}}
{{end}}`

		if err := os.WriteFile(templatePath, []byte(templateContent), 0o644); err != nil {
			t.Fatalf("Failed to create template file: %v", err)
		}

		// Test with slice of packages
		data := map[string][]string{
			"Packages": {"curl", "git", "neovim", "tmux"},
		}

		outputPath := filepath.Join(tempDir, "install_packages.sh")
		if err := files.GenerateFromTemplate(templatePath, outputPath, data); err != nil {
			t.Fatalf("GenerateFromTemplate failed: %v", err)
		}

		// Verify content
		outputContent, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("Failed to read output file: %v", err)
		}

		expectedContent := `# Packages to install
install curl
install git
install neovim
install tmux
`
		if string(outputContent) != expectedContent {
			t.Errorf("Content mismatch.\nExpected:\n%s\n\nGot:\n%s", expectedContent, outputContent)
		}
		t.Log("Successfully generated file with loops")
	})

	t.Run("error when template file does not exist", func(t *testing.T) {
		nonExistentTemplate := filepath.Join(tempDir, "nonexistent.tmpl")
		outputPath := filepath.Join(tempDir, "output.txt")
		data := map[string]string{"Key": "Value"}

		err := files.GenerateFromTemplate(nonExistentTemplate, outputPath, data)
		if err == nil {
			t.Fatal("Expected an error when template file does not exist, got nil")
		}

		if _, statErr := os.Stat(outputPath); !os.IsNotExist(statErr) {
			t.Error("Output file should not be created when template parsing fails")
		}

		t.Logf("Correctly handled nonexistent template file: %v", err)
	})

	t.Run("error when template has syntax error", func(t *testing.T) {
		// Create a template file with invalid syntax
		templatePath := filepath.Join(tempDir, "invalid.tmpl")
		templateContent := `Invalid template {{.MissingCloseBrace`

		if err := os.WriteFile(templatePath, []byte(templateContent), 0o644); err != nil {
			t.Fatalf("Failed to create template file: %v", err)
		}

		outputPath := filepath.Join(tempDir, "invalid_output.txt")
		data := map[string]string{"Key": "Value"}

		err := files.GenerateFromTemplate(templatePath, outputPath, data)
		if err == nil {
			t.Fatal("Expected an error when template has syntax error, got nil")
		}

		t.Logf("Correctly handled template syntax error: %v", err)
	})

	t.Run("template with missing field returns empty value", func(t *testing.T) {
		// Create a template that references a field not in data
		// Note: Go templates don't error on missing fields, they return empty values
		templatePath := filepath.Join(tempDir, "missing_field.tmpl")
		templateContent := `Value: {{.MissingField}} - Other: {{.ExistingField}}`

		if err := os.WriteFile(templatePath, []byte(templateContent), 0o644); err != nil {
			t.Fatalf("Failed to create template file: %v", err)
		}

		outputPath := filepath.Join(tempDir, "missing_field_output.txt")
		data := map[string]string{"ExistingField": "Present"}

		err := files.GenerateFromTemplate(templatePath, outputPath, data)
		if err != nil {
			t.Fatalf("GenerateFromTemplate failed: %v", err)
		}

		// Verify content - missing field should be empty
		outputContent, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("Failed to read output file: %v", err)
		}

		expectedContent := "Value: <no value> - Other: Present"
		if string(outputContent) != expectedContent {
			t.Errorf("Expected %q, got %q", expectedContent, outputContent)
		}

		t.Log("Template with missing field renders as <no value>")
	})

	t.Run("creates output directory when it does not exist", func(t *testing.T) {
		// Create valid template
		templatePath := filepath.Join(tempDir, "valid.tmpl")
		templateContent := `Value: {{.Key}}`

		if err := os.WriteFile(templatePath, []byte(templateContent), 0o644); err != nil {
			t.Fatalf("Failed to create template file: %v", err)
		}

		// Write to a path whose parent directory does not yet exist
		outputPath := filepath.Join(tempDir, "nonexistent_dir", "output.txt")
		data := map[string]string{"Key": "Value"}

		err := files.GenerateFromTemplate(templatePath, outputPath, data)
		if err != nil {
			t.Fatalf("Expected directory to be created automatically, got error: %v", err)
		}

		if _, err := os.Stat(outputPath); err != nil {
			t.Fatalf("Expected output file to exist after auto-creating directory: %v", err)
		}
	})

	t.Run("overwrite existing output file", func(t *testing.T) {
		// Create template
		templatePath := filepath.Join(tempDir, "overwrite.tmpl")
		templateContent := `New content: {{.Value}}`

		if err := os.WriteFile(templatePath, []byte(templateContent), 0o644); err != nil {
			t.Fatalf("Failed to create template file: %v", err)
		}

		// Create existing output file
		outputPath := filepath.Join(tempDir, "overwrite_output.txt")
		oldContent := "This should be overwritten"
		if err := os.WriteFile(outputPath, []byte(oldContent), 0o644); err != nil {
			t.Fatalf("Failed to create existing output file: %v", err)
		}

		// Generate from template (should overwrite)
		data := map[string]string{"Value": "Fresh data"}
		if err := files.GenerateFromTemplate(templatePath, outputPath, data); err != nil {
			t.Fatalf("GenerateFromTemplate failed: %v", err)
		}

		// Verify content was overwritten
		newContent, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("Failed to read output file: %v", err)
		}

		expectedContent := "New content: Fresh data"
		if string(newContent) != expectedContent {
			t.Errorf("Expected content %q, got %q", expectedContent, newContent)
		}

		t.Log("Successfully overwrote existing output file")
	})

	t.Run("empty template produces empty file", func(t *testing.T) {
		// Create empty template
		templatePath := filepath.Join(tempDir, "empty.tmpl")
		if err := os.WriteFile(templatePath, []byte(""), 0o644); err != nil {
			t.Fatalf("Failed to create empty template file: %v", err)
		}

		outputPath := filepath.Join(tempDir, "empty_output.txt")
		data := map[string]string{}

		if err := files.GenerateFromTemplate(templatePath, outputPath, data); err != nil {
			t.Fatalf("GenerateFromTemplate failed with empty template: %v", err)
		}

		// Verify empty output
		content, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("Failed to read output file: %v", err)
		}

		if len(content) != 0 {
			t.Errorf("Expected empty output file, got %d bytes: %q", len(content), content)
		}

		t.Log("Successfully generated empty file from empty template")
	})
}

func TestWriteFileAtomic(t *testing.T) {
	tempDir := createTempDir(t)
	defer os.RemoveAll(tempDir)
	logger.Init(false)

	// leakedTempFiles reports any lingering ".<base>.tmp.*" siblings of path,
	// which WriteFileAtomic must never leave behind after it returns.
	leakedTempFiles := func(t *testing.T, path string) []string {
		t.Helper()
		pattern := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp.*")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("Failed to glob for leaked temp files: %v", err)
		}
		return matches
	}

	t.Run(
		"successful write has correct content, permissions, and no leaked temp file",
		func(t *testing.T) {
			outputPath := filepath.Join(tempDir, "atomic.txt")
			content := []byte("atomic content")

			err := files.WriteFileAtomic(outputPath, content, 0o640)
			if err != nil {
				t.Fatalf("WriteFileAtomic returned an unexpected error: %v", err)
			}

			gotContent, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatalf("Failed to read output file: %v", err)
			}
			if string(gotContent) != string(content) {
				t.Errorf("Content mismatch.\nExpected: %q\nGot: %q", content, gotContent)
			}

			info, err := os.Stat(outputPath)
			if err != nil {
				t.Fatalf("Failed to stat output file: %v", err)
			}
			if info.Mode().Perm() != 0o640 {
				t.Errorf("Expected file permissions 0640, got %o", info.Mode().Perm())
			}

			if leaked := leakedTempFiles(t, outputPath); len(leaked) != 0 {
				t.Errorf("Expected no leaked temp files, found: %v", leaked)
			}
		},
	)

	t.Run("overwrites existing content instead of merging", func(t *testing.T) {
		outputPath := filepath.Join(tempDir, "overwrite.txt")
		oldContent := "this is a much longer original line that should be fully replaced"
		if err := os.WriteFile(outputPath, []byte(oldContent), 0o644); err != nil {
			t.Fatalf("Failed to seed existing file: %v", err)
		}

		newContent := []byte("short new")
		if err := files.WriteFileAtomic(outputPath, newContent, 0o644); err != nil {
			t.Fatalf("WriteFileAtomic returned an unexpected error: %v", err)
		}

		gotContent, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("Failed to read output file: %v", err)
		}
		if string(gotContent) != string(newContent) {
			t.Errorf(
				"Expected target to hold only the new content %q, got %q",
				newContent,
				gotContent,
			)
		}

		if leaked := leakedTempFiles(t, outputPath); len(leaked) != 0 {
			t.Errorf("Expected no leaked temp files, found: %v", leaked)
		}
	})
}

func TestWriteFileAtomicFrom(t *testing.T) {
	tempDir := createTempDir(t)
	t.Cleanup(func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir %s: %v", tempDir, err)
		}
	})
	logger.Init(false)

	leakedTempFiles := func(t *testing.T, path string) []string {
		t.Helper()
		pattern := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp.*")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("Failed to glob for leaked temp files: %v", err)
		}
		return matches
	}

	t.Run("streams reader content to the destination", func(t *testing.T) {
		outputPath := filepath.Join(tempDir, "streamed.txt")
		content := "streamed content"

		if err := files.WriteFileAtomicFrom(
			outputPath,
			strings.NewReader(content),
			0o640,
		); err != nil {
			t.Fatalf("WriteFileAtomicFrom returned an unexpected error: %v", err)
		}

		gotContent, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("Failed to read output file: %v", err)
		}
		if string(gotContent) != content {
			t.Errorf("Content mismatch.\nExpected: %q\nGot: %q", content, gotContent)
		}

		info, err := os.Stat(outputPath)
		if err != nil {
			t.Fatalf("Failed to stat output file: %v", err)
		}
		if info.Mode().Perm() != 0o640 {
			t.Errorf("Expected file permissions 0640, got %o", info.Mode().Perm())
		}

		if leaked := leakedTempFiles(t, outputPath); len(leaked) != 0 {
			t.Errorf("Expected no leaked temp files, found: %v", leaked)
		}
	})

	t.Run("a reader that fails midway leaves no file at the destination", func(t *testing.T) {
		outputPath := filepath.Join(tempDir, "interrupted.txt")

		r := io.MultiReader(
			strings.NewReader("the first half of the payload"),
			&failingReader{err: errors.New("connection reset")},
		)
		if err := files.WriteFileAtomicFrom(outputPath, r, 0o644); err == nil {
			t.Fatal("Expected an error when the reader fails midway, got nil")
		}

		if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
			t.Errorf(
				"Expected no file at %s after a failed write, stat error was %v",
				outputPath,
				err,
			)
		}
		if leaked := leakedTempFiles(t, outputPath); len(leaked) != 0 {
			t.Errorf("Expected no leaked temp files, found: %v", leaked)
		}
	})

	t.Run("a failed write preserves the previous complete file", func(t *testing.T) {
		outputPath := filepath.Join(tempDir, "preserved.txt")
		previous := "previous complete content"
		if err := os.WriteFile(outputPath, []byte(previous), 0o644); err != nil {
			t.Fatalf("Failed to seed existing file: %v", err)
		}

		r := io.MultiReader(
			strings.NewReader("partial"),
			&failingReader{err: errors.New("connection reset")},
		)
		if err := files.WriteFileAtomicFrom(outputPath, r, 0o644); err == nil {
			t.Fatal("Expected an error when the reader fails midway, got nil")
		}

		gotContent, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("Existing file disappeared after a failed write: %v", err)
		}
		if string(gotContent) != previous {
			t.Errorf("Existing file was clobbered.\nExpected: %q\nGot: %q", previous, gotContent)
		}
		if leaked := leakedTempFiles(t, outputPath); len(leaked) != 0 {
			t.Errorf("Expected no leaked temp files, found: %v", leaked)
		}
	})
}

// failingReader always fails, standing in for a transfer that dies partway.
type failingReader struct{ err error }

func (f *failingReader) Read(_ []byte) (int, error) { return 0, f.err }
