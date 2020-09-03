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

// RPCResponser handles returning results
type RPCResponser interface {
	SetResponseResult(interface{}) error
	SetResponseError(error) error
}

type rpcResponserPriv interface {
	RPCResponser
	io.WriteCloser
	SetID(*interface{})
	Writer() io.Writer
	MaxResponse(int64)
}

type baseResponse struct {
	transport io.Writer
	limiter   LimitedWriter
	encoder   *json.Encoder
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
	log.Debug("using default empty Close method for response")
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
	// NOTE: We need to recreate encoder or old instance
	// will be returning last encouter error
	b.encoder = json.NewEncoder(b.limiter)
}

func (b *baseResponse) SetID(id *interface{}) {
	if b.id != &nilInterface {
		log.Warn("SetID invoked twice")
	}
	b.id = id
}
