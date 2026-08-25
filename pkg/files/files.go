package files

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/cjairm/devgeta/pkg/logger"
)

const (
	// FilePermission is the default permission for regular files (rw-r--r--)
	FilePermission = 0o644
	// DirPermission is the default permission for directories (rwxr-xr-x)
	DirPermission = 0o755
)

// SoftCopyFile copies a file from src to dst only if dst does not already exist.
// If the destination file exists, it logs an info message and returns nil without copying.
// Returns an error if the copy operation fails.
func SoftCopyFile(src, dst string) error {
	logger.L().Debugw("Soft copying file", "src", src, "dst", dst)
	if FileAlreadyExist(dst) {
		logger.L().Info("File already exists, skipping copy", "dst", dst)
		return nil
	}
	if err := CopyFile(src, dst); err != nil {
		return fmt.Errorf("failed to soft copy file from %s to %s: %w", src, dst, err)
	}
	return nil
}

// SoftCopyDir copies a directory from src to dst only if dst does not exist or is empty.
// If the destination directory exists and contains files, it logs an info message and returns nil.
// Returns an error if the copy operation fails.
func SoftCopyDir(src, dst string) error {
	logger.L().Debugw("Soft copying directory", "src", src, "dst", dst)
	if DirAlreadyExist(dst) && !IsDirEmpty(dst) {
		logger.L().Info("Directory already exists with content, skipping copy", "dst", dst)
		return nil
	}
	if err := CopyDir(src, dst); err != nil {
		return fmt.Errorf("failed to soft copy directory from %s to %s: %w", src, dst, err)
	}
	return nil
}

// CopyFile copies a file from src to dst, creating or overwriting the destination file.
// The destination file will have FilePermission (0644). Callers that need an
// executable destination (e.g. hook scripts) must chmod it explicitly after
// copying — see internal/apps/claude/claude.go for the established pattern.
// Returns an error if reading the source or writing the destination fails.
func CopyFile(src, dst string) error {
	logger.L().Debugw("Copying file", "src", src, "dst", dst)
	input, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read source file %s: %w", src, err)
	}
	if err := os.WriteFile(dst, input, FilePermission); err != nil {
		return fmt.Errorf("failed to write destination file %s: %w", dst, err)
	}
	return nil
}

// CopyDir recursively copies a directory from src to dst, including all subdirectories and files.
// Creates the destination directory structure as needed with DirPermission (0755).
// Returns an error if any directory or file operation fails.
func CopyDir(src, dst string) error {
	logger.L().Debugw("Copying directory", "src", src, "dst", dst)
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("failed to read source directory %s: %w", src, err)
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := os.MkdirAll(dstPath, DirPermission); err != nil {
				return fmt.Errorf("failed to create destination directory %s: %w", dstPath, err)
			}
			if err := CopyDir(srcPath, dstPath); err != nil {
				return fmt.Errorf(
					"failed to copy subdirectory from %s to %s: %w",
					srcPath,
					dstPath,
					err,
				)
			}
		} else {
			dstDir := filepath.Dir(dstPath)
			if err := os.MkdirAll(dstDir, DirPermission); err != nil {
				return fmt.Errorf("failed to create parent directory %s: %w", dstDir, err)
			}
			if err := CopyFile(srcPath, dstPath); err != nil {
				return fmt.Errorf("failed to copy file from %s to %s: %w", srcPath, dstPath, err)
			}
		}
	}
	return nil
}

// FileAlreadyExist checks if a file exists at the given path.
// Returns true if the file exists, false otherwise.
// Note: This returns true for both files and directories.
func FileAlreadyExist(filePath string) bool {
	info := getEntryInfo(filePath)
	return info != nil
}

// DirAlreadyExist checks if a directory exists at the given path.
// Returns true only if the path exists and is a directory, false otherwise.
func DirAlreadyExist(folderPath string) bool {
	info := getEntryInfo(folderPath)
	if info == nil {
		return false
	}
	return info.IsDir()
}

// IsDirEmpty checks if a directory is empty (contains no files or subdirectories).
// Returns true if the directory is empty or if an error occurs while reading it.
// Non-existent directories are treated as empty.
func IsDirEmpty(dirPath string) bool {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return true // Treat read error as empty
	}
	return len(entries) == 0
}

// UpdateFile replaces all occurrences of searchText with replacementText in the specified file.
// The file is read entirely into memory, modified, and written back with FilePermission (0644).
// Returns an error if reading or writing the file fails.
func UpdateFile(filePath, searchText, replacementText string) error {
	logger.L().
		Debugw("Updating file", "filePath", filePath, "searchText", searchText, "replacementText", replacementText)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}
	updatedContent := strings.ReplaceAll(string(data), searchText, replacementText)
	if err := os.WriteFile(filePath, []byte(updatedContent), FilePermission); err != nil {
		return fmt.Errorf("failed to write updated content to file %s: %w", filePath, err)
	}
	return nil
}

// ContentExistsInFile checks if a substring exists in a file (case-insensitive search).
// Reads the file line by line to avoid loading large files entirely into memory.
// Returns true if the substring is found, false otherwise, along with any read errors.
func ContentExistsInFile(filePath, substringToFind string) (bool, error) {
	logger.L().
		Debugw("Checking if content exists in file", "filePath", filePath, "substringToFind", substringToFind)
	file, err := os.Open(filePath)
	if err != nil {
		return false, fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer func() {
		// Read-only handle; a close failure here is not actionable.
		_ = file.Close()
	}()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(strings.ToLower(line), strings.ToLower(substringToFind)) {
			return true, nil // Found the substring, return true
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("error scanning file %s: %w", filePath, err)
	}
	return false, nil
}

// AddLineToFile appends a line to a file, creating the file if it doesn't exist.
// The line is added on a new line. If the file exists, it is opened in append mode.
// The file is created with FilePermission (0644) if it doesn't exist.
// Returns an error if opening the file or writing fails.
func AddLineToFile(line, filePath string) error {
	logger.L().Debugw("Adding line to file", "line", line, "filePath", filePath)
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, FilePermission)
	if err != nil {
		return fmt.Errorf("failed to open file %s for appending: %w", filePath, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			logger.L().
				Debugw("failed to close file after append", "filePath", filePath, "error", closeErr)
		}
	}()
	if _, err := file.WriteString("\n" + line); err != nil {
		return fmt.Errorf("failed to write line to file %s: %w", filePath, err)
	}
	return nil
}

// GenerateFromTemplate parses a template file, applies the provided data, and writes
// the result to the output path. The template file should use Go's text/template syntax.
// The data parameter can be any struct, map, or value accessible in the template.
// The output file is created with FilePermission (0644).
//
// Example template usage:
//
//	export HOME="{{.Home}}"
//	export USER="{{.User}}"
//
// Returns an error if template parsing, execution, or file writing fails.
func GenerateFromTemplate(templatePath, outputPath string, data any) error {
	tmpl, err := template.ParseFiles(templatePath)
	if err != nil {
		return fmt.Errorf("failed to parse template %s: %w", templatePath, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute template %s: %w", templatePath, err)
	}
	if err := WriteFileAtomic(outputPath, buf.Bytes(), FilePermission); err != nil {
		return err
	}
	logger.L().
		Debugw("Successfully generated file from template", "outputPath", outputPath)
	return nil
}

// WriteFileAtomic writes data to path atomically: it writes to a temporary file in
// the same directory as path (so the final rename stays on one filesystem and is
// atomic), then renames it over path as the commit point. This guarantees a crash or
// interrupted write never leaves path truncated or partially written — either the old
// complete content or the new complete content is observed, never a partial mix.
// The target directory is created if it does not already exist.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	return WriteFileAtomicFrom(path, bytes.NewReader(data), perm)
}

// WriteFileAtomicFrom is the streaming form of WriteFileAtomic: it copies every byte
// read from r into path with the same temp-file-then-rename commit, so a transfer that
// fails or is interrupted midway never leaves a truncated file at path. Use it when the
// content is a stream (an HTTP response body, another file) rather than a slice already
// held in memory; WriteFileAtomic is a thin wrapper over this function.
func WriteFileAtomicFrom(path string, r io.Reader, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, DirPermission); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", path, err)
	}
	tempFile, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("failed to create temporary file for %s: %w", path, err)
	}
	tempPath := tempFile.Name()
	// closed tracks whether the success path below already closed tempFile, so
	// this cleanup only closes it on error paths that return before that point
	// (a double Close on the same *os.File always errors, which would otherwise
	// spam a bogus "failed to close" log line on every successful write).
	closed := false
	defer func() {
		if !closed {
			if closeErr := tempFile.Close(); closeErr != nil {
				logger.L().
					Debugw("failed to close temporary file", "tempPath", tempPath, "error", closeErr)
			}
		}
		if _, statErr := os.Stat(tempPath); statErr == nil {
			// Only reached if we returned before the rename; the temp file must
			// not linger next to the real config.
			if removeErr := os.Remove(tempPath); removeErr != nil {
				logger.L().
					Debugw("failed to remove temporary file", "tempPath", tempPath, "error", removeErr)
			}
		}
	}()
	if _, err := io.Copy(tempFile, r); err != nil {
		return fmt.Errorf("failed to write temporary file %s: %w", tempPath, err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close temporary file %s: %w", tempPath, err)
	}
	closed = true
	if err := os.Chmod(tempPath, perm); err != nil {
		return fmt.Errorf("failed to set permissions on temporary file %s: %w", tempPath, err)
	}
	// Atomically replace the target file (this is the commit point).
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("failed to replace file %s: %w", path, err)
	}
	return nil
}

// getEntryInfo retrieves file information for the given path.
// Returns nil if the path doesn't exist or if an error occurs (e.g., permission denied).
// This is a helper function used by FileAlreadyExist and DirAlreadyExist.
func getEntryInfo(path string) os.FileInfo {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		// An error occurred (e.g., permission denied)
		return nil
	}
	return info
}
