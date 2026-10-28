package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codefly-dev/core/solution/manifest"
)

// CONSUMPTION IS DECLARED ONCE.
//
// `api.consumes` in solution.codefly.yaml is the one place a solution says
// which modules it may reach: the CLI derives the delivered bindings and the
// solution SDK from it, and core-solutions' check-consumed-apis gate reads it.
// A second list in Go would be a second declaration, and the two have drifted
// in every solution that kept both by hand.
//
// So the Go half is GENERATED from the manifest, as `as` names only —
// consumed_gen.go — and the scaffolded consumes.go derives its declaration
// from that list rather than naming a module itself. The two cannot disagree
// about WHICH modules are consumed, by construction; what consumes.go adds is
// each module's scopes and methods, which no manifest can carry (a method name
// is a constant of the module's generated client).
//
// The generated file is rewritten by `codefly sync`, and the scaffolded
// consumes_test.go fails when the committed file no longer says what the
// manifest does.

// generatedConsumptionFile is the file this agent owns inside a solution
// backend. Nothing else in the service directory is agent-owned: everything
// else the Create flow writes is the author's from the moment it lands.
const generatedConsumptionFile = "consumed_gen.go"

// solutionManifestFrom locates the solution manifest for a backend at
// serviceDir. The manifest is the MODULE's, one level above services/ — where
// `codefly add solution` writes it and where core-solutions' gates read it —
// so a backend at <module>/services/backend finds it at <module>/.
func solutionManifestFrom(serviceDir string) string {
	return filepath.Join(serviceDir, "..", "..", manifest.FileName)
}

// consumedAliases reads the `as` of every api.consumes entry, in manifest
// order, from the solution manifest beside the module at serviceDir.
//
// A MISSING MANIFEST IS NOT AN ERROR, and that is deliberate. `codefly add
// solution` scaffolds the services before it writes the documents (cmd/add/
// solution.go), so at Create time there is no manifest yet and the honest
// answer is "this solution consumes nothing so far". The author's `codefly
// sync` — or the first one CI runs — picks the manifest up. A manifest that
// exists and cannot be read IS an error: silently reading an unreadable
// manifest as "consumes nothing" is how a declaration disappears.
func consumedAliases(serviceDir string) ([]string, error) {
	path := solutionManifestFrom(serviceDir)
	content, err := os.ReadFile(path) //nolint:gosec // the service's own module directory
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	// core's own consumption reader, the one every render, sync and authority
	// derivation uses. A manifest this refuses is one the platform refuses.
	consumption, err := manifest.LoadConsumption(content)
	if err != nil {
		return nil, fmt.Errorf("%s declares a consumption core cannot read: %w", path, err)
	}
	aliases := make([]string, 0, len(consumption.API.Consumes))
	for _, consumed := range consumption.API.Consumes {
		alias := strings.TrimSpace(consumed.As)
		if alias == "" {
			alias = strings.TrimSpace(consumed.ID)
		}
		if alias == "" {
			return nil, fmt.Errorf("%s: an api.consumes entry names neither `as` nor `id`, so the solution has no name to address it by", path)
		}
		aliases = append(aliases, alias)
	}
	return aliases, nil
}

// renderConsumption is consumed_gen.go for aliases. It is emitted already
// gofmt-formatted rather than piped through a formatter: the scaffolded drift
// check compares this output against the committed file, and a check whose
// answer depends on whether a formatter is on PATH is not a check.
func renderConsumption(aliases []string) []byte {
	var out bytes.Buffer
	out.WriteString(`// Code generated from ` + manifest.FileName + ` by the codefly.dev/solution-go
// agent. DO NOT EDIT.
//
// api.consumes is the one place this solution says which modules it may reach.
// These names are that list, so consumes.go cannot name a module the manifest
// does not — nor miss one it does. Regenerate it with ` + "`codefly sync`" + `;
// consumes_test.go fails while this file and the manifest disagree.

package main

`)
	if len(aliases) == 0 {
		out.WriteString("// This solution consumes no module yet. Declare one under api.consumes in " + manifest.FileName + ",\n")
		out.WriteString("// then run `codefly sync`.\nvar consumedAsAll = []string{}\n")
		return out.Bytes()
	}

	width := 0
	for _, alias := range aliases {
		if field := aliasField(alias); len(field) > width {
			width = len(field)
		}
	}
	out.WriteString("// consumedAs names each consumed module, so consumes.go spells no `as` by hand.\nvar consumedAs = struct {\n")
	for _, alias := range aliases {
		fmt.Fprintf(&out, "\t%-*s string\n", width, aliasField(alias))
	}
	out.WriteString("}{\n")
	for _, alias := range aliases {
		fmt.Fprintf(&out, "\t%-*s %q,\n", width+1, aliasField(alias)+":", alias)
	}
	out.WriteString("}\n\n// consumedAsAll is the same list, in manifest order.\nvar consumedAsAll = []string{")
	for index, alias := range aliases {
		if index > 0 {
			out.WriteString(", ")
		}
		fmt.Fprintf(&out, "%q", alias)
	}
	out.WriteString("}\n")
	return out.Bytes()
}

// aliasField is the exported Go field name for one `as`. An `as` is a route
// segment (core's manifest validates it as a lowercase id with `.`, `_` or `-`
// separators), so each separated word is capitalised: "document-store" becomes
// DocumentStore.
func aliasField(alias string) string {
	var field strings.Builder
	for _, word := range strings.FieldsFunc(alias, func(r rune) bool { return r == '-' || r == '_' || r == '.' }) {
		field.WriteString(strings.ToUpper(word[:1]))
		field.WriteString(word[1:])
	}
	return field.String()
}
