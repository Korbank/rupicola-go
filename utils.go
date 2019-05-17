package rupicola

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"

	log "github.com/inconshreveable/log15"
	"github.com/mkocot/rupicolarpc"
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

var (
	rpcUnauthorizedError = rupicolarpc.NewServerError(-32000, "Unauthorized")
)

type rpcMethod *MethodDef

type cmdEx *exec.Cmd

type rupicolaRPCContext struct {
	isAuthorized      bool
	allowPrivate      bool
	shouldRequestAuth bool
	isRPC             bool
	parent            *rupicolaProcessor
}

type rupicolaProcessor struct {
	limits    Limits
	processor rupicolarpc.JsonRpcProcessor
	config    *Config
}

type rupicolaProcessorChild struct {
	parent *rupicolaProcessor
	bind   *Bind
	mux    *http.ServeMux
}

// Start listening (this exits only on failure)
func (child *rupicolaProcessorChild) listen() error {
	return child.bind.Bind(child.mux, child.parent.config.Limits)
}

func (child *rupicolaProcessorChild) config() *Config {
	return child.parent.config
}

// Create separate context for given bind point (required for concurrent listening)
func (proc *rupicolaProcessor) spawnChild(bind *Bind) *rupicolaProcessorChild {
	child := &rupicolaProcessorChild{proc, bind, http.NewServeMux()}
	child.mux.Handle(proc.config.Protocol.URI.RPC, child)
	child.mux.Handle(proc.config.Protocol.URI.Streamed, child)
	return child
}

func newRupicolaProcessorFromConfig(conf *Config) *rupicolaProcessor {
	rupicolaProcessor := &rupicolaProcessor{
		config:    conf,
		limits:    conf.Limits,
		processor: rupicolarpc.NewJsonRpcProcessor()}

	for k, v := range conf.Methods {
		var metype rupicolarpc.MethodType
		if v.Streamed {
			metype = rupicolarpc.StreamingMethodLegacy
		} else {
			metype = rupicolarpc.RPCMethod
		}

		method := rupicolaProcessor.processor.AddMethod(k, metype, v)

		if v.Limits.ExecTimeout >= 0 {
			method.ExecutionTimeout(v.Limits.ExecTimeout)
		}
		if v.Limits.MaxResponse >= 0 {
			method.MaxSize(uint(v.Limits.MaxResponse))
		}
	}
	return rupicolaProcessor
}

type httpJSONRequest struct {
	err      error
	r        *http.Request
	m        rupicolarpc.MethodType
	userData rupicolaRPCContext
}

func (w *httpJSONRequest) Len() int64 {
	return w.r.ContentLength
}

func (w *httpJSONRequest) OutputMode() rupicolarpc.MethodType {
	return w.m
}

func (w *httpJSONRequest) UserData() rupicolarpc.UserData {
	return &w.userData
}

func (w *httpJSONRequest) Reader() io.ReadCloser {
	if w.err != nil {
		return &failureReader{w.err}
	}
	return w.r.Body
}

// ServeHTTP is implementation of http interface
func (child *rupicolaProcessorChild) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Debug("processing request",
		"address", r.RemoteAddr,
		"method", r.Method,
		"size", r.ContentLength,
		"path", r.URL)

	defer r.Body.Close()

	// Accept only POST [this is corrent transport level error]
	if r.Method != "POST" {
		log.Warn("not allowed", "method", r.Method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Check for "payload size". According to spec
	// Payload = 0 mean ignore
	if child.parent.limits.PayloadSize > 0 && r.ContentLength > int64(child.parent.limits.PayloadSize) {
		w.WriteHeader(http.StatusBadRequest)
		log.Warn("request too big")
		return
	}
	request := &httpJSONRequest{r: r}

	//var userData rupicolaRPCContext
	userData := &request.userData

	userData.parent = child.parent
	if r.RequestURI == child.config().Protocol.URI.RPC {
		userData.isRPC = true
		request.m = rupicolarpc.RPCMethod
	} else if r.RequestURI == child.config().Protocol.URI.Streamed {
		userData.isRPC = false
		request.m = rupicolarpc.StreamingMethodLegacy
	} else {
		log.Warn("Unrecognized URI", "uri", r.RequestURI)
		return
	}

	login, password, _ := r.BasicAuth()
	userData.isAuthorized = child.config().isValidAuth(login, password)

	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if ip.IsLoopback() {
		userData.allowPrivate = child.bind.AllowPrivate
		if !userData.allowPrivate {
			log.Debug("Request from loopback, but bindpoint will require authentification data", "bindpoint", child.bind.Address)
		}
	}

	if !userData.isAuthorized && !userData.allowPrivate {
		request.err = rpcUnauthorizedError
	}

	child.parent.processor.ProcessContext(r.Context(), request, w)
}

func ListenAndServe(configuration *Config) error {
	rupicolaProcessor := newRupicolaProcessorFromConfig(configuration)

	failureChannel := make(chan error)
	for _, bind := range configuration.Protocol.Bind {
		child := rupicolaProcessor.spawnChild(bind)
		log.Info("Spawning worker", "bind", bind)
		go func() {
			failureChannel <- child.listen()
		}()
	}
	log.Info("Listening for requests...")
	return <-failureChannel
}
