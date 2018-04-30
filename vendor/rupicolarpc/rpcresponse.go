package rupicolarpc

import (
	"bytes"
	"encoding/json"
	"io"

	log "github.com/inconshreveable/log15"
)

type rpcResponse struct {
	baseResponse
	// data -> [JSON] -> [LIMITER] -> [BUFFER]
	raw      io.Writer
	dst      *json.Encoder
	dataSent bool
	buffer   *bytes.Buffer
}

func newRPCResponse(w io.Writer) rpcResponserPriv {
	buffer := bytes.NewBuffer(nil)
	limiter := ExceptionalLimitWrite(buffer, -1)
	return &rpcResponse{
		baseResponse: newBaseResponse(buffer, limiter),
		raw:          w,
		dst:          json.NewEncoder(buffer),
		dataSent:     false,
		buffer:       buffer,
	}
}

func (b *rpcResponse) Close() error {
	if b.dataSent {
		return nil
	}
	b.dataSent = true
	if !b.resultSet {
		// disable limiter
		b.MaxResponse(0)
		strContent := b.buffer.String()
		b.buffer.Reset()
		if err := b.SetResponseResult(strContent); err != nil {
			return err
		}
	}

	// Using raw - all other operations are on limited buffer
	// so at this point we are under limit
	n, err := io.Copy(b.raw, b.buffer)
	if err != nil {
		log.Debug("close RpcResponse", "n", n, "err", err)
	}
	return err
}

func (b *rpcResponse) Write(p []byte) (int, error) {
	if b.resultSet {
		return 0, errResultAlreadySet
	}
	return b.limiter.Write(p)
}

func (b *rpcResponse) SetResponseError(e error) error {
	// Allow only when no data was written before
	if b.dataSent {
		return errResultAlreadySet
	}
	if b.id == nil {
		return nil
	}
	if b.resultSet {
		log.Debug("result already set (error)")
	}
	b.resultSet = true
	b.buffer.Reset()
	return b.dst.Encode(NewError(e, b.id))
}

func (b *rpcResponse) SetResponseResult(result interface{}) error {
	if b.resultSet {
		return errResultAlreadySet
	}
	if b.id == nil {
		return nil
	}
	if b.resultSet {
		log.Debug("result already set (result)")
	}
	b.resultSet = true
	// pass through limiter
	return b.encoder.Encode(NewResult(result, b.id))
}
