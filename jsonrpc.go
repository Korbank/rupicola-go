package main

import "log"
import "encoding/json"
import "io"
import "bytes"
import "errors"
import "fmt"
import "strconv"
import "os/exec"

// Yes, real jsonrpc server would need to
// handle all possible options but we
// convert them to string anyway...
type JsonRpcRequestOptions struct {
	Options map[string]string
}

type JsonRpcRequest struct {
	Jsonrpc string `json:"jsonrpc"`
	Method  string `json:"method"`
	Options JsonRpcRequestOptions
	// can be any json valid type (including null)
	// or not present at all
	ID *interface{} `json:"id"`
}

func (w *JsonRpcRequestOptions) UnmarshalJSON(data []byte) error {
	//todo: discard objects?
	var everything interface{}

	var unified map[string]string
	if err := json.Unmarshal(data, &everything); err == nil {
		switch converted := everything.(type) {
		case map[string]interface{}:
			unified = make(map[string]string, len(converted))
			for k, v := range converted {
				unified[k] = fmt.Sprint(v)
			}
		case []interface{}:
			unified = make(map[string]string, len(converted))
			for i, v := range converted {
				// Count arguments from 1 to N
				unified[strconv.Itoa(i+1)] = fmt.Sprint(v)
			}

		default:
			log.Println("Invalid case")
			return errors.New("Expected array or object")
		}
		log.Println(unified)
		w.Options = unified
		return nil
	} else {
		return err
	}
}

func (self JsonRpcRequest) IsValid() bool {
	return self.Jsonrpc == "2.0" && self.Method != ""
}

type JsonRpcResponse struct {
	Jsonrpc string      `json:"jsonrpc"`
	Error   *_Error     `json:"error,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	ID      interface{} `json:"id,omitempty"`
}

func NewResult(a interface{}) JsonRpcResponse {
	return JsonRpcResponse{"2.0", nil, a, nil}
}

func NewError(a *_Error) JsonRpcResponse {
	return JsonRpcResponse{"2.0", a, nil, nil}
}

type Argument struct {
	Name      string
	Required  bool
	childrens []*Argument
	constans  bool
	value     string
}

func NewDynamicArgument(name string, required bool) *Argument {
	a := &Argument{}
	a.constans = false
	a.Name = name
	a.Required = required
	return a
}

func NewStaticArgument(name string, required bool, value string) *Argument {
	a := NewDynamicArgument(name, required)
	a.constans = true
	a.value = value
	return a
}

func (a *Argument) AddArgument(arg *Argument) {
	a.childrens = append(a.childrens, arg)
}

type Jober interface {
	Jober(arguments []string) (*string, *_Error)
}

type Method struct {
	args []*Argument
	path string
	job  Jober
}

type ExecMethod struct {
	path string
}

func (m *ExecMethod) InvokeMe(args []string) (*string, *_Error) {
	return nil, nil
}

type _Error struct {
	internal error
	Code     int         `json:"code"`
	Message  string      `json:"message"`
	Data     interface{} `json:"data,omitempty"`
}

/*code 	message 	meaning
-32700 	Parse error 	Invalid JSON was received by the server.
An error occurred on the server while parsing the JSON text.
-32600 	Invalid Request 	The JSON sent is not a valid Request object.
-32601 	Method not found 	The method does not exist / is not available.
-32602 	Invalid params 	Invalid method parameter(s).
-32603 	Internal error 	Internal JSON-RPC error.
-32000 to -32099 	Server error 	Reserved for implementation-defined server-errors.*/
type StandardErrorType int

const (
	ParseError        StandardErrorType = -32700
	InvalidRequest                      = -32600
	MethodNotFound                      = -32601
	InvalidParams                       = -32602
	InternalError                       = -32603
	_ServerErrorStart                   = -32000
	_ServerErrorEnd                     = -32099
)

func _NewServerError(code int, message string) *_Error {
	if code < int(_ServerErrorStart) || code > int(_ServerErrorEnd) {
		log.Panic("Invalid code", code)
	}
	var theError _Error
	theError.Code = code
	theError.Message = message
	return &theError
}

func _NewStandardError(code StandardErrorType) *_Error {
	var theError _Error
	theError.Code = int(code)
	switch code {
	case ParseError:
		theError.Message = "Parse error"
	case InvalidRequest:
		theError.Message = "Invalid Request"
	case MethodNotFound:
		theError.Message = "Method not found"
	case InvalidParams:
		theError.Message = "Invalid params"
	case InternalError:
		theError.Message = "Internal error"
	default:
		log.Panic("WTF")
	}
	return &theError
}

func _New_Error(code int, desc string) *_Error {
	return &_Error{nil, code, desc, nil}
}

func _WrapError(any error) *_Error {
	a := &_Error{any, 0, "", nil}
	return a
}

func (e _Error) Error() string    { return e.Message }
func (e *_Error) Internal() error { return e.internal }

func _evalueateArgs(arg *Argument, arguments map[string]string, output *bytes.Buffer) *_Error {
	if arg.constans {
		_, e := output.WriteString(arg.value)
		if e != nil {
			return _WrapError(e)
		}
	} else {
		value, has := arguments[arg.Name]
		if has {
			output.WriteString(value)
			for _, arg := range arg.childrens {
				if err := _evalueateArgs(arg, arguments, output); err != nil {
					return err
				}
			}
		} else {
			if arg.Required {
				//required option not found
				return _NewStandardError(InvalidParams)
			}
			_, e := output.WriteString(arg.value)
			if e != nil {
				return _WrapError(e)
			}
		}
	}

	return nil
}

func (m *Method) Invoke(arguments map[string]string) (*string, *_Error) {
	buffer := bytes.NewBuffer(make([]byte, 0, 1024))
	appArguments := make([]string, 0, len(m.args))
	for _, arg := range m.args {
		err := _evalueateArgs(arg, arguments, buffer)
		if err != nil {
			return nil, _WrapError(err)
		}
		appArguments = append(appArguments, buffer.String())
		buffer.Reset()
	}

	log.Println(appArguments)
	process := exec.Command(m.path, appArguments...)
	// On Linux
	//process.SysProcAttr = &syscall.SysProcAttr{}
	//process.SysProcAttr.Credential = &syscall.Credential{Uid: uid, Gid: gid}

	stdin, err := process.StdinPipe()
	if err == nil {
		stdin.Close()
	} else {
		return nil, _NewStandardError(InternalError)
	}
	stdout, err := process.StdoutPipe()
	if err == nil {
		log.Println(stdout)
	} else {
		return nil, _NewStandardError(InternalError)
	}
	err = process.Start()
	log.Println(err)

	buffer.Reset()

	byteReadChunk := make([]byte, 512)
	for true {
		read, err := stdout.Read(byteReadChunk)
		if err != nil {
			log.Println(err)
			break
		}
		_, e := buffer.Write(byteReadChunk[0:read])
		if e != nil {
			return nil, _NewStandardError(InternalError)
		}
	}
	a := buffer.String()
	return &a, nil
}

type JsonRpcProcessor struct {
	_methods map[string]Method
}

func NewJsonRpcProcessor() JsonRpcProcessor {
	return JsonRpcProcessor{}
}

func (p *JsonRpcProcessor) Process(data io.Reader, response io.Writer) error {
	jsonDecoder := json.NewDecoder(data)
	var request JsonRpcRequest
	if err := jsonDecoder.Decode(&request); err != nil {
		// well we should write response here
		log.Println(err)
		return err
	}

	method, has := p._methods[request.Method]
	var jsonResponse JsonRpcResponse
	// Assign last known ID
	jsonResponse.ID = request.ID

	has = true
	if has {
		method = Method{[]*Argument{&Argument{"arg0", true, nil, true, "/C"}, &Argument{"2", true, nil, false, ""}}, "cmd", nil}

		resp, err := method.Invoke(request.Options.Options)
		if err == nil {
			jsonResponse = NewResult(resp)
		} else {
			jsonResponse = NewError(err)
		}
	} else {
		jsonResponse = NewError(_NewStandardError(MethodNotFound))
	}

	encoder := json.NewEncoder(response)
	return encoder.Encode(jsonResponse)
}
