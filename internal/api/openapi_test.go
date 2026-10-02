package api

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/anand34577/docveta/internal/platform/config"
)

type recordingRouter []string

func (r *recordingRouter) HandleFunc(pattern string, _ func(http.ResponseWriter, *http.Request)) {
	*r = append(*r, pattern)
}

// TestOpenAPIRoutes fails when a route is added or removed without updating openapi.yaml.
func TestOpenAPIRoutes(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(OpenAPISpec)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("openapi.yaml is invalid: %v", err)
	}
	var inSpec []string
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			inSpec = append(inSpec, method+" "+path)
		}
	}

	u, _ := url.Parse("https://docs.example.com")
	var rec recordingRouter
	New(Deps{Cfg: &config.Config{BaseURL: u}}).Register(&rec)
	var inCode []string
	for _, p := range rec {
		method, path, ok := strings.Cut(p, " ")
		if !ok { // catch-all 404 handlers
			continue
		}
		inCode = append(inCode, method+" "+path)
	}

	for _, r := range inCode {
		if !slices.Contains(inSpec, r) {
			t.Errorf("route %s is missing from openapi.yaml", r)
		}
	}
	for _, r := range inSpec {
		if !slices.Contains(inCode, r) {
			t.Errorf("openapi.yaml documents %s, which isn't registered", r)
		}
	}
}
