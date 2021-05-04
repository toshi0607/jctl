package main

import (
	"fmt"
	"os"

	"github.com/toshi0607/jctl/pkg/cli"
)

const version = "v0.1.0"

// These variables are set in build step
var (
	Version  = "unset" //nolint:deadcode,unused
	Revision = "unset" //nolint:deadcode,unused
)

func main() {
	defer func() {
		if err := recover(); err != nil {
			fmt.Fprintf(os.Stderr, "Error:\n%s\n", err)
			os.Exit(1)
		}
	}()
	cli := cli.New(os.Stdout, os.Stderr, version)
	os.Exit(cli.Run())
}
