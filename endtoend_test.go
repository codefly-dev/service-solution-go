package main

import (
	"os"
	"path/filepath"
	"strings"
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
// IT RUNS IN THE ORDER `codefly add solution` RUNS IN, and that order is the
// whole subtlety: the agent's Create goes FIRST and the solution's own
// documents are written after it returns. Writing the manifest first — which
// this file used to do — proves a tree no user ever has.

// TestAScaffoldedSolutionBuildsAndPassesItsOwnGates is that run, for a
// solution that consumes modules and one that consumes none. Both are real
// states of `codefly add solution`: with `--consumes` and without.
func TestAScaffoldedSolutionBuildsAndPassesItsOwnGates(t *testing.T) {
	for name, solution := range map[string]struct {
		manifest string
		consumes bool
	}{
		"consumes nothing": {manifest: emptyManifest},
		"consumes modules": {manifest: consumingManifest, consumes: true},
	} {
		t.Run(name, func(t *testing.T) {
			goTool := goToolchain(t)

			// Create, with no solution manifest in sight: `add solution`
			// scaffolds both services through the agents' own Create flows and
			// writes the solution's documents only once both exist.
			root := scaffoldInto(t, "notes", "")
			backend := filepath.Join(root, "services", backendService)
			completeAsTheCLIDoes(t, root, solution.manifest)

			// `go build` with the default -mod=readonly and no proxy: the
			// scaffolded go.mod and go.sum are tidy as generated, so a fresh
			// backend builds without a tidy and without reaching the network
			// for anything the agent's own module graph has not already
			// resolved.
			run(t, goTool, backend, "build", "./...")

			if !solution.consumes {
				// The suite the solution ships with: its consumption drift
				// check, its own refusal of a module edge (SP-SOL-04), and the
				// interface document, which must already be what
				// `go generate ./...` would render — nobody has run a
				// generator in this tree.
				run(t, goTool, backend, "test", "./...")
				return
			}

			// WITH --consumes, A FRESHLY SCAFFOLDED SOLUTION IS RED ON EXACTLY
			// ONE TEST, AND THAT IS THE TRUTH RATHER THAN A DEFECT.
			//
			// consumed_gen.go is rendered from api.consumes, which does not
			// exist when Create runs, so it is rendered empty (builder.go says
			// so). Until `codefly sync` has run, this backend relays nothing
			// its solution declares — and the drift check saying so is the
			// whole reason that check exists. The alternative is a backend
			// that silently reaches nothing.
			//
			// This is pinned rather than tolerated: if the failure ever
			// becomes a different one, or spreads to a second test, this fails
			// and names it. If `codefly add solution` ever syncs before it
			// returns, the tree is green here and this fails too — which is
			// the moment to delete this branch.
			output, err := attempt(t, goTool, backend, "test", "./...")
			require.Error(t, err, "a solution that consumes modules has an empty consumed_gen.go until `codefly sync` runs: the drift check must say so")
			require.Contains(t, output, "--- FAIL: TestTheGeneratedConsumptionMatchesTheManifest")
			require.Contains(t, output, "run `codefly sync` and commit it",
				"the failure must carry the one command that fixes it")
			require.Equal(t, 1, strings.Count(output, "--- FAIL:"),
				"exactly one test may fail on a freshly scaffolded solution; everything else is a defect:\n%s", output)

			// And the recovery is the whole of it: the agent's own Sync, which
			// is what `codefly sync service <module>/backend` calls.
			syncAsTheCLIDoes(t, root, "notes")
			run(t, goTool, backend, "test", "./...")
		})
	}
}

// completeAsTheCLIDoes writes what `codefly add solution` writes AFTER this
// agent's Create returns, and that this agent must not write (shape_test.go
// holds it to that):
//
//   - solution.codefly.yaml, the solution's own manifest, one directory above
//     the service. api.consumes lives there and nowhere else.
//   - solution.frontend.codefly.yaml, which the scaffolded main.go embeds. A
//     backend does not compile without it, by design: the runtime serves this
//     block and the CLI delivers it in the presence document it signs, so one
//     file is the only way the served manifest and the signed presence cannot
//     disagree.
//   - the rest of service.codefly.yaml. It is taken from the scaffolded
//     GETTING_STARTED.md's own example rather than written here, so what this
//     test proves is that THE DOCUMENTED MANIFEST is the one that works. An
//     example that drifts from what the CLI writes fails here.
func completeAsTheCLIDoes(t *testing.T, root, manifest string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, "solution.codefly.yaml"), []byte(manifest), 0o600))

	backend := filepath.Join(root, "services", backendService)
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
