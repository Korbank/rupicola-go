package main

import (
	"./rupicolarpc"
	"bytes"
	"encoding/base64"
	"encoding/json"
	log "github.com/inconshreveable/log15"
	"github.com/spf13/pflag"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"
)

var (
	RpcUnauthorizedError = rupicolarpc.NewServerError(-32000, "Unauthorized")
)

const commandBufferSize int32 = 1024

type CmdEx *exec.Cmd

type rupicolaRpcContext struct {
	isAuthorized      bool
	allowPrivate      bool
	shouldRequestAuth bool
	isRPC             bool
	parent            *RupicolaProcessor
}

type RupicolaProcessor struct {
	limits    Limits
	processor *rupicolarpc.JsonRpcProcessor
	config    *RupicolaConfig
}
type RupicolaProcessorChild struct {
	parent *RupicolaProcessor
	bind   *Bind
}

func (s *RupicolaProcessorChild) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	var context rupicolaRpcContext
	context.parent = s.parent
	if r.RequestURI == s.parent.config.Protocol.Uri.Rpc {
		context.isRPC = true
		rpcOperationMode = rupicolarpc.RpcMethod
	} else if r.RequestURI == s.parent.config.Protocol.Uri.Streamed {
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
		err := json.NewEncoder(w).Encode(rupicolarpc.NewError(RpcUnauthorizedError, nil))
		if err != nil {
			log.Crit("unknown error", "err", err)
			panic(err)
		}
	} else {
		s.parent.processor.Process(r.Body, w, &context, rpcOperationMode)
	}
}

func (m *MethodDef) prepareCommand(req rupicolarpc.JsonRpcRequest, context interface{}) (*rupicolaRpcContext, *exec.Cmd, error) {
	castedContext, ok := context.(*rupicolaRpcContext)
	if ok {
		if !castedContext.isAuthorized && castedContext.allowPrivate && m.Private {
			castedContext.shouldRequestAuth = true
			log.Warn("Unauthorized")
			return nil, nil, RpcUnauthorizedError
		}
	} else {
		log.Crit("Provided context is not pointer")
		panic("Provided context is not pointer")
		return nil, nil, rupicolarpc.NewStandardError(rupicolarpc.InternalError)
	}

	if err := m.CheckParams(&req); err != nil {
		return nil, nil, err
	}

	buffer := bytes.NewBuffer(make([]byte, 0, 1024))
	appArguments := make([]string, 0, len(m.InvokeInfo.Args))
	for _, arg := range m.InvokeInfo.Args {
		skip, err := arg._evalueateArgs(req.Params, buffer)
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
		m.logger.Error("stdin", err)
		return nil, nil, rupicolarpc.NewStandardErrorData(rupicolarpc.InternalError, "stdin")
	}

	return castedContext, process, nil
}

// Invoke is implementation of jsonrpc.Invoker
func (m *MethodDef) Invoke2(req rupicolarpc.JsonRpcRequest, context interface{}) (interface{}, error) {
	r, process, err := m.prepareCommand(req, context)
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

	if r.parent.limits.MaxResponse > 0 {
		writer = rupicolarpc.LimitWrite(writer, int64(r.parent.limits.MaxResponse))
	}

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
		}
	}()
	// TODO: Check thys
	if m.InvokeInfo.Delay != 0 {
		pw.Close()
		// for "delayed" execution we cannot provide meaningful data
		return "OK", nil
	}
	return pr, nil
}

func main() {
	configPath := pflag.String("config", `D:\Programowanie\rupicola-ng\sample.conf`, "Specify directory or config file")
	pflag.Parse()
	if *configPath == "" {
		pflag.Usage()
		os.Exit(1)
	}
	configuration, err := ParseConfig(*configPath)
	if err != nil {
		log.Crit("err", err)
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

	rupicolaProcessor := RupicolaProcessor{config: configuration, limits: configuration.Limits, processor: rupicolarpc.NewJsonRpcProcessor()}

	for k, v := range configuration.Methods {
		var metype rupicolarpc.MethodType
		if v.Streamed {
			metype = rupicolarpc.StreamingMethodLegacy
		} else {
			metype = rupicolarpc.RpcMethod
		}
		rupicolaProcessor.processor.AddMethod(k, metype, v)
	}

	failureChannel := make(chan error)
	for _, bind := range configuration.Protocol.Bind {
		// create local variable bind
		bind := bind
		child := RupicolaProcessorChild{&rupicolaProcessor, bind}
		mux := http.NewServeMux()
		mux.Handle(configuration.Protocol.Uri.Rpc, &child)
		mux.Handle(configuration.Protocol.Uri.Streamed, &child)

		srv := &http.Server{Addr: bind.Address + ":" + strconv.Itoa(int(bind.Port)), Handler: mux}

		if configuration.Limits.ReadTimeout > 0 {
			srv.ReadTimeout = configuration.Limits.ReadTimeout * time.Millisecond
		}

		go func() {
			switch bind.Type {
			case Http:
				log.Info("starting listener", "type", "http", "address", bind.Address, "port", bind.Port)
				failureChannel <- srv.ListenAndServe()

			case Https:
				log.Info("atarting listener", "type", "https", "address", bind.Address, "port", bind.Port)
				failureChannel <- srv.ListenAndServeTLS(bind.Cert, bind.Key)

			case Unix:
				//todo: check
				log.Info("atarting listener", "type", "unix", "address", bind.Address)
				srv.Addr = bind.Address
				ln, err := net.Listen("unix", bind.Address)
				if err != nil {
					failureChannel <- err
				} else {
					failureChannel <- srv.Serve(ln)
				}
			}
		}()
	}

	err = <-failureChannel
	log.Crit("Program will shut down now due to encountered error", "error", err)
	os.Exit(1)
}
