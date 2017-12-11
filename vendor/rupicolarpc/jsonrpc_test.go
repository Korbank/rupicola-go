package rupicolarpc

import (
	"bytes"
	"errors"
	"fmt"
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

func TestLegacyMethodStreaming(t *testing.T) {
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

func TestMethodStreaming(t *testing.T) {
	requestString := `{"jsonrpc":"2.0+s", "method": "method", "params":{}, "id":0}`
	responseString := fmt.Sprint(`{"jsonrpc":"2.0+s","result":"string","id":0}`, "\n",
		`{"jsonrpc":"2.0+s","result":"Done","id":0}`, "\n")
	response := bytes.NewBuffer(nil)
	rpc := NewJsonRpcProcessor()
	rpc.AddMethodFunc("method", StreamingMethod, func(in JsonRpcRequest, context interface{}) (interface{}, error) { return "string", nil })
	err := rpc.Process(strings.NewReader(requestString), response, nil, StreamingMethod)
	if err != nil {
		t.Fail()
	}
	if response.String() != responseString {
		t.Error("\n", response, "\n", responseString)
	}
}

func TestLegacyMethodStreamingError(t *testing.T) {
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

func TestMethodStreamingError(t *testing.T) {
	requestString := `{"jsonrpc":"2.0", "method": "method", "params":{}, "id":0}`
	responseString := `{"jsonrpc":"2.0+s","error":{"code":-32603,"message":"Internal error","data":"string"},"id":0}` + "\n"
	responseError := errors.New("string")
	response := bytes.NewBuffer(nil)
	rpc := NewJsonRpcProcessor()
	rpc.AddMethodFunc("method", StreamingMethod, func(in JsonRpcRequest, context interface{}) (interface{}, error) { return nil, responseError })
	err := rpc.Process(strings.NewReader(requestString), response, nil, StreamingMethod)
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
	rpc.ExecutionTimeout(RPCMethod, 20*time.Millisecond)
	rpc.AddMethodFunc("method", RPCMethod, func(in JsonRpcRequest, context interface{}) (interface{}, error) {
		time.Sleep(200 * time.Millisecond)
		t.Fail()
		return nil, responseError
	})
	err := rpc.Process(strings.NewReader(requestString), response, nil, RPCMethod)
	if err != TimeoutError {
		t.Log(err)
		t.Fail()
	}
	if response.String() != responseString {
		t.Error("\n", response, "\n", responseString)
	}
}

func TestLegacyMethodStreamingTimeout(t *testing.T) {
	requestString := `{"jsonrpc":"2.0", "method": "method", "params":{}, "id":0}`
	responseString := ""
	responseError := errors.New("string")
	response := bytes.NewBuffer(nil)
	rpc := NewJsonRpcProcessor()
	rpc.AddMethodFunc("method", StreamingMethodLegacy,
		func(in JsonRpcRequest, context interface{}) (interface{}, error) {
			time.Sleep(100 * time.Millisecond)
			return nil, responseError
		}).ExecutionTimeout(10 * time.Millisecond)
	err := rpc.Process(strings.NewReader(requestString), response, nil, StreamingMethodLegacy)
	if err != TimeoutError {
		t.Error(err)
	}
	if response.String() != responseString {
		t.Error("\n", response, "\n", responseString)
	}
}

func TestMethodStreamingTimeout(t *testing.T) {
	requestString := `{"jsonrpc":"2.0", "method": "method", "params":{}, "id":0}`
	responseString := `{"jsonrpc":"2.0+s","error":{"code":-32099,"message":"Timeout"},"id":0}`
	responseError := errors.New("string")
	response := bytes.NewBuffer(nil)
	rpc := NewJsonRpcProcessor()
	rpc.AddMethodFunc("method", StreamingMethod,
		func(in JsonRpcRequest, context interface{}) (interface{}, error) {
			time.Sleep(200 * time.Millisecond)
			return nil, responseError
		}).ExecutionTimeout(10 * time.Millisecond)
	err := rpc.Process(strings.NewReader(requestString), response, nil, StreamingMethod)
	if err != TimeoutError {
		t.Error(err)
	}
	if strings.TrimSpace(response.String()) != responseString {
		t.Error("\n", response, "\n", responseString)
	}
}

// The RFC tests examples

func TestRFC(t *testing.T) {
	type testCase struct {
		request  string
		response string
	}

	testCases := []testCase{
		testCase{`{"jsonrpc": "2.0", "method": "subtract", "params": [42, 23], "id": 1}`, `{"jsonrpc":"2.0","result":19,"id":1}`},
		testCase{`{"jsonrpc": "2.0", "method": "subtract", "params": [23, 42], "id": 2}`, `{"jsonrpc":"2.0","result":-19,"id":2}`},
		testCase{`{"jsonrpc": "2.0", "method": "subtract", "params": {"subtrahend": 23, "minuend": 42}, "id": 3}`, `{"jsonrpc":"2.0","result":19,"id":3}`},
		testCase{`{"jsonrpc": "2.0", "method": "subtract", "params": {"minuend": 42, "subtrahend": 23}, "id": 4}`, `{"jsonrpc":"2.0","result":19,"id":4}`},
		testCase{`{"jsonrpc": "2.0", "method": "update", "params": [1,2,3,4,5]}`, ``},
		testCase{`{"jsonrpc": "2.0", "method": "foobar"}`, ``},
		testCase{`{"jsonrpc": "2.0", "method": "foobar", "id": "1"}`, `{"jsonrpc":"2.0","error":{"code":-32601,"message":"Method not found"},"id":"1"}`},
		testCase{`{"jsonrpc": "2.0", "method": "foobar, "params": "bar", "baz]`, `{"jsonrpc":"2.0","error":{"code":-32700,"message":"Parse error"},"id":null}`},
		testCase{`{"jsonrpc": "2.0", "method": 1, "params": "bar"}`, `{"jsonrpc":"2.0","error":{"code":-32600,"message":"Invalid Request"},"id":null}`},
	}
	rpc := NewJsonRpcProcessor()
	rpc.AddMethodFunc("subtract", RPCMethod, func(in JsonRpcRequest, _ interface{}) (interface{}, error) {
		arg0, ok := in.Params["1"].(float64)
		if !ok {
			arg0, ok = in.Params["minuend"].(float64)
		}
		arg1, ok := in.Params["2"].(float64)
		if !ok {
			arg1, ok = in.Params["subtrahend"].(float64)
		}
		return arg0 - arg1, nil
	})
	for i, v := range testCases {
		response := bytes.NewBuffer(nil)
		err := rpc.Process(strings.NewReader(v.request), response, nil, RPCMethod)
		if v.response != strings.TrimSpace(response.String()) {
			t.Fatalf("Failed [%d] %s %s:%s", i, v.response, response.String(), err)
		}
	}
	/*
		-->
		<--

		-->
		<--

		rpc call with named parameters:

		-->
		<--

		-->
		<--

		a Notification:

		-->
		-->

		rpc call of non-existent method:

		-->
		<--

		rpc call with invalid JSON:

		-->
		<--

		rpc call with invalid Request object:

		-->
		<--

		rpc call Batch, invalid JSON:

		--> [
		  {"jsonrpc": "2.0", "method": "sum", "params": [1,2,4], "id": "1"},
		  {"jsonrpc": "2.0", "method"
		]
		<-- {"jsonrpc": "2.0", "error": {"code": -32700, "message": "Parse error"}, "id": null}

		rpc call with an empty Array:

		--> []
		<-- {"jsonrpc": "2.0", "error": {"code": -32600, "message": "Invalid Request"}, "id": null}

		rpc call with an invalid Batch (but not empty):

		--> [1]
		<-- [
		  {"jsonrpc": "2.0", "error": {"code": -32600, "message": "Invalid Request"}, "id": null}
		]

		rpc call with invalid Batch:

		--> [1,2,3]
		<-- [
		  {"jsonrpc": "2.0", "error": {"code": -32600, "message": "Invalid Request"}, "id": null},
		  {"jsonrpc": "2.0", "error": {"code": -32600, "message": "Invalid Request"}, "id": null},
		  {"jsonrpc": "2.0", "error": {"code": -32600, "message": "Invalid Request"}, "id": null}
		]

		rpc call Batch:

		--> [
				{"jsonrpc": "2.0", "method": "sum", "params": [1,2,4], "id": "1"},
				{"jsonrpc": "2.0", "method": "notify_hello", "params": [7]},
				{"jsonrpc": "2.0", "method": "subtract", "params": [42,23], "id": "2"},
				{"foo": "boo"},
				{"jsonrpc": "2.0", "method": "foo.get", "params": {"name": "myself"}, "id": "5"},
				{"jsonrpc": "2.0", "method": "get_data", "id": "9"}
			]
		<-- [
				{"jsonrpc": "2.0", "result": 7, "id": "1"},
				{"jsonrpc": "2.0", "result": 19, "id": "2"},
				{"jsonrpc": "2.0", "error": {"code": -32600, "message": "Invalid Request"}, "id": null},
				{"jsonrpc": "2.0", "error": {"code": -32601, "message": "Method not found"}, "id": "5"},
				{"jsonrpc": "2.0", "result": ["hello", 5], "id": "9"}
			]

		rpc call Batch (all notifications):

		--> [
				{"jsonrpc": "2.0", "method": "notify_sum", "params": [1,2,4]},
				{"jsonrpc": "2.0", "method": "notify_hello", "params": [7]}
			]
		<-- //Nothing is returned for all notification batches
	*/

}
