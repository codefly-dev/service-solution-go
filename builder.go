package main

import (
	"context"
	"embed"

	builderv0 "github.com/codefly-dev/core/generated/go/codefly/services/builder/v0"
	gobuilder "github.com/codefly-dev/service-go/pkg/builder"
)

// GoVersion is the exact Go patch release a solution backend is built with,
// in the builder stage and in the scaffolded go.mod alike.
const GoVersion = "1.27.3"

// AlpineVersion is the exact runtime Alpine patch release the image is built
// on.
const AlpineVersion = "3.23.5"

// Builder is the solution backend's builder. Load, Init, Build and Deploy are
// the generic Go agent's — a solution backend is compiled, imaged and
// deployed exactly as any Go service is, and a second copy of any of those
// here would be a second mechanism for a job that seam already does.
//
// Create and Sync are this agent's, and they are the whole of what
// `solution-go` is: Create writes a solution backend instead of a plain Go
// service, and Sync keeps the two files derived from the solution's own
// declarations current.
type Builder struct {
	*gobuilder.Builder

	service *Service
	factory embed.FS
	page    embed.FS
}

// NewBuilder wires the generic Go builder with this agent's template trees.
func NewBuilder(svc *Service) *Builder {
	return &Builder{
		Builder: gobuilder.New(svc.Service, gobuilder.BuildConfig{
			FactoryFS:     factoryFS,
			BuilderFS:     builderFS,
			DeploymentFS:  deploymentFS,
			Requirements:  requirements,
			GoVersion:     GoVersion,
			AlpineVersion: AlpineVersion,
		}),
		service: svc,
		factory: factoryFS,
		page:    pageFS,
	}
}

// Create scaffolds a solution backend and the ./Page remote beside it.
//
// The order matters and is the point of the whole file:
//
//  1. the templates, which are the author's from the moment they land;
//  2. consumed_gen.go, rendered from api.consumes — which does not exist yet
//     under `codefly add solution`, so it is rendered for an empty
//     consumption and `codefly sync` fills it in (consumption.go says why);
//  3. the interface document, rendered by the solution runtime's own renderer
//     so the committed bytes are the bytes the scaffolded generator writes;
//  4. CreateResponse, which saves the service manifest.
//
// Nothing here declares an endpoint, and that is deliberate. `codefly add
// solution` writes the one endpoint a solution backend serves — public,
// `exposure: none`, with the /health readiness probe this runtime answers —
// together with module-identity and the authority dependencies, and it writes
// them after this returns. An endpoint declared here would be replaced by an
// identical one a moment later, which reads as a disagreement between the
// agent and the CLI where there is none. A solution backend added with
// `codefly add service --agent solution-go`, outside `add solution`, is told
// what to declare by the GETTING_STARTED this Create writes.
func (s *Builder) Create(ctx context.Context, _ *builderv0.CreateRequest) (*builderv0.CreateResponse, error) {
	defer s.Wool.Catch()
	ctx = s.Wool.Inject(ctx)

	context, err := newScaffold(s.Information)
	if err != nil {
		return s.Base.Builder.CreateError(err)
	}
	if err := s.scaffoldSolution(ctx, context); err != nil {
		return s.Base.Builder.CreateError(err)
	}
	if err := s.writeGeneratedConsumption(ctx); err != nil {
		return s.Base.Builder.CreateError(err)
	}
	if err := s.writeInterfaceArtifact(ctx, context, s.serviceVersion()); err != nil {
		return s.Base.Builder.CreateError(err)
	}
	return s.Base.Builder.CreateResponse(ctx, s.service.Settings)
}

// Sync regenerates what a solution's own declarations project into the
// backend: consumed_gen.go from api.consumes, and the interface document from
// the routes the backend serves.
//
// The generic Go agent's Sync is a no-op — plain Go has nothing to
// regenerate. A solution backend has exactly two derived files, and both go
// stale on a change to a document the CLI owns rather than to any Go source,
// so there is no build that would notice.
//
// The interface document is NOT re-rendered here. Once a backend serves
// routes of its own, only the backend itself can render it — the document is
// rendered from the running declaration — so the scaffolded openapi.go
// carries a `go:generate` line and a test that fails on a stale artifact, and
// `go generate ./...` in the service directory is what moves it.
func (s *Builder) Sync(ctx context.Context, _ *builderv0.SyncRequest) (*builderv0.SyncResponse, error) {
	defer s.Wool.Catch()
	ctx = s.Wool.Inject(ctx)

	if err := s.writeGeneratedConsumption(ctx); err != nil {
		return s.Base.Builder.SyncError(err)
	}
	return s.Base.Builder.SyncResponse()
}

// serviceVersion is the version the service manifest declares, which the
// interface document carries. `codefly add service` creates a service at
// 0.0.0 and the solution runtime refuses a document with no version, so the
// zero value is spelled rather than left empty.
func (s *Builder) serviceVersion() string {
	if s.Information != nil && s.Information.Agent != nil && s.Base.Service != nil && s.Base.Service.Version != "" {
		return s.Base.Service.Version
	}
	return "0.0.0"
}
