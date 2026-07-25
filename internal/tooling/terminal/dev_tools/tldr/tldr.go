// tldr simplified command documentation tool with devgeta integration
//
// tldr (Too Long; Didn't Read) is a command-line utility that provides simplified
// and community-driven man pages with practical examples. It focuses on practical
// usage examples rather than comprehensive documentation.
//
// References:
// - tldr Documentation: https://github.com/tldr-pages/tldr
// - tldr Client Spec: https://github.com/tldr-pages/tldr/blob/main/CLIENT-SPECIFICATION.md
//
// Common tldr commands available through ExecuteCommand():
//   - tldr --version - Show tldr version information
//   - tldr <command> - Display simplified documentation for command
//   - tldr --list - List all available commands
//   - tldr --update - Update local cache of tldr pages
//   - tldr --platform <platform> - Show examples for specific platform
//   - tldr --language <lang> - Display pages in specific language
//   - tldr --render <file> - Render a specific markdown file

package tldr

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Tldr struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Tldr {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Tldr{Cmd: osCmd, Base: baseCmd}
}

func (t *Tldr) Install() error {
	return t.Cmd.InstallPackage(constants.Tldr)
}

func (t *Tldr) SoftInstall() error {
	return t.Cmd.MaybeInstallPackage(constants.Tldr)
}

func (t *Tldr) ForceInstall() error {
	err := t.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall tldr: %w", err)
	}
	return t.Install()
}

func (t *Tldr) Uninstall() error {
	return fmt.Errorf("tldr uninstall not supported through devgeta")
}

func (t *Tldr) ForceConfigure() error {
	// tldr doesn't require traditional configuration files
	// Users can optionally configure via environment variables:
	// - TLDR_CACHE_DIR: Custom cache directory
	// - TLDR_PAGES_SOURCE_LOCATION: Custom source for tldr pages
	// - TLDR_COLOR_BLANK, TLDR_COLOR_NAME, etc.: Color customization

	// TODO: Replace `man` with this app.
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

func (t *Tldr) SoftConfigure() error {
	// tldr doesn't require traditional configuration files

	// TODO: Replace `man` with this app.
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

func (t *Tldr) ExecuteCommand(args ...string) error {
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.Tldr,
		Args:    args,
	}
	if _, _, err := t.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run tldr command: %w", err)
	}
	return nil
}

func (t *Tldr) Update() error {
	return fmt.Errorf("tldr update not implemented through devgeta")
}
