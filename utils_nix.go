// +build !windows

package main

import (
	"log"
	"os/exec"
	"syscall"
)

// SetUserGroup assign UID and GID to process
func SetUserGroup(process *exec.Cmd, m *MethodDef) {
	log.Println("Nix code")
	process.SysProcAttr = &syscall.SysProcAttr{}
	process.SysProcAttr.Credential = &syscall.Credential{Uid: *m.InvokeInfo.RunAs.UID, Gid: *m.InvokeInfo.RunAs.GID}
}
