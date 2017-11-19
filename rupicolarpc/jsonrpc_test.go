package rupicolarpc

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMethodNotFound(t *testing.T) {
	requestString := `{"jsonrpc":"2.0", "method": "unknown-method", "params":{}, "id":0}`
	responseString := `{"jsonrpc":"2.0","error":{"code":-32601,"message":"Method not found"},"id":0}` + "\n"
	response := bytes.NewBuffer(nil)
	rpc := NewJsonRpcProcessor()
	rpc.AddMethodFunc("method", RPCMethod, func(in JsonRpcRequest, context interface{}) (interface{}, error) { return nil, nil })
	err := rpc.Process(strings.NewReader(requestString), response, nil, RPCMethod)
	if err != MethodNotFoundError {
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
	rpc.AddMethodFunc("method", RPCMethod, func(in JsonRpcRequest, context interface{}) (interface{}, error) {
		return strings.NewReader("string"), nil
	})
	err := rpc.Process(strings.NewReader(requestString), response, nil, RPCMethod)
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
	rpc.AddMethodFunc("method", RPCMethod, func(in JsonRpcRequest, context interface{}) (interface{}, error) { return "string", nil })
	err := rpc.Process(strings.NewReader(requestString), response, nil, RPCMethod)
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

func TestMethodStreamingError(t *testing.T) {
	requestString := `{"jsonrpc":"2.0", "method": "method", "params":{}, "id":0}`
	responseString := ""
	responseError := errors.New("string")
	response := bytes.NewBuffer(nil)
	rpc := NewJsonRpcProcessor()
	rpc.AddMethodFunc("method", StreamingMethodLegacy, func(in JsonRpcRequest, context interface{}) (interface{}, error) { return nil, responseError })
	err := rpc.Process(strings.NewReader(requestString), response, nil, StreamingMethodLegacy)
	if err != responseError {
		t.Fail()
	}
	if response.String() != responseString {
		t.Error("\n", response, "\n", responseString)
	}
}

func TestMethodRpcError(t *testing.T) {
	requestString := `{"jsonrpc":"2.0", "method": "method", "params":{}, "id":0}`
	responseString := `{"jsonrpc":"2.0","error":{"code":-32603,"message":"Internal error","data":"string"},"id":0}` + "\n"
	responseError := errors.New("string")
	response := bytes.NewBuffer(nil)
	rpc := NewJsonRpcProcessor()
	rpc.AddMethodFunc("method", RPCMethod, func(in JsonRpcRequest, context interface{}) (interface{}, error) { return nil, responseError })
	err := rpc.Process(strings.NewReader(requestString), response, nil, RPCMethod)
	if err != responseError {
		t.Fail()
	}
	if response.String() != responseString {
		t.Error("\n", response, "\n", responseString)
	}
}

func TestMethodRpcTimeout(t *testing.T) {
	requestString := `{"jsonrpc":"2.0", "method": "method", "params":{}, "id":0}`
	responseString := `{"jsonrpc":"2.0","error":{"code":-32099,"message":"Timeout"},"id":0}` + "\n"
	responseError := errors.New("string")
	response := bytes.NewBuffer(nil)
	rpc := NewJsonRpcProcessor()
	rpc.Limits.ExecTimeout = time.Second
	rpc.AddMethodFunc("method", RPCMethod, func(in JsonRpcRequest, context interface{}) (interface{}, error) {
		time.Sleep(2 * time.Second)
		t.Fail()
		return nil, responseError
	})
	err := rpc.Process(strings.NewReader(requestString), response, nil, RPCMethod)
	if err != TimeoutError {
		t.Fail()
	}
	if response.String() != responseString {
		t.Error("\n", response, "\n", responseString)
	}
}

func TestMethodStreamingTimeout(t *testing.T) {
	requestString := `{"jsonrpc":"2.0", "method": "method", "params":{}, "id":0}`
	responseString := ""
	responseError := errors.New("string")
	response := bytes.NewBuffer(nil)
	rpc := NewJsonRpcProcessor()
	rpc.AddMethodFunc("method", StreamingMethodLegacy, func(in JsonRpcRequest, context interface{}) (interface{}, error) { return nil, responseError })
	err := rpc.Process(strings.NewReader(requestString), response, nil, StreamingMethodLegacy)
	if err != responseError {
		t.Error(err)
	}
	if response.String() != responseString {
		t.Error("\n", response, "\n", responseString)
	}
}
