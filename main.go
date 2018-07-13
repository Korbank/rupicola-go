package main

import (
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/mkocot/rupicolarpc"

	log "github.com/inconshreveable/log15"
)

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
	config    *RupicolaConfig
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

func (child *rupicolaProcessorChild) config() *RupicolaConfig {
	return child.parent.config
}

// Create separate context for given bind point (required for concurrent listening)
func (proc *rupicolaProcessor) spawnChild(bind *Bind) *rupicolaProcessorChild {
	child := &rupicolaProcessorChild{proc, bind, http.NewServeMux()}
	child.mux.Handle(proc.config.Protocol.URI.RPC, child)
	child.mux.Handle(proc.config.Protocol.URI.Streamed, child)
	return child
}

func newRupicolaProcessorFromConfig(conf *RupicolaConfig) *rupicolaProcessor {
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
	}

	if !userData.isAuthorized && !userData.allowPrivate {
		request.err = rpcUnauthorizedError
	}

	child.parent.processor.ProcessContext(r.Context(), request, w)
}

func registerCleanupAtExit(config *RupicolaConfig) {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGHUP)
	go func(c chan os.Signal) {
		// todo: configuration reloading
		// Wait for a SIGINT or SIGKILL:
		for {
			sig := <-c
			log.Info("Caught signal: shutting down.", "signal", sig)

			switch sig {
			case syscall.SIGHUP:
				log.Warn("TODO: reloading config, please restart process")
			default:
				// Stop listening (and unlink the socket if unix type):
				for _, bind := range config.Protocol.Bind {
					if bind.Type != Unix {
						continue
					}
					if err := os.Remove(bind.Address); err != nil {
						log.Error("Unable to unlink", "address", bind.Address, "err", err)
					} else {
						log.Debug("Unlinked UNIX address", "address", bind.Address)
					}
				}
				// And we're done:
				os.Exit(0)
			}
		}
	}(sigc)
}

func main() {
	configPath := flag.String("config", "", "Specify directory or config file")
	flag.Parse()
	if *configPath == "" {
		flag.Usage()
		os.Exit(1)
	}

	configuration, err := ReadConfig(*configPath)

	if err != nil {
		log.Crit("Unable to parse config", "err", err)
		os.Exit(1)
	}

	if len(configuration.Methods) == 0 {
		log.Crit("No method defined in config")
		os.Exit(1)
	}

	if len(configuration.Protocol.Bind) == 0 {
		log.Crit("No valid bind points")
		os.Exit(1)
	}

	configuration.SetLogging()

	registerCleanupAtExit(configuration)

	rupicolaProcessor := newRupicolaProcessorFromConfig(configuration)

	failureChannel := make(chan error)
	for _, bind := range configuration.Protocol.Bind {
		child := rupicolaProcessor.spawnChild(bind)
		go func() {
			failureChannel <- child.listen()
		}()
	}

	err = <-failureChannel
	log.Crit("Program will shut down now due to encountered error", "error", err)
	os.Exit(1)
}
