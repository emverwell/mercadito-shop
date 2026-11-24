// Command docs regenerates (or with -check, verifies) the environment table in the README.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/emverwell/mercadito-shop/internal/cart"
	"github.com/emverwell/mercadito-shop/internal/catalog"
	"github.com/emverwell/mercadito-shop/internal/checkout"
	"github.com/emverwell/mercadito-shop/internal/config"
	"github.com/emverwell/mercadito-shop/internal/envdoc"
	"github.com/emverwell/mercadito-shop/internal/payments"
	"github.com/emverwell/mercadito-shop/internal/storefront"
)

func main() {
	readme := flag.String("readme", "README.md", "README to update")
	check := flag.Bool("check", false, "fail if the README table is out of date instead of writing it")
	flag.Parse()

	vars := config.Docs(
		storefront.StorefrontConfig{}, catalog.CatalogConfig{}, cart.CartConfig{},
		checkout.CheckoutConfig{}, payments.PaymentsConfig{},
	)
	for i := range vars {
		if vars[i].Service == "Payments" {
			vars[i].Service = "Payments-mock" // type is PaymentsConfig; the service is payments-mock
		}
	}
	in, err := os.ReadFile(*readme) // #nosec G304 -- developer tool, path from flag
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	out, err := envdoc.Splice(string(in), envdoc.Table(vars))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if *check {
		if out != string(in) {
			fmt.Fprintln(os.Stderr, "README env table is stale: run `make docs`")
			os.Exit(1)
		}
		return
	}
	if err := os.WriteFile(*readme, []byte(out), 0o600); err != nil { // #nosec G306 G703 -- developer tool, path from flag
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
