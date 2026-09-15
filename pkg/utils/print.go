package utils

import (
	"fmt"
	"io"

	"github.com/cjairm/devgeta/pkg/constants"
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
