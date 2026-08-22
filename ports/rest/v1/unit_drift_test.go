// SPDX-License-Identifier: CC0-1.0

package v1_test

import (
	"fmt"
	"sort"
	"testing"

	"github.com/m4schini/splitkauf/lists"
	v1 "github.com/m4schini/splitkauf/ports/rest/v1"
)

// TestUnitEnumMatchesDomain is the spec<->domain drift guard for the item unit
// tokens. lists.Units() is the single source of truth; the OpenAPI Unit schema
// (and, transitively, the items.unit CHECK constraint that mirrors it) must
// carry exactly the same tokens. Parsing the embedded spec via GetSwagger and
// comparing the sets means a change to one side without the other fails CI
// rather than diverging silently.
func TestUnitEnumMatchesDomain(t *testing.T) {
	t.Parallel()

	spec, err := v1.GetSwagger()
	if err != nil {
		t.Fatalf("GetSwagger: %v", err)
	}

	schema, ok := spec.Components.Schemas["Unit"]
	if !ok || schema.Value == nil {
		t.Fatal("spec has no Unit schema")
	}

	specUnits := make([]string, 0, len(schema.Value.Enum))
	for _, raw := range schema.Value.Enum {
		s, ok := raw.(string)
		if !ok {
			t.Fatalf("Unit enum value %v is not a string", raw)
		}

		specUnits = append(specUnits, s)
	}

	domainUnits := lists.Units()

	if got := normalise(specUnits); got != normalise(domainUnits) {
		t.Errorf("Unit enum drift:\n  spec:   %v\n  domain: %v", specUnits, domainUnits)
	}

	// The schema default must also be the domain default ("amount").
	if def, _ := schema.Value.Default.(string); def != "amount" {
		t.Errorf("Unit schema default = %q, want amount", def)
	}
}

// normalise renders a token slice order-insensitively for set comparison.
func normalise(in []string) string {
	cp := make([]string, len(in))
	copy(cp, in)
	sort.Strings(cp)

	return fmt.Sprintf("%v", cp)
}
