// SPDX-License-Identifier: CC0-1.0

package members

import "errors"

// ErrNotFound is returned by Repository.Get when no member has the requested
// subject.
var ErrNotFound = errors.New("member not found")
