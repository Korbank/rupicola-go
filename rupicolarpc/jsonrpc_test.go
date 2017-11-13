package rupicolarpc

import (
	"bytes"
	"strings"
	"testing"
)

func TestMethodNotFound(t *testing.T) {
	requestString := `{"jsonrpc":"2.0", "method": "unknown-method", "params":{}, "id":0}`
	responseString := `{"jsonrpc":"2.0","error":{"code":-32601,"message":"Method not found"},"id":0}` + "\n"
	response := bytes.NewBuffer(nil)
	rpc := NewJsonRpcProcessor()
	rpc.AddMethodFunc("method", RpcMethod, func(in JsonRpcRequest, context interface{}) (interface{}, error) { return nil, nil })
	err := rpc.Process(strings.NewReader(requestString), response, nil, RpcMethod)
	if err != nil {
		t.Fail()
	}
	if response.String() != responseString {
		t.Error(response, responseString)
	}
}

func TestMethodReader(t *testing.T) {
	requestString := `{"jsonrpc":"2.0", "method": "method", "params":{}, "id":0}`
	responseString := `{"jsonrpc":"2.0","result":"string","id":0}` + "\n"
	response := bytes.NewBuffer(nil)
	rpc := NewJsonRpcProcessor()
	rpc.AddMethodFunc("method", RpcMethod, func(in JsonRpcRequest, context interface{}) (interface{}, error) {
		return strings.NewReader("string"), nil
	})
	err := rpc.Process(strings.NewReader(requestString), response, nil, RpcMethod)
	if err != nil {
		t.Fail()
	}
	if response.String() != responseString {
		t.Error(response, responseString)
	}
}

func TestMethodImmediate(t *testing.T) {
	requestString := `{"jsonrpc":"2.0", "method": "method", "params":{}, "id":0}`
	responseString := `{"jsonrpc":"2.0","result":"string","id":0}` + "\n"
	response := bytes.NewBuffer(nil)
	rpc := NewJsonRpcProcessor()
	rpc.AddMethodFunc("method", RpcMethod, func(in JsonRpcRequest, context interface{}) (interface{}, error) { return "string", nil })
	err := rpc.Process(strings.NewReader(requestString), response, nil, RpcMethod)
	if err != nil {
		t.Fail()
	}
	if response.String() != responseString {
		t.Error(response, responseString)
	}
}

func TestMethodStreaming(t *testing.T) {
	requestString := `{"jsonrpc":"2.0", "method": "method", "params":{}, "id":0}`
	responseString := "string"
	response := bytes.NewBuffer(nil)
	rpc := NewJsonRpcProcessor()
	rpc.AddMethodFunc("method", StreamingMethodLegacy, func(in JsonRpcRequest, context interface{}) (interface{}, error) { return "string", nil })
	err := rpc.Process(strings.NewReader(requestString), response, nil, StreamingMethodLegacy)
	if err != nil {
		t.Fail()
	}
	if response.String() != responseString {
		t.Error("\n", response, "\n", responseString)
	}
}
