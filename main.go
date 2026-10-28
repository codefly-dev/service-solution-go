// Binary service-solution-go is the solution specialization of the generic Go
// agent: the `solution-go` service agent. It scaffolds, builds, runs and
// deploys a SOLUTION BACKEND — the entry service of a solution, built on
// github.com/codefly-dev/solution-runtime-go — where the `go` agent scaffolds
// a plain Go service that knows nothing about any of it.
//
// It is a codefly:service agent and nothing else. Services already have a
// protocol (the agent binary, driven by `services.Add`), so there is no core
// change here, no new wire protocol, and no codefly:solution executor: that
// option was weighed and declined in codefly-dev/cli#1215, and
// `codefly add solution --agent solution-go` reaches this binary through the
// path every other service agent is reached through.
//
// Everything that is not solution-specific is the generic Go agent's, imported
// from github.com/codefly-dev/service-go: the Runtime, the Code and Tooling
// servers, and the Builder's Load / Init / Build / Deploy. What this binary
// adds is the Create flow (builder.go, scaffold.go) and the agent's own
// advertisement (service.go).
package main

import (
	"embed"

	"github.com/codefly-dev/core/agents"
	"github.com/codefly-dev/core/builders"
	"github.com/codefly-dev/core/resources"
	"github.com/codefly-dev/core/shared"
	"github.com/codefly-dev/core/toolbox/lang"

	gocode "github.com/codefly-dev/service-go/pkg/code"
	goruntime "github.com/codefly-dev/service-go/pkg/runtime"
	gotooling "github.com/codefly-dev/service-go/pkg/tooling"
)

// agent is this agent's identity, read from the manifest beside this file so
// the version is declared once and `codefly publish` moves it in one place.
var agent = shared.Must(resources.LoadFromFs[resources.Agent](shared.Embed(infoFS)))

// requirements are the files a change to which makes a build stale. A solution
// backend keeps its Go sources at the service root (spec.source-dir: ".", as
// every solution in obin-ai/core-solutions does), so the selector is rooted
// there rather than at a `code/` subdirectory the way the generic Go agent's
// is.
var requirements = builders.NewDependencies(agent.Name,
	builders.NewDependency("service.codefly.yaml"),
	builders.NewDependency("solution.frontend.codefly.yaml"),
	builders.NewDependency(".").WithPathSelect(shared.NewSelect("*.go")),
)

func main() {
	svc := NewService()

	// Code, Tooling and Runtime are inherited wholesale: a solution backend is
	// an ordinary Go program to a compiler, a formatter and a process manager,
	// and a second copy of any of them here would be a second mechanism for a
	// job the generic Go agent already does.
	code := gocode.New(svc.Service)
	rt := goruntime.New(svc.Service)
	tooling := gotooling.New(code, rt)

	agents.Serve(agents.PluginRegistration{
		Agent:   svc,
		Runtime: rt,
		Builder: NewBuilder(svc),
		Code:    code,
		Tooling: tooling,
		Toolbox: lang.NewToolboxFromTooling(agent.Name, agent.Version, tooling),
	})
}

//go:embed agent.codefly.yaml
var infoFS embed.FS

//go:embed templates/agent
var readmeFS embed.FS

// factoryFS is the solution backend: what Create writes into the service
// directory.
//
//go:embed all:templates/factory
var factoryFS embed.FS

// pageFS is the ./Page remote: what Create writes into services/fe-remote,
// beside the backend. `codefly add solution` deliberately does not write it —
// it is an npm package, not a Codefly service — and nothing else does either,
// which is why a solution's page has been hand-copied out of a sibling every
// time (codefly-dev/cli#1217).
//
//go:embed all:templates/page
var pageFS embed.FS

//go:embed templates/builder
var builderFS embed.FS

//go:embed templates/deployment
var deploymentFS embed.FS
