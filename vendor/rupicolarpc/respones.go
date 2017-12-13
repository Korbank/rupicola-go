package rupicolarpc

import (
	"bytes"
	"encoding/json"
	"fmt"
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
type rpcResponser interface {
	io.WriteCloser
	SetID(*interface{})
	SetResponseResult(interface{}) error
	SetResponseError(error) error
	/*GetWriter() io.Writer
	Writer(io.Writer)*/
	MaxResponse(int64)
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

/*
func (b *rpcResponse) GetWriter() io.Writer {
	return b.raw
}

func (b *rpcResponse) Writer(w io.Writer) {
	b.raw = w
}*/

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

type streamingResponse struct {
	raw        io.Writer
	limited    io.Writer
	buffer     *bytes.Buffer
	enc        *json.Encoder
	id         *interface{}
	chunkSize  int
	firstWrite bool
	isClosed   bool
}

func newStreamingResponse(writer io.Writer, n int) rpcResponser {
	result := &streamingResponse{
		raw:        writer,
		limited:    writer,
		buffer:     bytes.NewBuffer(make([]byte, 0, 128)),
		enc:        json.NewEncoder(writer),
		chunkSize:  n,
		firstWrite: true,
	}
	return result
}

func (b *streamingResponse) MaxResponse(max int64) {
	b.limited = ExceptionalLimitWrite(b.raw, max)
	b.enc = json.NewEncoder(b.limited)
}

/*
func (b *streamingResponse) GetWriter() io.Writer {
	return b.raw
}

func (b *streamingResponse) Writer(w io.Writer) {
	b.raw = w
	b.enc = json.NewEncoder(b.raw)
}*/

func (b *streamingResponse) Close() (err error) {
	if err = b.commit(); err != nil {
		return
	}
	if !b.isClosed {
		// Write footer (if we got error somewhere its alrady send)
		err = b.SetResponseResult("Done")
	}
	b.isClosed = true
	return
}
func (b *streamingResponse) commit() (err error) {
	if b.buffer.Len() <= 0 {
		return
	}
	var resp struct {
		Data interface{} `json:"data"`
		ID   interface{} `json:"id,omitempty"`
	}
	if b.chunkSize <= 0 {
		resp.Data = b.buffer.String()
	} else {
		resp.Data = b.buffer.Bytes()
	}

	resp.ID = b.id
	err = b.enc.Encode(resp)
	b.buffer.Reset()
	return
}

func (b *streamingResponse) SetID(id *interface{}) {
	b.id = id
}

func (b *streamingResponse) Write(p []byte) (n int, err error) {
	if b.isClosed {
		return 0, io.EOF
	}
	n = 0
	if b.isClosed {
		log.Warn("Write disabled on closed response")
		return 0, io.EOF
	}
	if b.firstWrite {
		err = b.SetResponseResult("OK")
		b.firstWrite = false
		if err != nil {
			return
		}
	}
	var resp struct {
		Data interface{} `json:"data"`
		ID   interface{} `json:"id,omitempty"`
	}
	resp.ID = b.id
	//TODO: Can we do better?
	if b.chunkSize <= 0 {
		result := bytes.SplitAfter(p, []byte("\n"))
		var npart int
		for _, v := range result {
			lenv := len(v)

			if lenv == 0 {
				// Well looks like this can happen
				continue
			}

			if v[lenv-1] == '\n' {
				lineWithoutEnding := v[:len(v)-1]
				// special case with dangling data in buffer
				if b.buffer.Len() != 0 {
					npart, err = b.buffer.Write(lineWithoutEnding)
					if err != nil {
						return
					}
					// assign buffer bytes as source
					lineWithoutEnding = b.buffer.Bytes()
					b.buffer.Reset()
				}
				resp.Data = string(lineWithoutEnding)
				err = b.enc.Encode(resp)
				if err != nil {
					return
				}
				n += lenv
			} else {
				// Oh dang! We get some leftovers...
				npart, err = b.buffer.Write(v)
				if err != nil {
					return n, err
				}
				n += npart
			}
		}
		return
	}
	if len(p)+b.buffer.Len() >= b.chunkSize {
		var npart int
		missingBytes := (b.chunkSize - b.buffer.Len()) % b.chunkSize
		if missingBytes != b.chunkSize {
			npart, err = b.buffer.Write(p[0:missingBytes])
			if err != nil {
				return n, err
			}
			n += npart
			err = b.commit()
			if err != nil {
				return
			}
		}
		chunk := missingBytes

		for ; chunk+b.chunkSize < len(p); chunk += b.chunkSize {
			resp.Data = p[chunk : chunk+b.chunkSize]
			n += b.chunkSize
			if err = b.enc.Encode(resp); err != nil {
				return
			}
		}
		var lastN int
		lastN, err = b.buffer.Write(p[chunk:len(p)])
		if err != nil {
			return
		}
		n += lastN
		return
	}
	npart, erro := b.buffer.Write(p)
	if erro != nil {
		return
	}
	n += npart
	return
}

func (b *streamingResponse) SetResponseError(e error) (err error) {
	if !b.isClosed {
		if err = b.commit(); err != nil {
			return
		}
		// Force close flag, to prevent sending "DONE" after error
		b.isClosed = true
		err = b.enc.Encode(NewErrorEx(e, b.id, JsonRPCversion20s))
		defer b.Close()
		if err != nil {
			log.Debug("Unable to send error information", "err", err)
			return
		}
		return b.Close()
	}
	log.Warn("Setting error more than once!")
	return
}

func (b *streamingResponse) SetResponseResult(r interface{}) error {
	if b.isClosed {
		log.Warn("Unable to set result on closed response")
		return nil
	}
	if err := b.commit(); err != nil {
		return err
	}
	return b.enc.Encode(NewResultEx(r, b.id, JsonRPCversion20s))
}

type legacyStreamingResponse struct {
	raw     io.Writer
	limiter io.Writer
}

func newLegacyStreamingResponse(out io.Writer) rpcResponser {
	return &legacyStreamingResponse{out, out}
}

/*
func (b *legacyStreamingResponse) GetWriter() io.Writer {
	return b.raw
}

func (b *legacyStreamingResponse) Writer(w io.Writer) {
	b.raw = w
}
*/
func (b *legacyStreamingResponse) Close() error {
	b.raw = nil
	b.limiter = nil
	return nil
}

func (b *legacyStreamingResponse) SetID(id *interface{}) {
	log.Debug("SetId unused for Legacy streaming")
}

func (b *legacyStreamingResponse) Write(p []byte) (int, error) {
	return b.limiter.Write(p)
}

func (b *legacyStreamingResponse) SetResponseError(e error) error {
	log.Debug("SetResponseError unused for Legacy streaming")
	return nil
}

func (b *legacyStreamingResponse) SetResponseResult(result interface{}) (err error) {
	switch converted := result.(type) {
	case string, int, int16, int32, int64, int8:
		_, err = io.WriteString(b, fmt.Sprintf("%v", converted))
	default:
		log.Crit("Unknown input result", "result", result)
	}
	// And thats it
	b.raw = nil
	return
}

func (b *legacyStreamingResponse) MaxResponse(max int64) {
	b.limiter = ExceptionalLimitWrite(b.raw, max)
}
