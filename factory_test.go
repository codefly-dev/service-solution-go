package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/codefly-dev/core/agents/generator"
	"github.com/codefly-dev/core/generation"
	"github.com/codefly-dev/core/resources"
	"github.com/stretchr/testify/require"
)

// TEMPLATES/FACTORY IS GENERATED. NOTHING IN IT IS HAND-EDITED.
//
// `codefly agent generate --service .` is core's
// generator.GenerateServiceTemplate: it copies base/ into templates/factory,
// adds a .tmpl suffix to every name, and applies the literal replacements in
// base/service.generation.codefly.yaml.
//
// This test is that generator, run into a temporary directory and compared
// against what is committed — the drift check. It calls core's function
// directly rather than shelling out to the CLI, so the check needs no second
// binary and holds the committed templates to the exact code `codefly agent
// generate` runs.
//
// `go test -run TestFactoryTemplatesAreGeneratedFromBase -update` regenerates
// them in place. An edit made to a template instead of to base/ is reverted by
// the next regeneration, and fails here until it is.

// TestFactoryTemplatesAreGeneratedFromBase.
func TestFactoryTemplatesAreGeneratedFromBase(t *testing.T) {
	generated := generateFactory(t)
	committed := filepath.Join("templates", "factory")

	if *update {
		require.NoError(t, os.RemoveAll(committed),
			"the committed templates are replaced wholesale: regeneration overwrites a file but never deletes one, so a template whose source left base/ would survive")
		require.NoError(t, os.MkdirAll(filepath.Dir(committed), 0o750))
		require.NoError(t, os.CopyFS(committed, os.DirFS(generated)))
		return
	}

	want, got := treeOf(t, generated), treeOf(t, committed)

	// The file sets first, and both directions. Regeneration overwrites but
	// never deletes, so a template left behind by a file that has since been
	// removed from base/ would otherwise go on being scaffolded forever.
	require.Equal(t, keysOf(want), keysOf(got),
		"templates/factory is not the file set base/ generates — run `go test -run TestFactoryTemplatesAreGeneratedFromBase -update`")

	for _, name := range keysOf(want) {
		require.Equalf(t, want[name], got[name],
			"templates/factory/%s is not what base/ generates. It is GENERATED: edit base/ and run `go test -run TestFactoryTemplatesAreGeneratedFromBase -update`.", name)
	}
}

// TestGeneratedTemplatesCarryNoBaseSolutionName is the check that the
// replacements are complete.
//
// base/ is a real solution called `codefly-base`. Every mention of that name,
// in any of its three spellings, must have become a template action — a
// leftover would hard-code base/'s own name into every solution this agent
// scaffolds. The replacements are whole-file literal substitutions, so a new
// spelling introduced in base/ (a different capitalisation, say) silently
// survives generation; this is what catches it.
func TestGeneratedTemplatesCarryNoBaseSolutionName(t *testing.T) {
	for name, content := range treeOf(t, filepath.Join("templates", "factory")) {
		for _, spelling := range []string{baseSolutionID, "Codefly Base", "CodeflyBase", "codefly_base"} {
			require.NotContainsf(t, content, spelling,
				"templates/factory/%s still carries base/'s own solution name %q: add a replacement to base/service.generation.codefly.yaml", name, spelling)
		}
	}
}

// TestGeneratedTemplatesRenderWithTheScaffoldContext. A replacement whose `to`
// names a field the scaffold context does not carry produces a template that
// fails at Create, in a user's terminal. Every action in the generated tree
// must be one this agent can fill, which the scaffold tests below prove by
// rendering the whole tree; this states the narrower rule the replacements
// must obey.
func TestGeneratedTemplatesRenderWithTheScaffoldContext(t *testing.T) {
	known := map[string]bool{
		"{{ .SolutionID }}": true, "{{ .Title }}": true, "{{ .OperationPrefix }}": true,
	}
	for _, replacement := range baseGeneration(t).Replacements {
		require.Containsf(t, known, replacement.To,
			"base/service.generation.codefly.yaml replaces %q with %q, which the scaffold context does not carry",
			replacement.From, replacement.To)
	}
}

// generateFactory runs core's generator over a copy of base/ and returns the
// directory holding the generated templates/factory.
//
// base/ is copied rather than generated from in place, because
// GenerateServiceTemplate writes to <dir>/templates/factory and this must not
// touch the committed tree while comparing against it.
func generateFactory(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.CopyFS(filepath.Join(root, baseDir), os.DirFS(baseDir)))
	require.NoError(t, generator.GenerateServiceTemplate(context.Background(), root),
		"`codefly agent generate --service .` failed over base/")
	return filepath.Join(root, "templates", "factory")
}

// baseGeneration is base/'s generation configuration, read with core's own
// loader — the same one GenerateServiceTemplate reads it with.
func baseGeneration(t *testing.T) *generation.Service {
	t.Helper()
	configuration, err := resources.LoadFromDir[generation.Service](context.Background(), baseDir)
	require.NoError(t, err, "base/%s is not a readable generation configuration",
		generation.ServiceGenerationConfigurationName)
	return configuration
}

// treeOf is every file under root, by slash-separated path relative to it,
// with line endings normalised so the comparison is of content.
func treeOf(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		content, readErr := os.ReadFile(path) //nolint:gosec // a path just walked
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		files[filepath.ToSlash(rel)] = strings.ReplaceAll(string(content), "\r\n", "\n")
		return nil
	}), "cannot read the template tree at %s", root)
	require.NotEmpty(t, files, "%s holds no files", root)
	return files
}

func keysOf(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
