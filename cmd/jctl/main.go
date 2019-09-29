package main

import (
	"fmt"
	"os"

	"github.com/toshi0607/jctl/pkg/cli"
)

const version = "v0.1.0"

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
