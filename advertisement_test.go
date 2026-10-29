package main

import (
	"context"
	"strings"
	"testing"

	"github.com/codefly-dev/core/agents/services"
	agentv0 "github.com/codefly-dev/core/generated/go/codefly/services/agent/v0"
	"github.com/codefly-dev/core/resources"
	"github.com/codefly-dev/core/shared"
	"github.com/codefly-dev/core/templates"
	"github.com/stretchr/testify/require"
)

// THE ADVERTISEMENT IS ASKED FOR BEFORE A SERVICE EXISTS, and that is the
// whole reason this file is here.
//
// `codefly add service` calls GetAgentInformation to print what the agent
// scaffolds — at that moment nothing has been created, so core has only run
// the agent's Init and s.Information carries the AGENT and nothing else
// (core/agents/services/base.go: `s.Information = &Information{Agent:
// s.Agent}`). Service and Module are nil until Load runs against a service
// directory that exists.
//
// A README template that reaches for {{ .Service.Name.Title }} therefore does
// not render a blank — it fails the whole call with "nil pointer evaluating
// *resources.ServiceWithCase.Name", and `codefly add solution` dies after
// having already written the module, leaving a half-built tree behind. This
// agent shipped exactly that, and every test here passed: they all supply an
// Information built from a real identity, which is the state at Create time
// and NOT the state at advertisement time.
func TestTheAdvertisementRendersBeforeAnyServiceExists(t *testing.T) {
	// NewService and nothing else: the state core leaves the agent in when it
	// asks what the agent is, before Load.
	svc := NewService()
	require.Nil(t, svc.Base.Information,
		"this test is only honest while the agent carries no service here; if core now supplies one, make this test build the state it supplies")

	information, err := svc.GetAgentInformation(context.Background(), &agentv0.AgentInformationRequest{})
	require.NoError(t, err, "GetAgentInformation must render with no service loaded: this is what `codefly add service` calls first")
	require.NotEmpty(t, information.ReadMe)
	require.Contains(t, information.ReadMe, agent.Name,
		"the README a caller is shown must name the agent it is reading about")
}

// TestTheAgentReadmeNeedsNoServiceContext is the same rule one step earlier,
// stated against the template rather than the call: the agent README describes
// THE AGENT, so a service-shaped or module-shaped value in it has no source to
// come from. Reading the template says why the test above holds, rather than
// leaving it a fact about one rendering.
func TestTheAgentReadmeNeedsNoServiceContext(t *testing.T) {
	raw, err := readmeFS.ReadFile("templates/agent/README.md.tmpl")
	require.NoError(t, err)
	content := string(raw)
	for _, reach := range []string{".Service", ".Module", ".Information"} {
		require.NotContains(t, content, reach,
			"templates/agent/README.md.tmpl is rendered with the agent's own Information, where %s is nil until a service is loaded", reach)
	}
	require.NotContains(t, content, "{{",
		"the agent README takes no values at all: keep it prose, so there is nothing left to be nil")
	require.True(t, strings.Contains(content, agent.Name),
		"the agent README must name the agent")
}

// THE SECOND THING CORE RENDERS WITH A BARE Information, and the one that
// cost a half-built module.
//
// templates/factory/GETTING_STARTED.md is rendered TWICE, by two callers with
// two different contexts:
//
//   - by this agent's Create, with the scaffold context, into the service
//     directory (scaffold.go);
//   - by CORE, at builder Load in creation mode, with s.Information and
//     nothing else, to print to the author — core/agents/services/
//     base_builder.go does it by that exact path, on every agent.
//
// So this one template is constrained to the fields Information carries. A
// `{{ .SolutionID }}` in it is not an empty render: text/template fails the
// call with "can't evaluate field SolutionID in type *services.Information",
// core turns that into "builder load failed", and `codefly add solution` dies
// AFTER `add module` has already written the module — the half-built tree the
// command's own design goes out of its way to avoid.
//
// base/service.generation.codefly.yaml therefore spells the solution id
// `.Module.Name.LowerCase`, which both contexts carry. This test is what holds
// it there.
func TestTheGettingStartedRendersTheWayCoreRendersIt(t *testing.T) {
	// Exactly core's call: same FS, same path, same context type.
	information := &services.Information{
		Service: resources.ToServiceWithCase(&resources.ServiceIdentity{Name: backendService, Module: "notes", Workspace: "workspace"}),
		Module:  resources.ToModuleWithCase(&resources.ServiceIdentity{Name: backendService, Module: "notes", Workspace: "workspace"}),
		Agent:   agent,
	}
	rendered, err := templates.ApplyTemplateFrom(context.Background(), shared.Embed(factoryFS),
		"templates/factory/GETTING_STARTED.md", information)
	require.NoError(t, err,
		"core renders this template with a bare *services.Information at builder Load: a field Information does not carry fails `codefly add solution` after the module is already written")

	// A render that succeeds but says nothing is the other way to be wrong:
	// the solution's name is what the document is about.
	require.Contains(t, rendered, "notes", "the rendered document must name the solution")
	require.NotContains(t, rendered, "<no value>", "every action must resolve under Information, not render empty")
}
