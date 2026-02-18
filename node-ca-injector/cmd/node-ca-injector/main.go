package main

import (
	"fmt"
	"os"

	"github.com/educates/educates-training-platform/node-ca-injector/internal/controller"
	"github.com/educates/educates-training-platform/node-ca-injector/internal/sync"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: node-ca-injector <controller|sync> [flags]\n")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "controller":
		if err := controller.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "controller error: %v\n", err)
			os.Exit(1)
		}
	case "sync":
		if err := sync.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "sync error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\nUsage: node-ca-injector <controller|sync>\n", os.Args[1])
		os.Exit(1)
	}
}
