package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"

	"./rupicolarpc"
	"github.com/spf13/pflag"
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
	// Accept only POST
	if r.Method != "POST" {
		log.Printf("Method '%s' not allowed\n", r.Method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	// Check for "payload size". According to spec
	// Payload = 0 mean ignore
	if s.parent.limits.PayloadSize > 0 && r.ContentLength > int64(s.parent.limits.PayloadSize) {
		w.WriteHeader(http.StatusBadRequest)
		log.Println("Request to big")
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
		case "":
		case "1":
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
		err := json.NewEncoder(w).Encode(rupicolarpc.NewError(RpcUnauthorizedError))
		if err != nil {
			log.Panicln(err)
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
			log.Println("Unauth")
			return nil, nil, RpcUnauthorizedError
		}
	} else {
		log.Fatalln("Provided context is not pointer")
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
			log.Println(err)
			return nil, nil, err
		}
		if !skip {
			appArguments = append(appArguments, buffer.String())
		}
		buffer.Reset()
	}

	log.Println(appArguments)
	process := exec.Command(m.InvokeInfo.Exec, appArguments...)

	// Make it "better"
	SetUserGroup(process, m)

	stdin, err := process.StdinPipe()
	if err == nil {
		stdin.Close()
	} else {
		log.Println(err)
		return nil, nil, rupicolarpc.NewStandardErrorData(rupicolarpc.InternalError, "stdin")
	}

	return castedContext, process, nil
}

// Supported is implementation of jsonrpc.Invoker
func (m *MethodDef) Supported() rupicolarpc.MethodType {
	if m.Streamed {
		return rupicolarpc.StreamingMethodLegacy
	}
	return rupicolarpc.RpcMethod
}

// Invoke is implementation of jsonrpc.Invoker
func (m *MethodDef) Invoke(req rupicolarpc.JsonRpcRequest, context interface{}, out io.Writer) error {
	r, process, err := m.prepareCommand(req, context)
	if err != nil {
		return err
	}
	stdout, err := process.StdoutPipe()
	if err == nil {
		log.Println(stdout)
	} else {
		return rupicolarpc.NewStandardErrorData(rupicolarpc.InternalError, "stdout")
	}
	err = process.Start()
	if err != nil {
		log.Println(err)
		return rupicolarpc.NewStandardErrorData(rupicolarpc.InternalError, "start")
	}
	log.Println(err)

	byteReadChunk := make([]byte, commandBufferSize)
	var writerLen uint32
	var writer io.Writer
	if m.Encoding == Base64 {
		writerEncoder := base64.NewEncoder(base64.URLEncoding, out)
		// should we defer, or err check?
		defer writerEncoder.Close()
		writer = writerEncoder
	} else {
		writer = out
	}

	//todo: this is fooked up
	heartBeat := make(chan error)
	var timer <-chan time.Time
	// Exec timeout, czas na CALKOWITE wykonanie (tylko rpc)
	if r.parent.limits.ExecTimeout > 0 {
		timer = time.After(r.parent.limits.ExecTimeout)
	}

	// TODO: Run some task in bg?

	go func() {

		time.Sleep(m.InvokeInfo.Delay)

		for true {
			read, err := stdout.Read(byteReadChunk)
			if err != nil {
				stdout.Close()

				log.Println(err)
				heartBeat <- err
				break
			}

			wr, e := writer.Write(byteReadChunk[0:read])

			if e != nil {
				stdout.Close()
				heartBeat <- rupicolarpc.NewStandardErrorData(rupicolarpc.InternalError, "write")
			}
			writerLen += uint32(wr)
			if r.parent.limits.MaxResponse > 0 && writerLen > r.parent.limits.MaxResponse {
				stdout.Close()
				heartBeat <- rupicolarpc.NewStandardErrorData(rupicolarpc.InternalError, "limit")
			}
		}
	}()

	// TODO: Check thys
	if m.InvokeInfo.Delay != 0 {
		// for "delayed" execution we cannot provide meaningful data
		// so if we haven't failed before just
		_, err := io.WriteString(writer, "OK")
		return err
	}

	select {
	case beat := <-heartBeat:
		if beat == io.EOF {
			return nil
		}
		return beat
	case <-timer:
		log.Println("Timeout for Exec")
		return errors.New("Timeout")

	}
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
		log.Fatalln(err)
	}
	if len(configuration.Methods) == 0 {
		log.Fatalln("No method defined in config")
	}
	if len(configuration.Protocol.Bind) == 0 {
		log.Fatalf("No valid bind points")
	}

	rupicolaProcessor := RupicolaProcessor{config: configuration, limits: configuration.Limits, processor: rupicolarpc.NewJsonRpcProcessor()}

	for k, v := range configuration.Methods {
		rupicolaProcessor.processor.AddMethod(k, v)
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
				log.Printf("Starting HTTP listener %s@%d\n", bind.Address, bind.Port)
				failureChannel <- srv.ListenAndServe()

			case Https:
				log.Printf("Starting HTTPS listener %s@%d\n", bind.Address, bind.Port)
				failureChannel <- srv.ListenAndServeTLS(bind.Cert, bind.Key)

			case Unix:
				//todo: check
				log.Printf("Starting UNIX listener %s\n", bind.Address)
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
	log.Printf("Program will shut down now due to encountered error %v\n", err)
	os.Exit(1)
}
