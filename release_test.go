package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/codefly-dev/core/resources"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// HOW THIS AGENT REACHES A HOST, pinned where a change to it is visible.
//
// An agent is installed by core downloading one archive from one GitHub
// release and running one executable out of it. Every name in that path is
// core's to decide, and .goreleaser.yaml restates all of them — so a rename
// here, or a GoReleaser default that moves, publishes an agent that downloads
// and then cannot be found. These tests compare against core's own
// AgentKindRegistration rather than against a spelling repeated in a string.

// agentManifest is agent.codefly.yaml's publication half: the keys the CLI's
// `codefly publish` reads (cmd/agents/build.go), which resources.Agent — what
// main.go loads for the identity — does not carry.
type agentManifest struct {
	Publisher string `yaml:"publisher"`
	Kind      string `yaml:"kind"`
	Name      string `yaml:"name"`
	Version   string `yaml:"version"`
	Release   struct {
		Owner    string `yaml:"owner"`
		Workflow string `yaml:"workflow"`
	} `yaml:"release"`
	Source struct {
		Directory string   `yaml:"directory"`
		Agent     string   `yaml:"agent"`
		Bootstrap []string `yaml:"bootstrap"`
	} `yaml:"source"`
}

// stringOrSlice is a GitHub Actions field that accepts either one value or a
// list of them, such as `needs`. Both spellings mean the same thing, so both
// decode to the same slice rather than to a parse error on whichever this
// repository did not happen to use.
type stringOrSlice []string

func (s *stringOrSlice) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		var one string
		if err := node.Decode(&one); err != nil {
			return err
		}
		*s = stringOrSlice{one}
		return nil
	}
	var many []string
	if err := node.Decode(&many); err != nil {
		return err
	}
	*s = many
	return nil
}

func readYAML(t *testing.T, path string, target any) {
	t.Helper()
	payload, err := os.ReadFile(path) //nolint:gosec // a path spelled in this test
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(payload, target), "%s must be readable YAML", path)
}

// TestTheArchiveIsTheOneCoreDownloads is the contract with no second copy:
// core builds the asset name it fetches and the executable it runs from the
// agent's KIND and NAME, so both are derived here from core's own
// registration and compared to what GoReleaser is told to produce.
//
// A GoReleaser archive is "<name_template>.<format>", so the template carries
// everything but the extension.
func TestTheArchiveIsTheOneCoreDownloads(t *testing.T) {
	var manifest agentManifest
	readYAML(t, "agent.codefly.yaml", &manifest)

	registration, err := resources.AgentKindRegistrationFor(resources.AgentKind(manifest.Kind))
	require.NoErrorf(t, err, "core registers no agent kind %q", manifest.Kind)

	var config struct {
		ProjectName string `yaml:"project_name"`
		Builds      []struct {
			ID     string   `yaml:"id"`
			Binary string   `yaml:"binary"`
			GOOS   []string `yaml:"goos"`
			GOARCH []string `yaml:"goarch"`
		} `yaml:"builds"`
		Archives []struct {
			Formats      []string `yaml:"formats"`
			NameTemplate string   `yaml:"name_template"`
		} `yaml:"archives"`
	}
	readYAML(t, ".goreleaser.yaml", &config)

	// The executable core runs out of the archive. Every build must produce
	// it: core runs one name whatever platform it downloaded.
	executable := registration.ExecutableName(manifest.Name)
	require.NotEmpty(t, config.Builds, "an agent that builds nothing publishes nothing")
	for _, build := range config.Builds {
		require.Equalf(t, executable, build.Binary,
			"build %q produces %q; core runs %q", build.ID, build.Binary, executable)
	}

	// The archive core fetches, rendered from the same registration. The
	// GoReleaser template is resolved here for one concrete platform, so the
	// comparison is against a real name rather than against another template.
	require.Len(t, config.Archives, 1, "core downloads one archive per platform")
	archive := config.Archives[0]
	require.Equal(t, []string{"tar.gz"}, archive.Formats)

	const version = "0.0.0"
	rendered := strings.NewReplacer(
		"{{ .ProjectName }}", config.ProjectName,
		"{{ .Version }}", version,
		"{{ .Os }}", runtime.GOOS,
		"{{ .Arch }}", runtime.GOARCH,
	).Replace(archive.NameTemplate) + "." + archive.Formats[0]
	require.NotContains(t, rendered, "{{", "the archive name template uses a value this test does not resolve")
	require.Equal(t, registration.GitHubAsset(manifest.Name, version, runtime.GOOS, runtime.GOARCH), rendered,
		"the published archive is not the one core downloads")
}

// TestEveryPublishedPlatformIsBuiltWithCGO. core's authoritative source
// inspection links real tree-sitter grammars, so an agent built with
// CGO_ENABLED=0 loads and then cannot parse anything — a failure that looks
// like a bad service, not a bad build. The cross compilers are why the release
// job runs inside goreleaser-cross.
func TestEveryPublishedPlatformIsBuiltWithCGO(t *testing.T) {
	var config struct {
		Builds []struct {
			ID  string   `yaml:"id"`
			Env []string `yaml:"env"`
		} `yaml:"builds"`
	}
	readYAML(t, ".goreleaser.yaml", &config)
	for _, build := range config.Builds {
		require.Containsf(t, build.Env, "CGO_ENABLED=1",
			"build %q disables CGO, so core's tree-sitter source inspection cannot ship", build.ID)
	}
}

// TestTheTagWorkflowIsTheSolePublisher, and carries an SBOM per archive.
//
// `release.owner: workflow` says no artifact is published except by the
// workflow a `v*` tag starts, so the workflow must exist and must actually
// release. `release.prerelease: auto` is what keeps a `codefly publish dev`
// build (v<version>-dev.<sha>) off Latest, so `version: latest` can never
// resolve to a dev build.
func TestTheTagWorkflowIsTheSolePublisher(t *testing.T) {
	var manifest agentManifest
	readYAML(t, "agent.codefly.yaml", &manifest)
	require.Equal(t, "workflow", manifest.Release.Owner, "the tag workflow must be the sole artifact publisher")
	require.Equal(t, "releaser.yml", manifest.Release.Workflow)

	var config struct {
		Release struct {
			Prerelease string `yaml:"prerelease"`
		} `yaml:"release"`
		SBOMs []struct {
			Artifacts string   `yaml:"artifacts"`
			Documents []string `yaml:"documents"`
			Disable   bool     `yaml:"disable"`
		} `yaml:"sboms"`
	}
	readYAML(t, ".goreleaser.yaml", &config)
	require.Equal(t, "auto", config.Release.Prerelease,
		"a dev build's prerelease tag must publish as a GitHub prerelease, never as Latest")
	require.Len(t, config.SBOMs, 1)
	require.Equal(t, "archive", config.SBOMs[0].Artifacts, "each published archive carries its own SBOM")
	require.False(t, config.SBOMs[0].Disable)
	require.Equal(t, []string{"${artifact}.sbom.json"}, config.SBOMs[0].Documents)

	var workflow struct {
		On struct {
			Push struct {
				Tags []string `yaml:"tags"`
			} `yaml:"push"`
		} `yaml:"on"`
		Jobs map[string]struct {
			Needs stringOrSlice `yaml:"needs"`
			Steps []struct {
				ID   string            `yaml:"id"`
				Uses string            `yaml:"uses"`
				Run  string            `yaml:"run"`
				Env  map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	readYAML(t, filepath.Join(".github", "workflows", manifest.Release.Workflow), &workflow)
	require.Equal(t, []string{"v*"}, workflow.On.Push.Tags, "only a version tag publishes")

	// The suite runs before anything is published: a tag that does not pass
	// publishes nothing.
	release, found := workflow.Jobs["release"]
	require.True(t, found, "the release workflow declares no release job")
	require.Contains(t, release.Needs, "test", "a release must wait on the suite")

	// Syft installed AND mounted into the cross-build container, which has
	// none of its own — without the mount GoReleaser produces no SBOM and the
	// `sboms` block above is silently inert.
	installed, mounted := false, false
	for _, step := range release.Steps {
		if step.ID == "syft" && strings.HasPrefix(step.Uses, "anchore/sbom-action/download-syft@") {
			installed = true
		}
		if strings.Contains(step.Run, `-v "$SYFT_PATH:/usr/local/bin/syft:ro"`) &&
			step.Env["SYFT_PATH"] == "${{ steps.syft.outputs.cmd }}" {
			mounted = true
		}
	}
	require.True(t, installed, "the release job installs no Syft, so no archive SBOM is produced")
	require.True(t, mounted, "Syft is not mounted into the cross build, so the sboms block is inert")
}

// TestSelfHostingDeclaresWhatThePublisherRequires.
//
// This agent packages its OWN source (`source.agent: self`) rather than naming
// another agent's release. That is a deliberate choice: naming
// `codefly.dev/go:<v>` would make every release of this agent wait on a bump
// of that one, and this agent is a Go program that can build itself for a host
// with no codefly.dev service agent installed yet.
//
// The CLI enforces four preconditions on that choice before it will publish
// (cmd/agents/build.go, prepareAgentPackager). They are pinned here so a
// malformed manifest fails in `go test` rather than at a publish that has
// already tagged.
func TestSelfHostingDeclaresWhatThePublisherRequires(t *testing.T) {
	var manifest agentManifest
	readYAML(t, "agent.codefly.yaml", &manifest)
	require.Equal(t, ".", manifest.Source.Directory,
		"publication must select a source packager even when several compatible agents are installed")
	if manifest.Source.Agent != "self" {
		t.Skipf("this agent is packaged by %q, not self-hosted", manifest.Source.Agent)
	}

	// 1. a self-hosted agent must declare the command that builds it, and
	//    2. that command must exist and be executable.
	require.NotEmpty(t, manifest.Source.Bootstrap, "a self-hosted source agent must declare source.bootstrap")
	bootstrap := manifest.Source.Bootstrap[0]
	require.NotEmpty(t, strings.TrimSpace(bootstrap), "the source bootstrap command is required")
	info, err := os.Stat(strings.TrimPrefix(bootstrap, "./"))
	require.NoErrorf(t, err, "source.bootstrap names %s, which is not in this repository", bootstrap)
	require.NotZerof(t, info.Mode().Perm()&0o111, "%s is not executable, so the publisher cannot run it", bootstrap)

	// 3. the version must be a semver release: it becomes the canonical
	//    artifact version the self-built packager is installed under.
	require.Regexp(t, `^\d+\.\d+\.\d+`, manifest.Version,
		"a self-hosted source agent must declare its canonical artifact version")

	// 4. publisher and name each become ONE path component of the directory
	//    the packager is installed into, so neither may contain a separator.
	for _, component := range []string{manifest.Publisher, manifest.Name} {
		require.NotContains(t, component, "/",
			"a self-hosted agent's publisher and name must be single path components")
		require.NotContains(t, component, `\`)
		require.NotEqual(t, ".", component)
	}

	// The bootstrap builds for the host the publisher is running on, which it
	// learns from these three variables and nothing else.
	script, err := os.ReadFile(strings.TrimPrefix(bootstrap, "./")) //nolint:gosec // a path the manifest names
	require.NoError(t, err)
	for _, supplied := range []string{"CODEFLY_AGENT_OUTPUT", "CODEFLY_AGENT_OS", "CODEFLY_AGENT_ARCH"} {
		require.Containsf(t, string(script), supplied,
			"%s ignores %s, which is how the publisher states what to build and where to put it", bootstrap, supplied)
	}
	require.Contains(t, string(script), "CGO_ENABLED=1",
		"the self-built packager must keep CGO enabled, as every released build does")
}
