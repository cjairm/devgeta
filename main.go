/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package main

import (
	"github.com/cjairm/devgeta/cmd"
	"github.com/cjairm/devgeta/internal/embedded"
)

func main() {
	// Set the default extractor function for devgeta app
	embedded.DefaultExtractor = ExtractEmbeddedConfigs

	cmd.Execute()
}
