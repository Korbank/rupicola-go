// +build !windows

package main

import (
	"os/exec"
	"syscall"

	log "github.com/inconshreveable/log15"
)

// SetUserGroup assign UID and GID to process
func SetUserGroup(process *exec.Cmd, m *MethodDef) {
	// Requires root to work?
	log.Debug("Nix code", "uid", *m.InvokeInfo.RunAs.UID, "gid", *m.InvokeInfo.RunAs.GID)
	process.SysProcAttr = &syscall.SysProcAttr{}
	process.SysProcAttr.Credential = &syscall.Credential{
		Uid:         *m.InvokeInfo.RunAs.UID,
		Gid:         *m.InvokeInfo.RunAs.GID,
		NoSetGroups: true,
	}
}
