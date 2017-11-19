package rupicolarpc

import log "github.com/inconshreveable/log15"
import "encoding/json"
import "io"

import "fmt"
import "strconv"
import "bytes"
import "time"
import "context"

// ContextKey : Supported keys in context
type ContextKey int

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

// Yes, real jsonrpc server would need to
// handle all possible options but we
// convert them to string anyway...
type jsonRPCRequestOptions map[string]interface{}

// JsonRpcRequest
type JsonRpcRequest struct {
	Jsonrpc string
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

// NewResult : Generate new Result response
func NewResult(a interface{}, id interface{}) *JsonRpcResponse {
	return &JsonRpcResponse{"2.0", nil, a, id}
}

// NewError : Generate new Error response
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

var (
	ParseErrorError     = NewStandardError(ParseError)
	MethodNotFoundError = NewStandardError(MethodNotFound)
	InvalidRequestError = NewStandardError(InvalidRequest)
	TimeoutError        = NewServerError(-32099, "Timeout")
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

type ExceptionalLimitedReader struct {
	R io.Reader
	N int64
}

func ExceptionalLimitRead(r io.Reader, n int64) io.Reader {
	return &ExceptionalLimitedReader{r, n}
}

func (r *ExceptionalLimitedReader) Read(p []byte) (n int, err error) {
	if r.N <= 0 || int64(len(p)) > r.N {
		return 0, io.ErrUnexpectedEOF
	}

	n, err = r.R.Read(p)
	r.N -= int64(n)
	return
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
		return 0, io.ErrUnexpectedEOF
	}

	n, err = w.W.Write(p)
	w.N -= int64(n)
	return
}

type limits struct {
	ReadTimeout time.Duration
	ExecTimeout time.Duration
	MaxResponse uint32
}
type JsonRpcProcessor struct {
	methods map[string]map[MethodType]Invoker
	Limits  limits
}

func (p *JsonRpcProcessor) AddMethod(name string, metype MethodType, method Invoker) {
	container, ok := p.methods[name]
	if !ok {
		container = make(map[MethodType]Invoker)
		p.methods[name] = container
	}
	container[metype] = method
}

func (p *JsonRpcProcessor) AddMethodFunc(name string, metype MethodType, method methodFunc) {
	p.AddMethod(name, metype, method)
}

func NewJsonRpcProcessor() *JsonRpcProcessor {
	return &JsonRpcProcessor{
		make(map[string]map[MethodType]Invoker),
		limits{10 * time.Second, 0, 5242880},
	}

}

func ParseJsonRpcRequest(reader io.Reader) (JsonRpcRequest, error) {
	jsonDecoder := json.NewDecoder(reader)
	var request JsonRpcRequest
	err := jsonDecoder.Decode(&request)
	return request, err
}

func (p *JsonRpcProcessor) processWrapper(ctx context.Context, data io.Reader, response rpcResponser, metype MethodType) error {
	jsonDecoder := json.NewDecoder(data)
	var request JsonRpcRequest

	if err := jsonDecoder.Decode(&request); err != nil {
		response.SetResponseError(ParseErrorError)
		return err
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
		log.Error("required type for method not found\n", "type", metype, "method", request.Method)
		response.SetResponseError(MethodNotFoundError)
		return MethodNotFoundError
	}

	result, err := m.Invoke(ctx, request)
	if err != nil {
		response.SetResponseError(err)
		return err
	}
	// Rpc method could return reader and inside use routine to provide endless stream of data
	// to prevent this we are using same exec context and previously set timeout
	switch result := result.(type) {
	case io.Reader:
		_, err := io.Copy(response, result)
		if err != nil && err != io.EOF {
			response.SetResponseError(err)
			return err
		}
		return nil

	default:
		response.SetResponseResult(result)
		return nil
	}
}

type rpcResponse struct {
	dst       *json.Encoder
	raw       io.Writer
	id        interface{}
	writeUsed bool
	buffer    *bytes.Buffer
}
type rpcResponser interface {
	io.WriteCloser
	SetID(interface{})
	SetResponseResult(interface{})
	SetResponseError(error)
}

func newRPCResponse(w io.Writer) rpcResponser {
	buffer := bytes.NewBuffer(nil)
	return &rpcResponse{
		dst:       json.NewEncoder(buffer),
		raw:       w,
		writeUsed: false,
		buffer:    buffer,
	}
}

func (b *rpcResponse) Close() error {
	if b.buffer != nil {
		if b.writeUsed {
			b.SetResponseResult(b.buffer.String())
		}
		n, err := io.Copy(b.raw, b.buffer)
		log.Debug("close RpcResponse", "n", n, "err", err)
	}
	b.buffer = nil
	b.dst = nil
	b.raw = nil
	return nil
}

func (b *rpcResponse) SetID(id interface{}) {
	if b.id != nil {
		log.Warn("SetID invoked twice")
	}
	b.id = id
}
func (b *rpcResponse) Write(p []byte) (int, error) {
	if b.buffer != nil {
		b.writeUsed = true
		return b.buffer.Write(p)
	}
	return 0, io.ErrUnexpectedEOF
}

func (b *rpcResponse) SetResponseError(e error) {
	b.writeUsed = false
	b.buffer.Reset()
	b.dst.Encode(NewError(e, b.id))
}

func (b *rpcResponse) SetResponseResult(result interface{}) {
	b.writeUsed = false
	b.buffer.Reset()
	b.dst.Encode(NewResult(result, b.id))
}

type streamingResponse struct {
	raw       io.Writer
	buffer    *bytes.Buffer
	enc       *json.Encoder
	id        interface{}
	chunkSize int
}

func newStreamingResponse(writer io.Writer, n int) rpcResponser {
	return &streamingResponse{
		raw:       writer,
		buffer:    bytes.NewBuffer(make([]byte, 0, 128)),
		enc:       json.NewEncoder(writer),
		chunkSize: n,
	}
}

func (b *streamingResponse) Close() error {
	b.commit()
	if b.enc != nil {
		// Write footer (if we got error somewhere its alrady send)
		b.SetResponseResult("Done")
	}
	b.enc = nil
	return nil
}
func (b *streamingResponse) commit() {
	if b.buffer.Len() <= 0 {
		return
	}
	var resp struct {
		Data interface{} `json:"data"`
	}
	resp.Data = b.buffer.Bytes()
	b.enc.Encode(resp)
	b.buffer.Reset()
}
func (b *streamingResponse) SetID(id interface{}) {
	b.id = id
}

func (b *streamingResponse) Write(p []byte) (int, error) {
	if b.enc == nil {
		log.Warn("Write disabled on closed response")
		return 0, io.EOF
	}
	var resp struct {
		Data interface{} `json:"data"`
	}
	if b.chunkSize <= 0 {
		result := bytes.SplitAfter(p, []byte("\n"))
		for _, v := range result {
			if v[len(v)-1] == '\n' {
				// special case with dangling data in buffer
				if b.buffer.Len() != 0 {
					b.buffer.Write(v)
					// assign buffer bytes as source
					v = b.buffer.Bytes()
				}
				resp.Data = string(v[0 : len(v)-1])
				b.enc.Encode(resp)
			} else {
				// Oh dang! We get some leftovers...
				b.buffer.Write(v)
			}
		}
		return len(p), nil
	}
	if len(p)+b.buffer.Len() >= b.chunkSize {
		readTotal := 0
		missingBytes := (b.chunkSize - b.buffer.Len()) % b.chunkSize
		if missingBytes != b.chunkSize {
			n, err := b.buffer.Write(p[0:missingBytes])
			if err != nil {
				return n, err
			}
			readTotal += n
			b.commit()
		}
		chunk := missingBytes

		for ; chunk+b.chunkSize < len(p); chunk += b.chunkSize {
			resp.Data = p[chunk : chunk+b.chunkSize]
			readTotal += b.chunkSize
			if err := b.enc.Encode(resp); err != nil {
				return readTotal, err
			}
		}
		lastN, err := b.buffer.Write(p[chunk:len(p)])
		return readTotal + lastN, err
	}

	return b.buffer.Write(p)
}

func (b *streamingResponse) SetResponseError(e error) {
	if b.enc != nil {
		b.commit()
		b.enc.Encode(NewError(e, b.id))
		b.Close()
	} else {
		log.Warn("Setting error more than once!")
	}
}

func (b *streamingResponse) SetResponseResult(r interface{}) {
	if b.enc == nil {
		log.Warn("Unable to set result on closed response")
		return
	}
	b.commit()
	b.enc.Encode(NewResult(r, b.id))
}

type legacyStreamingResponse struct {
	raw io.Writer
}

func (b *legacyStreamingResponse) Close() error {
	b.raw = nil
	return nil
}

func (b *legacyStreamingResponse) SetID(id interface{}) {
	log.Debug("SetId unused for Legacy streaming")
}

func (b *legacyStreamingResponse) Write(p []byte) (int, error) {
	return b.raw.Write(p)
}

func (b *legacyStreamingResponse) SetResponseError(e error) {
	log.Debug("SetResponseError unused for Legacy streaming")
}

func (b *legacyStreamingResponse) SetResponseResult(result interface{}) {
	switch converted := result.(type) {
	case string, int, int16, int32, int64, int8:
		io.WriteString(b, fmt.Sprintf("%v", converted))
	default:
		log.Crit("Unknown input result", "result", result)
	}
	// And thats it
	b.raw = nil
}

// Process : Parse and process request from data
func (p *JsonRpcProcessor) Process(data io.Reader, response io.Writer, ctx interface{}, metype MethodType) error {
	var rpcResponser rpcResponser
	kontext := context.Background()
	kontext = context.WithValue(kontext, RupicalaContextKeyContext, ctx)

	if p.Limits.ExecTimeout > 0 {
		ctx, cancel := context.WithTimeout(kontext, p.Limits.ExecTimeout)
		defer cancel()
		kontext = ctx
	}

	switch metype {
	case RPCMethod:
		rpcResponser = newRPCResponse(response)
	case StreamingMethodLegacy:
		rpcResponser = &legacyStreamingResponse{raw: response}
	case StreamingMethod:
		rpcResponser = newStreamingResponse(response, 0)
	default:
		log.Crit("Unknown method type", "type", metype)
		panic("Unexpected method type")
	}

	defer rpcResponser.Close()

	done := make(chan struct{})
	var err error
	go func() {
		defer close(done)
		// TODO: In case of interrupt we end processing
		err = p.processWrapper(kontext, data, rpcResponser, metype)
	}()

	select {
	case <-done:
	case <-kontext.Done():
		rpcResponser.SetResponseError(TimeoutError)
		err = TimeoutError
	}

	return err
}
