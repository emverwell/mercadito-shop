package envdoc

import (
	"strings"
	"testing"

	"github.com/emverwell/mercadito-shop/internal/config"
)

func TestTableMergesIdenticalRows(t *testing.T) {
	vars := []config.Var{
		{Service: "Catalog", Name: "POD_NAME", Default: "local", Doc: "Pod name."},
		{Service: "Cart", Name: "POD_NAME", Default: "local", Doc: "Pod name."},
		{Service: "Catalog", Name: "PORT", Default: "8081", Doc: "Listen port."},
		{Service: "Cart", Name: "PORT", Default: "8082", Doc: "Listen port."},
		{Service: "Cart", Name: "OPT", Default: "", Doc: "Optional."},
	}
	got := Table(vars)
	if strings.Count(got, "`POD_NAME`") != 1 || !strings.Contains(got, "catalog, cart") {
		t.Fatalf("POD_NAME rows not merged:\n%s", got)
	}
	if strings.Count(got, "`PORT`") != 2 {
		t.Fatalf("PORT differs per service, want 2 rows:\n%s", got)
	}
	if !strings.Contains(got, "| `OPT` | cart | _(unset)_ | Optional. |") {
		t.Fatalf("empty default not rendered as unset:\n%s", got)
	}
}

func TestSpliceReplacesBetweenMarkers(t *testing.T) {
	in := "intro\n<!-- env:start -->\nold\n<!-- env:end -->\noutro\n"
	out, err := Splice(in, "NEW\n")
	if err != nil || out != "intro\n<!-- env:start -->\nNEW\n<!-- env:end -->\noutro\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestSpliceMissingMarkersErrors(t *testing.T) {
	if _, err := Splice("no markers", "x"); err == nil {
		t.Fatal("want error")
	}
	if _, err := Splice("<!-- env:end -->\n<!-- env:start -->", "x"); err == nil {
		t.Fatal("want error for reversed markers")
	}
}
