// SPDX-License-Identifier: CC0-1.0

package lists

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	t.Parallel()

	const (
		emptyMessage   = "name must not be empty"
		tooLongMessage = "name is too long"
	)

	// maxNameLength is a byte cap, so build fixtures whose byte length differs
	// from their rune count to pin that down.
	name200 := strings.Repeat("a", 200)
	name201 := strings.Repeat("a", 201)
	twoByte200 := strings.Repeat("ä", 100)  // 100 runes, 200 bytes
	threeByte201 := strings.Repeat("世", 67) //nolint:gosmopolitan // picked for its 3-byte UTF-8 width, not as text

	if len(twoByte200) != 200 || len(threeByte201) != 201 {
		t.Fatalf("bad fixtures: len(twoByte200)=%d, len(threeByte201)=%d",
			len(twoByte200), len(threeByte201))
	}

	tests := []struct {
		name string
		// input is passed to validateName.
		input string
		// want is the expected trimmed name; only checked when wantErr is empty.
		want string
		// wantErr is the expected *ValidationError message; empty means success.
		wantErr string
	}{
		{
			name:  "plain name is returned unchanged",
			input: "Groceries",
			want:  "Groceries",
		},
		{
			name:  "surrounding whitespace is trimmed",
			input: "  \t Groceries \n",
			want:  "Groceries",
		},
		{
			name:  "interior whitespace is preserved",
			input: "  a  b  ",
			want:  "a  b",
		},
		{
			name:    "empty string is rejected",
			input:   "",
			wantErr: emptyMessage,
		},
		{
			name:    "spaces only is rejected",
			input:   "   ",
			wantErr: emptyMessage,
		},
		{
			name:    "mixed ascii whitespace only is rejected",
			input:   " \t\n\v\f\r ",
			wantErr: emptyMessage,
		},
		{
			name:    "unicode whitespace only is rejected",
			input:   " 　",
			wantErr: emptyMessage,
		},
		{
			name:  "exactly max length is accepted",
			input: name200,
			want:  name200,
		},
		{
			name:    "one byte over max length is rejected",
			input:   name201,
			wantErr: tooLongMessage,
		},
		{
			name:  "length is measured after trimming",
			input: "     " + name200 + "     ",
			want:  name200,
		},
		{
			name:  "multi byte runes at exactly max bytes are accepted",
			input: twoByte200,
			want:  twoByte200,
		},
		{
			name:    "multi byte runes over max bytes are rejected despite few runes",
			input:   threeByte201,
			wantErr: tooLongMessage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := validateName(tt.input)

			if tt.wantErr != "" {
				assertValidationError(t, err, "name")

				var validationErr *ValidationError
				if !errors.As(err, &validationErr) {
					t.Fatalf("expected *ValidationError, got %v", err)
				}

				if validationErr.Message != tt.wantErr {
					t.Fatalf("expected message %q, got %q", tt.wantErr, validationErr.Message)
				}

				if got != "" {
					t.Fatalf("expected empty name on error, got %q", got)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.want {
				t.Fatalf("expected name %q, got %q", tt.want, got)
			}
		})
	}
}

func TestNormalizeQuantity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		quantity int
		want     int
		wantErr  bool
	}{
		{
			name:     "zero defaults to one",
			quantity: 0,
			want:     1,
		},
		{
			name:     "one passes through",
			quantity: 1,
			want:     1,
		},
		{
			name:     "two passes through",
			quantity: 2,
			want:     2,
		},
		{
			name:     "large positive has no upper bound",
			quantity: math.MaxInt,
			want:     math.MaxInt,
		},
		{
			name:     "negative one is rejected",
			quantity: -1,
			want:     0,
			wantErr:  true,
		},
		{
			name:     "large negative is rejected",
			quantity: math.MinInt,
			want:     0,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeQuantity(tt.quantity)

			if tt.wantErr {
				assertValidationError(t, err, fieldQuantity)

				var validationErr *ValidationError
				if errors.As(err, &validationErr) && validationErr.Error() != "quantity must be at least 1" {
					t.Fatalf("expected message %q, got %q", "quantity must be at least 1", validationErr.Error())
				}
			} else if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if got != tt.want {
				t.Fatalf("expected quantity %d, got %d", tt.want, got)
			}
		})
	}
}

// TestNormalizeQuantityErrorFieldIsQuantityConstant pins the error's Field to
// the literal string the REST layer turns into a JSON pointer.
func TestNormalizeQuantityErrorFieldIsQuantityConstant(t *testing.T) {
	t.Parallel()

	_, err := normalizeQuantity(-5)

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected *ValidationError, got %v", err)
	}

	if validationErr.Field != "quantity" {
		t.Fatalf("expected field %q, got %q", "quantity", validationErr.Field)
	}
}

// TestValidateUnitDefaultsEmptyToAmount pins the one piece of normalisation
// validateUnit actually performs: an empty token becomes the default unit.
func TestValidateUnitDefaultsEmptyToAmount(t *testing.T) {
	t.Parallel()

	got, err := validateUnit("")
	if err != nil {
		t.Fatalf("validateUnit(%q) returned error: %v", "", err)
	}

	if got != "amount" {
		t.Fatalf("validateUnit(%q) = %q, want %q", "", got, "amount")
	}
}

// TestValidateUnitAcceptsCanonicalTokens checks that every token advertised by
// Units() round-trips unchanged, and that Units() still advertises exactly the
// documented set (so list drift is caught here rather than at the REST layer).
func TestValidateUnitAcceptsCanonicalTokens(t *testing.T) {
	t.Parallel()

	documented := []string{
		"amount", "g", "kg", "ml", "l", "pack",
		"bottle", "can", "jar", "cup", "bunch", "bag",
	}

	units := Units()

	if len(units) != len(documented) {
		t.Fatalf("Units() has %d tokens (%v), want %d (%v)", len(units), units, len(documented), documented)
	}

	for i, want := range documented {
		if units[i] != want {
			t.Fatalf("Units()[%d] = %q, want %q", i, units[i], want)
		}
	}

	for _, unit := range units {
		t.Run(unit, func(t *testing.T) {
			t.Parallel()

			got, err := validateUnit(unit)
			if err != nil {
				t.Fatalf("validateUnit(%q) returned error: %v", unit, err)
			}

			if got != unit {
				t.Fatalf("validateUnit(%q) = %q, want it unchanged", unit, got)
			}
		})
	}
}

// TestValidateUnitRejectsUnknownTokens covers the failure path: comparison is
// exact byte equality, so case variants, untrimmed whitespace, invisible
// characters and plausible-but-unknown aliases all fail.
func TestValidateUnitRejectsUnknownTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		unit string
	}{
		{name: "uppercase kg", unit: "KG"},
		{name: "mixed case kg", unit: "Kg"},
		{name: "uppercase litre", unit: "L"},
		{name: "uppercase amount", unit: "AMOUNT"},
		{name: "leading space", unit: " kg"},
		{name: "trailing space", unit: "kg "},
		{name: "tab only", unit: "\t"},
		{name: "space only", unit: " "},
		{name: "newline only", unit: "\n"},
		{name: "plausible piece", unit: "piece"},
		{name: "plausible stk", unit: "stk"},
		{name: "plural amounts", unit: "amounts"},
		{name: "spelled out grams", unit: "grams"},
		{name: "zero width space suffix", unit: "kg\u200b"},
		{name: "non breaking space prefix", unit: " kg"},
		{name: "cyrillic lookalike", unit: "кkg"},
		{name: "quoted token", unit: `"kg"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := validateUnit(tt.unit)
			if err == nil {
				t.Fatalf("validateUnit(%q) = %q, want a validation error", tt.unit, got)
			}

			if got != "" {
				t.Fatalf("validateUnit(%q) returned %q alongside an error, want empty string", tt.unit, got)
			}

			assertValidationError(t, err, "unit")

			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("expected *ValidationError, got %v", err)
			}

			const wantMessage = "unit is not a recognised value"
			if validationErr.Message != wantMessage {
				t.Fatalf("validateUnit(%q) message = %q, want %q", tt.unit, validationErr.Message, wantMessage)
			}
		})
	}
}

func TestNormalizeNote(t *testing.T) {
	t.Parallel()

	longNote := strings.Repeat("a very long note ", 500)

	tests := []struct {
		name     string
		nilInput bool
		input    string
		want     string
		wantNil  bool
	}{
		{
			name:     "nil pointer",
			nilInput: true,
			wantNil:  true,
		},
		{
			name:    "empty string",
			input:   "",
			wantNil: true,
		},
		{
			name:    "ascii whitespace only",
			input:   "  \t\n ",
			wantNil: true,
		},
		{
			name:    "unicode whitespace only",
			input:   " 　",
			wantNil: true,
		},
		{
			name:  "already trimmed",
			input: "milk",
			want:  "milk",
		},
		{
			name:  "leading and trailing whitespace",
			input: "  milk \n",
			want:  "milk",
		},
		{
			name:  "interior whitespace preserved",
			input: " low  fat ",
			want:  "low  fat",
		},
		{
			name:  "multi-line note keeps inner newlines",
			input: "\n eggs\n bread \n",
			want:  "eggs\n bread",
		},
		{
			name:  "unicode content is not altered",
			input: "  Müsli  ",
			want:  "Müsli",
		},
		{
			name:  "long note is not capped",
			input: "  " + longNote + "  ",
			want:  strings.TrimSpace(longNote),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var in *string

			original := tt.input

			if !tt.nilInput {
				in = &tt.input
			}

			got := normalizeNote(in)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %q", *got)
				}

				return
			}

			if got == nil {
				t.Fatalf("expected %q, got nil", tt.want)
			}

			if *got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, *got)
			}

			if got == in {
				t.Fatal("expected a new pointer, got the input pointer back")
			}

			if tt.input != original {
				t.Fatalf("input string was mutated: expected %q, got %q", original, tt.input)
			}
		})
	}
}

// TestNormalizeNoteDoesNotAliasInput asserts that the returned pointer is
// independent of the caller's string: writing through it must not change the
// value the caller passed in.
func TestNormalizeNoteDoesNotAliasInput(t *testing.T) {
	t.Parallel()

	input := "  milk  "

	got := normalizeNote(&input)
	if got == nil {
		t.Fatal("expected a note, got nil")
	}

	*got = "overwritten"

	if input != "  milk  " {
		t.Fatalf("input string was mutated: got %q", input)
	}
}

// TestValidateNameTrimBeforeLengthCheck covers the cases the main validateName
// table leaves open: whitespace forms TrimSpace strips beyond ASCII and the
// space rune, padding that pushes an already-oversized name further past the
// cap, and a four-byte rune whose rune count is nowhere near the byte cap.
func TestValidateNameTrimBeforeLengthCheck(t *testing.T) {
	t.Parallel()

	const (
		emptyMessage   = "name must not be empty"
		tooLongMessage = "name is too long"
	)

	// maxNameLength is a byte cap, so the fixtures below are sized in bytes.
	name201 := strings.Repeat("a", 201)
	fourByte204 := strings.Repeat("😀", 51) // 51 runes, 204 bytes

	if len(name201) != 201 || len(fourByte204) != 204 {
		t.Fatalf("bad fixtures: len(name201)=%d, len(fourByte204)=%d",
			len(name201), len(fourByte204))
	}

	tests := []struct {
		name string
		// input is passed to validateName.
		input string
		// want is the expected trimmed name; only checked when wantErr is empty.
		want string
		// wantErr is the expected *ValidationError message; empty means success.
		wantErr string
	}{
		{
			name:    "non breaking space only is rejected",
			input:   " ",
			wantErr: emptyMessage,
		},
		{
			name:    "ideographic space only is rejected",
			input:   "　　",
			wantErr: emptyMessage,
		},
		{
			name:  "unicode whitespace around a name is trimmed",
			input: " 　Groceries　 ",
			want:  "Groceries",
		},
		{
			name:  "interior newline and tab survive trimming",
			input: "  first\tsecond\nthird  ",
			want:  "first\tsecond\nthird",
		},
		{
			name:    "padding does not rescue a name already over the cap",
			input:   "   \n" + name201 + "\t   ",
			wantErr: tooLongMessage,
		},
		{
			name:    "four byte runes over max bytes are rejected despite few runes",
			input:   fourByte204,
			wantErr: tooLongMessage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := validateName(tt.input)

			if tt.wantErr != "" {
				assertValidationError(t, err, "name")

				var validationErr *ValidationError
				if !errors.As(err, &validationErr) {
					t.Fatalf("expected *ValidationError, got %v", err)
				}

				if validationErr.Message != tt.wantErr {
					t.Fatalf("expected message %q, got %q", tt.wantErr, validationErr.Message)
				}

				if got != "" {
					t.Fatalf("expected empty name on error, got %q", got)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.want {
				t.Fatalf("expected name %q, got %q", tt.want, got)
			}
		})
	}
}

// TestValidateNameErrorSurfacesMessage pins the error shape both rejection
// paths share: field "name", and Error() reporting the message verbatim.
func TestValidateNameErrorSurfacesMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{name: "empty name", input: "   "},
		{name: "name too long", input: strings.Repeat("a", 201)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := validateName(tt.input)

			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("expected *ValidationError, got %v", err)
			}

			if validationErr.Field != "name" {
				t.Fatalf("expected field %q, got %q", "name", validationErr.Field)
			}

			if validationErr.Error() != validationErr.Message {
				t.Fatalf("expected Error() %q, got %q", validationErr.Message, validationErr.Error())
			}
		})
	}
}

// TestNormalizeQuantityErrorIsUnwrappedValidationError pins that the rejection
// is returned as a bare *ValidationError rather than something wrapping one,
// and that Error() surfaces the Message verbatim, so the REST layer can hand
// the message straight back to clients.
func TestNormalizeQuantityErrorIsUnwrappedValidationError(t *testing.T) {
	t.Parallel()

	got, err := normalizeQuantity(-1)
	if err == nil {
		t.Fatalf("expected an error for quantity -1, got quantity %d", got)
	}

	if unwrapped := errors.Unwrap(err); unwrapped != nil {
		t.Fatalf("expected an unwrapped error, got %T wrapping %v", err, unwrapped)
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected *ValidationError, got %T (%v)", err, err)
	}

	if validationErr.Field != fieldQuantity {
		t.Fatalf("expected field %q, got %q", fieldQuantity, validationErr.Field)
	}

	if validationErr.Message == "" {
		t.Fatal("expected a non-empty validation message")
	}

	if validationErr.Error() != validationErr.Message {
		t.Fatalf("expected Error() to return the message %q, got %q", validationErr.Message, validationErr.Error())
	}
}

// TestNormalizeQuantityIsIdempotent pins that a normalised quantity is itself a
// valid quantity: feeding the result back in returns it unchanged. A value
// AddItem persisted can therefore never be defaulted or rejected by a later
// normalisation.
func TestNormalizeQuantityIsIdempotent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		quantity int
	}{
		{name: "unset defaults then stays", quantity: 0},
		{name: "minimum allowed", quantity: 1},
		{name: "small value", quantity: 3},
		{name: "typical value", quantity: 42},
		{name: "maximum int", quantity: math.MaxInt},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			first, err := normalizeQuantity(testCase.quantity)
			if err != nil {
				t.Fatalf("expected no error for quantity %d, got %v", testCase.quantity, err)
			}

			if first < 1 {
				t.Fatalf("expected normalised quantity to be at least 1, got %d", first)
			}

			second, err := normalizeQuantity(first)
			if err != nil {
				t.Fatalf("expected no error re-normalising %d, got %v", first, err)
			}

			if second != first {
				t.Fatalf("expected re-normalising %d to return %d, got %d", first, first, second)
			}
		})
	}
}

// TestNormalizeQuantityRejectsValuesBelowZero pins that the "unset" special
// case applies to 0 alone: every other value below 1 is rejected on the
// "quantity" field and yields a zero quantity.
func TestNormalizeQuantityRejectsValuesBelowZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		quantity int
	}{
		{name: "minus two", quantity: -2},
		{name: "minus three", quantity: -3},
		{name: "minus one hundred", quantity: -100},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeQuantity(testCase.quantity)
			if err == nil {
				t.Fatalf("expected an error for quantity %d, got quantity %d", testCase.quantity, got)
			}

			assertValidationError(t, err, fieldQuantity)

			if got != 0 {
				t.Fatalf("expected zero quantity alongside the error, got %d", got)
			}
		})
	}
}

// TestValidateUnitRejectsPrefixAndSuperstringTokens pins that matching is whole
// token equality rather than a prefix or substring test: strings that are a
// proper prefix of a canonical token, extend one, or merely contain one are all
// rejected.
func TestValidateUnitRejectsPrefixAndSuperstringTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		unit string
	}{
		{name: "prefix of kg", unit: "k"},
		{name: "prefix of ml", unit: "m"},
		{name: "prefix of bottle", unit: "bot"},
		{name: "prefix of amount", unit: "am"},
		{name: "kg with plural suffix", unit: "kgs"},
		{name: "l with plural suffix", unit: "ls"},
		{name: "ml with digit suffix", unit: "ml2"},
		{name: "bag with plural suffix", unit: "bags"},
		{name: "canonical token embedded", unit: "xkgx"},
		{name: "two tokens concatenated", unit: "gkg"},
		{name: "two tokens comma separated", unit: "g,kg"},
		{name: "unrelated imperial unit", unit: "lb"},
		{name: "nul terminated kg", unit: "kg\x00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := validateUnit(tt.unit)
			if err == nil {
				t.Fatalf("validateUnit(%q) = %q, want a validation error", tt.unit, got)
			}

			if got != "" {
				t.Fatalf("validateUnit(%q) returned %q alongside an error, want empty string", tt.unit, got)
			}

			assertValidationError(t, err, "unit")
		})
	}
}

// TestValidateUnitIsIdempotent checks that every successful result is itself a
// canonical token that validateUnit accepts unchanged, so re-validating an
// already stored unit (as an item update does) can never change or reject it.
func TestValidateUnitIsIdempotent(t *testing.T) {
	t.Parallel()

	inputs := append([]string{""}, Units()...)

	for _, unit := range inputs {
		t.Run(fmt.Sprintf("%q", unit), func(t *testing.T) {
			t.Parallel()

			first, err := validateUnit(unit)
			if err != nil {
				t.Fatalf("validateUnit(%q) returned error: %v", unit, err)
			}

			if !slices.Contains(Units(), first) {
				t.Fatalf("validateUnit(%q) = %q, want one of %v", unit, first, Units())
			}

			second, err := validateUnit(first)
			if err != nil {
				t.Fatalf("validateUnit(%q) returned error on second pass: %v", first, err)
			}

			if second != first {
				t.Fatalf("validateUnit(%q) = %q on second pass, want %q", first, second, first)
			}
		})
	}
}

// TestValidateUnitErrorIsUnwrappedValidationError checks that the rejection
// error survives wrapping, so callers further up the stack can still classify
// it with errors.As and match it with errors.Is.
func TestValidateUnitErrorIsUnwrappedValidationError(t *testing.T) {
	t.Parallel()

	_, err := validateUnit("piece")
	if err == nil {
		t.Fatal("validateUnit(\"piece\") returned no error, want a validation error")
	}

	wrapped := fmt.Errorf("create item: %w", err)

	var validationErr *ValidationError
	if !errors.As(wrapped, &validationErr) {
		t.Fatalf("errors.As on wrapped error failed, got %v", wrapped)
	}

	if validationErr.Field != fieldUnit {
		t.Fatalf("wrapped error field = %q, want %q", validationErr.Field, fieldUnit)
	}

	if !errors.Is(wrapped, err) {
		t.Fatalf("errors.Is(wrapped, err) = false, want true")
	}
}

// TestNormalizeNoteUnicodeSpaceBoundaries covers the trimming boundary cases
// that TestNormalizeNote does not: a minimal single-rune note, and the
// non-ASCII space runes that strings.TrimSpace treats as whitespace (via
// unicode.IsSpace) versus the zero-width runes it does not.
func TestNormalizeNoteUnicodeSpaceBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantNil bool
	}{
		{
			name:  "single ascii rune",
			input: "x",
			want:  "x",
		},
		{
			name:  "single non-ascii rune",
			input: "ß",
			want:  "ß",
		},
		{
			name:  "single rune surrounded by whitespace",
			input: " \t x \n ",
			want:  "x",
		},
		{
			name:    "non-breaking space only",
			input:   " ",
			wantNil: true,
		},
		{
			name:    "mixed ascii and non-breaking space only",
			input:   "  \t  ",
			wantNil: true,
		},
		{
			name:    "em space only",
			input:   "  ",
			wantNil: true,
		},
		{
			name:    "next line only",
			input:   "",
			wantNil: true,
		},
		{
			name:    "line separator only",
			input:   "  ",
			wantNil: true,
		},
		{
			name:  "non-breaking space padding is trimmed",
			input: " milk ",
			want:  "milk",
		},
		{
			name:  "mixed ascii and non-breaking space padding is trimmed",
			input: "   milk  \n",
			want:  "milk",
		},
		{
			name:  "interior non-breaking space is preserved",
			input: "  low fat  ",
			want:  "low fat",
		},
		{
			name:  "zero width space is not whitespace",
			input: "\u200b",
			want:  "\u200b",
		},
		{
			name:  "zero width space padding is preserved",
			input: " \u200bmilk\u200b ",
			want:  "\u200bmilk\u200b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			original := tt.input
			in := &tt.input

			got := normalizeNote(in)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %q", *got)
				}

				return
			}

			if got == nil {
				t.Fatalf("expected %q, got nil", tt.want)
			}

			if *got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, *got)
			}

			if got == in {
				t.Fatal("expected a new pointer, got the input pointer back")
			}

			if tt.input != original {
				t.Fatalf("input string was mutated: expected %q, got %q", original, tt.input)
			}
		})
	}
}
