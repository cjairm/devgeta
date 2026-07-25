package languages

import (
	"context"
	"fmt"
	"strings"

	"github.com/cjairm/devgeta/internal/apps/mise"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/promptui"
	"github.com/cjairm/devgeta/pkg/utils"
)

// LanguageInstaller defines the interface for language installation
type LanguageInstaller interface {
	SoftInstall() error
	GetName() string
	GetDisplayName() string
}

// LanguageConfig defines a language's installation configuration
type LanguageConfig struct {
	DisplayName string
	Name        string
	Version     string
	UseMise     bool // true = mise, false = native package manager
}

// DevLanguages coordinates language installation
type DevLanguages struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *DevLanguages {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	dl := &DevLanguages{
		Cmd:  osCmd,
		Base: baseCmd,
	}
	dl.detectPreInstalledLanguages()
	return dl
}

// GetSelectionOptions returns the list of options for TUI selection
// Includes control options ("All", "None", "Done") plus all language display names
// Dynamically generated from language configurations
func (dl *DevLanguages) GetSelectionOptions() []string {
	// TUI control options
	languages := []string{"All", "None", "Done"}
	for _, langCfg := range GetLanguageConfigs() {
		languages = append(languages, langCfg.DisplayName)
	}
	return languages
}

// ChooseLanguages presents TUI for language selection with installed language detection
func (dl *DevLanguages) ChooseLanguages(ctx context.Context) (context.Context, error) {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		logger.L().Warnw("Failed to load global config", "error", err)
	}
	// Get installed languages and filter them from available options
	installedLanguages := dl.getInstalledLanguages(gc)
	availableLanguages := dl.GetSelectionOptions()
	// Remove already installed languages from selection
	filteredLanguages := filterSlice(availableLanguages, installedLanguages)
	if len(installedLanguages) > 0 {
		utils.PrintWarning(fmt.Sprintf(
			"Already installed languages (skipped from selection): %s",
			strings.Join(installedLanguages, ", "),
		))
	}
	selectedLanguages, err := promptui.MultiSelect(
		"Select programming languages to install",
		filteredLanguages,
	)
	logger.L().Info("Selected languages: ", selectedLanguages)
	if err != nil {
		return nil, err
	}
	initialConfig := config.ContextConfig{}
	initialConfig.SelectedLanguages = selectedLanguages
	return config.WithConfig(ctx, initialConfig), nil
}

// isLanguageInstalledOnSystem checks if a language is installed by running its version command
func (dl *DevLanguages) isLanguageInstalledOnSystem(langCfg LanguageConfig) bool {
	versionCmd, versionArgs := getVersionCommand(langCfg.Name)
	_, _, err := dl.Base.ExecCommand(cmd.CommandParams{
		Command: versionCmd,
		Args:    versionArgs,
	})
	return err == nil
}

// IsInstalledOnSystem reports whether the tracked language spec (as produced by
// formatSpec — e.g. "node@lts" for mise-managed languages, "php" for native ones)
// matches a known language config and is present on the system via its version
// command. Returns false for a spec that matches no current config.
func (dl *DevLanguages) IsInstalledOnSystem(name string) bool {
	for _, langCfg := range GetLanguageConfigs() {
		if formatSpec(langCfg.Name, langCfg.Version, langCfg.UseMise) == name {
			return dl.isLanguageInstalledOnSystem(langCfg)
		}
	}
	return false
}

// getInstalledLanguages returns list of already installed language display names
func (dl *DevLanguages) getInstalledLanguages(gc *config.GlobalConfig) []string {
	installed := []string{}
	languageConfigs := GetLanguageConfigs()
	for _, langCfg := range languageConfigs {
		langSpec := formatSpec(langCfg.Name, langCfg.Version, langCfg.UseMise)
		if gc.IsInstalledByDevgeta(langSpec, "dev_language") ||
			gc.IsAlreadyInstalled(langSpec, "dev_language") {
			installed = append(installed, langCfg.DisplayName)
		}
	}
	return installed
}

// InstallChosen installs the selected languages using structured approach
func (dl *DevLanguages) InstallChosen(ctx context.Context) {
	selections, ok := config.GetConfig(ctx)
	logger.L().Info("Installing chosen languages: ", selections.SelectedLanguages)
	if !ok || len(selections.SelectedLanguages) == 0 {
		utils.PrintInfo("No languages selected for installation")
		return
	}
	languageConfigs := GetLanguageConfigs()
	for _, langCfg := range languageConfigs {
		if containsIgnoreCase(langCfg.DisplayName, selections.SelectedLanguages) {
			dl.installLanguage(langCfg)
		}
	}
}

// installLanguage handles the installation and config tracking for a single language
func (dl *DevLanguages) installLanguage(langCfg LanguageConfig) {
	utils.PrintInfo(fmt.Sprintf("Installing %s (if not previously installed)...",
		langCfg.DisplayName))

	var err error
	if langCfg.UseMise {
		err = dl.installWithMise(langCfg)
	} else {
		err = dl.installNative(langCfg)
	}
	if err != nil {
		utils.PrintError(fmt.Sprintf("Error: Unable to install %s: %v",
			langCfg.DisplayName, err))
		logger.L().Errorw("Language installation failed",
			"language", langCfg.Name,
			"error", err)
		return
	}
	// Track successful installation in GlobalConfig
	langSpec := formatSpec(langCfg.Name, langCfg.Version, langCfg.UseMise)
	if err := dl.trackInstallation(langSpec); err != nil {
		logger.L().Warnw("Failed to track language installation",
			"language", langSpec,
			"error", err)
	}
}

// installWithMise installs a language via Mise runtime manager
func (dl *DevLanguages) installWithMise(langCfg LanguageConfig) error {
	m := mise.New()
	// Ensure Mise is installed and configured
	if err := m.SoftInstall(); err != nil {
		return fmt.Errorf("failed to install mise: %w", err)
	}
	if err := m.SoftConfigure(); err != nil {
		return fmt.Errorf("failed to configure mise: %w", err)
	}
	// Install the language globally via Mise
	if err := m.UseGlobal(langCfg.Name, langCfg.Version); err != nil {
		return fmt.Errorf("failed to install %s@%s via mise: %w",
			langCfg.Name, langCfg.Version, err)
	}
	return nil
}

// installNative installs a language via native package manager
func (dl *DevLanguages) installNative(langCfg LanguageConfig) error {
	if err := dl.Cmd.MaybeInstallPackage(langCfg.Name); err != nil {
		return fmt.Errorf("failed to install %s via package manager: %w",
			langCfg.Name, err)
	}
	return nil
}

// trackInstallation adds the language to GlobalConfig
func (dl *DevLanguages) trackInstallation(languageSpec string) error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.AddToInstalled(languageSpec, "dev_language")
	if err := gc.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	return nil
}

// detectPreInstalledLanguages checks system for pre-existing language installations
// and updates GlobalConfig accordingly
func (dl *DevLanguages) detectPreInstalledLanguages() {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		logger.L().Warnw("Failed to load global config during language detection", "error", err)
		return
	}
	languageConfigs := GetLanguageConfigs()
	configUpdated := false
	for _, langCfg := range languageConfigs {
		langSpec := formatSpec(langCfg.Name, langCfg.Version, langCfg.UseMise)
		if gc.IsInstalledByDevgeta(langSpec, "dev_language") ||
			gc.IsAlreadyInstalled(langSpec, "dev_language") {
			continue
		}
		if dl.isLanguageInstalledOnSystem(langCfg) {
			logger.L().Infow("Detected pre-existing language installation",
				"language", langCfg.DisplayName,
				"spec", langSpec)
			gc.AddToAlreadyInstalled(langSpec, "dev_language")
			configUpdated = true
		}
	}
	if configUpdated {
		if err := gc.Save(); err != nil {
			logger.L().Warnw("Failed to save global config after language detection", "error", err)
		}
	}
}
