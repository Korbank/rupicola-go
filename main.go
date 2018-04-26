package main

import (
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"rupicolarpc"
	"syscall"

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
	processor *rupicolarpc.JsonRpcProcessor
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

type wonkyJSONRPCrequest struct {
	err error
	r   *http.Request
	m   rupicolarpc.MethodType
}

func (w *wonkyJSONRPCrequest) Len() int64 {
	return w.r.ContentLength
}

func (w *wonkyJSONRPCrequest) OutputMode() rupicolarpc.MethodType {
	return rupicolarpc.RPCMethod
}

func (w *wonkyJSONRPCrequest) Reader() (io.ReadCloser, error) {
	if w.err != nil {
		return nil, w.err
	}
	return w.r.Body, nil
}

// ServeHTTP is implementation of http interface
func (s *rupicolaProcessorChild) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Debug("processing request", "address", r.RemoteAddr, "method", r.Method, "size", r.ContentLength, "path", r.URL)
	defer r.Body.Close()

	// Accept only POST [this is corrent transport level error]
	if r.Method != "POST" {
		log.Warn("not allowed", "method", r.Method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Check for "payload size". According to spec
	// Payload = 0 mean ignore
	if s.parent.limits.PayloadSize > 0 && r.ContentLength > int64(s.parent.limits.PayloadSize) {
		w.WriteHeader(http.StatusBadRequest)
		log.Warn("request too big")
		return
	}
	wonkyRequest := &wonkyJSONRPCrequest{r: r}
	rpcOperationMode := &wonkyRequest.m

	var context rupicolaRPCContext
	context.parent = s.parent
	if r.RequestURI == s.parent.config.Protocol.URI.RPC {
		context.isRPC = true
		*rpcOperationMode = rupicolarpc.RPCMethod
	} else if r.RequestURI == s.parent.config.Protocol.URI.Streamed {
		context.isRPC = false
		*rpcOperationMode = rupicolarpc.StreamingMethodLegacy
	} else {
		log.Warn("Unrecognized URI", "uri", r.RequestURI)
		return
	}

	login, password, _ := r.BasicAuth()
	context.isAuthorized = s.parent.config.isValidAuth(login, password)

	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if ip.IsLoopback() {
		context.allowPrivate = s.bind.AllowPrivate
	}

	if !context.isAuthorized && !context.allowPrivate {
		wonkyRequest.err = rpcUnauthorizedError
	}
	s.parent.processor.Process(wonkyRequest, w, &context)
}

func registerCleanupAtExit(config *RupicolaConfig) {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, os.Kill, syscall.SIGTERM)
	go func(c chan os.Signal) {
		// Wait for a SIGINT or SIGKILL:
		sig := <-c
		log.Info("Caught signal: shutting down.", "signal", sig)
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
	}(sigc)
}

func main() {
	configPath := poorString("config", "", "Specify directory or config file")
	poorParse()
	if *configPath == "" {
		poorUsage()
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
