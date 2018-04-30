package rupicolarpc

import (
	"fmt"
	"io"

	log "github.com/inconshreveable/log15"
)

type legacyStreamingResponse struct {
	baseResponse
}

func newLegacyStreamingResponse(out io.Writer) rpcResponserPriv {
	return &legacyStreamingResponse{newBaseResponse(out, ExceptionalLimitWrite(out, -1))}
}

func (b *legacyStreamingResponse) SetResponseError(e error) error {
	log.Debug("SetResponseError unused for Legacy streaming", "error", e)
	return nil
}

func (b *legacyStreamingResponse) SetResponseResult(result interface{}) (err error) {
	// we should ot get reader ever here
	switch converted := result.(type) {
	case string, int, int16, int32, int64, int8:
		_, err = io.WriteString(b, fmt.Sprintf("%v", converted))
	default:
		log.Crit("Unknown input result", "result", result)
	}
	if err != nil {
		b.SetResponseError(err)
	}
	return
}
