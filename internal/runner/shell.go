package runner

import "runtime"

// shellArgs returns the platform-appropriate shell and flag for executing a
// command string. On Windows this is cmd /c; everywhere else sh -c.
func shellArgs(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", command}
	}
	return "sh", []string{"-c", command}
}
