// fd-find is a fast and user-friendly alternative to 'find' with devgeta integration
//
// fd-find (commonly called 'fd') is a simple, fast, and user-friendly alternative to the
// traditional Unix 'find' command. It provides intuitive syntax, respects .gitignore by
// default, and uses regular expressions for pattern matching.
//
// References:
// - fd Documentation: https://github.com/sharkdp/fd
// - fd User Guide: https://github.com/sharkdp/fd#how-to-use
//
// Common fd commands available through ExecuteCommand():
//   - fd --version - Show fd version information
//   - fd <pattern> - Search for files matching pattern
//   - fd -e <ext> <pattern> - Search for files with specific extension
//   - fd -t f <pattern> - Search only for files
//   - fd -t d <pattern> - Search only for directories
//   - fd -H <pattern> - Include hidden files
//   - fd -I <pattern> - Include .gitignore files
//   - fd -x <cmd> <pattern> - Execute command on search results

package fdfind

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type FdFind struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *FdFind {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &FdFind{Cmd: osCmd, Base: baseCmd}
}

func (f *FdFind) Install() error {
	return f.Cmd.InstallPackage(constants.FdFind)
}

func (f *FdFind) SoftInstall() error {
	return f.Cmd.MaybeInstallPackage(constants.FdFind)
}

func (f *FdFind) ForceInstall() error {
	err := f.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall fd-find: %w", err)
	}
	return f.Install()
}

func (f *FdFind) Uninstall() error {
	return fmt.Errorf("fd-find uninstall not supported through devgeta")
}

func (f *FdFind) ForceConfigure() error {
	// fd-find doesn't require traditional configuration files
	// Users can optionally create ~/.fdignore for custom ignore patterns
	// or use FD_OPTS environment variable for default options

	// TODO: Replace `find` with this app.
	//
	// Ex, export HOME={{.Home}}
	//
	// func main() {
	// 	tmpl, err := template.ParseFiles("myfile.zsh")
	// 	if err != nil {
	// 		panic(err)
	// 	}
	//
	// 	data := map[string]string{
	// 		"Home": "/User/Somethin/haha",
	// 	}
	//
	// 	outputFile, err := os.Create("myfile.generated.zsh")
	// 	if err != nil {
	// 		panic(err)
	// 	}
	// 	defer outputFile.Close()
	//
	// 	err = tmpl.Execute(outputFile, data)
	// 	if err != nil {
	// 		panic(err)
	// 	}
	// }
	return nil
}

func (f *FdFind) SoftConfigure() error {
	// fd-find doesn't require traditional configuration files

	// TODO: Replace `find` with this app.
	//
	// Ex, export HOME={{.Home}}
	//
	// func main() {
	// 	tmpl, err := template.ParseFiles("myfile.zsh")
	// 	if err != nil {
	// 		panic(err)
	// 	}
	//
	// 	data := map[string]string{
	// 		"Home": "/User/Somethin/haha",
	// 	}
	//
	// 	outputFile, err := os.Create("myfile.generated.zsh")
	// 	if err != nil {
	// 		panic(err)
	// 	}
	// 	defer outputFile.Close()
	//
	// 	err = tmpl.Execute(outputFile, data)
	// 	if err != nil {
	// 		panic(err)
	// 	}
	// }
	return nil
}

func (f *FdFind) ExecuteCommand(args ...string) error {
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.FdFind,
		Args:    args,
	}
	if _, _, err := f.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run fd-find command: %w", err)
	}
	return nil
}

func (f *FdFind) Update() error {
	return fmt.Errorf("fd-find update not implemented through devgeta")
}
