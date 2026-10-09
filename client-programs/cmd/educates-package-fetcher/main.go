/*
Fetches extension packages declared as images into a workshop session.

This runs inside the workshop container, where it replaces the vendir fetch
for a package declared as an image. It resolves a package image index to the
platform the session runs on, so an author writes one reference whatever the
architecture, and it preserves the file modes the package image contract
normalises at publish, which a vendir download does not.
*/
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/educates/educates-training-platform/client-programs/pkg/packages"
)

const (
	defaultConfigPath  = "/opt/eduk8s/config/packages.yaml"
	defaultSecretsPath = "/opt/secrets"
)

func main() {
	configPath := flag.String("config", defaultConfigPath, "path to the package fetch configuration")
	secretsPath := flag.String("secrets", defaultSecretsPath, "directory holding registry credentials")
	architecture := flag.String("architecture", "", "architecture to fetch, defaulting to the one this binary was built for")
	insecure := flag.Bool("insecure", false, "allow plain HTTP when contacting a registry")

	flag.Parse()

	if err := run(*configPath, *secretsPath, *architecture, *insecure); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(configPath string, secretsPath string, architecture string, insecure bool) error {
	// A session with no package images has no configuration, which is not a
	// failure: there is simply nothing to fetch.
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil
	}

	config, err := packages.LoadFetchConfig(configPath)

	if err != nil {
		return err
	}

	if len(config.Packages) == 0 {
		return nil
	}

	if architecture == "" {
		architecture = runtime.GOARCH
	}

	platform, err := packages.ParsePlatform("linux/" + architecture)

	if err != nil {
		return err
	}

	keychain, err := packages.LoadKeychain(secretsPath)

	if err != nil {
		return err
	}

	options := packages.FetchOptions{
		Platform: platform,
		Keychain: keychain,
		Insecure: insecure,
	}

	for _, entry := range config.Packages {
		if err := packages.Fetch(entry, options, os.Stdout); err != nil {
			return err
		}
	}

	return nil
}
