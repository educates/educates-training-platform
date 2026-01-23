package config

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
)

// Header comment shown at the top of the editor (similar to kubectl edit)
const editHeader = `## Please edit the configuration below. Lines beginning with a '##' will be ignored,
## and an empty file will abort the edit. If an error occurs while saving, this file
## will be reopened with the relevant failures.
##
`

type LocalConfigEditConfig struct{}

func (o *LocalConfigEditConfig) Edit() error {
	// Create the configuration directory if it doesn't exist
	err := os.MkdirAll(utils.GetEducatesHomeDir(), os.ModePerm)
	if err != nil {
		return errors.Wrapf(err, "unable to create configuration directory %q", utils.GetEducatesHomeDir())
	}

	valuesFilePath := path.Join(utils.GetEducatesHomeDir(), "values.yaml")

	// Read existing configuration file if it exists
	var valuesFileData []byte
	valuesFileData, err = os.ReadFile(valuesFilePath)
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "unable to read existing configuration file %q", valuesFilePath)
	}

	// Create a temporary file in the OS temp directory (e.g., /tmp on Unix)
	tmpFile, err := os.CreateTemp("", "educates-config-*.yaml")
	if err != nil {
		return errors.Wrapf(err, "unable to create temporary configuration file")
	}
	tmpValuesFilePath := tmpFile.Name()
	tmpFile.Close() // Close immediately since we'll use os.WriteFile

	// Track whether to preserve temp file on exit (set to true when user cancels after making changes)
	preserveTempFile := false

	// Clean up temp file when done (unless we need to preserve it for user recovery)
	defer func() {
		if preserveTempFile {
			return // Don't delete - user's changes are stored there
		}
		if removeErr := os.Remove(tmpValuesFilePath); removeErr != nil && !os.IsNotExist(removeErr) {
			// Log but don't fail on cleanup errors
			fmt.Fprintf(os.Stderr, "Warning: unable to remove temporary file %q: %v\n", tmpValuesFilePath, removeErr)
		}
	}()

	// Determine which editor to use
	// Check VISUAL first (common convention), then EDITOR, then default to vi
	editor := os.Getenv("VISUAL")
	if strings.TrimSpace(editor) == "" {
		editor = os.Getenv("EDITOR")
	}
	if strings.TrimSpace(editor) == "" {
		editor = "vi"
	}
	editor = strings.TrimSpace(editor)

	// Look up the editor executable path
	editorPath, err := exec.LookPath(editor)
	if err != nil {
		return errors.Wrapf(err, "unable to find editor %q in PATH", editor)
	}

	// Write the initial configuration with header comment
	err = writeEditFile(tmpValuesFilePath, editHeader, valuesFileData)
	if err != nil {
		return errors.Wrapf(err, "unable to write to temporary configuration file %q", tmpValuesFilePath)
	}

	// Track edit iterations to distinguish first edit from subsequent edits
	isFirstEdit := true
	// Keep track of the last valid user content (stripped of comments) for detecting no-save exits
	var lastStrippedContent []byte

	// Edit loop: keep reopening editor on validation errors (like kubectl edit)
	for {
		// Read file content before editing to detect if user saved or quit without saving
		contentBeforeEdit, err := os.ReadFile(tmpValuesFilePath)
		if err != nil {
			return errors.Wrapf(err, "unable to read temporary configuration file %q", tmpValuesFilePath)
		}

		// Launch the editor
		cmd := exec.Command(editorPath, tmpValuesFilePath)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err = cmd.Start()
		if err != nil {
			return errors.Wrapf(err, "unable to start editor %q", editor)
		}

		err = cmd.Wait()
		if err != nil {
			return errors.Wrapf(err, "editor %q exited with an error", editor)
		}

		// Read the edited file
		editedData, err := os.ReadFile(tmpValuesFilePath)
		if err != nil {
			return errors.Wrapf(err, "unable to read edited configuration file %q", tmpValuesFilePath)
		}

		// Check if the file content changed (detect quit without saving like :q!)
		if string(editedData) == string(contentBeforeEdit) {
			if isFirstEdit {
				// First edit, user quit without making any changes
				fmt.Println("Edit cancelled, no changes made.")
				return nil
			} else {
				// Subsequent edit after validation error, user quit without saving
				// Preserve the temp file with the user's last changes
				err = os.WriteFile(tmpValuesFilePath, lastStrippedContent, 0644)
				if err == nil {
					preserveTempFile = true
					fmt.Printf("A copy of your changes has been stored to %q\n", tmpValuesFilePath)
				}
				return errors.New("Edit cancelled, no valid changes were saved.")
			}
		}

		strippedData := stripComments(editedData)

		// Check if the file is empty (abort edit)
		if len(strings.TrimSpace(string(strippedData))) == 0 {
			if isFirstEdit {
				fmt.Println("Edit cancelled, no changes made.")
				return nil
			} else {
				// User cleared all content after a validation error
				err = os.WriteFile(tmpValuesFilePath, lastStrippedContent, 0644)
				if err == nil {
					preserveTempFile = true
					fmt.Printf("A copy of your changes has been stored to %q\n", tmpValuesFilePath)
				}
				return errors.New("Edit cancelled, no valid changes were saved.")
			}
		}

		// Save the stripped content for potential recovery
		lastStrippedContent = strippedData

		// Write stripped data to temp file for validation
		err = os.WriteFile(tmpValuesFilePath, strippedData, 0644)
		if err != nil {
			return errors.Wrapf(err, "unable to write configuration for validation")
		}

		// Validate the edited configuration file
		_, validationErr := NewInstallationConfigFromFileForConfigEdit(tmpValuesFilePath)
		if validationErr != nil {
			// Validation failed: rewrite file with error comment and reopen editor
			errorHeader := fmt.Sprintf("%s## %s\n##\n", editHeader, validationErr.Error())
			err = writeEditFile(tmpValuesFilePath, errorHeader, strippedData)
			if err != nil {
				return errors.Wrapf(err, "unable to write error feedback to configuration file")
			}
			isFirstEdit = false // Mark that we've had at least one validation attempt
			continue            // Reopen editor
		}

		// Validation succeeded: save the configuration
		err = os.WriteFile(valuesFilePath, strippedData, 0644)
		if err != nil {
			return errors.Wrapf(err, "unable to update configuration file %q", valuesFilePath)
		}

		fmt.Println("Configuration updated successfully.")
		return nil
	}
}

// writeEditFile writes the header comment followed by the configuration data to the file
func writeEditFile(filePath string, header string, data []byte) error {
	content := header + string(data)
	return os.WriteFile(filePath, []byte(content), 0644)
}

// stripComments removes lines starting with '##' from the data
func stripComments(data []byte) []byte {
	var result strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(string(data)))

	for scanner.Scan() {
		line := scanner.Text()
		// Skip lines that start with '##' (with optional leading whitespace)
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "##") {
			continue
		}
		result.WriteString(line)
		result.WriteString("\n")
	}

	return []byte(result.String())
}
