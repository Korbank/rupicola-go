package rupicolarpc

import "log"
import "encoding/json"
import "io"

import "fmt"
import "strconv"
import "bytes"

type MethodType int

const (
	RpcMethod             MethodType = 0
	StreamingMethodLegacy            = 1
	StreamingMethod                  = 2
)

type Invoker interface {
	Invoke(request JsonRpcRequest, context interface{}, writer io.Writer) error
	Supported() MethodType
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

func (r *JsonRpcRequest) IsValid() bool {
	return r.Jsonrpc == "2.0" && r.Method != ""
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

func NewError(a error) JsonRpcResponse {
	var b *_Error
	switch err := a.(type) {
	case *_Error:
		b = err
	default:
		log.Printf("Should not happen - wrapping unknown error: %v", a)
		b, _ = NewServerError(InternalError, err.Error()).(*_Error)
	}
	return JsonRpcResponse{"2.0", b, nil, nil}
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
		log.Panic("Invalid code", code)
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
		log.Panic("WTF")
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

type MethodFunc func(in JsonRpcRequest, ctx interface{}, out io.Writer) error

func (m MethodFunc) Invoke(in JsonRpcRequest, ctx interface{}, out io.Writer) error {
	return m(in, ctx, out)
}
func (m MethodFunc) Supported() MethodType { return RpcMethod }

type JsonRpcProcessor struct {
	methods map[string]Invoker
}

func (p *JsonRpcProcessor) AddMethod(name string, method Invoker) {
	p.methods[name] = method
}

func (p *JsonRpcProcessor) AddMethodFunc(name string, method MethodFunc) {
	p.AddMethod(name, method)
}

func NewJsonRpcProcessor() *JsonRpcProcessor {
	return &JsonRpcProcessor{make(map[string]Invoker)}

}

func ParseJsonRpcRequest(reader io.Reader) (JsonRpcRequest, error) {
	jsonDecoder := json.NewDecoder(reader)
	var request JsonRpcRequest
	err := jsonDecoder.Decode(&request)
	return request, err
}

func (p *JsonRpcProcessor) Process(data io.Reader, response io.Writer, context interface{}, metype MethodType) error {
	jsonDecoder := json.NewDecoder(data)
	var request JsonRpcRequest
	var jsonResponse JsonRpcResponse
	var tempBuffer *bytes.Buffer

	if err := jsonDecoder.Decode(&request); err != nil {
		// well we should write response here
		switch err := err.(type) {
		case *_Error:
			jsonResponse = NewError(err)
		default:
			jsonResponse = NewError(NewStandardError(ParseError))
		}
		log.Println(err)

	} else {
		if request.IsValid() {
			m, okm := p.methods[request.Method]
			if !okm || okm && m.Supported() != metype {
				log.Println("Not found")
				jsonResponse = NewError(NewStandardError(MethodNotFound))
			} else {
				var placeToWriteData io.Writer
				// Only LegacyStreaming is Raw
				if metype == RpcMethod || metype == StreamingMethod {
					tempBuffer = bytes.NewBuffer(nil)
					placeToWriteData = tempBuffer
				} else {
					placeToWriteData = response
				}

				err := m.Invoke(request, context, placeToWriteData)
				if request.ID == nil {
					return nil
				}

				if err == nil {
					if tempBuffer.Len() == 0 {
						jsonResponse = NewResult(nil)
					} else {
						jsonResponse = NewResult(tempBuffer.String())
					}
				} else {
					jsonResponse = NewError(err)
				}
			}
		} else {
			jsonResponse = NewError(NewStandardError(InvalidRequest))
		}
		// Assign last known ID
		jsonResponse.ID = request.ID
	}
	if metype == RpcMethod || metype == StreamingMethod {
		encoder := json.NewEncoder(response)
		return encoder.Encode(jsonResponse)
	}
	return nil
}