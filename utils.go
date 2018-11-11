package main

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	log "github.com/inconshreveable/log15"
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

// Accepts are OS specific

// ListenKeepAlive start listening with KeepAlive active
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

// Bind to interface and Start listening using provided mux and limits
func (bind *Bind) Bind(mux *http.ServeMux, limits Limits) error {
	srv := &http.Server{
		Addr:        bind.Address + ":" + strconv.Itoa(int(bind.Port)),
		Handler:     mux,
		ReadTimeout: limits.ReadTimeout,
		IdleTimeout: limits.ReadTimeout,
	}

	switch bind.Type {
	case HTTP:
		log.Info("starting listener", "type", "http", "address", bind.Address, "port", bind.Port)
		ln, err := ListenKeepAlive("tcp", srv.Addr)
		if err != nil {
			return err
		}
		return srv.Serve(ln)

	case HTTPS:
		srv.TLSConfig = &tls.Config{
			MinVersion:               tls.VersionTLS12,
			PreferServerCipherSuites: true,
		}
		log.Info("starting listener", "type", "https", "address", bind.Address, "port", bind.Port)
		ln, err := ListenKeepAlive("tcp", srv.Addr)
		if err != nil {
			return err
		}
		return srv.ServeTLS(ln, bind.Cert, bind.Key)

	case Unix:
		//todo: check
		log.Info("starting listener", "type", "unix", "address", bind.Address)
		srv.Addr = bind.Address
		// Change umask to ensure socker is created with right
		// permissions (at this point no other IO opeations are running)
		// and then restore previous umask
		oldmask := myUmask(int(bind.Mode) ^ 0777)
		ln, err := net.Listen("unix", bind.Address)
		myUmask(oldmask)

		if err != nil {
			return err
		}

		defer ln.Close()
		if err := os.Chown(bind.Address, bind.UID, bind.GID); err != nil {
			log.Crit("Setting permission failed", "address", bind.Address, "uid", bind.UID, "gid", bind.GID)
			return err
		}
		return srv.Serve(ln)
	}
	return errors.New("Unknown case")
}

type failureReader struct {
	error
}

func (f *failureReader) Close() error {
	return f.error
}
func (f *failureReader) Read([]byte) (int, error) {
	return 0, f.error
}
