// +build !windows

package main

import (
	"log"
	"os/exec"
	"syscall"
)

func SetUserGroup(process *exec.Cmd, m *MethodDef) {
	log.Println("Nix code")
	process.SysProcAttr = &syscall.SysProcAttr{}
	process.SysProcAttr.Credential = &syscall.Credential{Uid: *m.InvokeInfo.RunAs.Uid, Gid: *m.InvokeInfo.RunAs.Gid}
}
