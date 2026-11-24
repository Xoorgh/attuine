//go:build !unix

package ownership

import "io/fs"

// Windows has no POSIX uids to compare, so ownership is not checked there.
const supported = false

func ownerOf(fs.FileInfo) (int, bool) { return 0, false }
