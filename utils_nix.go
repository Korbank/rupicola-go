// +build !windows

package rupicola

import (
	"net"
	"os/exec"
	"syscall"
	"time"

	"github.com/felixge/tcpkeepalive"
)

// func setKeepAlive(c *net.TCPConn, idle time.Duration, count int, interval time.Duration) error {
// 	if err := c.SetKeepAlive(true); err != nil {
// 		return err
// 	}
// 	file, err := c.File()
// 	if err != nil {
// 		return err
// 	}

// }

func (ln tcpKeepAliveListener) Accept() (net.Conn, error) {
	c, err := ln.AcceptTCP()
	if err != nil {
		return nil, err
	}
	// Wait 30s before sending probes
	// Use 4 probes
	// And wait for each 5s
	if err := tcpkeepalive.SetKeepAlive(c, 30*time.Second, 4, 5*time.Second); err != nil {
		return nil, err
	}
	return &writeWithGuard{c}, nil
}

// SetUserGroup assign UID and GID to process
func SetUserGroup(process *exec.Cmd, m *MethodDef) {
	// Requires root to work?
	m.logger.Debug().Uint32("uid", m.InvokeInfo.RunAs.UID).Uint32("gid", m.InvokeInfo.RunAs.GID).Msg("Nix code")
	process.SysProcAttr = &syscall.SysProcAttr{}
	process.SysProcAttr.Credential = &syscall.Credential{
		Uid:         m.InvokeInfo.RunAs.UID,
		Gid:         m.InvokeInfo.RunAs.GID,
		NoSetGroups: true,
	}
}

func myUmask(mask int) int {
	return syscall.Umask(mask)
}
