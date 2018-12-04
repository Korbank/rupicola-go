package rupicola

import (
	"net"
	"os/exec"
	"time"
)

func (ln tcpKeepAliveListener) Accept() (net.Conn, error) {
	c, err := ln.AcceptTCP()
	if err != nil {
		return nil, err
	}
	// Wait 30s before sending probes
	if err := c.SetKeepAlive(true); err != nil {
		return nil, err
	}
	// Use 4 probes
	if err := c.SetKeepAlivePeriod(30 * time.Second); err != nil {
		return nil, err
	}

	return &writeWithGuard{c}, nil
}

// SetUserGroup sets group and user for process (not working on windows)
func SetUserGroup(process *exec.Cmd, m *MethodDef) {
	m.logger.Warn("windows doesn't support uid/guid")
}

func myUmask(mask int) int {
	return mask
}
