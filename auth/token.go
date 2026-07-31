// SPDX-License-Identifier: TODO

package auth

import (
	"crypto/rand"
	"encoding/base64"
)

// randomTokenBytes is the entropy, in bytes, of the state and nonce values.
// 32 bytes (256 bits) is comfortably beyond guessing range.
const randomTokenBytes = 32

// randomToken returns a cryptographically random, URL-safe token used for the
// OIDC state and nonce parameters. It reads from crypto/rand and returns an
// error only when the system entropy source fails.
func randomToken() (string, error) {
	b := make([]byte, randomTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
