package rupicolarpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"time"

	log "github.com/rs/zerolog"
)

var Logger = log.Nop()

// ContextKey : Supported keys in context
type ContextKey int

var (
	defaultRPCLimits       = limits{0, 5242880}
	defaultStreamingLimits = limits{0, 0}
)

type JSONRPCversion string

const (
	JSONRPCversion20  JSONRPCversion = "2.0"
	JSONRPCversion20s                = JSONRPCversion20 + "+s"
)

// MethodType : Supported or requested method response format.
type MethodType int

const (
	// RPCMethod : Json RPC 2.0.
	RPCMethod MethodType = 1
	// StreamingMethodLegacy : Basic streaming format without header or footer. Can be base64 encoded.
	StreamingMethodLegacy MethodType = 2
	// StreamingMethod : Line JSON encoding with header and footer.
	StreamingMethod MethodType = 4
	unknownMethod   MethodType = 0
)

const (
	forcedFlushTimeout = time.Second * 10
)

func (me MethodType) String() string {
	switch me {
	case RPCMethod:
		return "RPC"
	case StreamingMethod:
		return "Streaming"
	case StreamingMethodLegacy:
		return "Streaming (legacy)"
	case unknownMethod:
		fallthrough
	default:
		return fmt.Sprintf("unknown(%d)", me)
	}
}

// Invoker is interface for method invoked by rpc server.
type Invoker interface {
	Invoke(context.Context, JSONRPCRequest) (interface{}, error)
}

// This have more sense.
type NewInvoker interface {
	Invoke(context.Context, JSONRPCRequest, RPCResponser)
}

// UserData is user provided custom data passed to invocation.
type UserData interface{}

// RequestDefinition ...
type RequestDefinition interface {
	Reader() io.ReadCloser
	OutputMode() MethodType
	UserData() UserData
}

type MethodDef struct {
	Invoker NewInvoker
	limits
}

type oldToNewAdapter struct {
	Invoker
}

func (o *oldToNewAdapter) Invoke(ctx context.Context, r JSONRPCRequest, out RPCResponser) {
	re, e := o.Invoker.Invoke(ctx, r)
	if e != nil {
		out.SetResponseError(e)
	} else {
		out.SetResponseResult(re)
	}
}

// Execution timmeout change maximum allowed execution time
// 0 = unlimited.
func (m *MethodDef) ExecutionTimeout(timeout time.Duration) {
	m.ExecTimeout = timeout
}

// MaxSize sets maximum response size in bytes. 0 = unlimited
// Note this excludes JSON wrapping and count final encoding
// eg. size after base64 encoding.
func (m *MethodDef) MaxSize(size uint) {
	m.MaxResponse = size
}

func (m *MethodDef) Invoke(c context.Context, r JSONRPCRequest, out RPCResponser) {
	m.Invoker.Invoke(c, r, out)
}

// Yes, real jsonrpc server would need to
// handle all possible options but we
// convert them to string anyway...
type jsonRPCRequestOptions map[string]interface{}

// JsonRpcRequest ... Just POCO.
type requestData struct {
	Jsonrpc JSONRPCversion
	Method  string
	Params  jsonRPCRequestOptions
	// can be any json valid type (including null)
	// or not present at all
	ID *interface{}
}

type JSONRPCRequest interface {
	Method() string
	Version() JSONRPCversion
	UserData() UserData
	Params() jsonRPCRequestOptions
}

// UnmarshalJSON is custom unmarshal for JsonRpcRequestOptions.
func (w *jsonRPCRequestOptions) UnmarshalJSON(data []byte) error {
	// todo: discard objects?
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
			unified[strconv.Itoa(i+1)] = v // fmt.Sprint(v)
		}
	default:
		Logger.Warn().Str("received", fmt.Sprintf("%+v", converted)).Msg("invalid 'param' type only map or struct")

		return NewStandardError(InvalidRequest)
	}

	*w = unified

	return nil
}

func (r *requestData) isValid() bool {
	return (r.Jsonrpc == JSONRPCversion20 || r.Jsonrpc == JSONRPCversion20s) && r.Method != ""
}

type JSONRpcResponse struct {
	Jsonrpc JSONRPCversion `json:"jsonrpc"`
	Error   *_Error        `json:"error,omitempty"`
	Result  interface{}    `json:"result,omitempty"`
	ID      interface{}    `json:"id,omitempty"`
}

// NewResult : Generate new Result response
func NewResult(a interface{}, id interface{}) *JSONRpcResponse {
	return NewResultEx(a, id, JSONRPCversion20)
}

func NewResultEx(a, id interface{}, vers JSONRPCversion) *JSONRpcResponse {
	return &JSONRpcResponse{vers, nil, a, id}
}

// NewErrorEx : Generate new Error response with custom version
func NewErrorEx(inputError error, requestID interface{}, vers JSONRPCversion) *JSONRpcResponse {
	var errResponse *_Error
	switch err := inputError.(type) {
	case *_Error:
		errResponse = err
	default:
		Logger.Debug().Err(inputError).Msg("Should not happen - wrapping unknown error")
		errResponse, _ = NewStandardErrorData(InternalError, err.Error()).(*_Error)
	}
	return &JSONRpcResponse{vers, errResponse, nil, requestID}
}

// NewError : Generate new error response with JsonRpcversion20
func NewError(a error, id interface{}) *JSONRpcResponse {
	return NewErrorEx(a, id, JSONRPCversion20)
}

type _Error struct {
	internal error
	Code     int         `json:"code"`
	Message  string      `json:"message"`
	Data     interface{} `json:"data,omitempty"`
}

/*StandardErrorType error code
*code 	message 	meaning
*-32700 	Parse error 	Invalid JSON was received by the server.
						    An error occurred on the server while parsing the JSON text.
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
		Logger.Panic().Int("code", code).Msg("Invalid code")
		panic("invalid code")
	}

	return &_Error{
		Code:    code,
		Message: message,
	}
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
		Logger.Panic().Msg("WTF")
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

type (
	methodFunc    func(in JSONRPCRequest, ctx interface{}) (interface{}, error)
	newMethodFunc func(in JSONRPCRequest, ctx interface{}, out RPCResponser)
)

func (m newMethodFunc) Invoke(ctx context.Context, in JSONRPCRequest, out RPCResponser) {
	m(in, ctx, out)
}

func (m methodFunc) Invoke(ctx context.Context, in JSONRPCRequest) (interface{}, error) {
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

type JSONRPCProcessor struct {
	methods map[string]map[MethodType]*MethodDef
	limits  map[MethodType]*limits
}

// AddMethod add method
func (p *JSONRPCProcessor) AddMethod(name string, metype MethodType, action Invoker) *MethodDef {
	return p.AddMethodNew(name, metype, &oldToNewAdapter{action})
}

func (p *JSONRPCProcessor) AddMethodNew(name string, metype MethodType, action NewInvoker) *MethodDef {
	container, ok := p.methods[name]
	if !ok {
		container = make(map[MethodType]*MethodDef)
		p.methods[name] = container
	}
	method := &MethodDef{
		Invoker: action,
		// Each method will have copy of default values
		limits: *p.limits[metype],
	}
	container[metype] = method
	return method
}

// AddMethodFunc add method as func
func (p *JSONRPCProcessor) AddMethodFunc(name string, metype MethodType, action methodFunc) *MethodDef {
	return p.AddMethod(name, metype, action)
}

func (p *JSONRPCProcessor) AddMethodFuncNew(name string, metype MethodType, action newMethodFunc) *MethodDef {
	return p.AddMethodNew(name, metype, action)
}

func (p *JSONRPCProcessor) ExecutionTimeout(metype MethodType, timeout time.Duration) {
	p.limits[metype].ExecTimeout = timeout
}

// NewJSONRPCProcessor create new json rpc processor
func NewJSONRPCProcessor() JSONRPCProcessor {
	copyRPCLimits := defaultRPCLimits
	copyStreamingLimits := defaultStreamingLimits
	copyStreamingLegacyLimits := defaultStreamingLimits

	return JSONRPCProcessor{
		methods: make(map[string]map[MethodType]*MethodDef),
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
		Logger.Panic().Str("type", metype.String()).Msg("Unknown method type")
		panic("Unexpected method type")
	}
	return responser
}

func changeProtocolIfRequired(responser rpcResponserPriv, request *requestData) rpcResponserPriv {
	if request.Jsonrpc == JSONRPCversion20s {
		switch responser.(type) {
		case *legacyStreamingResponse:
			Logger.Info().Msg("Upgrading streaming protocol")
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
		case *_Error: // do nothing
		default:
			err = ErrParseError
		}
	} else if decoder.More() {
		Logger.Warn().Msg("Request contains more data. This is currently disallowed")
		err = ErrBatchUnsupported
	}

	n = 0
	return
}

type jsonRPCrequestPriv struct {
	requestData
	rpcResponserPriv
	err error
	req RequestDefinition
	p   *JSONRPCProcessor
	// NOTE(m): Do we need separate 'done' channel if we have context?
	done chan struct{}
	log  log.Logger
}

func (f *jsonRPCrequestPriv) Method() string {
	return f.requestData.Method
}

func (f *jsonRPCrequestPriv) Version() JSONRPCversion {
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
	f.log = Logger.With().Str("method", f.Method()).Logger()
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

func (f *jsonRPCrequestPriv) process(ctx context.Context) error {
	// Prevent processing if we got errors on transport level
	if f.err != nil {
		return f.err
	}

	f.ensureProtocol()
	f.SetID(f.ID)

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

	f.MaxResponse(int64(m.MaxResponse))

	timeout := f.p.limit(metype).ExecTimeout
	if timeout != 0 {
		// We have some global timeout
		// So check if method is time-bound too
		// if no just use global timeout
		// otherwise select lower value
		if m.ExecTimeout != 0 && m.ExecTimeout < timeout {
			timeout = m.ExecTimeout
		}
	} else {
		// No global limit so use from method
		timeout = m.ExecTimeout
	}

	if timeout > 0 {
		f.log.Debug().Dur("timeout", timeout).Msg("Seting time limit for method")
		kontext, cancel := context.WithTimeout(ctx, timeout)
		ctx = kontext

		defer cancel()
	}

	go func() {
		defer close(f.done)
		m.Invoke(ctx, f, f)
	}()
	// 1) Timeout or we done
	select {
	case <-ctx.Done():
		// oooor transport error?
		f.log.Info().Msg("timed out")
		f.log.Info().Err(ctx.Err()).Msg("context error")
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
			f.log.Warn().Msg("procedure didn't exit gracefuly, forcing exit")
		case <-f.done:
		}
	}
	return f.err
}

// SetResponseError saves passed error
// should have context
func (f *jsonRPCrequestPriv) SetResponseError(errs error) error {
	if f.err != nil {
		f.log.Warn().Err(errs).Msg("trying to set error after deadline")
	} else {
		// should we keep original error or overwrite it with response?
		f.err = errs
		if err := f.rpcResponserPriv.SetResponseError(errs); err != nil {
			f.log.Warn().Err(err).Msg("error while setting error response")
		}
	}
	return f.err
}

// SetResponseResult handle Reader case
// should have context
func (f *jsonRPCrequestPriv) SetResponseResult(result interface{}) error {
	if f.err != nil {
		f.log.Warn().Msg("trying to set result after deadline")

		return f.err
	}

	switch converted := result.(type) {
	case io.Reader:
		// This might stall so just ensure we flush after some fixed time
		// flush is required only on plain RPC method
		switch f.req.OutputMode() {
		case RPCMethod, unknownMethod:
			go func() {
				t := time.NewTimer(forcedFlushTimeout)
				defer t.Stop()

				select {
				case <-f.done: // Meh, we failed
				case <-t.C:
					f.log.Warn().Msg("forced flush after timeout")
					// Force flush
					f.Flush()
				}
			}()
		}

		_, f.err = io.Copy(f.rpcResponserPriv, converted)
		if f.err != nil {
			f.log.Error().Err(f.err).Msg("stream copy failed")
		}
	default:
		f.err = f.rpcResponserPriv.SetResponseResult(result)
	}
	// Errors (if any) are set on Close
	return f.err
}

func (p *JSONRPCProcessor) spawnRequest(request RequestDefinition, response io.Writer) jsonRPCrequestPriv {
	jsonRequest := jsonRPCrequestPriv{
		req:              request,
		rpcResponserPriv: newResponser(response, request.OutputMode()),
		done:             make(chan struct{}),
		p:                p,
	}
	return jsonRequest
}

// limit for given type
func (p *JSONRPCProcessor) limit(metype MethodType) limits {
	return *p.limits[metype]
}

// method for given name
func (p *JSONRPCProcessor) method(name string, metype MethodType) (*MethodDef, error) {
	// check if method with given name exists
	method, okm := p.methods[name]
	if !okm {
		Logger.Error().Str("method", name).Msg("not found")

		return nil, ErrMethodNotFound
	}

	// now check is requested type is supported
	selectedMethod, okm := method[metype]
	if !okm {
		// We colud have added this method as legacy
		// For now leave this as "compat"
		if metype == StreamingMethod {
			selectedMethod, okm = method[StreamingMethodLegacy]
		}
	}

	if !okm {
		Logger.Error().Str("type", metype.String()).Str("method", name).Msg("required type for method not found")

		return nil, ErrMethodNotFound
	}
	return selectedMethod, nil
}

// ProcessContext : Parse and process request from data
func (p *JSONRPCProcessor) ProcessContext(kontext context.Context,
	request RequestDefinition, response io.Writer,
) error {
	// IMPORTANT!! When using real network dropped peer is signaled after 3 minutes!
	// We should just check if any write success
	// NEW: Check behavior after using connection context
	jsonRequest := p.spawnRequest(request, response)

	if err := jsonRequest.readFromTransport(); err != nil {
		Logger.Debug().Err(err).Msg("transport error")
	}

	if err := jsonRequest.process(kontext); err != nil {
		Logger.Debug().Err(err).Msg("process")
	}

	return jsonRequest.Close()
}
