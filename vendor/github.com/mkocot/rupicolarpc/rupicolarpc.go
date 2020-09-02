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

// MethodType : Supported or requested method response format
type MethodType int

const (
	// RPCMethod : Json RPC 2.0
	RPCMethod MethodType = 1
	// StreamingMethodLegacy : Basic streaming format without header or footer. Can be base64 encoded.
	StreamingMethodLegacy MethodType = 2
	// StreamingMethod : Line JSON encoding with header and footer
	StreamingMethod MethodType = 4
	unknownMethod   MethodType = 0
)

// Invoker is interface for method invoked by rpc server
type Invoker interface {
	Invoke(context.Context, JsonRpcRequest) (interface{}, error)
}

// This have more sense
type NewInvoker interface {
	Invoke(context.Context, JsonRpcRequest, RPCResponser)
}

// UserData is user provided custom data passed to invocation
type UserData interface{}

// RequestDefinition ...
type RequestDefinition interface {
	Reader() io.ReadCloser
	OutputMode() MethodType
	UserData() UserData
}
type methodDef struct {
	Invoker NewInvoker
	limits
}

type oldToNewAdapter struct {
	Invoker
}

func (o *oldToNewAdapter) Invoke(ctx context.Context, r JsonRpcRequest, out RPCResponser) {
	re, e := o.Invoker.Invoke(ctx, r)
	if e != nil {
		out.SetResponseError(e)
	} else {
		out.SetResponseResult(re)
	}
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

func (m *methodDef) Invoke(c context.Context, r JsonRpcRequest, out RPCResponser) {
	m.Invoker.Invoke(c, r, out)
}

// Yes, real jsonrpc server would need to
// handle all possible options but we
// convert them to string anyway...
type jsonRPCRequestOptions map[string]interface{}

// JsonRpcRequest ... Just POCO
type requestData struct {
	Jsonrpc JsonRPCversion
	Method  string
	Params  jsonRPCRequestOptions
	// can be any json valid type (including null)
	// or not present at all
	ID *interface{}
}

type JsonRpcRequest interface {
	Method() string
	Version() JsonRPCversion
	UserData() UserData
	Params() jsonRPCRequestOptions
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

func (r *requestData) isValid() bool {
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
	// ErrBatchUnsupported - Returned when batch is disabled
	ErrBatchUnsupported = NewServerError(-32097, "Batch is disabled")
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

type methodFunc func(in JsonRpcRequest, ctx interface{}) (interface{}, error)
type newMethodFunc func(in JsonRpcRequest, ctx interface{}, out RPCResponser)

func (m newMethodFunc) Invoke(ctx context.Context, in JsonRpcRequest, out RPCResponser) {
	m(in, ctx, out)
}

func (m methodFunc) Invoke(ctx context.Context, in JsonRpcRequest) (interface{}, error) {
	return m(in, ctx)
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

type JsonRpcProcessor interface {
	AddMethod(name string, metype MethodType, action Invoker) *methodDef
	AddMethodNew(name string, metype MethodType, action NewInvoker) *methodDef
	AddMethodFunc(name string, metype MethodType, action methodFunc) *methodDef
	AddMethodFuncNew(name string, metype MethodType, action newMethodFunc) *methodDef
	Process(request RequestDefinition, response io.Writer) error
	ProcessContext(ctx context.Context, request RequestDefinition, response io.Writer) error
	ExecutionTimeout(metype MethodType, timeout time.Duration)
}

type jsonRpcProcessorPriv interface {
	JsonRpcProcessor
	limit(MethodType) limits
	method(string, MethodType) (*methodDef, error)
}

type jsonRpcProcessor struct {
	methods map[string]map[MethodType]*methodDef
	limits  map[MethodType]*limits
}

// AddMethod add method
func (p *jsonRpcProcessor) AddMethod(name string, metype MethodType, action Invoker) *methodDef {
	return p.AddMethodNew(name, metype, &oldToNewAdapter{action})
}

func (p *jsonRpcProcessor) AddMethodNew(name string, metype MethodType, action NewInvoker) *methodDef {
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
func (p *jsonRpcProcessor) AddMethodFunc(name string, metype MethodType, action methodFunc) *methodDef {
	return p.AddMethod(name, metype, action)
}

func (p *jsonRpcProcessor) AddMethodFuncNew(name string, metype MethodType, action newMethodFunc) *methodDef {
	return p.AddMethodNew(name, metype, action)
}

func (p *jsonRpcProcessor) ExecutionTimeout(metype MethodType, timeout time.Duration) {
	p.limits[metype].ExecTimeout = timeout
}

// NewJsonRpcProcessor create new json rpc processor
func NewJsonRpcProcessor() JsonRpcProcessor {
	copyRPCLimits := defaultRPCLimits
	copyStreamingLimits := defaultStreamingLimits
	copyStreamingLegacyLimits := defaultStreamingLimits

	return &jsonRpcProcessor{
		methods: make(map[string]map[MethodType]*methodDef),
		limits: map[MethodType]*limits{
			RPCMethod:             &copyRPCLimits,
			StreamingMethod:       &copyStreamingLimits,
			StreamingMethodLegacy: &copyStreamingLegacyLimits,
		},
	}
}

func newResponser(out io.Writer, metype MethodType) rpcResponserPriv {
	var responser rpcResponserPriv
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
	return responser
}

func changeProtocolIfRequired(responser rpcResponserPriv, request *requestData) rpcResponserPriv {
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
func (r *requestData) ReadFrom(re io.Reader) (n int64, err error) {
	decoder := json.NewDecoder(re)
	err = decoder.Decode(r)
	if err != nil {
		switch err.(type) {
		case *json.UnmarshalTypeError:
			err = ErrInvalidRequest
		case *_Error: //do nothing
		default:
			err = ErrParseError
		}
	} else if decoder.More() {
		log.Warn("Request contains more data. This is currently disallowed")
		err = ErrBatchUnsupported
	}
	n = 0
	return
}

type jsonRPCrequestPriv struct {
	requestData
	ctx context.Context
	rpcResponserPriv
	err  error
	req  RequestDefinition
	p    jsonRpcProcessorPriv
	done chan struct{}
	log  log.Logger
}

func (f *jsonRPCrequestPriv) Method() string {
	return f.requestData.Method
}

func (f *jsonRPCrequestPriv) Version() JsonRPCversion {
	return f.Jsonrpc
}

func (f *jsonRPCrequestPriv) Params() jsonRPCRequestOptions {
	return f.requestData.Params
}

func (f *jsonRPCrequestPriv) UserData() UserData {
	return f.req.UserData()
}

func (f *jsonRPCrequestPriv) readFromTransport() error {
	r := f.req.Reader()
	_, f.err = f.requestData.ReadFrom(r)
	f.log = log.New("method", f.Method)
	r.Close()
	return f.err
}

func (f *jsonRPCrequestPriv) ensureProtocol() {
	f.rpcResponserPriv = changeProtocolIfRequired(f.rpcResponserPriv, &f.requestData)
}

func (f *jsonRPCrequestPriv) Close() error {
	// this is noop if we already upgraded
	f.ensureProtocol()
	if f.err != nil {
		// For errors we ALWAYS disable payload limits
		f.rpcResponserPriv.MaxResponse(0)
		// we ended with errors somewhere
		f.rpcResponserPriv.SetResponseError(f.err)
		f.rpcResponserPriv.Close()
		return f.err
	}
	return f.rpcResponserPriv.Close()
}

func (f *jsonRPCrequestPriv) process() error {
	// Prevent processing if we got errors on transport level
	if f.err != nil {
		return f.err
	}

	f.ensureProtocol()
	f.rpcResponserPriv.SetID(f.ID)

	if !f.isValid() {
		f.err = ErrInvalidRequest
		return f.err
	}

	metype := f.req.OutputMode()
	m, err := f.p.method(f.requestData.Method, metype)
	if err != nil {
		f.err = err
		return f.err
	}

	f.rpcResponserPriv.MaxResponse(int64(m.MaxResponse))

	timeout := f.p.limit(metype).ExecTimeout
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
		f.log.Debug("Seting time limit for method", "timeout", timeout)
		kontext, cancel := context.WithTimeout(f.ctx, timeout)
		defer cancel()
		f.ctx = kontext
	}

	go func() {
		defer close(f.done)
		m.Invoke(f.ctx, f, f)
	}()
	// 1) Timeout or we done
	select {
	case <-f.ctx.Done():
		// oooor transport error?
		f.log.Info("method timed out", "method", f.Method)
		f.log.Info("context error", "error", f.ctx.Err())
		// this could be canceled too... but
		// why it should be like that? if we get
		// cancel from transport then whatever no one is
		// going to read answer anyway
		f.err = ErrTimeout
	case <-f.done:
	}
	// 2) If we exit from timeout wait for ending
	if f.err != nil {
		select {
		// This is just to ensure we won't hang forever if procedure
		// is misbehaving
		case <-time.NewTimer(time.Millisecond * 500).C:
			f.log.Warn("procedure didn't exit gracefuly, forcing exit")
		case <-f.done:
		}
	}
	return f.err
}

// SetResponseError saves passed error
func (f *jsonRPCrequestPriv) SetResponseError(errs error) error {
	if f.ctx.Err() != nil {
		f.log.Warn("trying to set error after deadline", "error", errs)
	} else {
		// should we keep original error or overwrite it with response?
		f.err = errs
		if err := f.rpcResponserPriv.SetResponseError(errs); err != nil {
			f.log.Warn("error while setting error response", "error", err)
		}
	}
	return f.err
}

// SetResponseResult handle Reader case
func (f *jsonRPCrequestPriv) SetResponseResult(result interface{}) error {
	if f.ctx.Err() != nil {
		f.log.Warn("trying to set result after deadline")
		return f.err
	}

	switch converted := result.(type) {
	case io.Reader:
		_, f.err = io.Copy(f.rpcResponserPriv, converted)
		if f.err != nil {
			f.log.Error("stream copy failed", "method", f.Method, "err", f.err)
		}
	default:
		f.err = f.rpcResponserPriv.SetResponseResult(result)
	}
	// Errors (if any) are set on Close
	return f.err
}

func (p *jsonRpcProcessor) spawnRequest(context context.Context, request RequestDefinition, response io.Writer) jsonRPCrequestPriv {
	jsonRequest := jsonRPCrequestPriv{
		ctx:              context,
		req:              request,
		rpcResponserPriv: newResponser(response, request.OutputMode()),
		done:             make(chan struct{}),
		p:                p,
	}
	return jsonRequest
}

// limit for given type
func (p *jsonRpcProcessor) limit(metype MethodType) limits {
	return *p.limits[metype]
}

// method for given name
func (p *jsonRpcProcessor) method(name string, metype MethodType) (*methodDef, error) {
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
func (p *jsonRpcProcessor) Process(request RequestDefinition, response io.Writer) error {
	return p.ProcessContext(context.Background(), request, response)
}

func (p *jsonRpcProcessor) ProcessContext(kontext context.Context, request RequestDefinition, response io.Writer) error {
	// IMPORTANT!! When using real network dropped peer is signaled after 3 minutes!
	// We should just check if any write success
	// NEW: Check behavior after using connection context
	jsonRequest := p.spawnRequest(kontext, request, response)
	jsonRequest.readFromTransport()
	jsonRequest.process()

	return jsonRequest.Close()
}
