package utils

import (
	"fmt"
	"os"

	"github.com/cjairm/devgeta/pkg/constants"
)

// MaybeExitWithError is the one place a devgeta failure reaches the user.
// cmd.Execute silences Cobra's own error printing and calls this instead, so
// the message appears once, on stderr, where a caller redirecting stdout to a
// file still sees why the run failed.
//
// The "Error:" prefix is the one Cobra used to print, kept because it is what
// makes a multi-line message readable: the first line is the refusal, the
// lines under it are the reason.
func MaybeExitWithError(err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "%sError: %s%s\n", constants.Red, err.Error(), constants.Reset)
	os.Exit(1)
}
