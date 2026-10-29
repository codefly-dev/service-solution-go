package main

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	solution "github.com/codefly-dev/solution-runtime-go"
	"github.com/stretchr/testify/require"
)

// base/ IS THE HAND-WRITTEN ARTIFACT, and templates/factory/ is generated
// from it (factory_test.go). That is the whole authoring model of a service
// agent, and the reason for it is this file: a template cannot be compiled or
// tested, so nothing proves what it scaffolds even builds. base/ is a real
// solution backend — it compiles, it has a tidy go.sum, and ITS OWN TEST SUITE
// IS THE SUITE EVERY SCAFFOLDED SOLUTION SHIPS WITH. Running it here is what
// makes the scaffolded tree known-good before anybody scaffolds it.
//
// `go test -update` rewrites the three derived files in base/ and regenerates
// templates/factory. Without it, every one of them is checked.

// update rewrites what is derived rather than asserting it. One flag for this
// file and factory_test.go, so `-update` moves base/ and the templates
// together and they cannot be refreshed one without the other.
var update = flag.Bool("update", false,
	"rewrite base/'s derived files and regenerate templates/factory from base/")

const (
	// baseDir is the real service this agent is authored as.
	baseDir = "base"
	// baseSolutionID is base/'s solution name, and the token the generation
	// config replaces to produce the templates. Spelled here so a test reads
	// the same name the config does.
	baseSolutionID = "codefly-base"
)

// baseSolutionManifest is the solution manifest base/ belongs to: the document
// `codefly add solution --consumes` writes one directory above services/.
//
// It is NOT in base/ — base/ is the service directory, so the manifest sits at
// ../../ from it, outside this repository. stageBase puts it where a real
// solution has it.
//
// It declares one consumed module ON PURPOSE. A solution that consumes
// something is the state that was broken before: the runtime refuses a
// consumed module declaring no method, so a backend whose composition is still
// empty must leave an uncomposed module OUT rather than declare it empty.
// base/ is therefore permanently in that state, and its suite would fail if
// consumes.go stopped filtering.
const baseSolutionManifest = `schema_version: codefly.solution-manifest/v1
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
lifecycle:
    package: true
    render: true
`

// stageBase lays base/ out the way a real solution lays its backend out —
// <root>/solution.codefly.yaml beside <root>/services/backend — and returns
// the backend directory.
//
// base/ cannot be tested where it sits: consumes_test.go reads the solution
// manifest at ../../solution.codefly.yaml, which from <repo>/base is outside
// the repository. Staging is not a workaround for that, it is the only honest
// way to run the suite, because ../../ is where a scaffolded backend really
// finds its manifest and a test that reached for it anywhere else would not be
// checking the shipped path.
func stageBase(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	backend := filepath.Join(root, "services", backendService)
	require.NoError(t, os.MkdirAll(filepath.Dir(backend), 0o750))
	require.NoError(t, os.CopyFS(backend, os.DirFS(baseDir)))
	require.NoError(t, os.WriteFile(filepath.Join(root, "solution.codefly.yaml"),
		[]byte(baseSolutionManifest), 0o600))
	return backend
}

// TestBaseCompilesAndPassesItsOwnSuite is the check a hand-written template
// could never carry.
//
// `go build` runs with the default -mod=readonly: base/'s go.mod and go.sum are
// tidy as committed, so a scaffolded backend builds without a tidy. `go vet`
// runs because a scaffolded solution is code somebody will read and extend.
// `go test` runs base/'s own suite — the consumption drift check, the refusal
// of a module edge (SP-SOL-04) and the interface document — which is the suite
// every scaffolded solution gets.
func TestBaseCompilesAndPassesItsOwnSuite(t *testing.T) {
	goTool := goToolchain(t)
	backend := stageBase(t)
	for _, command := range [][]string{
		{"build", "./..."},
		{"vet", "./..."},
		{"test", "./..."},
	} {
		run(t, goTool, backend, command...)
	}
}

// TestBaseConsumptionIsWhatThisAgentGenerates. consumed_gen.go is the one
// agent-OWNED file in a scaffolded backend: rendered from api.consumes by
// renderConsumption, never templatized. base/ commits a copy so it compiles,
// and this holds that copy to the generator — so base/ cannot drift into
// compiling against a shape the agent no longer produces.
func TestBaseConsumptionIsWhatThisAgentGenerates(t *testing.T) {
	aliases, err := consumedAliases(stageBase(t))
	require.NoError(t, err)
	require.Equal(t, []string{"documents"}, aliases,
		"base/'s manifest declares the one consumed module its suite checks")

	path := filepath.Join(baseDir, generatedConsumptionFile)
	rendered := renderConsumption(aliases)
	if *update {
		require.NoError(t, os.WriteFile(path, rendered, 0o600))
		return
	}
	committed, err := os.ReadFile(path) //nolint:gosec // a path in this repository
	require.NoErrorf(t, err, "base/%s is missing; run `go test -update`", generatedConsumptionFile)
	require.Equalf(t, string(rendered), strings.ReplaceAll(string(committed), "\r\n", "\n"),
		"base/%s is not what renderConsumption produces; run `go test -update`", generatedConsumptionFile)
}

// TestBaseInterfaceDocumentIsTheRuntimesRender. The agent renders a new
// solution's first interface document with the solution runtime's own
// renderer, so the committed bytes are the bytes the scaffolded `go generate
// ./...` writes. base/ commits the same document for its own openapi_test.go,
// and it must come from the same render — otherwise base/'s suite passes on
// bytes a scaffolded tree would never hold.
func TestBaseInterfaceDocumentIsTheRuntimesRender(t *testing.T) {
	server := solution.New(solution.Manifest{ID: baseSolutionID, Contract: baseSolutionID}).
		Operations(scaffoldedOperations(baseSolutionID)...)
	document, err := server.InterfaceArtifact("", solution.InterfaceInfo{
		Title:           baseSolutionID + " solution backend",
		Version:         "0.0.0",
		Description:     interfaceDescription(baseSolutionID),
		OperationPrefix: aliasField(baseSolutionID),
		Tag:             baseSolutionID,
	})
	require.NoError(t, err)

	path := filepath.Join(baseDir, filepath.FromSlash(interfaceArtifactPath))
	if *update {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, document, 0o600))
		return
	}
	committed, err := os.ReadFile(path) //nolint:gosec // a path in this repository
	require.NoErrorf(t, err, "base/%s is missing; run `go test -update`", interfaceArtifactPath)
	require.Equalf(t, strings.TrimSpace(string(document)),
		strings.TrimSpace(strings.ReplaceAll(string(committed), "\r\n", "\n")),
		"base/%s is not the runtime's render; run `go test -update`", interfaceArtifactPath)
}

// TestBasePinsTheRuntimeThisAgentDeclares. The pin is the AGENT's choice, one
// value in every solution it scaffolds, so it is a literal in base/go.mod
// rather than a template action — and it must be the constant this agent
// declares. It must also clear the floor a consumer's consumed-API gate
// enforces: a release at or after v0.0.12, the native projected-token runtime.
// A pseudo-version would pin a commit that may never reach a tag.
func TestBasePinsTheRuntimeThisAgentDeclares(t *testing.T) {
	modules := read(t, filepath.Join(baseDir, "go.mod"))
	require.Containsf(t, modules, solutionRuntimePath+" "+solutionRuntimeVersion,
		"base/go.mod must pin %s at %s, the version scaffold.go declares",
		solutionRuntimePath, solutionRuntimeVersion)
	require.NotContains(t, modules, solutionRuntimePath+" =>",
		"a replace makes the pin invisible to the consumed-API gate")
	require.Regexp(t, `^v\d+\.\d+\.\d+$`, solutionRuntimeVersion,
		"the runtime pin is a release, never a pseudo-version")
	require.GreaterOrEqual(t, solutionRuntimeVersion, "v0.0.12",
		"the pin must clear the native projected-token floor")
}

// TestBaseManifestIsTheHostsDoorOnly. base/service.codefly.yaml is excluded
// from generation — the CLI writes it — but base/'s own suite reads it to
// refuse a module edge, so it must be what the CLI actually writes. If it
// drifted to something laxer, base's refusal would be checked against a
// manifest no solution has.
func TestBaseManifestIsTheHostsDoorOnly(t *testing.T) {
	assertHostsDoorOnly(t, read(t, filepath.Join(baseDir, "service.codefly.yaml")))
}

// TestBaseHoldsEveryFileItsGenerationConfigExcludes, and no others.
//
// An ignore is only correct while the file it names is present: an ignore for
// a file nobody committed silently protects nothing, and a file that should be
// excluded but is not becomes a template that overwrites one of the CLI's
// documents. Both directions are checked here.
func TestBaseHoldsEveryFileItsGenerationConfigExcludes(t *testing.T) {
	configuration := baseGeneration(t)
	// Every ignore that names a real path must name a file base/ holds. The
	// workspace entries are the exception: a go.work is a local convenience
	// that is deliberately NOT committed, and the ignore exists so one in a
	// working tree never reaches the templates.
	local := map[string]bool{"go.work": true, "go.work.sum": true}
	for _, ignore := range configuration.Ignores {
		if local[ignore] {
			require.NoFileExists(t, filepath.Join(baseDir, ignore), "a workspace is never committed")
			continue
		}
		require.FileExistsf(t, filepath.Join(baseDir, filepath.FromSlash(ignore)),
			"the generation config ignores %q, which base/ does not hold: the ignore protects nothing", ignore)
	}
	// The documents the CLI owns must each be excluded. This is the direction
	// that matters: one of them templatized would land in a scaffolded tree
	// and be overwritten by the CLI a moment later.
	for _, owned := range []string{
		"service.codefly.yaml",
		"solution.frontend.codefly.yaml",
		interfaceArtifactPath,
		generatedConsumptionFile,
	} {
		require.Containsf(t, configuration.Ignores, owned,
			"base/%s is not excluded from generation, so it would be scaffolded as a template", owned)
	}
}

// goToolchain is the `go` to build base/ with, or a skip. This package is a Go
// program, so a tree that can run it has one; the skip is for a sandbox that
// has stripped PATH.
func goToolchain(t *testing.T) string {
	t.Helper()
	tool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH to build base/ with")
	}
	return tool
}

// run is one go command in a staged tree, with its full output on failure:
// what failed to compile is the finding, not the exit status.
//
// GOWORK=off keeps the staged module out of any go.work this checkout sits in
// — it is a separate module in a temporary directory, and a workspace would
// try to resolve it against this repository's.
func run(t *testing.T, goTool, dir string, args ...string) {
	t.Helper()
	output, err := attempt(t, goTool, dir, args...)
	require.NoErrorf(t, err, "`go %s` in %s:\n%s", strings.Join(args, " "), dir, output)
}

// attempt is run without the assertion, for the one caller that expects a
// failure and has something to say about which one it is.
func attempt(t *testing.T, goTool, dir string, args ...string) (string, error) {
	t.Helper()
	command := exec.Command(goTool, args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.CombinedOutput()
	return string(output), err
}
