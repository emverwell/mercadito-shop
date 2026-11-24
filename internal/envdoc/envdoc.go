// Package envdoc renders the environment-variable table in the README from
// the services' config struct tags, so docs and code cannot drift.
package envdoc

import (
	"fmt"
	"strings"

	"github.com/emverwell/mercadito-shop/internal/config"
)

const (
	startMarker = "<!-- env:start -->"
	endMarker   = "<!-- env:end -->"
)

// Table renders a Markdown table. Rows with identical name, default and
// description are merged and list every service that uses them.
func Table(vars []config.Var) string {
	type row struct {
		name, def, doc string
		services       []string
	}
	var rows []*row
	index := map[string]*row{}
	for _, v := range vars {
		k := v.Name + "\x00" + v.Default + "\x00" + v.Doc
		r, ok := index[k]
		if !ok {
			r = &row{name: v.Name, def: v.Default, doc: v.Doc}
			index[k] = r
			rows = append(rows, r)
		}
		r.services = append(r.services, strings.ToLower(v.Service))
	}
	var b strings.Builder
	b.WriteString("| Variable | Services | Default | Description |\n|---|---|---|---|\n")
	for _, r := range rows {
		def := "`" + r.def + "`"
		if r.def == "" {
			def = "_(unset)_"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", r.name, strings.Join(r.services, ", "), def, r.doc)
	}
	return b.String()
}

// Splice replaces the text between the env markers with table.
func Splice(readme, table string) (string, error) {
	s := strings.Index(readme, startMarker)
	e := strings.Index(readme, endMarker)
	if s < 0 || e < 0 || e < s {
		return "", fmt.Errorf("README must contain %s then %s", startMarker, endMarker)
	}
	return readme[:s+len(startMarker)] + "\n" + table + readme[e:], nil
}
