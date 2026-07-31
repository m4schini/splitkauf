// SPDX-License-Identifier: TODO

package members

import "errors"

// ErrNotFound is returned by Repository.Get when no member has the requested
// subject.
var ErrNotFound = errors.New("member not found")
