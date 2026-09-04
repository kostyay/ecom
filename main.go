// Command ecom provides machine-readable e-commerce utilities.
package main

import (
	"context"
	"os"

	"github.com/kostyay/ecom/internal/cli"
	_ "github.com/kostyay/ecom/providers/bikediscount"
	_ "github.com/kostyay/ecom/providers/wallapop"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
