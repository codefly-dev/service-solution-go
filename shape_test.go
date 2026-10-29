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
	"gopkg.in/yaml.v3"
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
//
// A FENCED BLOCK IN A MARKDOWN TEMPLATE IS AN EXAMPLE, not a declaration:
// nothing reads Markdown as a manifest, and GETTING_STARTED.md deliberately
// shows the block an author adds by hand when the service was created outside
// `add solution`. The fence is what marks it as prose, so fenced blocks come
// out before matching and everything else — prose outside a fence, and every
// line of a template that is not Markdown — is still held to the rule.
// TestTheExampleManifestIsTheHostsDoorOnly reads that example structurally.
func TestNoTemplateDeclaresWhatTheServiceManifestDeclares(t *testing.T) {
	declarations := []*regexp.Regexp{
		regexp.MustCompile(`(?m)^module-identity:`),
		regexp.MustCompile(`(?m)^service-dependencies:`),
		regexp.MustCompile(`(?m)^endpoints:`),
		regexp.MustCompile(`(?m)^workspace-configuration-dependencies:`),
	}
	for name, content := range templateText(t) {
		prose := withoutFencedExamples(name, content)
		for _, declaration := range declarations {
			require.Falsef(t, declaration.MatchString(prose),
				"%s declares %s, which `codefly add solution` writes after this Create returns", name, declaration)
		}
	}
}

// withoutFencedExamples is a Markdown template with its fenced code blocks
// removed. A template that is not Markdown is returned whole: a YAML template
// has no fences and no examples, and every line of it is a declaration.
func withoutFencedExamples(name, content string) string {
	if !strings.HasSuffix(name, ".md.tmpl") {
		return content
	}
	var prose []string
	fenced := false
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if !fenced {
			prose = append(prose, line)
		}
	}
	return strings.Join(prose, "\n")
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
	for name, content := range templateText(t) {
		if !strings.Contains(content, "service-dependencies") {
			continue
		}
		require.Containsf(t, mayMentionModuleEdges, name,
			"%s mentions service-dependencies; only the example an author copies and the scaffolded refusal of an edge may", name)
	}
	// Both are reached, so neither entry can rot into a stale exemption for a
	// template that no longer says anything about an edge.
	for name := range mayMentionModuleEdges {
		require.Containsf(t, templateText(t)[name], "service-dependencies",
			"%s is exempt from the module-edge rule but no longer mentions one: drop the exemption", name)
	}
}

// mayMentionModuleEdges are the two templates that may say
// `service-dependencies`, and what each says. NEITHER DECLARES ONE.
//
//   - GETTING_STARTED.md shows the block inside a fenced example, for a
//     service created outside `add solution`.
//     TestTheExampleManifestIsTheHostsDoorOnly parses it and holds it to the
//     host's door.
//   - consumes_test.go READS the service manifest in order to refuse an edge:
//     it is this very rule, enforced inside the scaffolded solution's own
//     suite, so the refusal lands in `go test` where whoever added the edge is
//     standing rather than only at the consumer repository's gate. The string
//     is a YAML struct tag and the prose explaining it.
var mayMentionModuleEdges = map[string]string{
	"templates/factory/GETTING_STARTED.md.tmpl": "the example an author copies",
	"templates/factory/consumes_test.go.tmpl":   "the scaffolded refusal of an edge",
}

// TestTheExampleManifestIsTheHostsDoorOnly reads the YAML example in
// GETTING_STARTED.md STRUCTURALLY, as obin-ai/core-solutions'
// check_solution_mesh.py reads a real manifest and for the same reason: four
// spellings of one object defeat a pattern scan, and all four parse to the
// same value. An earlier line-regex here read the example's `- name: grpc` —
// an endpoint of the hop — as a module edge, and would have read a real edge
// spelled as a flow mapping as nothing at all.
//
// The example is what an author copies when the service was added with
// `codefly add service` rather than `codefly add solution`, so an edge shown
// here becomes an edge in a real manifest. The only ones it may show are the
// host's own door: the authority interface required by `interface:` alone, and
// `authority/hop` as `kind: external`.
func TestTheExampleManifestIsTheHostsDoorOnly(t *testing.T) {
	assertHostsDoorOnly(t, fencedYAML(t, templateText(t)["templates/factory/GETTING_STARTED.md.tmpl"]))
}

// assertHostsDoorOnly reads a solution backend's manifest STRUCTURALLY and
// holds it to the host's own door, as obin-ai/core-solutions'
// check_solution_mesh.py reads a real manifest and for the same reason: four
// spellings of one object defeat a pattern scan, and all four parse to the
// same value. An earlier line-regex here read `- name: grpc` — an endpoint of
// the hop — as a module edge, and would have read a real edge spelled as a
// flow mapping as nothing at all.
//
// Two manifests are held to this, and neither is written by this agent:
// GETTING_STARTED.md's fenced example, which an author copies when the service
// was added with `codefly add service` rather than `codefly add solution`, and
// base/service.codefly.yaml, which base/'s own suite reads to refuse an edge.
// Both must be what the CLI writes, or what they teach and what they check is
// a manifest no solution has.
func assertHostsDoorOnly(t *testing.T, document string) {
	t.Helper()
	var declared struct {
		ModuleIdentity bool `yaml:"module-identity"`
		Dependencies   []struct {
			Interface string `yaml:"interface"`
			Name      string `yaml:"name"`
			Module    string `yaml:"module"`
			Kind      string `yaml:"kind"`
		} `yaml:"service-dependencies"`
		Endpoints []struct {
			Name   string `yaml:"name"`
			Health struct {
				Readiness struct {
					Path string `yaml:"path"`
				} `yaml:"readiness"`
			} `yaml:"health"`
		} `yaml:"endpoints"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(document), &declared), "the manifest must be readable YAML")

	require.True(t, declared.ModuleIdentity,
		"a solution backend presents the solution's identity: the authority takes its workload credential as the actor of every Work Context it issues")
	require.Len(t, declared.Dependencies, 2,
		"the host's door is the authority interface and its hop, and nothing else")

	// The interface, required by `interface:` alone so the workspace binds it
	// to the one authority in scope. check_solution_mesh reads an
	// interface-only entry as the door; one that also names a service is an
	// ordinary module edge, and the hop is a door ONLY beside this entry.
	authority := declared.Dependencies[0]
	require.Equal(t, "codefly.dev/authority", strings.Split(authority.Interface, "@")[0])
	require.Empty(t, authority.Name, "the interface requirement names no service")
	require.Empty(t, authority.Module, "the interface requirement names no module")

	// The authority's hop. `external`: composed by the workspace that composes
	// this solution, so it consumes no endpoint and derives no mesh allow rule
	// — it names the edge and opens none.
	hop := declared.Dependencies[1]
	require.Equal(t, "authority", hop.Module)
	require.Equal(t, "hop", hop.Name)
	require.Equal(t, "external", hop.Kind,
		"a hop edge that is not external derives a mesh allow rule to the hop's service")

	// The one endpoint, with the readiness probe the scaffolded backend answers.
	require.Len(t, declared.Endpoints, 1, "a solution backend exposes one endpoint")
	require.Equal(t, "http", declared.Endpoints[0].Name)
	require.Equal(t, "/health", declared.Endpoints[0].Health.Readiness.Path)
}

// templateActions stand where a value goes, so each becomes a scalar before
// the YAML is parsed: `{{ … }}` opens a YAML flow mapping, and that is the
// example's rendering, not its shape.
var templateActions = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// fencedYAML is the one fenced yaml block in a Markdown template.
func fencedYAML(t *testing.T, content string) string {
	t.Helper()
	require.NotEmpty(t, content)
	_, after, found := strings.Cut(content, "```yaml\n")
	require.True(t, found, "the template shows no fenced yaml example")
	body, _, closed := strings.Cut(after, "\n```")
	require.True(t, closed, "the fenced yaml example is never closed")
	return templateActions.ReplaceAllString(body, "example")
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
