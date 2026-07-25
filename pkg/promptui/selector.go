package promptui

import (
	"github.com/cjairm/devgeta/pkg/utils"
	"github.com/manifoldco/promptui"
)

// Helper function to check if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// Function to remove an item from a slice
func removeItem(slice []string, item string) []string {
	for i, s := range slice {
		if s == item {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

func MultiSelect(label string, options []string) ([]string, error) {
	var selectedOptions []string
	availableOptions := options

	for {
		// Remove "All" and "None" if all available options are selected
		if len(selectedOptions) == len(options)-3 { // Exclude "All", "None", "Done"
			availableOptions = removeItem(availableOptions, "All")
			availableOptions = removeItem(availableOptions, "None")
		}

		// Create a prompt for selecting languages
		prompt := promptui.Select{
			Label: label,
			Items: availableOptions,
		}

		// Show the prompt to the user
		_, result, err := prompt.Run()
		if err != nil {
			return nil, err
		}

		// Handle special cases
		switch result {
		case "All":
			selectedOptions = append(
				selectedOptions,
				availableOptions[3:]...)
			return selectedOptions, nil
		case "None":
			return []string{}, nil
		case "Done":
			return selectedOptions, nil
		default:
			// Add the available options to the list of selected options
			if !contains(selectedOptions, result) {
				selectedOptions = append(selectedOptions, result)
			}
			// Remove the selected item from the available options
			availableOptions = removeItem(availableOptions, result)
		}
	}
}

func DisplayInstructions(promptLabel string, instructions string, isConfirm bool) error {
	prompt := promptui.Prompt{
		Label:     promptLabel,
		IsConfirm: isConfirm,
	}
	utils.PrintWarning(instructions)
	_, err := prompt.Run()
	if err != nil {
		return err
	}
	return nil
}
