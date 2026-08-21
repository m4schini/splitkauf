// SPDX-License-Identifier: CC0-1.0

package rest

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/m4schini/splitkauf/telemetry"
)

// openAPISpec holds the raw OpenAPI spec bytes, registered via SetOpenAPISpec.
var openAPISpec []byte

// SetOpenAPISpec registers the OpenAPI spec served at /openapi.yaml and
// referenced by the /.well-known/api-catalog endpoint. Must be called before
// Serve.
func SetOpenAPISpec(spec []byte) {
	openAPISpec = spec
}

const catalogContentType = `application/linkset+json; profile="https://www.rfc-editor.org/info/rfc9727"`

// ApiDocsHandler returns an HTTP handler that serves the RFC 9727 API catalog,
// the raw OpenAPI spec (YAML and JSON), and the Scalar human-readable docs UI.
// It is mounted at "/" by the root router so all paths below are absolute.
func ApiDocsHandler() http.Handler {
	r := chi.NewRouter()
	r.Get("/.well-known/api-catalog", apiCatalogHandler())
	r.Get("/openapi.yaml", openAPISpecHandler())
	r.Get("/openapi.json", openAPISpecJSONHandler())
	r.Get("/docs", docsHandler())
	r.Get("/problems/{slug}", problemPageHandler())
	return r
}

type linksetDoc struct {
	Linkset []linksetEntry `json:"linkset"`
}

type linksetEntry struct {
	Anchor      string       `json:"anchor"`
	Item        []linkTarget `json:"item,omitempty"`
	ServiceDesc []linkTarget `json:"service-desc,omitempty"`
	ServiceDoc  []linkTarget `json:"service-doc,omitempty"`
}

type linkTarget struct {
	Href  string `json:"href"`
	Type  string `json:"type,omitempty"`
	Title string `json:"title,omitempty"`
}

// apiCatalogHandler serves the RFC 9727 well-known API catalog.
func apiCatalogHandler() http.HandlerFunc {
	log := telemetry.Logger("api", "catalog")
	return func(w http.ResponseWriter, r *http.Request) {
		base := requestBaseURL(r)
		catalog := linksetDoc{
			Linkset: []linksetEntry{
				{
					Anchor: base + "/.well-known/api-catalog",
					Item: []linkTarget{
						{Href: base + "/api/v1", Title: "API"},
					},
				},
				{
					Anchor: base + "/api/v1",
					ServiceDesc: []linkTarget{
						{Href: base + "/openapi.yaml", Type: "application/yaml"},
						{Href: base + "/openapi.json", Type: "application/json"},
					},
					ServiceDoc: []linkTarget{
						{Href: base + "/docs", Type: "text/html"},
					},
				},
			},
		}
		w.Header().Set("Content-Type", catalogContentType)
		w.WriteHeader(http.StatusOK)
		err := json.NewEncoder(w).Encode(catalog)
		if err != nil {
			log.Error("serving api-catalog", zap.Error(err))
		}
	}
}

// openAPISpecHandler serves the raw OpenAPI spec at /openapi.yaml.
func openAPISpecHandler() http.HandlerFunc {
	log := telemetry.Logger("api", "spec")
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write(openAPISpec)
		if err != nil {
			log.Error("serving open api spec", zap.String("format", "yaml"), zap.Error(err))
		}
	}
}

// openAPISpecJSONHandler serves the OpenAPI spec converted to JSON at /openapi.json.
func openAPISpecJSONHandler() http.HandlerFunc {
	log := telemetry.Logger("api", "spec")
	return func(w http.ResponseWriter, r *http.Request) {
		j, err := yamlToJSON(openAPISpec)
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err = w.Write(j)
		if err != nil {
			log.Error("serving open api spec", zap.String("format", "json"), zap.Error(err))
		}
	}
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	} else if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return scheme + "://" + r.Host
}

func yamlToJSON(src []byte) ([]byte, error) {
	var data any
	if err := yaml.Unmarshal(src, &data); err != nil {
		return nil, err
	}
	return json.MarshalIndent(data, "", "  ")
}
