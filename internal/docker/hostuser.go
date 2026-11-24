package docker

import (
	"strconv"
	"strings"
)

// HostUserEnv returns environ with HOST_UID and HOST_GID set to uid and gid.
//
// A compose file can then run its bind-mounting services as the invoking user,
// with `user: "${HOST_UID:-1000}:${HOST_GID:-1000}"`, so the files those
// containers write into a checkout stay owned by the developer instead of root.
// A variable already present in environ is kept, so the caller can override
// either one. A negative uid or gid adds nothing for that id: on Windows
// os.Getuid and os.Getgid return -1, since there are no POSIX ids.
func HostUserEnv(environ []string, uid, gid int) []string {
	env := append([]string(nil), environ...)
	env = setDefault(env, "HOST_UID", uid)
	env = setDefault(env, "HOST_GID", gid)
	return env
}

func setDefault(env []string, key string, id int) []string {
	if id < 0 {
		return env
	}
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			return env
		}
	}
	return append(env, key+"="+strconv.Itoa(id))
}
