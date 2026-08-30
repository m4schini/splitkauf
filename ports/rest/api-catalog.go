// SPDX-License-Identifier: CC0-1.0

// Package rest implements the HTTP port: v1 API mounting, docs, health, SSE
// and the RFC 9727 API catalog.
package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/m4schini/splitkauf/telemetry"
)

// openAPISpec holds the raw OpenAPI spec bytes, registered via SetOpenAPISpec.
//
//nolint:gochecknoglobals // registered once at startup via SetOpenAPISpec; the spec is embedded in package main
var openAPISpec []byte

// SetOpenAPISpec registers the OpenAPI spec served at /openapi.yaml and
// referenced by the /.well-known/api-catalog endpoint. Must be called before
// Serve.
func SetOpenAPISpec(spec []byte) {
	openAPISpec = spec
}

const catalogContentType = `application/linkset+json; profile="https://www.rfc-editor.org/info/rfc9727"`

// APIDocsHandler returns an HTTP handler that serves the RFC 9727 API catalog,
// the raw OpenAPI spec (YAML and JSON), and the Scalar human-readable docs UI.
// It is mounted at "/" by the root router so all paths below are absolute.
func APIDocsHandler() http.Handler {
	router := chi.NewRouter()
	router.Get("/.well-known/api-catalog", apiCatalogHandler())
	router.Get("/openapi.yaml", openAPISpecHandler())
	router.Get("/openapi.json", openAPISpecJSONHandler())
	router.Get("/docs", docsHandler())
	router.Get("/problems/{slug}", problemPageHandler())

	return router
}

type linksetDoc struct {
	Linkset []linksetEntry `json:"linkset"`
}

type linksetEntry struct {
	Anchor      string       `json:"anchor"`
	Item        []linkTarget `json:"item,omitempty"`
	ServiceDesc []linkTarget `json:"service-desc,omitempty"` //nolint:tagliatelle // RFC 8631 kebab-case relation name
	ServiceDoc  []linkTarget `json:"service-doc,omitempty"`  //nolint:tagliatelle // RFC 8631 kebab-case relation name
}

type linkTarget struct {
	Href  string `json:"href"`
	Type  string `json:"type,omitempty"`
	Title string `json:"title,omitempty"`
}

// apiCatalogHandler serves the RFC 9727 well-known API catalog.
func apiCatalogHandler() http.HandlerFunc {
	log := telemetry.Logger("api", "catalog")

	return func(writer http.ResponseWriter, req *http.Request) {
		base := requestBaseURL(req)
		catalog := linksetDoc{
			Linkset: []linksetEntry{
				{
					Anchor: base + "/.well-known/api-catalog",
					Item: []linkTarget{
						{Href: base + "/api/v1", Type: "", Title: "API"},
					},
					ServiceDesc: nil,
					ServiceDoc:  nil,
				},
				{
					Anchor: base + "/api/v1",
					Item:   nil,
					ServiceDesc: []linkTarget{
						{Href: base + "/openapi.yaml", Type: "application/yaml", Title: ""},
						{Href: base + "/openapi.json", Type: "application/json", Title: ""},
					},
					ServiceDoc: []linkTarget{
						{Href: base + "/docs", Type: "text/html", Title: ""},
					},
				},
			},
		}

		writer.Header().Set("Content-Type", catalogContentType)
		writer.WriteHeader(http.StatusOK)

		err := json.NewEncoder(writer).Encode(catalog)
		if err != nil {
			log.Error("serving api-catalog", zap.Error(err))
		}
	}
}

// openAPISpecHandler serves the raw OpenAPI spec at /openapi.yaml.
func openAPISpecHandler() http.HandlerFunc {
	log := telemetry.Logger("api", "spec")

	return func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/yaml")
		writer.WriteHeader(http.StatusOK)

		_, err := writer.Write(openAPISpec)
		if err != nil {
			log.Error("serving open api spec", zap.String("format", "yaml"), zap.Error(err))
		}
	}
}

// openAPISpecJSONHandler serves the OpenAPI spec converted to JSON at /openapi.json.
func openAPISpecJSONHandler() http.HandlerFunc {
	log := telemetry.Logger("api", "spec")

	return func(writer http.ResponseWriter, _ *http.Request) {
		jsonSpec, err := yamlToJSON(openAPISpec)
		if err != nil {
			http.Error(writer, "internal server error", http.StatusInternalServerError)

			return
		}

		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)

		_, err = writer.Write(jsonSpec)
		if err != nil {
			log.Error("serving open api spec", zap.String("format", "json"), zap.Error(err))
		}
	}
}

func requestBaseURL(req *http.Request) string {
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	} else if proto := req.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}

	return scheme + "://" + req.Host
}

// errNonStringMappingKey reports a YAML mapping whose key is not a string
// ("1: a"). JSON object member names are strings, so such a document has no
// faithful JSON form.
var errNonStringMappingKey = errors.New("openapi spec has a non-string mapping key")

// jsonObjectKeys walks a value decoded from YAML and rejects any mapping that
// yaml.v3 handed back as map[any]any — its fallback for a mapping with a
// non-string key.
//
// The check is explicit rather than left to the JSON encoder because the
// encoder's answer is not stable: encoding/json v1 refuses a non-string map
// key outright, while v2 stringifies integer and boolean keys and would
// silently serve "1" as an object member name. The spec is served as-is or
// not at all, so the conversion fails here either way.
func jsonObjectKeys(value any) error {
	switch typed := value.(type) {
	case map[any]any:
		for key := range typed {
			return fmt.Errorf("%w: %v (%T)", errNonStringMappingKey, key, key)
		}

		return nil
	case map[string]any:
		for _, nested := range typed {
			if err := jsonObjectKeys(nested); err != nil {
				return err
			}
		}

		return nil
	case []any:
		for _, nested := range typed {
			if err := jsonObjectKeys(nested); err != nil {
				return err
			}
		}

		return nil
	default:
		return nil
	}
}

func yamlToJSON(src []byte) ([]byte, error) {
	var data any
	if err := yaml.Unmarshal(src, &data); err != nil {
		return nil, fmt.Errorf("unmarshalling openapi spec yaml: %w", err)
	}

	if err := jsonObjectKeys(data); err != nil {
		return nil, fmt.Errorf("marshalling openapi spec to json: %w", err)
	}

	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling openapi spec to json: %w", err)
	}

	return out, nil
}
