package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefly-dev/core/agents/services"
	"github.com/codefly-dev/core/resources"
	"github.com/stretchr/testify/require"
)

// scaffoldInto drives everything Create writes, into a throwaway solution laid
// out the way `codefly add solution` lays one out:
//
//	<root>/solution.codefly.yaml        (only when manifest is non-empty)
//	<root>/services/backend             the backend this agent scaffolds
//	<root>/services/fe-remote           the ./Page remote it writes beside it
//
// It stops short of CreateResponse, which saves the service manifest through
// core and needs a loaded Builder: what is under test here is the tree.
func scaffoldInto(t *testing.T, module, manifest string) (root string) {
	t.Helper()
	root = t.TempDir()
	if manifest != "" {
		require.NoError(t, os.WriteFile(filepath.Join(root, "solution.codefly.yaml"), []byte(manifest), 0o600))
	}
	builder := builderFor(t, root, module)
	ctx := context.Background()
	context, err := newScaffold(builder.Information)
	require.NoError(t, err)
	require.NoError(t, builder.scaffoldSolution(ctx, context))
	require.NoError(t, builder.writeGeneratedConsumption(ctx))
	require.NoError(t, builder.writeInterfaceArtifact(ctx, context, "0.0.0"))
	return root
}

// builderFor is this agent loaded against <root>/services/backend, the way
// core loads it before any lifecycle call. It is shared by scaffoldInto and by
// syncAsTheCLIDoes, so what Create runs and what Sync runs are the same agent
// against the same tree.
func builderFor(t *testing.T, root, module string) *Builder {
	t.Helper()
	location := filepath.Join(root, "services", backendService)
	require.NoError(t, os.MkdirAll(location, 0o750))

	svc := NewService()
	identity := &resources.ServiceIdentity{Name: backendService, Module: module, Workspace: "workspace"}
	svc.Base.Location = location
	svc.Base.Identity = identity
	svc.Base.Information = &services.Information{
		Service: resources.ToServiceWithCase(identity),
		Module:  resources.ToModuleWithCase(identity),
		Agent:   agent,
	}
	return NewBuilder(svc)
}

// syncAsTheCLIDoes is `codefly sync service <module>/backend`: the agent's
// Sync, which re-renders consumed_gen.go from the solution manifest as it now
// stands. It is a distinct step from Create because the manifest does not
// exist when Create runs — see TestAScaffoldedSolutionBuildsAndPassesItsOwnGates.
func syncAsTheCLIDoes(t *testing.T, root, module string) {
	t.Helper()
	require.NoError(t, builderFor(t, root, module).writeGeneratedConsumption(context.Background()))
}

// emptyManifest is a solution manifest that consumes nothing — what `codefly
// add solution` writes with no --consumes.
const emptyManifest = `schema_version: codefly.solution-manifest/v0
protocol_version: codefly.solution/v0
api:
    exposes:
        - id: http
          protocol: http
lifecycle:
    package: true
    render: true
`

// consumingManifest declares two modules, as `codefly add solution --consumes`
// writes them: route names only, no scopes — authority is declared once, in
// the module contract.
const consumingManifest = `schema_version: codefly.solution-manifest/v0
protocol_version: codefly.solution/v0
api:
    exposes:
        - id: http
          protocol: http
    consumes:
        - id: documents
          protocol: connect
          module: documents
          service: documents
          endpoint: grpc
          as: documents
        - id: record-store
          protocol: connect
          module: records
          service: records
          endpoint: connect
          as: record-store
lifecycle:
    package: true
    render: true
`

func TestCreateWritesTheBackendAndThePageBesideIt(t *testing.T) {
	root := scaffoldInto(t, "notes", emptyManifest)
	backend := filepath.Join(root, "services", backendService)
	page := filepath.Join(root, "services", pageDirectory)

	for _, name := range []string{
		"main.go", "operations.go", "consumes.go", "consumes_test.go",
		generatedConsumptionFile, "openapi.go", "openapi_test.go",
		filepath.FromSlash(interfaceArtifactPath),
		"go.mod", "go.sum", "GETTING_STARTED.md", ".gitignore",
	} {
		require.FileExistsf(t, filepath.Join(backend, name), "the backend is missing %s", name)
	}
	// The ./Page remote: an npm package, beside the backend, which `codefly add
	// solution` deliberately does not write and no `add service` call reaches.
	for _, name := range []string{
		"package.json", "rsbuild.config.ts", "tsconfig.json", "vitest.config.ts",
		".npmrc", "README.md",
		filepath.Join("src", "Page.tsx"), filepath.Join("src", "Page.test.tsx"),
		filepath.Join("src", "binding.ts"), filepath.Join("src", "page.css"),
	} {
		require.FileExistsf(t, filepath.Join(page, name), "the ./Page remote is missing %s", name)
	}
}

// TestTheSolutionIdIsTheModuleName pins what a solution's one name is. It is
// the MODULE's, not the service's: the service is `backend` in every solution,
// and a backend that called itself `backend` to the host would be routed at
// /v1/backend and named that in every presence document.
func TestTheSolutionIdIsTheModuleName(t *testing.T) {
	root := scaffoldInto(t, "notes", emptyManifest)
	main := read(t, filepath.Join(root, "services", backendService, "main.go"))
	require.Contains(t, main, `const solutionID = "notes"`)

	page := read(t, filepath.Join(root, "services", pageDirectory, "package.json"))
	var declared struct {
		Name    string            `json:"name"`
		Exports map[string]string `json:"exports"`
	}
	require.NoError(t, json.Unmarshal([]byte(page), &declared), "the page's package.json must be valid JSON")
	require.Equal(t, "notes-fe-remote", declared.Name)
	require.Equal(t, "./src/Page.tsx", declared.Exports["./Page"], "a host federates ./Page")
}

// TestAModuleNameThatIsNoJavaScriptIdentifierIsRefused: Module Federation
// emits a remote's name as a JavaScript global, so a name that cannot become
// an identifier fails inside a bundler, long after the scaffold. It is refused
// here instead, by name.
func TestAModuleNameThatIsNoJavaScriptIdentifierIsRefused(t *testing.T) {
	identity := &resources.ServiceIdentity{Name: backendService, Module: "9lives", Workspace: "workspace"}
	_, err := newScaffold(&services.Information{
		Service: resources.ToServiceWithCase(identity),
		Module:  resources.ToModuleWithCase(identity),
		Agent:   agent,
	})
	require.ErrorContains(t, err, "JavaScript identifier")
}

// TestTheRemoteNameIsAnIdentifier: a hyphenated module name is legal Codefly
// and illegal JavaScript, so the remote's name carries underscores while
// everything else keeps the hyphens.
func TestTheRemoteNameIsAnIdentifier(t *testing.T) {
	root := scaffoldInto(t, "client-notes", emptyManifest)
	config := read(t, filepath.Join(root, "services", pageDirectory, "rsbuild.config.ts"))
	require.Contains(t, config, `const name = "client_notes"`)
	require.Contains(t, config, `"./Page": "./src/Page.tsx"`)
}

// TestTheGeneratedConsumptionIsTheManifests holds the one rule that made this
// agent worth building: consumption is declared ONCE, in api.consumes, and the
// Go half is generated from it.
func TestTheGeneratedConsumptionIsTheManifests(t *testing.T) {
	root := scaffoldInto(t, "notes", consumingManifest)
	generated := read(t, filepath.Join(root, "services", backendService, generatedConsumptionFile))

	require.Contains(t, generated, "DO NOT EDIT")
	require.Contains(t, generated, `Documents   string`)
	require.Contains(t, generated, `RecordStore string`)
	require.Contains(t, generated, `var consumedAsAll = []string{"documents", "record-store"}`)
}

// TestAMissingManifestIsAnEmptyConsumption is the ordering `codefly add
// solution` imposes: it scaffolds the services and writes the documents
// afterwards, so at Create time there is no manifest to read and the honest
// answer is "nothing so far". `codefly sync` picks it up.
func TestAMissingManifestIsAnEmptyConsumption(t *testing.T) {
	root := scaffoldInto(t, "notes", "")
	generated := read(t, filepath.Join(root, "services", backendService, generatedConsumptionFile))
	require.Contains(t, generated, "var consumedAsAll = []string{}")
	require.Contains(t, generated, "consumes no module yet")
	require.NotContains(t, generated, "consumedAs = struct")
}

// TestSyncRewritesTheGeneratedConsumption: the generated file goes stale on a
// change to a document the CLI owns rather than to any Go source, so no build
// would notice. `codefly sync` is what moves it, and this is that path.
func TestSyncRewritesTheGeneratedConsumption(t *testing.T) {
	root := scaffoldInto(t, "notes", "")
	backend := filepath.Join(root, "services", backendService)
	require.Contains(t, read(t, filepath.Join(backend, generatedConsumptionFile)), "[]string{}")

	require.NoError(t, os.WriteFile(filepath.Join(root, "solution.codefly.yaml"), []byte(consumingManifest), 0o600))

	svc := NewService()
	identity := &resources.ServiceIdentity{Name: backendService, Module: "notes", Workspace: "workspace"}
	svc.Base.Location = backend
	svc.Base.Identity = identity
	svc.Base.Information = &services.Information{
		Service: resources.ToServiceWithCase(identity),
		Module:  resources.ToModuleWithCase(identity),
		Agent:   agent,
	}
	require.NoError(t, NewBuilder(svc).writeGeneratedConsumption(context.Background()))
	require.Contains(t, read(t, filepath.Join(backend, generatedConsumptionFile)),
		`var consumedAsAll = []string{"documents", "record-store"}`)
}

// TestScaffoldingKeepsWhatIsAlreadyThere: Create writes with the factory's own
// rule — an existing file is kept, never truncated — so re-running it over a
// solution somebody has worked in leaves their code alone.
func TestScaffoldingKeepsWhatIsAlreadyThere(t *testing.T) {
	root := scaffoldInto(t, "notes", emptyManifest)
	backend := filepath.Join(root, "services", backendService)
	page := filepath.Join(root, "services", pageDirectory)
	authored := "package main\n\n// mine\n"
	require.NoError(t, os.WriteFile(filepath.Join(backend, "operations.go"), []byte(authored), 0o600))
	authoredPage := "// mine\n"
	require.NoError(t, os.WriteFile(filepath.Join(page, "src", "Page.tsx"), []byte(authoredPage), 0o600))

	svc := NewService()
	identity := &resources.ServiceIdentity{Name: backendService, Module: "notes", Workspace: "workspace"}
	svc.Base.Location = backend
	svc.Base.Identity = identity
	svc.Base.Information = &services.Information{
		Service: resources.ToServiceWithCase(identity),
		Module:  resources.ToModuleWithCase(identity),
		Agent:   agent,
	}
	builder := NewBuilder(svc)
	rendered, err := newScaffold(svc.Base.Information)
	require.NoError(t, err)
	require.NoError(t, builder.scaffoldSolution(context.Background(), rendered))

	require.Equal(t, authored, read(t, filepath.Join(backend, "operations.go")))
	require.Equal(t, authoredPage, read(t, filepath.Join(page, "src", "Page.tsx")))
}

// TestTheInterfaceDocumentIsRenderedByTheRuntime: the document is what
// `codefly generate contracts` publishes as the http endpoint's contract, and
// the CLI derives the rows the authority's Check applies from it, so a backend
// without one publishes no contract at all. The agent renders the first copy
// with the runtime's own renderer, so the committed bytes are the bytes the
// scaffolded `go generate ./...` writes — openapi_test.go passes on a tree
// nobody has run a generator in yet.
func TestTheInterfaceDocumentIsRenderedByTheRuntime(t *testing.T) {
	root := scaffoldInto(t, "notes", emptyManifest)
	document := read(t, filepath.Join(root, "services", backendService, filepath.FromSlash(interfaceArtifactPath)))

	var declared struct {
		Swagger string `json:"swagger"`
		Info    struct {
			Title       string `json:"title"`
			Version     string `json:"version"`
			Description string `json:"description"`
		} `json:"info"`
	}
	require.NoError(t, json.Unmarshal([]byte(document), &declared))
	require.Equal(t, "2.0", declared.Swagger)
	require.Equal(t, "notes solution backend", declared.Info.Title)
	require.Equal(t, "0.0.0", declared.Info.Version)
	require.Equal(t, interfaceDescription("notes"), declared.Info.Description)

	// The scaffolded generator must ask for the same document, or the first
	// `go generate ./...` reports a diff nobody introduced.
	generator := read(t, filepath.Join(root, "services", backendService, "openapi.go"))
	require.Contains(t, generator, `Title:       "notes solution backend"`)
	require.Contains(t, generator, interfaceDescription("notes"))
	require.Contains(t, generator, `OperationPrefix: "Notes"`)
	require.Contains(t, generator, interfaceArtifactPath)
}

func read(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path) //nolint:gosec // a path this test just wrote
	require.NoError(t, err)
	return strings.ReplaceAll(string(content), "\r\n", "\n")
}
