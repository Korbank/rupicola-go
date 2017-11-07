package rupicolarpc

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func Test(t *testing.T) {
	requestString := `{"jsonrpc":"2.0", "method": "method", "params":{}, "id":0}`
	responseString := `{"jsonrpc":"2.0","error":{"code":-32601,"message":"Method not found"},"id":0}` + "\n"
	response := bytes.NewBuffer(nil)
	rpc := NewJsonRpcProcessor()
	rpc.AddMethodFunc("method", func(in JsonRpcRequest, context interface{}, out io.Writer) error { return nil })
	err := rpc.Process(strings.NewReader(requestString), response, nil, RpcMethod)
	if err != nil {
		t.Fail()
	}
	if response.String() != responseString {
		t.Error(response, responseString)
	}
}
