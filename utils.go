package rupicola

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/korbank/rupicola-go/rupicolarpc"
	log "github.com/rs/zerolog"
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
		Logger.Info().Str("type", "http").Str("address", bind.Address).Uint16("port", bind.Port).Msg("starting listener")
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
		Logger.Info().Str("type", "https").Str("address", bind.Address).Uint16("port", bind.Port).Msg("starting listener")
		ln, err := ListenKeepAlive("tcp", srv.Addr)
		if err != nil {
			return err
		}
		return srv.ServeTLS(ln, bind.Cert, bind.Key)

	case Unix:
		//todo: check
		Logger.Info().Str("type", "unix").Str("address", bind.Address).Msg("starting listener")
		srv.Addr = bind.Address
		// Change umask to ensure socker is created with right
		// permissions (at this point no other IO opeations are running)
		// and then restore previous umask
		oldmask := myUmask(int(bind.Mode) ^ 0777)
		ln, err := ListenUnixLock(bind.Address)
		myUmask(oldmask)

		if err != nil {
			return err
		}

		defer ln.Close()
		if err := os.Chown(bind.Address, bind.UID, bind.GID); err != nil {
			Logger.Error().Str("address", bind.Address).Int("uid", bind.UID).Int("gid", bind.GID).Msg("Setting permission failed")
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
	log    log.Logger
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
	child := &rupicolaProcessorChild{proc, bind, http.NewServeMux(), Logger.With().Str("bindpoint", bind.Address).Logger()}
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

type flusher interface {
	io.Writer
	http.Flusher
}

type flushWrapper struct {
	flusher
	flush sync.Once
}

func (f *flushWrapper) Write(p []byte) (n int, err error) {
	n, err = f.flusher.Write(p)
	f.flusher.Flush()
	return
}

func wrapWithFlusher(out io.Writer) io.Writer {
	if f, ok := out.(flusher); ok {
		return &flushWrapper{flusher: f}
	}
	return out
}

// ServeHTTP is implementation of http interface
func (child *rupicolaProcessorChild) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	child.log.Debug().Str("address", r.RemoteAddr).Str("method", r.Method).Int64("size", r.ContentLength).Str("path", r.URL.String()).Msg("processing request")

	defer r.Body.Close()

	// Accept only POST [this is corrent transport level error]
	if r.Method != "POST" {
		child.log.Warn().Str("method", r.Method).Msg("not allowed")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Check for "payload size". According to spec
	// Payload = 0 mean ignore
	if child.parent.limits.PayloadSize > 0 && r.ContentLength > int64(child.parent.limits.PayloadSize) {
		w.WriteHeader(http.StatusBadRequest)
		child.log.Warn().Msg("request too big")
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
		child.log.Warn().Str("uri", r.RequestURI).Msg("Unrecognized URI")
		return
	}

	login, password, _ := r.BasicAuth()
	userData.isAuthorized = child.config().isValidAuth(login, password)

	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if ip.IsLoopback() || ip == nil && r.RemoteAddr == "@" {
		userData.allowPrivate = child.bind.AllowPrivate
		if !userData.allowPrivate {
			child.log.Debug().Msg("Request from loopback, but bindpoint will require authentification data")
		}
	}

	if !userData.isAuthorized && !userData.allowPrivate {
		request.err = rpcUnauthorizedError
	}
	var writer io.Writer = w
	if !userData.isRPC && request.err == nil {
		// http output is picky as it tries to buffer some data to respond with nice
		// Conent-Length header, but for our streaming method most of the time it's
		// just hold output until procedure finished
		child.log.Debug().Msg("wrap writer with flusher")
		writer = wrapWithFlusher(writer)
	}

	if err := child.parent.processor.ProcessContext(r.Context(), request, writer); err != nil {
		child.log.Error().Err(err).Msg("request failed")
	}

}

func ListenAndServe(configuration *Config) error {
	rupicolaProcessor := newRupicolaProcessorFromConfig(configuration)

	failureChannel := make(chan error)
	for _, bind := range configuration.Protocol.Bind {
		child := rupicolaProcessor.spawnChild(bind)
		Logger.Info().Str("bind", fmt.Sprintf("%+v", bind)).Msg("Spawning worker")
		go func() {
			failureChannel <- child.listen()
		}()
	}
	Logger.Info().Msg("Listening for requests...")
	return <-failureChannel
}
