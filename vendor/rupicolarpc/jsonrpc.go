package rupicolarpc

import log "github.com/inconshreveable/log15"
import "encoding/json"
import "io"

import "strconv"

import "time"
import "context"

// ContextKey : Supported keys in context
type ContextKey int

var (
	defaultRPCLimits       = limits{0, 5242880}
	defaultStreamingLimits = limits{0, 0}
)

type JsonRPCversion string

var (
	JsonRPCversion20  JsonRPCversion = "2.0"
	JsonRPCversion20s                = JsonRPCversion20 + "+s"
)

const (
	// RupicalaContextKeyContext : Custom data assigned to process
	RupicalaContextKeyContext ContextKey = 0
)

// MethodType : Supported or requested method response format
type MethodType int

const (
	// RPCMethod : Json RPC 2.0
	RPCMethod MethodType = 1
	// StreamingMethodLegacy : Basic streaming format without header or footer. Can be base64 encoded.
	StreamingMethodLegacy = 2
	// StreamingMethod : Line JSON encoding with header and footer
	StreamingMethod = 4
	unknownMethod   = 0
)

// Invoker is interface for method invoked by rpc server
type Invoker interface {
	Invoke(context.Context, JsonRpcRequest) (interface{}, error)
}

type MethodDef struct {
	Invoker
	limits
}

// Execution timmeout change maximum allowed execution time
// 0 = unlimited
func (m *MethodDef) ExecutionTimeout(timeout time.Duration) {
	m.ExecTimeout = timeout
}

// MaxSize sets maximum response size in bytes. 0 = unlimited
// Note this excludes JSON wrapping and count final encoding
// eg. size after base64 encoding
func (m *MethodDef) MaxSize(size uint) {
	m.MaxResponse = size
}

func (m *MethodDef) Invoke(c context.Context, r JsonRpcRequest) (interface{}, error) {
	return m.Invoker.Invoke(c, r)
}

// Yes, real jsonrpc server would need to
// handle all possible options but we
// convert them to string anyway...
type jsonRPCRequestOptions map[string]interface{}

// JsonRpcRequest
type JsonRpcRequest struct {
	Jsonrpc JsonRPCversion
	Method  string
	Params  jsonRPCRequestOptions
	// can be any json valid type (including null)
	// or not present at all
	ID *interface{}
}

// UnmarshalJSON is custom unmarshal for JsonRpcRequestOptions
func (w *jsonRPCRequestOptions) UnmarshalJSON(data []byte) error {
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
			unified[strconv.Itoa(i+1)] = v //fmt.Sprint(v)
		}

	default:
		//log.Printf("Invalid case %v\n", converted)
		return NewStandardError(InvalidRequest)
	}
	*w = unified
	return nil

}

func (r *JsonRpcRequest) isValid() bool {
	return (r.Jsonrpc == JsonRPCversion20 || r.Jsonrpc == JsonRPCversion20s) && r.Method != ""
}

type JsonRpcResponse struct {
	Jsonrpc JsonRPCversion `json:"jsonrpc"`
	Error   *_Error        `json:"error,omitempty"`
	Result  interface{}    `json:"result,omitempty"`
	ID      interface{}    `json:"id,omitempty"`
}

// NewResult : Generate new Result response
func NewResult(a interface{}, id interface{}) *JsonRpcResponse {
	return NewResultEx(a, id, JsonRPCversion20)
}

func NewResultEx(a, id interface{}, vers JsonRPCversion) *JsonRpcResponse {
	return &JsonRpcResponse{vers, nil, a, id}
}

// NewErrorEx : Generate new Error response with custom version
func NewErrorEx(a error, id interface{}, vers JsonRPCversion) *JsonRpcResponse {
	var b *_Error
	switch err := a.(type) {
	case *_Error:
		b = err
	default:
		log.Debug("Should not happen - wrapping unknown error:", "err", a)
		b, _ = NewStandardErrorData(InternalError, err.Error()).(*_Error)
	}
	return &JsonRpcResponse{vers, b, nil, id}
}

// NewError : Generate new error response with JsonRpcversion20
func NewError(a error, id interface{}) *JsonRpcResponse {
	return NewErrorEx(a, id, JsonRPCversion20)
}

type _Error struct {
	internal error
	Code     int         `json:"code"`
	Message  string      `json:"message"`
	Data     interface{} `json:"data,omitempty"`
}

/*StandardErrorType error code
*code 	message 	meaning
*-32700 	Parse error 	Invalid JSON was received by the server. An error occurred on the server while parsing the JSON text.
*-32600 	Invalid Request 	The JSON sent is not a valid Request object.
*-32601 	Method not found 	The method does not exist / is not available.
*-32602 	Invalid params 	Invalid method parameter(s).
*-32603 	Internal error 	Internal JSON-RPC error.
*-32000 to -32099 	Server error 	Reserved for implementation-defined server-errors.
 */
type StandardErrorType int

const (
	// ParseError code for parse error
	ParseError StandardErrorType = -32700
	// InvalidRequest code for invalid request
	InvalidRequest = -32600
	// MethodNotFound code for method not found
	MethodNotFound = -32601
	// InvalidParams code for invalid params
	InvalidParams = -32602
	// InternalError code for internal error
	InternalError     = -32603
	_ServerErrorStart = -32000
	_ServerErrorEnd   = -32099
)

var (
	// ParseErrorError - Provided data is not JSON
	ParseErrorError = NewStandardError(ParseError)
	// MethodNotFoundError - no such method
	MethodNotFoundError = NewStandardError(MethodNotFound)
	// InvalidRequestError - Request was invalid
	InvalidRequestError = NewStandardError(InvalidRequest)
	// TimeoutError - Timeout during request processing
	TimeoutError = NewServerError(-32099, "Timeout")
	// LimitExceed - Returned when response is too big
	LimitExceed = NewServerError(-32098, "Limit Exceed")
)

// NewServerError from code and message
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

// NewStandardErrorData with code and custom data
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

// NewStandardError from code
func NewStandardError(code StandardErrorType) error {
	return NewStandardErrorData(code, nil)
}

func (e *_Error) Error() string {
	if e.internal != nil {
		return e.internal.Error()
	}
	return e.Message
}
func (e *_Error) Internal() error { return e.internal }

type methodFunc func(in JsonRpcRequest, ctx interface{}) (interface{}, error)

func (m methodFunc) Invoke(ctx context.Context, in JsonRpcRequest) (interface{}, error) {
	return m(in, ctx)
}

// ExceptionalLimitedWriter returns LimitExceed after reaching limit
type ExceptionalLimitedWriter struct {
	R io.Writer
	N int64
}

// ExceptionalLimitWrite construct from reader
func ExceptionalLimitWrite(r io.Writer, n int64) io.Writer {
	return &ExceptionalLimitedWriter{r, n}
}

// Write implement io.Writer
func (r *ExceptionalLimitedWriter) Write(p []byte) (n int, err error) {
	if r.N <= 0 || int64(len(p)) > r.N {
		return 0, LimitExceed
	}

	n, err = r.R.Write(p)
	r.N -= int64(n)
	return
}

// ExceptionalLimitedReader returns LimitExceed after reaching limit
type ExceptionalLimitedReader struct {
	R io.Reader
	N int64
}

// ExceptionalLimitRead construct from reader
func ExceptionalLimitRead(r io.Reader, n int64) io.Reader {
	return &ExceptionalLimitedReader{r, n}
}

// Read implement io.Read
func (r *ExceptionalLimitedReader) Read(p []byte) (n int, err error) {
	if r.N <= 0 || int64(len(p)) > r.N {
		return 0, LimitExceed
	}

	n, err = r.R.Read(p)
	r.N -= int64(n)
	return
}

type limits struct {
	ExecTimeout time.Duration
	MaxResponse uint
}

type JsonRpcProcessor struct {
	methods map[string]map[MethodType]*MethodDef
	limits  map[MethodType]*limits
}

// AddMethod add method
func (p *JsonRpcProcessor) AddMethod(name string, metype MethodType, method Invoker) *MethodDef {
	container, ok := p.methods[name]
	if !ok {
		container = make(map[MethodType]*MethodDef)
		p.methods[name] = container
	}
	methodDef := &MethodDef{
		Invoker: method,
		// Each method will have copy of default values
		limits: *p.limits[metype],
	}
	container[metype] = methodDef
	return methodDef
}

// AddMethodFunc add method as func
func (p *JsonRpcProcessor) AddMethodFunc(name string, metype MethodType, method methodFunc) *MethodDef {
	return p.AddMethod(name, metype, method)
}
func (p *JsonRpcProcessor) ExecutionTimeout(metype MethodType, timeout time.Duration) {
	p.limits[metype].ExecTimeout = timeout
}

// NewJsonRpcProcessor create new json rpc processor
func NewJsonRpcProcessor() *JsonRpcProcessor {
	copyRPCLimits := defaultRPCLimits
	copyStreamingLimits := defaultStreamingLimits
	copyStreamingLegacyLimits := defaultStreamingLimits

	return &JsonRpcProcessor{
		methods: make(map[string]map[MethodType]*MethodDef),
		limits: map[MethodType]*limits{
			RPCMethod:             &copyRPCLimits,
			StreamingMethod:       &copyStreamingLimits,
			StreamingMethodLegacy: &copyStreamingLegacyLimits,
		},
	}
}

func newResponser(out io.Writer, metype MethodType) rpcResponser {
	switch metype {
	case RPCMethod:
		return newRPCResponse(out)
	case StreamingMethodLegacy:
		return &legacyStreamingResponse{raw: out}
	case StreamingMethod:
		return newStreamingResponse(out, 0)
	default:
		log.Crit("Unknown method type", "type", metype)
		panic("Unexpected method type")
	}
}

func (p *JsonRpcProcessor) processWrapper(ctx context.Context, data io.Reader, responseX io.Writer, metype MethodType) error {
	jsonDecoder := json.NewDecoder(data)
	var request JsonRpcRequest

	// Create default responser
	response := newResponser(responseX, metype)

	defer response.Close()

	if err := jsonDecoder.Decode(&request); err != nil {
		nilInterface := interface{}(nil)
		response.SetID(&nilInterface)
		switch err.(type) {
		case *json.SyntaxError:
			response.SetResponseError(ParseErrorError)
		default:
			response.SetResponseError(InvalidRequestError)
		}
		return err
	}
	if request.Jsonrpc == JsonRPCversion20s && metype == StreamingMethodLegacy {
		log.Info("Upgrading streaming protocol")
		metype = StreamingMethod
		response = newResponser(responseX, metype)
	}
	response.SetID(request.ID)

	if !request.isValid() {
		err := NewStandardError(InvalidRequest)
		response.SetResponseError(err)
		return err
	}
	// check if method with given name exists
	ms, okm := p.methods[request.Method]
	if !okm {
		log.Error("not found", "method", request.Method)
		response.SetResponseError(MethodNotFoundError)
		return MethodNotFoundError
	}
	// now check is requested type is supported
	m, okm := ms[metype]
	if !okm {
		// We colud have added this method as legacy
		// For now leave this as "compat"
		if metype == StreamingMethod {
			m, okm = ms[StreamingMethodLegacy]
		}
	}
	if !okm {
		log.Error("required type for method not found", "type", metype, "method", request.Method)
		response.SetResponseError(MethodNotFoundError)
		return MethodNotFoundError
	}

	if m.limits.MaxResponse != 0 {
		response.Writer(ExceptionalLimitWrite(response.GetWriter(), int64(m.limits.MaxResponse)))
	}

	timeout := m.limits.ExecTimeout
	if timeout > 0 {
		log.Crit("Exec", "t", timeout)
		kontext, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		ctx = kontext
	}

	done := make(chan struct{})
	var masterError error

	go func() {
		defer close(done)

		result, err := m.Invoke(ctx, request)
		if err != nil {
			log.Error("method failed", "method", request.Method, "err", err)
			response.SetResponseError(err)
			masterError = err
			return
		}
		//TODO: Use limiter here not in method implementation
		// Rpc method could return reader and inside use routine to provide endless stream of data
		// to prevent this we are using same exec context and previously set timeout
		switch result := result.(type) {
		case io.Reader:
			_, err := io.Copy(response, result)
			if err != nil && err != io.EOF {
				if err == LimitExceed {
					// We can bypass limiter now
					response.Writer(responseX)
					log.Error("stream copy failed", "err", "limit", "method", request.Method)
				} else {
					log.Error("stream copy failed", "method", request.Method, "err", err)
				}
				response.SetResponseError(err)
				masterError = err
			}
		default:
			masterError = response.SetResponseResult(result)
		}
	}()

	select {
	case <-done:
	case <-ctx.Done():
		response.SetResponseError(TimeoutError)
		masterError = TimeoutError
	}

	return masterError
}

// Process : Parse and process request from data
func (p *JsonRpcProcessor) Process(data io.Reader, response io.Writer, ctx interface{}, metype MethodType) error {
	kontext := context.Background()
	kontext = context.WithValue(kontext, RupicalaContextKeyContext, ctx)

	return p.processWrapper(kontext, data, response, metype)
}
