package main

import (
	"context"
	"net/http"

	solution "github.com/codefly-dev/solution-runtime-go"
)

// scaffoldedOperations is what a solution this agent scaffolds SERVES, as a
// declaration this module can render without compiling the scaffolded tree.
//
// It mirrors base/operations.go, and it has to exist because the two live in
// different Go modules: base/ is the scaffolded backend's source and cannot
// be imported from here, but the interface document this agent writes at
// Create must list exactly the routes that source serves. Render it from an
// empty server and the scaffolded tree ships a document saying it serves
// nothing while its code serves a route — the authority then derives no check
// row for a route that is being served, and the scaffolded
// TestInterfaceArtifactIsCurrent fails on a tree nobody has touched.
//
// THE DRIFT IS GUARDED, which is the only reason a second declaration is
// tolerable here: TestBaseInterfaceDocumentIsTheRuntimesRender renders this
// and compares it byte for byte against base/openapi/api.swagger.json, which
// base's own `go generate` produces from base/operations.go. Change one
// without the other and that test fails.
func scaffoldedOperations(solutionID string) []solution.Operation {
	route := "/v1/" + solutionID
	return []solution.Operation{{
		Path:     route + "/overview",
		Method:   http.MethodGet,
		Summary:  "report what this solution is and which modules it can reach",
		Callers:  "This solution's own page, to show that it is installed and reachable as the viewer",
		Behavior: "Answers the solution's id and the `as` name of every module this backend relays, in the order they are declared. Takes no parameters and reads nothing outside this backend",
		Response: overviewResponse{},
		Policy:   solution.Requires(solutionID+".page", "read"),
		Handler: func(context.Context, *solution.Gateway) (any, error) {
			// Never called: this declaration exists to be RENDERED. The
			// scaffolded tree's own handler is base/operations.go's.
			return overviewResponse{}, nil
		},
	}}
}

// overviewResponse mirrors base/operations.go's, and must stay identical: the
// document's schema is derived from this type, and the comparison against
// base's committed render is what catches a field added to one and not the
// other.
type overviewResponse struct {
	Solution string   `json:"solution"`
	Consumes []string `json:"consumes"`
}
