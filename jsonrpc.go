package main

import "log"
import "encoding/json"
import "io"

import "fmt"
import "strconv"

type Invoker interface {
	Invoke(request JsonRpcRequest, context interface{}) (string, error)
	InvokeV2(request JsonRpcRequest, context interface{}, writer io.Writer) error
}

// Yes, real jsonrpc server would need to
// handle all possible options but we
// convert them to string anyway...
type JsonRpcRequestOptions struct {
	Params map[string]string
}

type JsonRpcRequest struct {
	Jsonrpc string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  JsonRpcRequestOptions
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
			return NewStandardError(InvalidRequest)
		}
		log.Println(unified)
		w.Params = unified
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

func NewError(a error) JsonRpcResponse {
	var b *_Error
	switch err := a.(type) {
	case *_Error:
		b = err
	default:
		log.Fatal("Should not happen")
		b, _ = _NewServerError(InternalError, err.Error()).(*_Error)
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

func _NewServerError(code int, message string) error {
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

func _WrapError(any error) error {
	switch err := any.(type) {
	case *_Error:
		return err
	default:
		return &_Error{any, 0, "", nil}
	}
}

func (e _Error) Error() string {
	if e.internal == nil {
		return e.internal.Error()
	}
	return e.Message
}
func (e *_Error) Internal() error { return e.internal }

type JsonRpcProcessor struct {
	invoker Invoker
}

func NewJsonRpcProcessor(invoker Invoker) *JsonRpcProcessor {
	return &JsonRpcProcessor{
		invoker,
	}

}
func (p *JsonRpcProcessor) ProcessStreamed(data io.Reader, response io.Writer, context interface{}) error {
	return nil
}

func (p *JsonRpcProcessor) Process(data io.Reader, response io.Writer, context interface{}) error {
	jsonDecoder := json.NewDecoder(data)
	var request JsonRpcRequest
	var jsonResponse JsonRpcResponse

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
			resp, err := p.invoker.Invoke(request, context)
			if request.ID == nil {
				return nil
			}

			if err == nil {
				jsonResponse = NewResult(resp)
			} else {
				jsonResponse = NewError(err)
			}
		} else {
			jsonResponse = NewError(NewStandardError(InvalidRequest))
		}
		// Assign last known ID
		jsonResponse.ID = request.ID
	}
	encoder := json.NewEncoder(response)
	return encoder.Encode(jsonResponse)
}
