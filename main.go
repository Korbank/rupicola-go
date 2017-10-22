package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"

	"github.com/spf13/pflag"
	"github.com/yhat/phash"
)

type RupicolaRpcContext struct {
	isAuthorized      bool
	allowPrivate      bool
	shouldRequestAuth bool
	isRpc             bool
}

type StreamProcessor struct {
}

type RupicolaProcessor struct {
	methods         map[string]MethodDef
	limits          Limits
	processor       *JsonRpcProcessor
	streamProcessor *StreamProcessor
	config          *RupicolaConfig
}
type RupicolaProcessorChild struct {
	parent *RupicolaProcessor
	bind   *Bind
}

func (s *RupicolaProcessorChild) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Accept only POST
	if r.Method != "POST" {
		w.Write([]byte("Unsupported"))
		return
	}
	// Check for "payload size". According to spec
	// Payload = 0 mean ignore
	if s.parent.limits.PayloadSize > 0 && r.ContentLength > int64(s.parent.limits.PayloadSize) {
		log.Println("Request to big")
		return
	}

	var context RupicolaRpcContext
	if r.RequestURI == s.parent.config.Protocol.Uri.Rpc {
		context.isRpc = true
	} else if r.RequestURI == s.parent.config.Protocol.Uri.Streamed {
		context.isRpc = false
	} else {
		return
	}
	// Use "basic auth" only if specified in config
	if s.parent.config.Protocol.AuthBasic.Login != "" {
		login, password, ok := r.BasicAuth()
		if !ok {
			context.isAuthorized = false
		} else {
			context.isAuthorized = s.parent.isValidAuth(login, password)
		}
	} else {
		context.isAuthorized = true
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if ip.IsLoopback() {
		// TODO: Detect correct bind point
		context.allowPrivate = s.bind.AllowPrivate
	}

	if !context.isAuthorized && !context.allowPrivate {
		err := json.NewEncoder(w).Encode(NewError(_NewServerError(-32000, "Unauthorized")))
		log.Panicln(err)
	} else {
		if context.isRpc {
			s.parent.processor.Process(r.Body, w, &context)
		} else {
			rpcReq, err := ParseJsonRpcRequest(r.Body)
			if err == nil {
				s.parent.InvokeV2(rpcReq, &context, w)
			} else {
				log.Println(err)
			}
		}
	}
}

func (r *RupicolaProcessor) isValidAuth(login string, password string) bool {
	// NOTE: Verify method is not time constant!
	passOk := phash.Verify(password, r.config.Protocol.AuthBasic.password)
	loginOk := login == r.config.Protocol.AuthBasic.Login
	return passOk || loginOk
}

func (p *StreamProcessor) Process(reader io.Reader, writer io.Writer, context interface{}) error {
	// This is version 1, just start streaming
	return nil
}

func (arg *MethodArgs) _evalueateArgs(arguments map[string]string, output *bytes.Buffer) (bool, error) {
	if arg.Static {
		_, e := output.WriteString(arg.Param)
		if e != nil {
			return false, _WrapError(e)
		}
	} else {
		value, has := arguments[arg.Param]
		if arg.compound != (len(arg.Child) != 0) {
			log.Panicln("Oooh..")
		}
		if arg.Param == "self" {
			has = true
			bytes, ok := json.Marshal(arguments)
			if ok == nil {
				value = string(bytes)
			} else {
				return false, NewStandardErrorData(InternalError, "self")
			}
		}
		if has || arg.compound {
			// We should skip expanding for markers only
			if !arg.Skip {
				output.WriteString(value)
			}
			var skipCompound bool
			for _, arg := range arg.Child {
				var skip bool
				var err error
				if skip, err = arg._evalueateArgs(arguments, output); err != nil {
					return skip, err
				}
				skipCompound = skipCompound || skip
			}
			return skipCompound, nil
		}

		// All arguments in param are filtered
		// so we are sure that we don't have "wild" param in args
		return arg.Skip, nil
	}

	return false, nil
}

func (r *RupicolaProcessor) prepareCommand(req JsonRpcRequest, context interface{}) (*MethodDef, *exec.Cmd, error) {
	log.SetPrefix(req.Method)
	m, okm := r.methods[req.Method]
	castedContext, ok := context.(*RupicolaRpcContext)
	if ok {
		if !castedContext.isAuthorized && castedContext.allowPrivate && m.Private {
			castedContext.shouldRequestAuth = true
			log.Println("Unauth")
			return nil, nil, _NewServerError(-32000, "Unauthorized")
		}
	} else {
		log.Fatalln("Provided context is not pointer")
		return nil, nil, NewStandardError(InternalError)
	}

	if !okm || okm && m.Streamed == castedContext.isRpc {
		log.Println("Not found")
		return nil, nil, NewStandardError(MethodNotFound)
	}

	// Check if required arguments are present
	for name, arg := range m.Params {
		va, ok := req.Params.Params[name]
		val := interface{}(va)
		if !ok && !arg.Optional {
			log.Println("invalid param")
			return nil, nil, NewStandardError(InvalidParams)
		}

		switch arg.Type {
		case String:
			_, ok = val.(string)
		case Int:
			_, ok = val.(int)
		case Bool:
			_, ok = val.(bool)
		default:
			ok = false
		}
		if !ok {
			log.Println("invalid param")
			return nil, nil, NewStandardError(InvalidParams)
		}
	}
	buffer := bytes.NewBuffer(make([]byte, 0, 1024))
	appArguments := make([]string, 0, len(m.Invoke.Args))
	for _, arg := range m.Invoke.Args {
		skip, err := arg._evalueateArgs(req.Params.Params, buffer)
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
	process := exec.Command(m.Invoke.Exec, appArguments...)
	// On Linux
	//process.SysProcAttr = &syscall.SysProcAttr{}
	//process.SysProcAttr.Credential = &syscall.Credential{Uid: m.Invoke.RunAs.Uid, Gid: m.Invoke.RunAs.Gid}

	stdin, err := process.StdinPipe()
	if err == nil {
		stdin.Close()
	} else {
		log.Println(err)
		return nil, nil, NewStandardErrorData(InternalError, "stdin")
	}

	return &m, process, nil
}

func (r *RupicolaProcessor) InvokeV2(req JsonRpcRequest, context interface{}, writer io.Writer) error {
	m, process, err := r.prepareCommand(req, context)
	if err != nil {
		return err
	}
	stdout, err := process.StdoutPipe()
	if err == nil {
		log.Println(stdout)
	} else {
		return NewStandardErrorData(InternalError, "stdout")
	}
	err = process.Start()
	if err != nil {
		return NewStandardErrorData(InternalError, "start")
	}
	log.Println(err)

	byteReadChunk := make([]byte, 512)
	var writerLen uint32
	var writerEnc io.Writer
	if m.Encoding == Base64 {
		writerEncoder := base64.NewEncoder(base64.URLEncoding, writer)
		// should we defer, or err check?
		defer writerEncoder.Close()
		writerEnc = writerEncoder
	} else {
		writerEnc = writer
	}

	defer stdout.Close()

	for true {
		read, err := stdout.Read(byteReadChunk)
		if err != nil {
			log.Println(err)
			break
		}

		wr, e := writerEnc.Write(byteReadChunk[0:read])

		if e != nil {
			return NewStandardErrorData(InternalError, "write")
		}
		writerLen += uint32(wr)
		if r.limits.MaxResponse > 0 && writerLen > r.limits.MaxResponse {
			return NewStandardErrorData(InternalError, "limit")
		}
	}
	return nil
}

func (r *RupicolaProcessor) Invoke(req JsonRpcRequest, context interface{}) (string, error) {
	outputBuffer := bytes.NewBuffer(nil)
	err := r.InvokeV2(req, context, outputBuffer)
	return outputBuffer.String(), err
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
		log.Fatalln(err)
	}
	rupicolaProcessor := RupicolaProcessor{}
	rupicolaProcessor.config = configuration
	rupicolaProcessor.limits = configuration.Limits
	rupicolaProcessor.methods = configuration.Methods
	rupicolaProcessor.processor = NewJsonRpcProcessor(&rupicolaProcessor)

	for _, bind := range configuration.Protocol.Bind {
		if bind.Type == "http" {
			child := RupicolaProcessorChild{&rupicolaProcessor, &bind}
			mux := http.NewServeMux()
			mux.Handle(configuration.Protocol.Uri.Rpc, &child)
			mux.Handle(configuration.Protocol.Uri.Streamed, &child)
			log.Fatal(http.ListenAndServe(bind.Address+":"+strconv.Itoa(int(bind.Port)), mux))
		}
	}
	log.Panic("AAA!!")
}
