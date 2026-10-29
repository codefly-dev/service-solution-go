package main

import (
	"context"

	"github.com/codefly-dev/core/agents/contract"
	"github.com/codefly-dev/core/agents/services"
	agentv0 "github.com/codefly-dev/core/generated/go/codefly/services/agent/v0"
	"github.com/codefly-dev/core/languages"
	runnersbase "github.com/codefly-dev/core/runners/base"
	"github.com/codefly-dev/core/shared"
	"github.com/codefly-dev/core/templates"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	goservice "github.com/codefly-dev/service-go/pkg/service"
)

// Settings is this agent's service configuration. It is the generic Go agent's
// settings and nothing more: a solution backend has no toggle of its own,
// because everything that distinguishes one is DECLARED rather than
// configured — what it is in solution.codefly.yaml, what it asks for in
// module.contract.codefly.yaml, and what it consumes in api.consumes.
//
// `yaml:",inline"` keeps the shape flat, so a solution backend's
// service.codefly.yaml `spec:` reads exactly like any Go service's
// (hot-reload, debug-symbols, source-dir, …).
type Settings struct {
	goservice.Settings `yaml:",inline"`
}

// Service is the solution specialization. It embeds *goservice.Service to
// inherit the identity, the logger, the location and source resolution, and
// overrides only the advertisement.
type Service struct {
	*goservice.Service

	Settings *Settings
}

// NewService builds the agent's service state with the solution backend's own
// source convention: the Go module at the SERVICE ROOT, not under `code/`.
//
// That is not a preference. A solution backend embeds
// solution.frontend.codefly.yaml, which lives beside service.codefly.yaml
// because the CLI reads the same file for the presence document it signs
// (pkg/solutionrun) — and //go:embed cannot reach up out of its own directory.
// A module under `code/` therefore could not embed the declaration it serves,
// and the served manifest and the signed presence would become two files.
func NewService() *Service {
	generic := goservice.New(agent)
	settings := &Settings{}
	settings.SourceDir = solutionSourceDir
	generic.Settings = &settings.Settings
	generic.InputPlans = goservice.GenericGoInputPlans
	return &Service{Service: generic, Settings: settings}
}

// solutionSourceDir is where a solution backend's Go module lives, relative to
// the service directory: the service root. See NewService.
const solutionSourceDir = "."

// GetAgentInformation is this agent's advertisement. It differs from the
// generic Go agent's in two ways that a caller acts on: it serves HTTP (the
// one endpoint a solution backend exposes, which the authority's edge fronts),
// and it carries this repository's own README so `codefly add service` prints
// what a solution backend is rather than "Generic Go service".
func (s *Service) GetAgentInformation(ctx context.Context, _ *agentv0.AgentInformationRequest) (*agentv0.AgentInformation, error) {
	defer s.Wool.Catch()

	info := s.Information
	if info == nil {
		info = &services.Information{}
	}
	readme, err := templates.ApplyTemplateFrom(ctx, shared.Embed(readmeFS), "templates/agent/README.md", info)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	advertisement := services.Advertisement{
		Backends: runnersbase.BackendSupport{
			Local:  func() bool { return languages.HasGoRuntime(nil) },
			Nix:    true,
			Docker: true,
		},
		Toolchains: []agentv0.Toolchain_Type{agentv0.Toolchain_GO},
		Languages:  []agentv0.Language_Type{agentv0.Language_GO},
		// A solution backend serves one HTTP endpoint and the authority's edge
		// fronts it; it speaks no gRPC of its own, and the module calls it
		// relays are the runtime's gRPC client, not a served protocol.
		Protocols:  []agentv0.Protocol_Type{agentv0.Protocol_HTTP},
		ReadMe:     readme,
		Validation: goservice.ValidationCapabilities(),
		// Sync is supported: it regenerates what api.consumes projects into Go
		// and re-renders the interface document (builder.go).
		EffectiveInputsVersions: []uint32{1},
	}.Build()
	advertisement.Validation.Sync.Supported = true
	// The deployment path is core's DeployKustomize, which judges dependency
	// edges with the request's composition provenance on this core; the host
	// requires the live advertisement, not the linked core version.
	advertisement.Contract = contract.Current()
	advertisement.Contract.Capabilities = append(advertisement.Contract.Capabilities,
		contract.DeploymentCompositionProvenance,
		// The inherited Runtime reports what it started on every native start,
		// which is the only thing that earns this capability.
		contract.RuntimeStartedArtifact,
	)
	return advertisement, nil
}
