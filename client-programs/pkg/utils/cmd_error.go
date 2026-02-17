package utils

import (
	"fmt"

	"github.com/spf13/cobra"
)

func CmdError(cmd *cobra.Command, errorMessage string, additionalMessage string) error {
	return cmdError(cmd, errorMessage, additionalMessage, false)
}

func CmdErrorFullUsage(cmd *cobra.Command, errorMessage string, additionalMessage string) error {
	return cmdError(cmd, errorMessage, additionalMessage, true)
}

func cmdError(cmd *cobra.Command, errorMessage string, additionalMessage string, fullUsage bool) error {
	if fullUsage {
		return fmt.Errorf("%s\n\n%s", errorMessage, cmd.UsageString())
	}

	return fmt.Errorf("%s\n\n%s %s\nRun '%s --help' for details.", errorMessage, cmd.CommandPath(), additionalMessage, cmd.CommandPath())
}
