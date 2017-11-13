package rupicolarpc

import log "github.com/inconshreveable/log15"
import "encoding/json"
import "io"

import "fmt"
import "strconv"
import "bytes"
import "time"
import "context"
import "errors"

type MethodType int

const (
	RpcMethod             MethodType = 1
	StreamingMethodLegacy            = 2
	StreamingMethod                  = 4
	unknownMethod                    = 0
)

type Invoker interface {
	//Invoke(request JsonRpcRequest, context interface{}, writer io.Writer) error
	Invoke2(request JsonRpcRequest, context interface{}) (interface{}, error)
	//Supported() MethodType
}

// Yes, real jsonrpc server would need to
// handle all possible options but we
// convert them to string anyway...
type JsonRpcRequestOptions map[string]interface{}

// JsonRpcRequest
type JsonRpcRequest struct {
	Jsonrpc string
	Method  string
	Params  JsonRpcRequestOptions
	// can be any json valid type (including null)
	// or not present at all
	ID *interface{}
}

// UnmarshalJSON is custom unmarshal for JsonRpcRequestOptions
func (w *JsonRpcRequestOptions) UnmarshalJSON(data []byte) error {
	//todo: discard objects?
	var everything interface{}

	var unified map[string]interface{}
	if err := json.Unmarshal(data, &everything); err != nil {
		return err
	}

	switch converted := everything.(type) {
	case map[string]interface{}:
		unified = converted
	case []interface{}:
		unified = make(map[string]interface{}, len(converted))
		for i, v := range converted {
			// Count arguments from 1 to N
			unified[strconv.Itoa(i+1)] = fmt.Sprint(v)
		}

	default:
		//log.Printf("Invalid case %v\n", converted)
		return NewStandardError(InvalidRequest)
	}
	*w = unified
	return nil

}

func (r *JsonRpcRequest) isValid() bool {
	return r.Jsonrpc == "2.0" && r.Method != ""
}

type JsonRpcResponse struct {
	Jsonrpc string      `json:"jsonrpc"`
	Error   *_Error     `json:"error,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	ID      interface{} `json:"id,omitempty"`
}

func NewResult(a interface{}, id interface{}) *JsonRpcResponse {
	return &JsonRpcResponse{"2.0", nil, a, id}
}

func NewError(a error, id interface{}) *JsonRpcResponse {
	var b *_Error
	switch err := a.(type) {
	case *_Error:
		b = err
	default:
		log.Debug("Should not happen - wrapping unknown error:", "err", a)
		b, _ = NewStandardErrorData(InternalError, err.Error()).(*_Error)
	}
	return &JsonRpcResponse{"2.0", b, nil, id}
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

func NewServerError(code int, message string) error {
	if code > int(_ServerErrorStart) || code < int(_ServerErrorEnd) {
		log.Crit("Invalid code", "code", code)
		panic("invlid code")
	}
	var theError _Error
	theError.Code = code
	theError.Message = message
	return &theError
}

func NewStandardErrorData(code StandardErrorType, data interface{}) error {
	var theError _Error
	theError.Code = int(code)
	theError.Data = data
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
		log.Crit("WTF")
		panic("WTF")
	}
	return &theError
}

func NewStandardError(code StandardErrorType) error {
	return NewStandardErrorData(code, nil)
}

func _New_Error(code int, desc string) error {
	return &_Error{nil, code, desc, nil}
}

func (e *_Error) Error() string {
	if e.internal != nil {
		return e.internal.Error()
	}
	return e.Message
}
func (e *_Error) Internal() error { return e.internal }

type MethodFunc func(in JsonRpcRequest, ctx interface{}) (interface{}, error)

func (m MethodFunc) Invoke(in JsonRpcRequest, ctx interface{}, out io.Writer) error {
	return nil
}
func (m MethodFunc) Invoke2(in JsonRpcRequest, ctx interface{}) (interface{}, error) {
	return m(in, ctx)
}

type LimitedWriter struct {
	W io.Writer
	N int64
}

func LimitWrite(w io.Writer, n int64) io.Writer {
	return &LimitedWriter{w, n}
}

func (w *LimitedWriter) Write(p []byte) (n int, err error) {
	if w.N <= 0 || int64(len(p)) > w.N {
		return 0, io.ErrShortWrite
	}

	n, err = w.W.Write(p)
	w.N -= int64(n)
	return
}

type Limits struct {
	ReadTimeout time.Duration
	ExecTimeout time.Duration
	MaxResponse uint32
}
type JsonRpcProcessor struct {
	methods map[string]map[MethodType]Invoker
	Limits  Limits
}

func (p *JsonRpcProcessor) AddMethod(name string, metype MethodType, method Invoker) {
	container, ok := p.methods[name]
	if !ok {
		container = make(map[MethodType]Invoker)
		p.methods[name] = container
	}
	container[metype] = method
}

func (p *JsonRpcProcessor) AddMethodFunc(name string, metype MethodType, method MethodFunc) {
	p.AddMethod(name, metype, method)
}

func NewJsonRpcProcessor() *JsonRpcProcessor {
	return &JsonRpcProcessor{
		make(map[string]map[MethodType]Invoker),
		Limits{10 * time.Second, 0, 5242880},
	}

}

func ParseJsonRpcRequest(reader io.Reader) (JsonRpcRequest, error) {
	jsonDecoder := json.NewDecoder(reader)
	var request JsonRpcRequest
	err := jsonDecoder.Decode(&request)
	return request, err
}
func (p *JsonRpcProcessor) processWrapper(data io.Reader, response io.Writer, ctx interface{}, metype MethodType) *JsonRpcResponse {
	jsonDecoder := json.NewDecoder(data)
	var request JsonRpcRequest

	if err := jsonDecoder.Decode(&request); err != nil {
		return NewError(NewStandardError(ParseError), nil)
	}

	if !request.isValid() {
		return NewError(NewStandardError(InvalidRequest), request.ID)
	}
	// check if method with given name exists
	ms, okm := p.methods[request.Method]
	if !okm {
		log.Error("not found", "method", request.Method)
		return NewError(NewStandardError(MethodNotFound), request.ID)
	}
	// now check is requested type is supported
	m, okm := ms[metype]
	if !okm {
		log.Error("required type for method not found\n", "type", metype, "method", request.Method)
		return NewError(NewStandardError(MethodNotFound), request.ID)
	}

	// TIMEOUT HANDLING START
	kontext := context.Background()
	if metype == RpcMethod && p.Limits.ExecTimeout > 0 {
		ctx, cancel := context.WithTimeout(kontext, p.Limits.ExecTimeout)
		defer cancel()
		kontext = ctx
	}

	var result interface{}
	var err error
	done := make(chan struct{})
	go func() {
		result, err = m.Invoke2(request, ctx)
		close(done)
	}()

	select {
	case <-done:
		log.Debug("Done")
	case <-kontext.Done():
		log.Debug("timeout", "err", kontext.Err())
		return NewError(errors.New("Timeout"), request.ID)
	}
	// TIMEOUT HANDLING END

	// Rpc method could return reader and inside use routine to provide endless stream of data
	// to prevent this we are using same exec context and previously set timeout
	switch result := result.(type) {
	case io.Reader:
		var resultOut *JsonRpcResponse
		done = make(chan struct{})
		go func() {
			defer close(done)

			// Legacy streaming is dumb, just copy bytes from source to targes
			switch metype {
			case StreamingMethodLegacy:
				io.Copy(response, result)
				resultOut = nil
			case StreamingMethod:
				log.Crit("UNIMPLEMENTED")
				resultOut = NewResult("OK", request.ID)
			case RpcMethod:
				buffer := bytes.NewBuffer(nil)
				io.Copy(buffer, result)
				resultOut = NewResult(buffer.String(), request.ID)
			default:
				log.Crit("Should never happen")
				resultOut = nil
				panic("AAA")
			}
		}()
		select {
		case <-done:
			return resultOut
		case <-kontext.Done():
			return NewError(errors.New("Timeout"), request.ID)
		}
	default:
		return NewResult(result, request.ID)
	}
}

func (p *JsonRpcProcessor) Process(data io.Reader, response io.Writer, context interface{}, metype MethodType) error {
	rponse := p.processWrapper(data, response, context, metype)
	if rponse != nil {
		if rponse.ID == nil {
			return nil
		}

		if metype == RpcMethod || metype == StreamingMethod {
			encoder := json.NewEncoder(response)
			return encoder.Encode(rponse)
		}
		if rponse.Error == nil {
			switch result := rponse.Result.(type) {
			case string:
				_, err := response.Write([]byte(result))
				return err
			default:
				log.Crit("WTH")
				panic("AAA")
			}
		} else {
			return nil
		}
	}
	return nil
}
