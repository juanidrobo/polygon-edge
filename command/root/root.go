package root

import (
	"fmt"
	"github.com/juanidrobo/polygon-edge/command/backup"
	"github.com/juanidrobo/polygon-edge/command/genesis"
	"github.com/juanidrobo/polygon-edge/command/helper"
	"github.com/juanidrobo/polygon-edge/command/ibft"
	"github.com/juanidrobo/polygon-edge/command/license"
	"github.com/juanidrobo/polygon-edge/command/loadbot"
	"github.com/juanidrobo/polygon-edge/command/monitor"
	"github.com/juanidrobo/polygon-edge/command/peers"
	"github.com/juanidrobo/polygon-edge/command/secrets"
	"github.com/juanidrobo/polygon-edge/command/server"
	"github.com/juanidrobo/polygon-edge/command/status"
	"github.com/juanidrobo/polygon-edge/command/txpool"
	"github.com/juanidrobo/polygon-edge/command/version"
	"github.com/spf13/cobra"
	"os"
)

type RootCommand struct {
	baseCmd *cobra.Command
}

func NewRootCommand() *RootCommand {
	rootCommand := &RootCommand{
		baseCmd: &cobra.Command{
			Short: "Polygon Edge is a framework for building Ethereum-compatible Blockchain networks",
		},
	}

	helper.RegisterJSONOutputFlag(rootCommand.baseCmd)

	rootCommand.registerSubCommands()

	return rootCommand
}

func (rc *RootCommand) registerSubCommands() {
	rc.baseCmd.AddCommand(
		version.GetCommand(),
		txpool.GetCommand(),
		status.GetCommand(),
		secrets.GetCommand(),
		peers.GetCommand(),
		monitor.GetCommand(),
		loadbot.GetCommand(),
		ibft.GetCommand(),
		backup.GetCommand(),
		genesis.GetCommand(),
		server.GetCommand(),
		license.GetCommand(),
	)
}

func (rc *RootCommand) Execute() {
	if err := rc.baseCmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)

		os.Exit(1)
	}
}
