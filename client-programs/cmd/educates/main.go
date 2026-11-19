/*
Command line client for Educates.
*/
package main

import (
	"os"
	"strings"

	"github.com/educates/educates-training-platform/client-programs/pkg/cmd"
	"github.com/educates/educates-training-platform/client-programs/pkg/logger"
	"github.com/go-logr/logr"
	"k8s.io/klog/v2"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// NOTE: The version of Educates which is installed by the CLI is overridden
// with the actual version at build time when making a release.

var projectVersion string = "develop"
var imageRepository string = "ghcr.io/educates"

// gitCommit and buildDate are informational build metadata stamped via
// -ldflags so that otherwise-identical development builds (all reporting
// the floating "develop"/"latest" version) can be told apart. Empty for
// a plain `go build` with no ldflags.
var gitCommit string = ""
var buildDate string = ""

// Main entrypoint for execution of Educates CLI.

func main() {
	// All the functions for setting up commands are implemented as receiver
	// functions on ProjectInfo object so they can have access to compiled in
	// default values such as the release version of Educates.
	log := logr.New(logger.NullLogSink{}) //zap.New(zap.UseDevMode(false))
	logf.SetLogger(log)                   // This one is used in the reconciler code
	klog.SetLogger(log)                   // This one is used in the k8s client-go code

	p := cmd.NewProjectInfo(strings.TrimSpace(projectVersion), strings.TrimSpace(imageRepository), strings.TrimSpace(gitCommit), strings.TrimSpace(buildDate))

	c := p.NewEducatesCmdGroup()

	// An unknown command of a command group fails, as one of the root
	// does, rather than showing the group's help.

	if cmd.RejectUnknownCommand(c, os.Args[1:]) {
		os.Exit(1)
	}

	// Execute the actual command with arguments sourced from os.Args.

	err := c.Execute()

	if err != nil {
		os.Exit(1)
	}
}
