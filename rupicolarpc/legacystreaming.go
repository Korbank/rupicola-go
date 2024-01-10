package rupicolarpc

import (
	"fmt"
	"io"
)

type legacyStreamingResponse struct {
	baseResponse
}

func newLegacyStreamingResponse(out io.Writer) *legacyStreamingResponse {
	return &legacyStreamingResponse{newBaseResponse(out, ExceptionalLimitWrite(out, -1))}
}

func (b *legacyStreamingResponse) SetResponseError(e error) error {
	Logger.Debug().Err(e).Msg("SetResponseError unused for Legacy streaming")

	return nil
}

func (b *legacyStreamingResponse) SetResponseResult(result interface{}) (err error) {
	// we should ot get reader ever here
	switch converted := result.(type) {
	case string, int, int16, int32, int64, int8:
		_, err = io.WriteString(b, fmt.Sprintf("%v", converted))
	default:
		Logger.Error().Str("result", fmt.Sprintf("%v", result)).Msg("Unknown input result")
	}

	if err != nil {
		// NOTE(m): Ignore error, at this point it's meaningless
		ignoredError := b.SetResponseError(err)
		Logger.Error().Err(ignoredError).Send()
	}

	return
}
