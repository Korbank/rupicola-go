package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/syslog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"rupicolarpc"

	log "github.com/inconshreveable/log15"
	"github.com/spf13/pflag"
)

var (
	rpcUnauthorizedError = rupicolarpc.NewServerError(-32000, "Unauthorized")
)

const commandBufferSize int32 = 1024

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
}

func (s *rupicolaProcessorChild) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Debug("processing request", "address", r.RemoteAddr, "method", r.Method, "size", r.ContentLength, "path", r.URL)
	// Accept only POST
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

	var rpcOperationMode rupicolarpc.MethodType

	var context rupicolaRPCContext
	context.parent = s.parent
	if r.RequestURI == s.parent.config.Protocol.URI.RPC {
		context.isRPC = true
		rpcOperationMode = rupicolarpc.RPCMethod
	} else if r.RequestURI == s.parent.config.Protocol.URI.Streamed {
		context.isRPC = false
		streamingVersion := r.Header.Get("RupicolaStreamingVersion")
		switch streamingVersion {
		case "", "1":
			rpcOperationMode = rupicolarpc.StreamingMethodLegacy
		case "2":
			rpcOperationMode = rupicolarpc.StreamingMethod
		}
	} else {
		return
	}

	login, password, _ := r.BasicAuth()
	context.isAuthorized = s.parent.config.isValidAuth(login, password)

	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if ip.IsLoopback() {
		context.allowPrivate = s.bind.AllowPrivate
	}

	// Don't like this hack... we should not use this outside framework...
	if !context.isAuthorized && !context.allowPrivate {
		err := json.NewEncoder(w).Encode(rupicolarpc.NewError(rpcUnauthorizedError, nil))
		if err != nil {
			log.Crit("unknown error", "err", err)
			panic(err)
		}
	} else {
		s.parent.processor.Process(r.Body, w, &context, rpcOperationMode)
	}
}

func (m *MethodDef) prepareCommand(ctx context.Context, req rupicolarpc.JsonRpcRequest) (*rupicolaRPCContext, *exec.Cmd, error) {
	uncastedContext := ctx.Value(rupicolarpc.RupicalaContextKeyContext)
	var ok bool
	var castedContext *rupicolaRPCContext
	if uncastedContext != nil {
		castedContext, ok = uncastedContext.(*rupicolaRPCContext)
	}
	if ok {
		if !castedContext.isAuthorized && castedContext.allowPrivate && m.Private {
			castedContext.shouldRequestAuth = true
			log.Warn("Unauthorized")
			return nil, nil, rpcUnauthorizedError
		}
	} else {
		log.Crit("Provided context is not pointer")
		return nil, nil, rupicolarpc.NewStandardError(rupicolarpc.InternalError)
	}

	if err := m.CheckParams(&req); err != nil {
		return nil, nil, err
	}

	buffer := bytes.NewBuffer(make([]byte, 0, 1024))
	appArguments := make([]string, 0, len(m.InvokeInfo.Args))
	for _, arg := range m.InvokeInfo.Args {
		skip, err := arg.evalueateArgs(req.Params, buffer)
		if err != nil {
			log.Error("error", "err", err)
			return nil, nil, err
		}
		if !skip {
			appArguments = append(appArguments, buffer.String())
		}
		buffer.Reset()
	}

	m.logger.Debug("prepared method invocation", "args", appArguments)
	process := exec.Command(m.InvokeInfo.Exec, appArguments...)

	// Make it "better"
	SetUserGroup(process, m)

	stdin, err := process.StdinPipe()
	if err == nil {
		stdin.Close()
	} else {
		m.logger.Error("stdin", "err", err)
		return nil, nil, rupicolarpc.NewStandardErrorData(rupicolarpc.InternalError, "stdin")
	}

	return castedContext, process, nil
}

// Invoke is implementation of jsonrpc.Invoker
func (m *MethodDef) Invoke(ctx context.Context, req rupicolarpc.JsonRpcRequest) (interface{}, error) {
	// We can cancel or set deadline for current context (only shorter - default no limit)
	_, process, err := m.prepareCommand(ctx, req)
	if err != nil {
		return nil, err
	}
	stdout, err := process.StdoutPipe()
	if err != nil {
		return nil, rupicolarpc.NewStandardErrorData(rupicolarpc.InternalError, "stdout")
	}
	err = process.Start()
	if err != nil {
		m.logger.Error("err", "err", err)
		return nil, rupicolarpc.NewStandardErrorData(rupicolarpc.InternalError, err)
	}

	pr, pw := io.Pipe()
	writer := io.Writer(pw)
	reader := io.Reader(pr)

	if m.Encoding == Base64 {
		writerEncoder := base64.NewEncoder(base64.URLEncoding, writer)
		// should we defer, or err check?
		defer writerEncoder.Close()
		writer = writerEncoder
	}

	go func() {
		defer pw.Close()

		time.Sleep(m.InvokeInfo.Delay)
		m.logger.Debug("Read loop started")
		byteReadChunk := make([]byte, commandBufferSize)
		for true {
			read, err := stdout.Read(byteReadChunk)
			if err != nil {
				stdout.Close()
				if err != io.EOF {
					m.logger.Error("error reading from pipe", "err", err)
				} else {
					m.logger.Debug("reading from pipe finished")
				}
				pw.CloseWithError(err)
				//heartBeat <- err
				break
			}

			if _, e := writer.Write(byteReadChunk[0:read]); e != nil {
				// Ignore pipe error for delayed execution
				if e != io.ErrClosedPipe || m.InvokeInfo.Delay <= 0 {
					log.Error("Writing to output failed", "err", e)
				}
				stdout.Close()
				pw.CloseWithError(rupicolarpc.NewStandardErrorData(rupicolarpc.InternalError, "write"))
				break
			}

			select {
			case <-ctx.Done():
				pw.CloseWithError(rupicolarpc.TimeoutError)
				break
			default:
			}
		}
	}()
	// TODO: Check thys
	if m.InvokeInfo.Delay != 0 {
		pw.Close()
		// for "delayed" execution we cannot provide meaningful data
		return "OK", nil
	}
	return reader, nil
}

func registerCleanup(config *RupicolaConfig) {
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
	configPath := pflag.String("config", "", "Specify directory or config file")
	pflag.Parse()
	if *configPath == "" {
		pflag.Usage()
		os.Exit(1)
	}
	configuration, err := ParseConfig(*configPath)
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
	var logLevel log.Lvl
	switch configuration.Log.LogLevel {
	case LLError:
		logLevel = log.LvlError
	case LLWarn:
		logLevel = log.LvlWarn
	case LLInfo:
		logLevel = log.LvlInfo
	case LLDebug:
		fallthrough
	case LLTrace:
		logLevel = log.LvlDebug
	case LLOff:
		logLevel = -1
	}
	var handler log.Handler
	switch configuration.Log.Backend {
	case BackendStdout:
		handler = log.StdoutHandler
	case BackendSyslog:
		h, err := log.SyslogNetHandler("", configuration.Log.Path, syslog.LOG_DAEMON, "rupicola", log.JsonFormat())
		if err != nil {
			log.Error("Syslog connection failed", "err", err)
			os.Exit(1)
		}
		handler = h
	}
	log.Root().SetHandler(log.LvlFilterHandler(logLevel, handler))
	registerCleanup(configuration)

	rupicolaProcessor := rupicolaProcessor{
		config:    configuration,
		limits:    configuration.Limits,
		processor: rupicolarpc.NewJsonRpcProcessor()}

	for k, v := range configuration.Methods {
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

	failureChannel := make(chan error)
	for _, bind := range configuration.Protocol.Bind {
		// create local variable bind
		bind := bind
		child := rupicolaProcessorChild{&rupicolaProcessor, bind}
		mux := http.NewServeMux()
		mux.Handle(configuration.Protocol.URI.RPC, &child)
		mux.Handle(configuration.Protocol.URI.Streamed, &child)

		srv := &http.Server{Addr: bind.Address + ":" + strconv.Itoa(int(bind.Port)), Handler: mux}

		if configuration.Limits.ReadTimeout > 0 {
			srv.ReadTimeout = configuration.Limits.ReadTimeout * time.Millisecond
		}

		go func() {
			switch bind.Type {
			case HTTP:
				log.Info("starting listener", "type", "http", "address", bind.Address, "port", bind.Port)
				failureChannel <- srv.ListenAndServe()

			case HTTPS:
				log.Info("atarting listener", "type", "https", "address", bind.Address, "port", bind.Port)
				failureChannel <- srv.ListenAndServeTLS(bind.Cert, bind.Key)

			case Unix:
				//todo: check
				log.Info("atarting listener", "type", "unix", "address", bind.Address)
				srv.Addr = bind.Address

				// Change umask to ensure socker is created with right
				// permissions (at this point no other IO opeations are running)
				// and then restore previous umask
				oldmask := syscall.Umask(int(bind.Mode) ^ 0777)
				ln, err := net.Listen("unix", bind.Address)
				syscall.Umask(oldmask)

				if err != nil {
					failureChannel <- err
				} else {
					defer ln.Close()
					if err := os.Chown(bind.Address, *bind.UID, *bind.GID); err != nil {
						log.Crit("Setting permission failed", "address", bind.Address, "uid", *bind.UID, "gid", *bind.GID)
						failureChannel <- err
						return
					}
					failureChannel <- srv.Serve(ln)
				}
			}
		}()
	}

	err = <-failureChannel
	log.Crit("Program will shut down now due to encountered error", "error", err)
	os.Exit(1)
}
