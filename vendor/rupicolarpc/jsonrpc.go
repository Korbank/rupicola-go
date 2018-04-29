package rupicolarpc

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"strconv"
	"time"

	log "github.com/inconshreveable/log15"
)

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
type Invok3r interface {
	Invok3(context.Context, JsonRpcRequest) (interface{}, error)
}

// This have more sense
type Invoker interface {
	Invoke(context.Context, JsonRpcRequest, PublicRpcResponser)
}

type RequestDefinition interface {
	Reader() io.ReadCloser
	OutputMode() MethodType
}

type methodDef struct {
	Invoker
	limits
}

// Execution timmeout change maximum allowed execution time
// 0 = unlimited
func (m *methodDef) ExecutionTimeout(timeout time.Duration) {
	m.ExecTimeout = timeout
}

// MaxSize sets maximum response size in bytes. 0 = unlimited
// Note this excludes JSON wrapping and count final encoding
// eg. size after base64 encoding
func (m *methodDef) MaxSize(size uint) {
	m.MaxResponse = size
}

//func (m *methodDef) Invoke(c context.Context, r JsonRpcRequest) (interface{}, error) {
//	return m.Invoker.Invoke(c, r)
//}

func (m *methodDef) Invoke(c context.Context, r JsonRpcRequest, out PublicRpcResponser) {
	m.Invoker.Invoke(c, r, out)
}

// Yes, real jsonrpc server would need to
// handle all possible options but we
// convert them to string anyway...
type jsonRPCRequestOptions map[string]interface{}

// JsonRpcRequest ...
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
	// ErrParseError - Provided data is not JSON
	ErrParseError = NewStandardError(ParseError)
	// ErrMethodNotFound - no such method
	ErrMethodNotFound = NewStandardError(MethodNotFound)
	// ErrInvalidRequest - Request was invalid
	ErrInvalidRequest = NewStandardError(InvalidRequest)
	// ErrTimeout - Timeout during request processing
	ErrTimeout = NewServerError(-32099, "Timeout")
	// ErrLimitExceed - Returned when response is too big
	ErrLimitExceed = NewServerError(-32098, "Limit Exceed")
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

// Error is implementation of error interface
func (e *_Error) Error() string {
	if e.internal != nil {
		return e.internal.Error()
	}
	return e.Message
}
func (e *_Error) Internal() error { return e.internal }

//type methodFunc func(in JsonRpcRequest, ctx interface{}) (interface{}, error)
type methodFunc func(in JsonRpcRequest, ctx interface{}, out PublicRpcResponser)

func (m methodFunc) Invoke(ctx context.Context, in JsonRpcRequest, out PublicRpcResponser) {
	m(in, ctx, out)
}

// LimitedWriter returns LimitExceed after reaching limit
type LimitedWriter interface {
	io.Writer
	SetLimit(int64)
}
type exceptionalLimitedWriter struct {
	R io.Writer
	N int64
}

// ExceptionalLimitWrite construct from reader
// Use <0 to limit it to "2^63-1" bytes (practicaly infinity)
func ExceptionalLimitWrite(r io.Writer, n int64) LimitedWriter {
	lw := &exceptionalLimitedWriter{R: r}
	lw.SetLimit(n)
	return lw
}

func (r *exceptionalLimitedWriter) SetLimit(limit int64) {
	if limit > 0 {
		r.N = limit
	} else {
		r.N = math.MaxInt64
	}
}

// Write implement io.Writer
func (r *exceptionalLimitedWriter) Write(p []byte) (n int, err error) {
	if r.N <= 0 || int64(len(p)) > r.N {
		return 0, ErrLimitExceed
	}

	n, err = r.R.Write(p)
	r.N -= int64(n)
	return
}

type limits struct {
	ExecTimeout time.Duration
	MaxResponse uint
}

type JsonRpcProcessor struct {
	methods map[string]map[MethodType]*methodDef
	limits  map[MethodType]*limits
}

// AddMethod add method
func (p *JsonRpcProcessor) AddMethod(name string, metype MethodType, action Invoker) *methodDef {
	container, ok := p.methods[name]
	if !ok {
		container = make(map[MethodType]*methodDef)
		p.methods[name] = container
	}
	method := &methodDef{
		Invoker: action,
		// Each method will have copy of default values
		limits: *p.limits[metype],
	}
	container[metype] = method
	return method
}

// AddMethodFunc add method as func
func (p *JsonRpcProcessor) AddMethodFunc(name string, metype MethodType, action methodFunc) *methodDef {
	return p.AddMethod(name, metype, action)
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
		methods: make(map[string]map[MethodType]*methodDef),
		limits: map[MethodType]*limits{
			RPCMethod:             &copyRPCLimits,
			StreamingMethod:       &copyStreamingLimits,
			StreamingMethodLegacy: &copyStreamingLegacyLimits,
		},
	}
}

func newResponser(out io.Writer, metype MethodType) rpcResponser {
	var responser rpcResponser
	switch metype {
	case RPCMethod:
		responser = newRPCResponse(out)
	case StreamingMethodLegacy:
		responser = newLegacyStreamingResponse(out)
	case StreamingMethod:
		responser = newStreamingResponse(out, 0)
	default:
		log.Crit("Unknown method type", "type", metype)
		panic("Unexpected method type")
	}
	return &baseResponseWrapper{responser}
}

func changeProtocolIfRequired(responser rpcResponser, request *JsonRpcRequest) rpcResponser {
	if request.Jsonrpc == JsonRPCversion20s {
		switch responser.(type) {
		case *legacyStreamingResponse:
			log.Info("Upgrading streaming protocol")
			return newResponser(responser.Writer(), StreamingMethod)
		}
	}
	return responser
}

// ReadFrom implements io.Reader
func (r *JsonRpcRequest) ReadFrom(re io.Reader) (n int64, err error) {
	decoder := json.NewDecoder(re)
	err = decoder.Decode(r)
	if err != nil {
		switch err.(type) {
		case *json.UnmarshalTypeError:
			err = ErrInvalidRequest
		default:
			err = ErrParseError
		}
	} else if decoder.More() {
		log.Warn("Request contains more data. This is currently disallowed")
		err = ErrInvalidRequest
	}
	n = 0
	return
}

type jsonRpcProcessor interface {
	Limit(MethodType) limits
	Method(string, MethodType) (*methodDef, error)
}

type jsonRPCrequest struct {
	JsonRpcRequest
	ctx       context.Context
	responser rpcResponser
	err       error
	req       RequestDefinition
	p         jsonRpcProcessor
	done      chan struct{}
}

func (f *jsonRPCrequest) readFromTransport() error {
	r := f.req.Reader()
	_, f.err = f.JsonRpcRequest.ReadFrom(r)
	r.Close()
	return f.err
}

func (f *jsonRPCrequest) ensureProtocol() {
	f.responser = changeProtocolIfRequired(f.responser, &f.JsonRpcRequest)
}

func (f *jsonRPCrequest) Close() error {
	// this is noop if we already upgraded
	f.ensureProtocol()
	if f.err != nil {
		// For errors we ALWAYS disable payload limits
		f.responser.MaxResponse(0)
		// we ended with errors somewhere
		f.responser.SetResponseError(f.err)
		f.responser.Close()
		return f.err
	}
	return f.responser.Close()
}

func (f *jsonRPCrequest) process() error {
	// Prevent processing if we got errors on transport level
	if f.err != nil {
		return f.err
	}

	f.ensureProtocol()
	f.responser.SetID(f.ID)

	if !f.isValid() {
		f.err = ErrInvalidRequest
		return f.err
	}

	metype := f.req.OutputMode()
	m, err := f.p.Method(f.Method, metype)
	if err != nil {
		f.err = err
		return f.err
	}

	f.responser.MaxResponse(int64(m.MaxResponse))

	timeout := f.p.Limit(metype).ExecTimeout
	if timeout != 0 {
		//We have some global timeout
		//So check if method is time-bound too
		//if no just use global timeout
		//otherwise select lower value
		if m.ExecTimeout != 0 && m.ExecTimeout < timeout {
			timeout = m.ExecTimeout

		}
	} else {
		// No global limit so use from method
		timeout = m.ExecTimeout
	}
	if timeout > 0 {
		log.Debug("Seting time limit for method", "timeout", timeout)
		kontext, cancel := context.WithTimeout(f.ctx, timeout)
		defer cancel()
		f.ctx = kontext
	}

	go func() {
		defer close(f.done)
		m.Invoke(f.ctx, f.JsonRpcRequest, f.responser)
	}()
	// Wait only for done - cheking for context lead to nasty bugs
	// created from race condition
	select {
	case <-f.ctx.Done():
		f.err = ErrTimeout
	case <-f.done:
	}
	return f.err
}

/*
func (f *jsonRPCrequest) invoke(m *methodDef) error {
	defer close(f.done)

	result, err := m.Invoke(f.ctx, f.JsonRpcRequest)
	if err != nil {
		log.Error("method failed", "method", f.Method, "err", err)
		f.err = err
		return f.err
	}
	// this is done inside set response result
	//if closer, ok := result.(io.Closer); ok {
	//	defer closer.Close()
	//}
	//TODO: Use limiter here not in method implementation
	// Rpc method could return reader and inside use routine to provide endless stream of data
	// to prevent this we are using same exec context and previously set timeout
	f.err = f.responser.SetResponseResult(result)
	return f.err
}
*/

func (p *JsonRpcProcessor) spawnRequest(context context.Context, request RequestDefinition, response io.Writer) jsonRPCrequest {
	jsonRequest := jsonRPCrequest{
		ctx:       context,
		req:       request,
		responser: newResponser(response, request.OutputMode()),
		done:      make(chan struct{}),
		p:         p,
	}
	return jsonRequest
}

func (p *JsonRpcProcessor) Limit(metype MethodType) limits {
	return *p.limits[metype]
}

func (p *JsonRpcProcessor) Method(name string, metype MethodType) (*methodDef, error) {
	// check if method with given name exists
	ms, okm := p.methods[name]
	if !okm {
		log.Error("not found", "method", name)
		return nil, ErrMethodNotFound
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
		log.Error("required type for method not found", "type", metype, "method", name)
		return nil, ErrMethodNotFound
	}
	return m, nil
}

// Process : Parse and process request from data
func (p *JsonRpcProcessor) Process(request RequestDefinition, response io.Writer, ctx interface{}) error {
	kontext := context.Background()
	kontext = context.WithValue(kontext, RupicalaContextKeyContext, ctx)
	// IMPORTANT!! When using real network dropped peer is signaled after 3 minutes!
	// We should just check if any write success
	jsonRequest := p.spawnRequest(kontext, request, response)
	jsonRequest.readFromTransport()
	jsonRequest.process()

	return jsonRequest.Close()
}
