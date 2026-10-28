package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// THE VERIFICATION BAR, run rather than asserted.
//
// base_test.go compiles base/, which is the hand-written source. This compiles
// what a USER ends up with, which is a different thing and can fail while
// base/ is green: it goes through this agent's own Create — the generated
// templates rendered with the scaffold context, plus the two files the agent
// writes itself (consumed_gen.go and the interface document) — and then
// completes the tree the way the CLI completes it. A replacement that renders
// into Go that does not compile, or an agent-written file that disagrees with
// what the templates expect, passes every other test here and fails this one.
//
// It is what caught both defects this file was added with: a solution that
// consumed anything was born red, because the runtime refuses a consumed
// module declaring no method, and the scaffolded drift check asserted an
// equality the reference solution does not hold (api.consumes names every
// module the solution reaches anywhere; this backend relays a subset).

// TestAScaffoldedSolutionBuildsAndPassesItsOwnGates is that run, for a
// solution that consumes modules and one that consumes none. Both are real
// states of `codefly add solution`: with `--consumes` and without.
func TestAScaffoldedSolutionBuildsAndPassesItsOwnGates(t *testing.T) {
	for name, solutionManifest := range map[string]string{
		"consumes nothing": emptyManifest,
		"consumes modules": consumingManifest,
	} {
		t.Run(name, func(t *testing.T) {
			goTool := goToolchain(t)
			backend := filepath.Join(scaffoldInto(t, "notes", solutionManifest), "services", backendService)
			completeAsTheCLIDoes(t, backend)

			// `go build` with the default -mod=readonly and no proxy: the
			// scaffolded go.mod and go.sum are tidy as generated, so a fresh
			// backend builds without a tidy and without reaching the network
			// for anything the agent's own module graph has not already
			// resolved.
			run(t, goTool, backend, "build", "./...")
			// The suite the solution ships with: its consumption drift check,
			// its own refusal of a module edge (SP-SOL-04), and the interface
			// document, which must already be what `go generate ./...` would
			// render — nobody has run a generator in this tree.
			run(t, goTool, backend, "test", "./...")
		})
	}
}

// completeAsTheCLIDoes writes the two files `codefly add solution` writes into
// the service directory AFTER this agent's Create returns, and that this agent
// must not write (shape_test.go holds it to that):
//
//   - solution.frontend.codefly.yaml, which the scaffolded main.go embeds. A
//     backend does not compile without it, by design: the runtime serves this
//     block and the CLI delivers it in the presence document it signs, so one
//     file is the only way the served manifest and the signed presence cannot
//     disagree.
//   - the rest of service.codefly.yaml. It is taken from the scaffolded
//     GETTING_STARTED.md's own example rather than written here, so what this
//     test proves is that THE DOCUMENTED MANIFEST is the one that works. An
//     example that drifts from what the CLI writes fails here.
func completeAsTheCLIDoes(t *testing.T, backend string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(backend, "solution.frontend.codefly.yaml"),
		[]byte("title: Notes\norder: 60\nexposed_module: ./Page\nhost_contract: 1\n"), 0o600))

	// What `codefly add service` writes before the solution's own block: the
	// identity and the source convention. `spec.source-dir: .` is this agent's
	// (service.go says why the module is at the service root).
	header := "kind: service\nname: " + backendService + "\nversion: 0.0.0\n" +
		"agent:\n    publisher: " + agent.Publisher + "\n    kind: codefly:service\n" +
		"    name: " + agent.Name + "\n    version: " + agent.Version + "\n" +
		"spec:\n    source-dir: " + solutionSourceDir + "\n"
	declared := fencedYAML(t, read(t, filepath.Join(backend, "GETTING_STARTED.md")))
	require.NoError(t, os.WriteFile(filepath.Join(backend, "service.codefly.yaml"),
		[]byte(header+declared+"\n"), 0o600))
}
