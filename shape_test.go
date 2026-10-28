package main

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/codefly-dev/core/composition"
	modulecontract "github.com/codefly-dev/core/contracts/module"
	"github.com/codefly-dev/core/solution/manifest"
	"github.com/stretchr/testify/require"
)

// THE LINE BETWEEN THIS AGENT AND THE CLI, held by tests rather than by a
// comment.
//
// `codefly add solution` (codefly-dev/cli#1216) writes the solution's
// declarations — the five documents, module-identity, the endpoints, and the
// only service-dependencies a solution backend has — and it writes them AFTER
// this agent's Create returns. Anything this agent wrote in those places would
// be overwritten silently, by landing first, and a reader would have two
// sources for one statement.
//
// These tests read the TEMPLATES, not a scaffolded tree, so a line added to a
// template fails here whether or not any test happens to scaffold it.

// templateText is every factory and page template, by its path inside the
// embedded tree.
func templateText(t *testing.T) map[string]string {
	t.Helper()
	all := map[string]string{}
	for _, tree := range []struct {
		files fs.FS
		root  string
	}{{factoryFS, "templates/factory"}, {pageFS, "templates/page"}} {
		require.NoError(t, fs.WalkDir(tree.files, tree.root, func(name string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			content, readErr := fs.ReadFile(tree.files, name)
			if readErr != nil {
				return readErr
			}
			all[name] = string(content)
			return nil
		}))
	}
	require.NotEmpty(t, all)
	return all
}

// TestNoTemplateIsOneOfTheSolutionsDocuments. The five documents a solution is
// installed from are the CLI's: solution.codefly.yaml,
// module.contract.codefly.yaml, module.package.codefly.yaml,
// solution.frontend.codefly.yaml and the module manifest. This agent reads the
// first one and embeds the fourth; it writes none of them.
func TestNoTemplateIsOneOfTheSolutionsDocuments(t *testing.T) {
	owned := []string{
		manifest.FileName,
		modulecontract.FileName,
		composition.PackageManifestFileName,
		"solution.frontend.codefly.yaml",
		"module.codefly.yaml",
	}
	for name := range templateText(t) {
		rendered := strings.TrimSuffix(path.Base(name), ".tmpl")
		for _, document := range owned {
			require.NotEqualf(t, document, rendered,
				"%s would write %s, which `codefly add solution` owns and writes after this Create returns", name, document)
		}
	}
}

// TestNoTemplateDeclaresWhatTheServiceManifestDeclares. A solution backend's
// manifest carries module-identity, one endpoint with the /health readiness
// probe, and exactly two service-dependencies (the authority interface and its
// hop). All of it is the CLI's, so no template may state any of it — not even
// as a value that happens to agree today.
func TestNoTemplateDeclaresWhatTheServiceManifestDeclares(t *testing.T) {
	// Matched at the start of a line, so prose and a commented example in
	// GETTING_STARTED.md — which deliberately shows the block an author adds
	// by hand when the service was created outside `add solution` — is not a
	// declaration. A template that is itself a manifest would declare them at
	// column zero.
	declarations := []*regexp.Regexp{
		regexp.MustCompile(`(?m)^module-identity:`),
		regexp.MustCompile(`(?m)^service-dependencies:`),
		regexp.MustCompile(`(?m)^endpoints:`),
		regexp.MustCompile(`(?m)^workspace-configuration-dependencies:`),
	}
	for name, content := range templateText(t) {
		for _, declaration := range declarations {
			require.Falsef(t, declaration.MatchString(content),
				"%s declares %s, which `codefly add solution` writes after this Create returns", name, declaration)
		}
	}
}

// TestNoTemplateNamesAModuleEdge is SP-SOL-04 at the source. A solution
// reaches every module through the authority's hop, which CHECKS the call;
// Codefly renders a `service-dependencies` edge into a mesh allow rule
// straight to that module's service, past the check, and
// obin-ai/core-solutions' check-solution-mesh gate refuses it.
//
// The scaffolded consumes_test.go holds a generated solution to the same rule
// in its own suite. This holds the agent to never writing one.
func TestNoTemplateNamesAModuleEdge(t *testing.T) {
	edge := regexp.MustCompile(`(?m)^\s*-\s+name:\s+\S+\s*$`)
	for name, content := range templateText(t) {
		if !strings.Contains(content, "service-dependencies") {
			continue
		}
		// GETTING_STARTED.md is the one template that shows the block, inside
		// a fenced example for a service created outside `add solution`. It
		// may show the host's own door and nothing else.
		require.Equal(t, "templates/factory/GETTING_STARTED.md.tmpl", name,
			"%s mentions service-dependencies; only GETTING_STARTED may, as the example an author copies", name)
		for _, line := range edge.FindAllString(content, -1) {
			require.Containsf(t, line, "hop",
				"%s shows a service-dependency on something other than the authority's hop: %q", name, strings.TrimSpace(line))
		}
	}
}

// TestNoTemplateCarriesProductContent. This agent is published from
// codefly-dev and scaffolds a solution for anyone. The template it was modelled
// on (obin-ai/core-solutions' templates/solution) depends on that
// repository's own vendored kits and shared packages by relative path, which
// is exactly why it could not leave the repository. None of it comes along.
func TestNoTemplateCarriesProductContent(t *testing.T) {
	for name, content := range templateText(t) {
		require.NotContainsf(t, content, "@obin-ai/", "%s depends on a product package", name)
		require.NotContainsf(t, content, "obin-ai/", "%s names a product repository as a dependency", name)
		require.NotContainsf(t, content, `"file:`, "%s depends on a vendored archive by path, which only resolves inside one repository", name)
		require.NotContainsf(t, content, "../../../", "%s reaches outside the solution it scaffolds", name)
	}
}

// TestThePageSharesEveryHostSingleton. Each entry in the Module Federation
// shared policy must be the HOST's single instance, or its React context and
// its skin tokens do not line up across the federation boundary: the kit
// renders unstyled, stale, or — for react-dom/client — not at all (React error
// #527, a page that never mounts).
//
// A missing entry is invisible until a browser loads the remote inside a real
// host, so it is pinned here.
func TestThePageSharesEveryHostSingleton(t *testing.T) {
	config := templateText(t)["templates/page/rsbuild.config.ts.tmpl"]
	require.NotEmpty(t, config)
	for _, shared := range []string{
		"react", "react/jsx-runtime", "react-dom", "react-dom/client",
		"@codefly-dev/ui", "@codefly-dev/saas-ui", "@codefly-dev/saas-sdk",
		"@codefly-dev/saas-plugin-contract", "@codefly-dev/saas-plugin-contract/capabilities",
		"@codefly-dev/saas-plugin-react", "@codefly-dev/saas-plugin-react/runtime",
		"@codefly-dev/saas-plugin-react/ui",
	} {
		require.Containsf(t, config, `"`+shared+`"`, "the shared policy does not seal %s to the host's instance", shared)
	}
	// "auto" lets the container resolve its chunk URLs relative to wherever its
	// remoteEntry was loaded from, so one build is servable from any origin.
	require.Contains(t, config, `process.env.ASSET_PREFIX ?? "auto"`)
	// A host's CSP refuses a cross-origin stylesheet, so the remote's CSS must
	// be inlined or the page fails to mount.
	require.Contains(t, config, "injectStyles: true")
}
