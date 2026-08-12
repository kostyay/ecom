// Command ecom provides machine-readable e-commerce utilities.
package main

import (
	"context"
	"os"

	"github.com/kostyay/ecom/internal/cli"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
