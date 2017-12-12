package main

import (
	"net"
	"time"
)

const (
	// The only "guard" agains mising FIN (happens on shitty networks)
	// is setting deadline before each write so write call should
	// end before running out of time (if you don't send petabytes of data
	// in one call this is sufficient)
	globalTCPwriteTimeout = time.Second * 60
)

type writeWithGuard struct {
	net.Conn
}

func (bb *writeWithGuard) Write(b []byte) (int, error) {
	bb.SetWriteDeadline(time.Now().Add(globalTCPwriteTimeout))
	return bb.Conn.Write(b)
}

type tcpKeepAliveListener struct {
	*net.TCPListener
}

// Accept are OS specific

func ListenKeepAlive(network, address string) (ln net.Listener, err error) {
	ln, err = net.Listen(network, address)
	if err != nil {
		return
	}
	switch casted := ln.(type) {
	case *net.TCPListener:
		ln = tcpKeepAliveListener{casted}
	}
	return
}
