package rupicolarpc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	log "github.com/inconshreveable/log15"
)

type rpcResponse struct {
	dst       *json.Encoder
	raw       io.Writer
	id        *interface{}
	writeUsed bool
	buffer    *bytes.Buffer
}
type rpcResponser interface {
	io.WriteCloser
	SetID(*interface{})
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

func (b *rpcResponse) SetID(id *interface{}) {
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
	if b.id != nil {
		b.dst.Encode(NewError(e, b.id))
	}
}

func (b *rpcResponse) SetResponseResult(result interface{}) {
	b.writeUsed = false
	b.buffer.Reset()
	if b.id != nil {
		b.dst.Encode(NewResult(result, b.id))
	}
}

type streamingResponse struct {
	raw       io.Writer
	buffer    *bytes.Buffer
	enc       *json.Encoder
	id        *interface{}
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
func (b *streamingResponse) SetID(id *interface{}) {
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

func (b *legacyStreamingResponse) SetID(id *interface{}) {
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
