// SPDX-License-Identifier: CC0-1.0

package rest

import (
	"html/template"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/m4schini/splitkauf/ports/rest/problem"
	"github.com/m4schini/splitkauf/telemetry"
)

// problemPageTemplate renders the human-readable explanation page for one
// problem type. Minimal markup, no styling framework — consistent with the
// lightweight docs approach.
var problemPageTemplate = template.Must(template.New("problem").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} — Splitkauf problem</title>
</head>
<body>
<main>
<h1>{{.Title}} <small>(HTTP {{.Status}})</small></h1>
<p><code>/problems/{{.Slug}}</code></p>
<p>{{.Description}}</p>
</main>
</body>
</html>
`))

// notFoundPageTemplate is served for an unknown problem slug.
var notFoundPageTemplate = template.Must(template.New("problem-404").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Unknown problem type — Splitkauf</title>
</head>
<body>
<main>
<h1>Unknown problem type <small>(HTTP 404)</small></h1>
<p>No problem type is registered for <code>/problems/{{.}}</code>.</p>
</main>
</body>
</html>
`))

// problemPageHandler renders the explanation page for the problem type matching
// the {slug} path parameter. An unknown slug yields a 404 HTML page.
func problemPageHandler() http.HandlerFunc {
	log := telemetry.Logger("api", "problems")

	bySlug := make(map[string]problem.Type, len(problem.Types()))
	for _, t := range problem.Types() {
		bySlug[t.Slug] = t
	}

	return func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		t, ok := bySlug[slug]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			if err := notFoundPageTemplate.Execute(w, slug); err != nil {
				log.Error("rendering unknown problem page", zap.String("slug", slug), zap.Error(err))
			}
			return
		}

		w.WriteHeader(http.StatusOK)
		if err := problemPageTemplate.Execute(w, t); err != nil {
			log.Error("rendering problem page", zap.String("slug", slug), zap.Error(err))
		}
	}
}
