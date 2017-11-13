package main

import (
	"os/exec"
)

func SetUserGroup(process *exec.Cmd, m *MethodDef) {
	m.logger.Warn("windows doesn't support uid/guid")
}
