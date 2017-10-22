package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os/exec"
	"strconv"

	"github.com/yhat/phash"
)

func (s *RupicolaProcessorChild) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.Write([]byte("Unsupported"))
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
	login, password, ok := r.BasicAuth()
	if s.parent.config.Protocol.AuthBasic.Login != "" {
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
		s.parent.processor.Process(r.Body, w, &context)
	}
}

func (r *RupicolaProcessor) isValidAuth(login string, password string) bool {
	// NOTE: Verify method is not time constant!
	passOk := phash.Verify(password, r.config.Protocol.AuthBasic.password)
	loginOk := login == r.config.Protocol.AuthBasic.Login
	return passOk || loginOk
}

type RupicolaRpcContext struct {
	isAuthorized      bool
	allowPrivate      bool
	shouldRequestAuth bool
	isRpc             bool
}
type RupicolaProcessor struct {
	methods   map[string]MethodDef
	limits    Limits
	processor *JsonRpcProcessor
	config    RupicolaConfig
}
type RupicolaProcessorChild struct {
	parent *RupicolaProcessor
	bind   *Bind
}

func _evalueateArgs(arg MethodArgs, arguments map[string]string, output *bytes.Buffer) (bool, error) {
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
				if skip, err = _evalueateArgs(arg, arguments, output); err != nil {
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
func (r *RupicolaProcessor) InvokeV2(req JsonRpcRequest, context interface{}, writer io.Writer) error {
	log.SetPrefix(req.Method)
	m, ok := r.methods[req.Method]
	castedContext, ok := context.(*RupicolaRpcContext)
	if ok {
		if !castedContext.isAuthorized && castedContext.allowPrivate && m.Private {
			castedContext.shouldRequestAuth = true
			return _NewServerError(-32000, "Unauthorized")
		}
	} else {
		return NewStandardError(InternalError)
	}

	if !ok || ok && m.Streamed == castedContext.isRpc {
		log.Println("Not found")
		return NewStandardError(MethodNotFound)
	}

	// Check if required arguments are present
	for name, arg := range m.Params {
		va, ok := req.Params.Params[name]
		val := interface{}(va)
		if !ok && !arg.Optional {
			return NewStandardError(InvalidParams)
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
			return NewStandardError(InvalidParams)
		}
	}
	buffer := bytes.NewBuffer(make([]byte, 0, 1024))
	appArguments := make([]string, 0, len(m.Invoke.Args))
	for _, arg := range m.Invoke.Args {
		skip, err := _evalueateArgs(arg, req.Params.Params, buffer)
		if err != nil {
			return err
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
		return NewStandardErrorData(InternalError, "stdin")
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
	for true {
		read, err := stdout.Read(byteReadChunk)
		if err != nil {
			log.Println(err)
			break
		}
		wr, e := writer.Write(byteReadChunk[0:read])
		if e != nil {
			return NewStandardErrorData(InternalError, "write")
		}
		writerLen += uint32(wr)
		if writerLen > r.limits.MaxResponse {
			return NewStandardErrorData(InternalError, "limit")
		}
	}
	return nil
}

func (r *RupicolaProcessor) Invoke(req JsonRpcRequest, context interface{}) (string, error) {
	outputBuffer := bytes.NewBuffer(nil)
	r.InvokeV2(req, context, outputBuffer)
	return outputBuffer.String(), nil
}

func main() {
	rupicolaProcessor := RupicolaProcessor{}
	configuration := Fuu()
	/*for name, methodDef := range configuration.Methods {
		log.Printf("%v %v", name, methodDef)
	}*/
	rupicolaProcessor.config = configuration
	rupicolaProcessor.limits = configuration.Limits
	rupicolaProcessor.methods = configuration.Methods
	rupicolaProcessor.processor = NewJsonRpcProcessor(&rupicolaProcessor)

	//return
	//{\"jsonrpc\":\"2.0\",\"method\":\"m\",\"options\":[1,2,3]}
	//jsonRequest := "[{\"jsonrpc\":\"2.0\",\"method\":\"m\",\"options\":[1,2,3]},{\"jsonrpc\":\"2.0\",\"method\":\"me\",\"options\":[1,2,3]}]"
	//jsonRequest := "{\"jsonrpc\":\"2.0\",\"method\":\"m\",\"options\":[1,2,3]}"
	//jsonRequest := "{\"jsonrpc\":\"2.0\",\"method\":\"upgrade\",\"options\":[\"1\",\"echo\",\"3\"],\"id\":0}"
	jsonRequest := "{\"jsonrpc\":\"2.0\",\"method\":\"upgrade\",\"params\":{\"urit\":\"echo\"},\"id\":0}"

	response := bytes.NewBuffer(nil)
	err := rupicolaProcessor.processor.Process(bytes.NewReader([]byte(jsonRequest)), response, nil)
	log.Println(err)
	log.Println(response.String())
	//var request []JsonRpcRequest
	//json.Unmarshal([]byte(jsonRequest), &request)
	//log.Println(request)
	//json.Unmarshal([]byte(jsonRequest), &request[0])
	//log.Println(request)
	//log.Println(request.Jsonrpc)
	//log.Println(request.Method)
	//log.Println(request.Options)
	//log.Println("hello world")
	//log.Println(request.Options.OptionsMap)
	//log.Println(request.Options.OptionsTab)
	//log.Println("DDD")

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
