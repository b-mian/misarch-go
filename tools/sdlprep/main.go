// Command sdlprep converts a canonical MiSArch subgraph SDL (a full printed
// federation schema, as stored in the MiSArch/schemas repo) into a gqlgen
// input schema: it strips the federation machinery that gqlgen injects itself
// (directive definitions, link__*/FieldSet/_Any/_Entity/_Service, the
// _entities/_service query fields and the schema block) and prepends the
// federation v2.5 @link extension. The public contract is unchanged — gqlgen
// regenerates exactly the machinery that was stripped.
//
// Usage: sdlprep <canonical.graphql> <output.graphql>
package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/formatter"
	"github.com/vektah/gqlparser/v2/parser"
)

var dropDefinitions = map[string]bool{
	"link__Import":  true,
	"link__Purpose": true,
	"FieldSet":      true,
	"_Any":          true,
	"_Entity":       true,
	"_Service":      true,
}

var dropDirectiveDefs = map[string]bool{
	"link":             true,
	"key":              true,
	"shareable":        true,
	"inaccessible":     true,
	"external":         true,
	"provides":         true,
	"requires":         true,
	"override":         true,
	"composeDirective": true,
	"interfaceObject":  true,
	"extends":          true,
	"tag":              true,
}

var dropQueryFields = map[string]bool{
	"_entities": true,
	"_service":  true,
}

const header = `extend schema @link(url: "https://specs.apollo.dev/federation/v2.5", import: ["@key", "@shareable", "@inaccessible"])

`

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: sdlprep <canonical.graphql> <output.graphql>")
		os.Exit(2)
	}
	src, err := os.ReadFile(os.Args[1])
	if err != nil {
		fatal(err)
	}
	doc, err := parser.ParseSchema(&ast.Source{Name: os.Args[1], Input: string(src)})
	if err != nil {
		fatal(fmt.Errorf("parse %s: %w", os.Args[1], err))
	}

	doc.Schema = nil          // drop `schema @link(...) { ... }`
	doc.SchemaExtension = nil // drop `extend schema ...` (re-added via header)

	var defs ast.DefinitionList
	for _, def := range doc.Definitions {
		if dropDefinitions[def.Name] {
			continue
		}
		if def.Name == "Query" {
			var fields ast.FieldList
			for _, f := range def.Fields {
				if !dropQueryFields[f.Name] {
					fields = append(fields, f)
				}
			}
			def.Fields = fields
		}
		defs = append(defs, def)
	}
	doc.Definitions = defs

	var dirs ast.DirectiveDefinitionList
	for _, d := range doc.Directives {
		if !dropDirectiveDefs[d.Name] {
			dirs = append(dirs, d)
		}
	}
	doc.Directives = dirs

	var buf bytes.Buffer
	buf.WriteString(header)
	formatter.NewFormatter(&buf).FormatSchemaDocument(doc)

	if err := os.WriteFile(os.Args[2], buf.Bytes(), 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "sdlprep:", err)
	os.Exit(1)
}
