# Working in this repository

`solution-go` is a `codefly:service` agent. It scaffolds a **solution
backend** — the entry service of a solution, built on
`github.com/codefly-dev/solution-runtime-go` — where the generic `go` agent
scaffolds a plain Go service.

## base/ is hand-written. templates/factory/ is GENERATED. Never edit a template.

This is the authoring model of every service agent, and the single most
important thing to know before changing anything here.

```
base/                 a REAL, COMPILING, TESTED solution backend  <- edit this
templates/factory/    generated from base/                        <- never edit
```

`templates/factory/` is produced by core's
`generator.GenerateServiceTemplate` — `codefly agent generate --service .` —
which copies every file in `base/`, adds a `.tmpl` suffix, and applies the
literal replacements in `base/service.generation.codefly.yaml`.

**Why, and not just convention.** A template cannot be compiled, vetted or
tested, so hand-written templates let you ship a scaffold that does not build
and nothing notices until someone scaffolds it. `base/` is a real service:
`go build`, `go vet` and `go test` run against it in CI
(`TestBaseCompilesAndPassesItsOwnSuite`), and **its own test suite is the suite
every scaffolded solution ships with**. The thing this agent scaffolds is
known-good before anybody scaffolds it.

### To change what a solution is scaffolded with

1. Edit `base/`. Keep it compiling: `cd base && GOWORK=off go build ./...`.
2. Regenerate and refresh the derived files:
   ```
   GOWORK=off go test -update ./...
   ```
   That one flag rewrites `base/consumed_gen.go`, `base/openapi/api.swagger.json`
   and all of `templates/factory/`, so `base/` and the templates can never be
   refreshed one without the other.
3. `GOWORK=off go test ./...`

`TestFactoryTemplatesAreGeneratedFromBase` fails on any difference between
`templates/factory/` and what `base/` generates, in both directions — a stale
template whose source left `base/` is caught too, because regeneration
overwrites a file but never deletes one. An edit made to a template instead of
to `base/` is reverted by the next regeneration and fails the suite until it is.

### The replacements are literal and whole-file

`base/` is a real solution named `codefly-base`, spelled three ways, each
derived from the name exactly as `scaffold.go` derives it for a real solution:

| in `base/` | becomes | derived by |
|---|---|---|
| `codefly-base` | `{{ .Module.Name.LowerCase }}` | core's own `Information` |
| `Codefly Base` | `{{ .Title }}` | `scaffold.go` `titleFrom` |
| `CodeflyBase` | `{{ .OperationPrefix }}` | `scaffold.go` `aliasField` |

Order matters and no `from` may be a substring of an earlier `to`.
`TestGeneratedTemplatesCarryNoBaseSolutionName` fails if any spelling of
`codefly-base` survives into a template, which is what catches a new
capitalisation introduced in `base/` that no replacement covers.

**The solution id is spelled `.Module.Name.LowerCase`, never `.SolutionID`.**
They are the same value — `scaffold.go` derives `SolutionID` from exactly that
field — but not renderable in the same places. Two files in a generated tree
are rendered by CORE rather than by this agent, each with a bare
`*services.Information` that carries only `Service`, `Module` and `Agent`:

| rendered by core | when | where |
|---|---|---|
| `templates/factory/GETTING_STARTED.md` | builder Load, creation mode | `core/agents/services/base_builder.go` |
| `templates/agent/README.md` | `GetAgentInformation`, before any service exists | this repo's `service.go` |

`{{ .SolutionID }}` there is not an empty render but a hard failure — *can't
evaluate field SolutionID in type \*services.Information* — and
`codefly add solution` dies with the module already written. The agent README
takes no values at all for the same reason: at advertisement time `Service` and
`Module` are themselves nil. `advertisement_test.go` renders both the way core
renders them.

`backend` is **not** replaced. A solution's entry service is named `backend` by
the shape's own definition — the CLI's `solutionEntryService` and this agent's
`backendService` are both that constant — so it is not a parameter, and it
appears in prose on nearly every line (`a solution backend`, `the compiled
backend`) which a whole-file substitution would corrupt.

## The other template trees are not generated

| tree | generated? | why |
|---|---|---|
| `templates/factory/` | yes, from `base/` | the service directory |
| `templates/page/` | **no — hand-written** | the `./Page` npm remote is not a Go service, so `GenerateServiceTemplate` does not produce it, and it is written to a SIBLING directory (`services/fe-remote`) rather than into the service. See `main.go`'s `pageFS`. |
| `templates/builder/` | no | Dockerfile and dockerignore, rendered with the builder's own context |
| `templates/deployment/` | no | kustomize base and overlays, rendered with the deployment context |
| `templates/agent/` | no | this agent's own README, not part of a scaffolded tree |

## What this agent must never scaffold

`codefly add solution` (codefly-dev/cli#1216) writes the solution's
declarations **after** this agent's `Create` returns. Anything written here in
those places is overwritten silently, by landing first:

- `module-identity`, the endpoints, and the only two `service-dependencies` a
  solution backend has (the authority interface plus `authority/hop` as
  `kind: external`);
- the five documents a solution is installed from.

**In particular, never a `service-dependency` on a consumed module.** A
solution reaches every module through the authority's hop, which *checks* the
call; Codefly renders a `service-dependency` into a mesh allow rule straight to
that module's service, past the check, and `check_solution_mesh.py` refuses it
(SP-SOL-04). `shape_test.go` holds the agent to all of this, and the files in
`base/` that carry the CLI's declarations are listed under `ignores` in
`base/service.generation.codefly.yaml` so they are never templatized.

## Build and test

| what | command |
|---|---|
| the agent | `GOWORK=off go build ./... && GOWORK=off go vet ./...` |
| everything | `GOWORK=off go test ./...` |
| refresh generated files | `GOWORK=off go test -update ./...` |
| `base/` alone | `cd base && GOWORK=off go build ./...` |

`base/` is a nested module, so the root `./...` does not include it;
`TestBaseCompilesAndPassesItsOwnSuite` builds, vets and tests it by staging it
into the layout a real solution has (`<root>/services/backend` beside
`<root>/solution.codefly.yaml`), which is where its `consumes_test.go` really
finds the solution manifest.
