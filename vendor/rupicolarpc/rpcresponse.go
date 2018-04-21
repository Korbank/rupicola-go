package rupicolarpc

import (
	"bytes"
	"encoding/json"
	"io"

	log "github.com/inconshreveable/log15"
)

type rpcResponse struct {
	dst            *json.Encoder
	raw            io.Writer
	limiter        io.Writer
	id             *interface{}
	explicitStatus bool
	buffer         *bytes.Buffer
	maxResponse    int64
}

func newRPCResponse(w io.Writer) rpcResponser {
	buffer := bytes.NewBuffer(nil)
	return &rpcResponse{
		dst:            json.NewEncoder(buffer),
		raw:            w,
		explicitStatus: false,
		buffer:         buffer,
		limiter:        buffer,
	}
}

func (b *rpcResponse) MaxResponse(max int64) {
	b.limiter = ExceptionalLimitWrite(b.buffer, max)
	b.dst = json.NewEncoder(b.limiter)
}

func (b *rpcResponse) Writer() io.Writer {
	return b.raw
}

func (b *rpcResponse) Close() error {
	defer func() {
		b.buffer = nil
		b.dst = nil
		b.raw = nil
	}()
	if b.buffer != nil {
		if !b.explicitStatus {
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
			return err
		}
	}
	return nil
}

func (b *rpcResponse) SetID(id *interface{}) {
	if b.id != nil {
		log.Warn("SetID invoked twice")
	}
	b.id = id
}
func (b *rpcResponse) Write(p []byte) (int, error) {
	if !b.explicitStatus {
		return b.limiter.Write(p)
	}
	return 0, io.ErrUnexpectedEOF
}

func (b *rpcResponse) SetResponseError(e error) error {

	// Allow only when no data was written before
	if b.buffer.Len() == 0 {
		b.explicitStatus = true
		if b.id != nil {
			return b.dst.Encode(NewError(e, b.id))
		}
	}
	return nil
}

func (b *rpcResponse) SetResponseResult(result interface{}) error {

	if b.buffer.Len() == 0 {
		b.explicitStatus = true
		if b.id != nil {
			return b.dst.Encode(NewResult(result, b.id))
		}
	}
	return nil
}
