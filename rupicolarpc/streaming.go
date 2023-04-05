package rupicolarpc

import (
	"bytes"
	"io"
)

// TODO: How do we handle empty id?
// Disallow? in rpc it's "notification"
// but for stream... pointless.
type streamingResponse struct {
	baseResponse
	buffer     *bytes.Buffer
	chunkSize  int
	firstWrite bool
	isClosed   bool
}

func newStreamingResponse(writer io.Writer, n int) *streamingResponse {
	limited := ExceptionalLimitWrite(writer, 0)
	result := &streamingResponse{
		baseResponse: newBaseResponse(writer, limited),
		buffer:       &bytes.Buffer{},
		chunkSize:    n,
		firstWrite:   true,
		isClosed:     false,
	}

	return result
}

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
		Data interface{}  `json:"data"`
		ID   *interface{} `json:"id,omitempty"`
	}

	if b.chunkSize <= 0 {
		resp.Data = b.buffer.String()
	} else {
		resp.Data = b.buffer.Bytes()
	}

	resp.ID = b.id
	err = b.encoder.Encode(resp)
	b.buffer.Reset()

	return
}

func (b *streamingResponse) SetID(id *interface{}) {
	b.id = id
}

type streamingChunk struct {
	Data interface{}  `json:"data"`
	ID   *interface{} `json:"id,omitempty"`
}

func (b *streamingResponse) handleLineChunks(p []byte, resp streamingChunk) (int, error) {
	var n int

	result := bytes.SplitAfter(p, []byte("\n"))
	for _, line := range result {
		lenv := len(line)

		if lenv == 0 {
			// Well looks like this can happen
			continue
		}

		if line[lenv-1] == '\n' {
			lineWithoutEnding := line[:len(line)-1]
			// special case with dangling data in buffer
			if b.buffer.Len() != 0 {
				_, err := b.buffer.Write(lineWithoutEnding)
				if err != nil {
					return n, err
				}
				// assign buffer bytes as source
				lineWithoutEnding = b.buffer.Bytes()
				b.buffer.Reset()
			}

			resp.Data = string(lineWithoutEnding)

			if err := b.encoder.Encode(resp); err != nil {
				return n, err
			}

			n += lenv
		} else {
			// Oh dang! We get some leftovers...
			npart, err := b.buffer.Write(line)
			if err != nil {
				return n, err
			}
			n += npart
		}
	}

	return n, nil
}

func (b *streamingResponse) handleChunks(p []byte, resp streamingChunk) (int, error) {
	var npart int
	var err error
	var n int

	missingBytes := (b.chunkSize - b.buffer.Len()) % b.chunkSize
	if missingBytes != b.chunkSize {
		npart, err = b.buffer.Write(p[0:missingBytes])
		if err != nil {
			return n, err
		}

		n += npart

		err = b.commit()
		if err != nil {
			return n, err
		}
	}

	chunk := missingBytes
	for ; chunk+b.chunkSize < len(p); chunk += b.chunkSize {
		resp.Data = p[chunk : chunk+b.chunkSize]
		n += b.chunkSize

		if err = b.encoder.Encode(resp); err != nil {
			return n, err
		}
	}

	var lastN int

	lastN, err = b.buffer.Write(p[chunk:])
	if err != nil {
		return n, err
	}

	n += lastN

	return n, err
}

func (b *streamingResponse) Write(p []byte) (int, error) {
	if b.isClosed {
		return 0, io.EOF
	}

	var n int
	var err error

	n = 0

	if b.isClosed {
		Logger.Warn().Msg("Write disabled on closed response")

		return 0, io.EOF
	}

	if b.firstWrite {
		err = b.SetResponseResult("OK")
		b.firstWrite = false

		if err != nil {
			return n, err
		}
	}

	resp := streamingChunk{
		Data: nil,
		ID:   b.id,
	}

	if b.chunkSize <= 0 {
		return b.handleLineChunks(p, resp)
	}

	if len(p)+b.buffer.Len() >= b.chunkSize {
		return b.handleChunks(p, resp)
	}

	// TODO: it was typo, and should return erro and not err?
	npart, erro := b.buffer.Write(p)
	if erro != nil {
		return n, err
	}

	n += npart

	return n, err
}

func (b *streamingResponse) SetResponseError(respErr error) (err error) {
	if b.isClosed {
		Logger.Warn().Msg("Setting error more than once!")

		return
	}

	if err = b.commit(); err != nil {
		return
	}
	defer b.Close()
	// Make unlimited writer (errors should always be written)
	b.MaxResponse(0)
	// Force close flag, to prevent sending "DONE" after error
	b.isClosed = true

	err = b.encoder.Encode(NewErrorEx(respErr, b.id, JSONRPCversion20s))
	if err != nil {
		Logger.Debug().Err(err).Msg("Unable to send error information")

		return
	}

	return b.Close()
}

func (b *streamingResponse) SetResponseResult(result interface{}) error {
	if b.isClosed {
		Logger.Warn().Msg("Unable to set result on closed response")

		return nil
	}

	if err := b.commit(); err != nil {
		return err
	}
	// Warning this will not be chunked, pass it through chunker?
	return b.encoder.Encode(NewResultEx(result, b.id, JSONRPCversion20s))
}
