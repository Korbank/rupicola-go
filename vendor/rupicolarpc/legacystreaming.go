package rupicolarpc

import (
	"fmt"
	"io"

	log "github.com/inconshreveable/log15"
)

type legacyStreamingResponse struct {
	raw     io.Writer
	limiter io.Writer
}

func newLegacyStreamingResponse(out io.Writer) rpcResponser {
	return &legacyStreamingResponse{out, out}
}

func (b *legacyStreamingResponse) Writer() io.Writer {
	return b.raw
}

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
