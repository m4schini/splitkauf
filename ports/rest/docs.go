// SPDX-License-Identifier: CC0-1.0

package rest

import (
	"fmt"
	"net/http"

	scalargo "github.com/bdpiprava/scalar-go"
	"go.uber.org/zap"

	"github.com/m4schini/splitkauf/telemetry"
)

func docsHandler() http.HandlerFunc {
	log := telemetry.Logger("api", "docs")

	return func(writer http.ResponseWriter, _ *http.Request) {
		docsHTML, err := scalargo.NewV2(
			scalargo.WithSpecBytes(openAPISpec),
		)
		if err != nil {
			log.Error("rendering scalar docs", zap.Error(err))
			http.Error(writer, "internal server error", http.StatusInternalServerError)

			return
		}

		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.WriteHeader(http.StatusOK)

		_, err = fmt.Fprint(writer, docsHTML)
		if err != nil {
			log.Error("serving scalar docs", zap.Error(err))
		}
	}
}
