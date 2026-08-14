// SPDX-License-Identifier: CC0-1.0

package rest

import (
	"fmt"
	"net/http"

	scalargo "github.com/bdpiprava/scalar-go"
	"github.com/m4schini/splitkauf/telemetry"
	"go.uber.org/zap"
)

func docsHandler() http.HandlerFunc {
	log := telemetry.Logger("api", "docs")
	docsHTML, err := scalargo.NewV2(
		scalargo.WithSpecBytes(openAPISpec),
	)
	if err != nil {
		panic(fmt.Sprintf("scalar-go: failed to render docs: %v", err))
	}

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, err := fmt.Fprint(w, docsHTML)
		if err != nil {
			log.Error("serving scalar docs", zap.Error(err))
		}
	}
}
