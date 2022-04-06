package main

import (
	_ "embed"
	"github.com/juanidrobo/polygon-edge/command/root"
	"github.com/juanidrobo/polygon-edge/licenses"
)

var (
	//go:embed LICENSE
	license string
)

func main() {
	licenses.SetLicense(license)

	root.NewRootCommand().Execute()
}
