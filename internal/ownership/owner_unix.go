//go:build unix

package ownership

import (
	"io/fs"
	"syscall"
)

const supported = true

func ownerOf(fi fs.FileInfo) (int, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Uid), true
}
