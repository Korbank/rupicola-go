package main

import (
	"os/exec"
)

// SetUserGroup sets group and user for process (not working on windows)
func SetUserGroup(process *exec.Cmd, m *MethodDef) {
	m.logger.Warn("windows doesn't support uid/guid")
}
