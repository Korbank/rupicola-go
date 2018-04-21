package rupicolarpc

import "io"

type rpcResponser interface {
	io.WriteCloser
	SetID(*interface{})
	SetResponseResult(interface{}) error
	SetResponseError(error) error
	Writer() io.Writer
	MaxResponse(int64)
}
