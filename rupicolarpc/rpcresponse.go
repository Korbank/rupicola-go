package rupicolarpc

import (
	"bytes"
	"encoding/json"
	"io"
)

type rpcResponse struct {
	baseResponse
	// data -> [JSON] -> [LIMITER] -> [BUFFER]
	raw      io.Writer
	dst      *json.Encoder
	dataSent bool
	buffer   *bytes.Buffer
	prevErr  error
}

func newRPCResponse(writer io.Writer) *rpcResponse {
	buffer := bytes.NewBuffer(nil)
	limiter := ExceptionalLimitWrite(buffer, -1)

	return &rpcResponse{
		baseResponse: newBaseResponse(buffer, limiter),
		raw:          writer,
		dst:          json.NewEncoder(buffer),
		dataSent:     false,
		buffer:       buffer,
	}
}

func (b *rpcResponse) Flush() {
	if f, ok := b.raw.(rpcFlusher); ok {
		f.Flush()
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
		Logger.Debug().Int64("n", n).Err(err).Msg("close RpcResponse")
	}

	return err
}

func (b *rpcResponse) Write(p []byte) (int, error) {
	if b.resultSet {
		return 0, errResultAlreadySet
	}

	return b.limiter.Write(p)
}

func (b *rpcResponse) SetResponseError(respError error) error {
	// Allow only when no data was written before
	if b.dataSent {
		return errResultAlreadySet
	}

	if b.id == nil {
		return nil
	}

	if b.resultSet {
		Logger.Debug().AnErr("previous", b.prevErr).AnErr("now", respError).Msg("change error")
	}

	b.prevErr = respError
	b.resultSet = true
	b.buffer.Reset()

	return b.dst.Encode(NewError(respError, b.id))
}

func (b *rpcResponse) SetResponseResult(result interface{}) error {
	if b.resultSet {
		return errResultAlreadySet
	}

	if b.id == nil {
		return nil
	}

	if b.resultSet {
		Logger.Debug().Msg("result already set (result)")
	}

	b.resultSet = true
	// pass through limiter
	return b.encoder.Encode(NewResult(result, b.id))
}
