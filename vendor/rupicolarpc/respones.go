package rupicolarpc

import (
	"encoding/json"
	"errors"
	"io"

	log "github.com/inconshreveable/log15"
)

var (
	nilInterface = interface{}(nil)
)
var (
	errResultAlreadySet = errors.New("Result already set")
)

type PublicRpcResponser interface {
	SetResponseResult(interface{}) error
	SetResponseError(error) error
}
type rpcResponser interface {
	io.WriteCloser
	SetID(*interface{})
	PublicRpcResponser
	//SetResponseResult(interface{}) error
	//SetResponseError(error) error
	Writer() io.Writer
	MaxResponse(int64)
}

type encoder interface {
	Encode(interface{}) error
}

type baseResponseWrapper struct {
	rpcResponser
}

func (b *baseResponseWrapper) SetResponseResult(result interface{}) (err error) {
	switch converted := result.(type) {
	case io.Reader:
		if closer, ok := result.(io.Closer); ok {
			defer closer.Close()
		}
		var n int64
		//TODO: Use limiter here not in method implementation
		// Rpc method could return reader and inside use routine to provide endless stream of data
		// to prevent this we are using same exec context and previously set timeout
	READLOOP:
		for {
			//select {
			//case <-f.ctx.Done():
			//	f.err = ErrTimeout
			//	return f.err
			//default:
			n, err = io.Copy(b.rpcResponser, converted)
			// This is from spec, Copy never return err = nill
			// just n = 0 and err = nil
			if n == 0 && err == nil {
				// End of stream, nothing to do
				break READLOOP
			}
			if err != nil {
				if err == ErrLimitExceed {
					// We can bypass limiter now
					//	log.Error("stream copy failed", "err", "limit", "method", f.Method)
				} else {
					//log.Error("stream copy failed", "method", f.Method, "err", err)
				}
				break READLOOP
			}
			//	}
		}
	default:
		b.rpcResponser.SetResponseResult(result)
	}
	if err != nil {
		b.rpcResponser.SetResponseError(err)
	}
	return
}

type baseResponse struct {
	transport io.Writer
	limiter   LimitedWriter
	encoder   encoder
	resultSet bool
	id        *interface{}
}

func newBaseResponse(t io.Writer, l LimitedWriter) baseResponse {
	return baseResponse{
		transport: t,
		limiter:   l,
		resultSet: false,
		id:        &nilInterface, // this will ensure we will send data on errors
		encoder:   json.NewEncoder(l),
	}
}

func (b *baseResponse) Writer() io.Writer {
	return b.transport
}

func (b *baseResponse) Close() error {
	return nil
}

func (b *baseResponse) Write(p []byte) (int, error) {
	return b.limiter.Write(p)
}

func (b *baseResponse) SetResponseError(e error) error {
	log.Debug("SetResponseError unused for Legacy streaming", "error", e)
	return nil
}

func (b *baseResponse) SetResponseResult(result interface{}) (err error) {
	log.Crit("Unknown input result", "result", result)
	return
}

func (b *baseResponse) MaxResponse(max int64) {
	b.limiter.SetLimit(max)
}

func (b *baseResponse) SetID(id *interface{}) {
	if b.id != &nilInterface {
		log.Warn("SetID invoked twice")
	}
	b.id = id
}
