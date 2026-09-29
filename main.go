/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package main

import (
	"sync"

	"github.com/cjairm/devgeta/cmd"
	"github.com/cjairm/devgeta/internal/embedded"
)

func main() {
	// Set the default extractor function for devgeta app
	embedded.DefaultExtractor = ExtractEmbeddedConfigs
	// Hashed on first use only: just configure and install ask for it, so
	// hooks and `dg task` calls never pay for reading 3 MB of configs.
	embedded.ContentStamp = sync.OnceValue(configsContentHash)

	cmd.Execute()
}
