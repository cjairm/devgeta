package databases

import (
	"context"
	"fmt"
	"strings"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/promptui"
	"github.com/cjairm/devgeta/pkg/utils"
)

// DatabaseConfig defines a database's installation configuration
type DatabaseConfig struct {
	DisplayName string
	Name        string
}

// Databases coordinates database installation
type Databases struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Databases {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	d := &Databases{
		Cmd:  osCmd,
		Base: baseCmd,
	}
	d.detectPreInstalledDatabases()
	return d
}

// GetSelectionOptions returns the list of options for TUI selection
// Includes control options ("All", "None", "Done") plus all database display names
// Dynamically generated from database configurations
func (d *Databases) GetSelectionOptions() []string {
	// TUI control options
	databases := []string{"All", "None", "Done"}
	for _, dbCfg := range GetDatabaseConfigs() {
		databases = append(databases, dbCfg.DisplayName)
	}
	return databases
}

// ChooseDatabases presents TUI for database selection with installed database detection
func (d *Databases) ChooseDatabases(ctx context.Context) (context.Context, error) {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		logger.L().Warnw("Failed to load global config", "error", err)
	}
	// Get installed databases and filter them from available options
	installedDatabases := d.getInstalledDatabases(gc)
	availableDatabases := d.GetSelectionOptions()
	// Remove already installed databases from selection
	filteredDatabases := filterSlice(availableDatabases, installedDatabases)
	if len(installedDatabases) > 0 {
		utils.PrintWarning(fmt.Sprintf(
			"Already installed databases (skipped from selection): %s",
			strings.Join(installedDatabases, ", "),
		))
	}
	selectedDatabases, err := promptui.MultiSelect(
		"Select databases to install",
		filteredDatabases,
	)
	logger.L().Info("Selected databases: ", selectedDatabases)
	if err != nil {
		return nil, err
	}
	initialConfig := config.ContextConfig{}
	initialConfig.SelectedDbs = selectedDatabases
	return config.WithConfig(ctx, initialConfig), nil
}

// isDatabaseInstalledOnSystem checks if a database is installed by running its version command
func (d *Databases) isDatabaseInstalledOnSystem(dbCfg DatabaseConfig) bool {
	versionCmd, versionArgs := getVersionCommand(dbCfg.Name)
	_, _, err := d.Base.ExecCommand(cmd.CommandParams{
		Command: versionCmd,
		Args:    versionArgs,
	})
	return err == nil
}

// IsInstalledOnSystem reports whether the tracked database name matches a known
// database config and is present on the system via its version command. Returns
// false for a name that matches no current config.
func (d *Databases) IsInstalledOnSystem(name string) bool {
	for _, dbCfg := range GetDatabaseConfigs() {
		if dbCfg.Name == name {
			return d.isDatabaseInstalledOnSystem(dbCfg)
		}
	}
	return false
}

// getInstalledDatabases returns list of already installed database display names
func (d *Databases) getInstalledDatabases(gc *config.GlobalConfig) []string {
	installed := []string{}
	databaseConfigs := GetDatabaseConfigs()
	for _, dbCfg := range databaseConfigs {
		dbSpec := dbCfg.Name
		if gc.IsInstalledByDevgeta(dbSpec, "database") ||
			gc.IsAlreadyInstalled(dbSpec, "database") {
			installed = append(installed, dbCfg.DisplayName)
		}
	}
	return installed
}

// InstallChosen installs the selected databases using structured approach
func (d *Databases) InstallChosen(ctx context.Context) {
	selections, ok := config.GetConfig(ctx)
	logger.L().Info("Installing chosen databases: ", selections.SelectedDbs)
	if !ok || len(selections.SelectedDbs) == 0 {
		utils.PrintInfo("No databases selected for installation")
		return
	}
	databaseConfigs := GetDatabaseConfigs()
	for _, dbCfg := range databaseConfigs {
		if containsIgnoreCase(dbCfg.DisplayName, selections.SelectedDbs) {
			d.installDatabase(dbCfg)
		}
	}
}

// installDatabase handles the installation and config tracking for a single database
func (d *Databases) installDatabase(dbCfg DatabaseConfig) {
	utils.PrintInfo(fmt.Sprintf("Installing %s (if not previously installed)...",
		dbCfg.DisplayName))
	err := d.installNative(dbCfg)
	if err != nil {
		utils.PrintError(fmt.Sprintf("Error: Unable to install %s: %v",
			dbCfg.DisplayName, err))
		logger.L().Errorw("Database installation failed",
			"database", dbCfg.Name,
			"error", err)
		return
	}
	dbSpec := dbCfg.Name
	if err := d.trackInstallation(dbSpec); err != nil {
		logger.L().Warnw("Failed to track database installation",
			"database", dbSpec,
			"error", err)
	}
}

// installNative installs a database via native package manager
func (d *Databases) installNative(dbCfg DatabaseConfig) error {
	if err := d.Cmd.MaybeInstallPackage(dbCfg.Name); err != nil {
		return fmt.Errorf("failed to install %s via package manager: %w",
			dbCfg.Name, err)
	}
	return nil
}

// trackInstallation adds the database to GlobalConfig
func (d *Databases) trackInstallation(databaseSpec string) error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.AddToInstalled(databaseSpec, "database")
	if err := gc.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	return nil
}

// detectPreInstalledDatabases checks system for pre-existing database installations
// and updates GlobalConfig accordingly
func (d *Databases) detectPreInstalledDatabases() {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		logger.L().Warnw("Failed to load global config during database detection", "error", err)
		return
	}
	databaseConfigs := GetDatabaseConfigs()
	configUpdated := false
	for _, dbCfg := range databaseConfigs {
		dbSpec := dbCfg.Name
		if gc.IsInstalledByDevgeta(dbSpec, "database") ||
			gc.IsAlreadyInstalled(dbSpec, "database") {
			continue
		}
		if d.isDatabaseInstalledOnSystem(dbCfg) {
			logger.L().Infow("Detected pre-existing database installation",
				"database", dbCfg.DisplayName,
				"spec", dbSpec)
			gc.AddToAlreadyInstalled(dbSpec, "database")
			configUpdated = true
		}
	}
	if configUpdated {
		if err := gc.Save(); err != nil {
			logger.L().Warnw("Failed to save global config after database detection", "error", err)
		}
	}
}
