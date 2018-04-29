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
	raw io.Writer // raw transport
	//limiter        LimitedWriter // limited writer OVER buffer
	dst *json.Encoder // encoder over limiter
	//id             *interface{}  // id
	dataSent bool          // blah
	buffer   *bytes.Buffer // buffer
}

func newRPCResponse(w io.Writer) rpcResponser {
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
	//if !b.explicitStatus {
	//	strContent := b.buffer.String()
	//	b.buffer.Reset()
	//	if err := b.SetResponseResult(strContent); err != nil {
	//		return err
	//	}
	//}
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

	return b.dst.Encode(NewError(e, b.id))
}

func (b *rpcResponse) SetResponseResult(result interface{}) error {
	if b.resultSet {
		return errResultAlreadySet
	}
	if b.id == nil {
		return nil
	}

	return b.dst.Encode(NewResult(result, b.id))
}
