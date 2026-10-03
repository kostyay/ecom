// Command ecom provides machine-readable e-commerce utilities.
package main

import (
	"context"
	"os"

	"github.com/kostyay/ecom/internal/cli"
	_ "github.com/kostyay/ecom/providers/bike24"
	_ "github.com/kostyay/ecom/providers/bikediscount"
	_ "github.com/kostyay/ecom/providers/buscocotxe"
	_ "github.com/kostyay/ecom/providers/canyon"
	_ "github.com/kostyay/ecom/providers/propain"
	_ "github.com/kostyay/ecom/providers/tradeinn"
	_ "github.com/kostyay/ecom/providers/wallapop"
	_ "github.com/kostyay/ecom/providers/ytindustries"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
