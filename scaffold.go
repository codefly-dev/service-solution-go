package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/codefly-dev/core/agents/services"
	"github.com/codefly-dev/core/shared"
	solution "github.com/codefly-dev/solution-runtime-go"
)

// THE SHAPE THIS AGENT SCAFFOLDS, and the line between it and the CLI.
//
// A solution is one shape: obin-ai/core-solutions' `wiki`, the solution that
// has been running longest, independently confirmed by `proposals`. It is two
// Codefly services and a page:
//
//	backend    the entry service. This agent scaffolds it.
//	web        the shell that serves the page where no host shell mounts it.
//	           A Next.js service; the `nextjs` agent scaffolds it.
//	fe-remote  the ./Page Module Federation remote. An npm package, not a
//	           Codefly service, so no `add service` call reaches it — which is
//	           why this agent writes it, beside the backend, as part of the
//	           backend's own Create.
//
// WHAT THIS AGENT DOES NOT WRITE, and must not. `codefly add solution`
// (codefly-dev/cli#1216) owns the solution's declarations and writes them
// AFTER this Create returns:
//
//   - module-identity, the endpoints, and the only service-dependencies a
//     solution backend has (the authority interface, plus `authority/hop` as
//     `kind: external`);
//   - solution.codefly.yaml, module.contract.codefly.yaml,
//     module.package.codefly.yaml and solution.frontend.codefly.yaml.
//
// Writing any of them here would be a second declaration of something the CLI
// already states, and the CLI's would win — silently, by landing second.
//
// In particular NO service-dependency on a consumed module is ever written.
// A solution reaches every module through the authority's hop, which checks
// the call; Codefly renders a service-dependency into a mesh allow rule
// straight to that module's service, PAST the check, which is what
// core-solutions' check-solution-mesh gate refuses (SP-SOL-04). The
// scaffolded consumes_test.go holds the generated solution to that rule in its
// own test suite, so the refusal is local rather than only at the consumer
// repository's gate.

// scaffoldedService is the service directory this agent scaffolds, and the
// page directory it writes beside it. The names are the shape's, not a
// setting: `codefly add solution` names the same two.
const (
	backendService = "backend"
	pageDirectory  = "fe-remote"
)

// scaffold is the template context. It embeds core's own Information so a
// template may reach anything `codefly add service` knows, and adds the four
// values the solution shape derives from the module's name.
type scaffold struct {
	*services.Information

	// SolutionID is the solution's one name: the id the host's presence
	// document names it by, the alias the authority's edge routes it at
	// (/v1/<id>), the principal the authority issues to, and the Codefly
	// module name. It is the MODULE's name, never the service's: the service
	// is `backend` in every solution.
	SolutionID string
	// Title is a legible placeholder for the host's shell. The real one is
	// solution.frontend.codefly.yaml's, which `codefly add solution --title`
	// writes; this is what the page's own heading says until an author
	// changes it.
	Title string
	// Identifier is SolutionID as a JavaScript identifier. Module Federation
	// emits a remote's name as one, so a name that cannot become an
	// identifier fails inside a bundler — the last hand-scaffolded solution's
	// final friction item. newScaffold refuses it here instead.
	Identifier string
	// InterfaceDescription and OperationPrefix are the two values the
	// interface document carries that are derived rather than typed. They are
	// in the context, rather than only in the scaffolded openapi.go, because
	// this agent renders the FIRST copy of that document itself and the
	// scaffolded generator renders every later one: both must read the same
	// values or the first `go generate ./...` reports a spurious diff.
	InterfaceDescription string
	OperationPrefix      string
}

// solutionRuntimeVersion is the solution runtime a freshly scaffolded backend
// pins, and the one templates/factory/go.mod.tmpl requires.
//
// It is a RELEASE, never a pseudo-version: core-solutions'
// check-consumed-apis accepts v0.0.12 or later (or a pseudo-version at or
// after the native projected-token cutover), and a pseudo-version pins a
// commit that may never reach a tag. factory_test.go holds the template to
// this constant and to that floor.
const solutionRuntimeVersion = "v0.0.16"

// solutionRuntimePath is the module that pin names. It is the module a
// consumer's consumed-API gate looks for in a solution backend's go.mod.
const solutionRuntimePath = "github.com/codefly-dev/solution-runtime-go"

// jsIdentifier is what Module Federation can emit a remote's name as.
var jsIdentifier = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

// newScaffold derives the template context from what the agent was asked to
// create, refusing a module name the shape cannot carry.
func newScaffold(info *services.Information) (scaffold, error) {
	if info == nil || info.Module == nil || info.Service == nil {
		return scaffold{}, fmt.Errorf("the solution-go agent was asked to create a service with no module or service identity")
	}
	id := info.Module.Name.LowerCase
	if strings.TrimSpace(id) == "" {
		return scaffold{}, fmt.Errorf("the solution-go agent was asked to create a service in a module with no name")
	}
	identifier := strings.NewReplacer("-", "_", ".", "_").Replace(id)
	if !jsIdentifier.MatchString(identifier) {
		return scaffold{}, fmt.Errorf("module name %q does not yield a JavaScript identifier (%q), and Module Federation emits the ./Page remote's name as one: a solution needs a name of lowercase letters, digits and hyphens starting with a letter", id, identifier)
	}
	return scaffold{
		Information:          info,
		SolutionID:           id,
		Title:                titleFrom(id),
		Identifier:           identifier,
		InterfaceDescription: interfaceDescription(id),
		OperationPrefix:      aliasField(id),
	}, nil
}

// titleFrom is the placeholder title: the module name, hyphens read as spaces,
// each word capitalised. It is the product's name, so it is the author's to
// give — this only keeps the host's shell and the page's heading legible
// instead of showing the raw slug. It is derived exactly as `codefly add
// solution` derives its default --title, so the two agree when neither is
// given one.
func titleFrom(id string) string {
	words := strings.FieldsFunc(id, func(r rune) bool { return r == '-' || r == '_' })
	for index, word := range words {
		words[index] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

// scaffoldSolution writes the solution backend and its page.
//
// Every tree is written with the factory's own rule — an existing file is
// KEPT, never truncated — so re-running Create over a solution that already
// exists leaves the author's code alone.
func (s *Builder) scaffoldSolution(ctx context.Context, context scaffold) error {
	if err := s.Templates(ctx, context,
		services.WithFactory(s.factory).WithPathSelect(shared.NewIgnore("go.work*")),
	); err != nil {
		return fmt.Errorf("cannot write the solution backend: %w", err)
	}

	// The page, beside the backend rather than inside it. `codefly add
	// solution` scaffolds `backend` and `web` through their agents and
	// deliberately writes no ./Page remote, because a remote is an npm package
	// and no `add service` call reaches one — so a solution's page has been
	// hand-copied out of a sibling every time. It is written here, from the
	// backend's own Create, because the backend is the only part of a solution
	// that has an agent at all.
	page := filepath.Join(s.pageLocation())
	if err := s.Templates(ctx, context,
		services.WithTemplate(s.page, "page", "").
			WithDestination("%s", page).
			WithOverride(shared.SkipAll()),
	); err != nil {
		return fmt.Errorf("cannot write the solution's ./Page remote at %s: %w", page, err)
	}
	return nil
}

// pageLocation is where the ./Page remote lives: services/fe-remote, beside
// this backend. The backend's own location is services/<service>, so the
// remote is its sibling.
func (s *Builder) pageLocation() string {
	return filepath.Join(filepath.Dir(s.Location), pageDirectory)
}

// writeGeneratedConsumption renders consumed_gen.go from the solution
// manifest's api.consumes and writes it into the service directory. It is the
// one file in a scaffolded backend this agent OWNS: `codefly sync` rewrites
// it, and the scaffolded consumes_test.go fails while it and the manifest
// disagree.
func (s *Builder) writeGeneratedConsumption(ctx context.Context) error {
	aliases, err := consumedAliases(s.Location)
	if err != nil {
		return err
	}
	path := filepath.Join(s.Location, generatedConsumptionFile)
	if err := shared.WriteFileAtomic(ctx, path, renderConsumption(aliases), 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", generatedConsumptionFile, err)
	}
	return nil
}

// writeInterfaceArtifact renders the backend's interface document and writes
// it into the service directory.
//
// It is rendered by the solution runtime's own renderer, from a server built
// exactly as the scaffolded main.go builds one — so the bytes this agent
// writes are the bytes the scaffolded `go generate ./...` writes, and the
// scaffolded openapi_test.go passes on a tree nobody has run a generator in
// yet. That document is what `codefly generate contracts` publishes as the
// http endpoint's contract, and the CLI derives the route rows the authority's
// Check applies from it (codefly-dev/cli#1104), so a backend without one
// publishes no contract at all.
//
// The version is passed rather than read: the runtime reads it from
// service.codefly.yaml, which `CreateResponse` has not written yet when this
// runs.
func (s *Builder) writeInterfaceArtifact(ctx context.Context, context scaffold, version string) error {
	server := solution.New(solution.Manifest{ID: context.SolutionID, Contract: context.SolutionID})
	document, err := server.InterfaceArtifact("", solution.InterfaceInfo{
		Title:           context.SolutionID + " solution backend",
		Version:         version,
		Description:     context.InterfaceDescription,
		OperationPrefix: context.OperationPrefix,
		Tag:             context.SolutionID,
	})
	if err != nil {
		return fmt.Errorf("cannot render the backend's interface document: %w", err)
	}
	path := filepath.Join(s.Location, filepath.FromSlash(interfaceArtifactPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("cannot create %s: %w", filepath.Dir(path), err)
	}
	if err := shared.WriteFileAtomic(ctx, path, document, 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", interfaceArtifactPath, err)
	}
	return nil
}

// interfaceArtifactPath is where a solution backend commits its interface
// document, as wiki and proposals both do. The scaffolded openapi.go names the
// same path.
const interfaceArtifactPath = "openapi/api.swagger.json"

// interfaceDescription is the document's own description. It is written once
// here and rendered into the scaffolded openapi.go, so the agent's render and
// the backend's own render produce identical bytes.
func interfaceDescription(id string) string {
	return "The " + id + " solution's backend under the authority interface, at the route the authority's edge forwards it on: " +
		"the routes it serves itself, and the module calls the solution runtime relays as the viewer. " +
		"A relayed call is any checked caller's, because the child the authority issues through this solution's binding for that module, " +
		"and the module's own method policy, gate it."
}
