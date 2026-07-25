package utils

import (
	"fmt"
	"io"
	"strings"

	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/spf13/cobra"
)

var Logger io.Writer = io.Discard

func Log(msg string) {
	fmt.Fprintln(Logger, msg)
}

func PrintError(errMsg string) {
	Print(errMsg, constants.Red)
}

func PrintSuccess(errMsg string) {
	Print(errMsg, constants.Green)
}

func PrintSecondary(msg string) {
	Print(msg, constants.Gray)
}

func PrintInfo(msg string) {
	Print(msg, constants.Blue)
}

func PrintWarning(msg string) {
	Print(msg, constants.Yellow)
}

func PrintBold(msg string) {
	Print(msg, constants.Bold)
}

func Print(msg, custom string) {
	if msg != "" {
		if custom == "" {
			fmt.Println(msg)
		} else {
			fmt.Printf("%s%s%s\n", custom, msg, constants.Reset)
		}
	}
}

func PrompCustomHelp(cmd *cobra.Command, args []string) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Usage:\n  %s\n\n", cmd.Use))
	sb.WriteString(cmd.Long + "\n\n")
	PrintBold(sb.String())
}
