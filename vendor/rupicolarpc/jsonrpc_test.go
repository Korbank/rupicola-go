package rupicolarpc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"strings"
	"testing"
	"time"
)

type dummyRequest struct {
	r io.Reader
	m MethodType
}

func (d *dummyRequest) Reader() (io.ReadCloser, error) {
	return ioutil.NopCloser(d.r), nil
}
func (d *dummyRequest) OutputMode() MethodType {
	return d.m
}

func TestMaxSize(t *testing.T) {
	types := []MethodType{RPCMethod, StreamingMethodLegacy, StreamingMethod}
	requestString := []string{
		`{"jsonrpc":"2.0", "method": "method", "params":{"stream":true}, "id":0}`,
		`{"jsonrpc":"2.0", "method": "method", "params":{"stream":false}, "id":0}`,
	}
	responseString := map[MethodType]string{
		RPCMethod:             `{"jsonrpc":"2.0","error":{"code":-32098,"message":"Limit Exceed"},"id":0}`,
		StreamingMethodLegacy: ``,
		StreamingMethod:       `{"jsonrpc":"2.0+s","error":{"code":-32098,"message":"Limit Exceed"},"id":0}`,
	}

	rpc := NewJsonRpcProcessor()
	for _, metype := range types {
		rpc.AddMethodFunc("method", metype,
			func(in JsonRpcRequest, context interface{}) (interface{}, error) {
				stream := in.Params["stream"].(bool)
				response := strings.Repeat("x", 40)
				if stream {
					return strings.NewReader(response), nil
				}
				return response, nil
			}).MaxSize(10)
		for _, req := range requestString {
			response := bytes.NewBuffer(nil)
			err := rpc.Process(&dummyRequest{strings.NewReader(req), metype}, response, nil)
			if err != ErrLimitExceed {
				t.Fatalf("%v %v %v", metype, req, err)
			}

			if strings.TrimSpace(response.String()) != responseString[metype] {
				t.Fatalf("%v %v - %v", metype, response, responseString[metype])
			}
		}
	}
}
func TestMethodNotFound(t *testing.T) {
	requestString := `{"jsonrpc":"2.0", "method": "unknown-method", "params":{}, "id":0}`
	responseString := `{"jsonrpc":"2.0","error":{"code":-32601,"message":"Method not found"},"id":0}` + "\n"
	response := bytes.NewBuffer(nil)
	rpc := NewJsonRpcProcessor()
	rpc.AddMethodFunc("method", RPCMethod, func(in JsonRpcRequest, context interface{}) (interface{}, error) { return nil, nil })
	err := rpc.Process(&dummyRequest{strings.NewReader(requestString), RPCMethod}, response, nil)
	if err != ErrMethodNotFound {
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
	err := rpc.Process(&dummyRequest{strings.NewReader(requestString), RPCMethod}, response, nil)
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
	err := rpc.Process(&dummyRequest{strings.NewReader(requestString), RPCMethod}, response, nil)
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
	err := rpc.Process(&dummyRequest{strings.NewReader(requestString), StreamingMethodLegacy}, response, nil)
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
	err := rpc.Process(&dummyRequest{strings.NewReader(requestString), StreamingMethod}, response, nil)
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
	err := rpc.Process(&dummyRequest{strings.NewReader(requestString), StreamingMethodLegacy}, response, nil)
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
	err := rpc.Process(&dummyRequest{strings.NewReader(requestString), StreamingMethod}, response, nil)
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
	err := rpc.Process(&dummyRequest{strings.NewReader(requestString), RPCMethod}, response, nil)
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
	err := rpc.Process(&dummyRequest{strings.NewReader(requestString), RPCMethod}, response, nil)
	if err != ErrTimeout {
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
	err := rpc.Process(&dummyRequest{strings.NewReader(requestString), StreamingMethodLegacy}, response, nil)
	if err != ErrTimeout {
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
	err := rpc.Process(&dummyRequest{strings.NewReader(requestString), StreamingMethod}, response, nil)
	if err != ErrTimeout {
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
		//	testCase{`{"jsonrpc": "2.0", "method": "subtract", "params": [42, 23], "id": 1}`, `{"jsonrpc":"2.0","result":19,"id":1}`},
		//	testCase{`{"jsonrpc": "2.0", "method": "subtract", "params": [23, 42], "id": 2}`, `{"jsonrpc":"2.0","result":-19,"id":2}`},
		//	testCase{`{"jsonrpc": "2.0", "method": "subtract", "params": {"subtrahend": 23, "minuend": 42}, "id": 3}`, `{"jsonrpc":"2.0","result":19,"id":3}`},
		//	testCase{`{"jsonrpc": "2.0", "method": "subtract", "params": {"minuend": 42, "subtrahend": 23}, "id": 4}`, `{"jsonrpc":"2.0","result":19,"id":4}`},
		//	testCase{`{"jsonrpc": "2.0", "method": "update", "params": [1,2,3,4,5]}`, ``},
		//	testCase{`{"jsonrpc": "2.0", "method": "foobar"}`, ``},
		//	testCase{`{"jsonrpc": "2.0", "method": "foobar", "id": "1"}`, `{"jsonrpc":"2.0","error":{"code":-32601,"message":"Method not found"},"id":"1"}`},
		testCase{`{"jsonrpc": "2.0", "method": "foobar, "params": "bar", "baz]`, `{"jsonrpc":"2.0","error":{"code":-32700,"message":"Parse error"},"id":null}`},
		//	testCase{`{"jsonrpc": "2.0", "method": 1, "params": "bar"}`, `{"jsonrpc":"2.0","error":{"code":-32600,"message":"Invalid Request"},"id":null}`},
		//	testCase{`{"jsonrpc": "2.0", "method": "foobar", "params": [1], "id":1}{"jsonrpc": "2.0", "method": "foobar", "params": [1], "id":2}`, `{"jsonrpc":"2.0","error":{"code":-32700,"message":"Parse error"},"id":null}`},
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
		err := rpc.Process(&dummyRequest{strings.NewReader(v.request), RPCMethod}, response, nil)
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
