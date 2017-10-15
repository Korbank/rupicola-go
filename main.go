package main

import "net/http"
import "log"
import "encoding/json"
import "bytes"

func handlerSlash(w http.ResponseWriter, r *http.Request) {
	var m JsonRpcRequest
	dec := json.NewDecoder(r.Body)
	err := dec.Decode(&m)

	log.Println(err)
	log.Print("Is valid: ")
	log.Println(m.IsValid())
	//if m.request == "" {
	//	log.Println("No request")
	//}

	//if m.options == nil {
	//	log.Println("No options")
	//}

	//log.Println(m.jsonrpc)
	log.Println(m)
	w.Write([]byte("borked"))
}

func main() {

	//{\"jsonrpc\":\"2.0\",\"method\":\"m\",\"options\":[1,2,3]}
	//jsonRequest := "[{\"jsonrpc\":\"2.0\",\"method\":\"m\",\"options\":[1,2,3]},{\"jsonrpc\":\"2.0\",\"method\":\"me\",\"options\":[1,2,3]}]"
	//jsonRequest := "{\"jsonrpc\":\"2.0\",\"method\":\"m\",\"options\":[1,2,3]}"
	jsonRequest := "{\"jsonrpc\":\"2.0\",\"method\":\"m\",\"options\":[\"1\",\"echo\",\"3\"]}"
	processor := NewJsonRpcProcessor()
	response := bytes.NewBuffer(nil)
	err := processor.Process(bytes.NewReader([]byte(jsonRequest)), response)
	log.Println(err)
	log.Println(response.String())
	//var request []JsonRpcRequest
	//json.Unmarshal([]byte(jsonRequest), &request)
	//log.Println(request)
	//json.Unmarshal([]byte(jsonRequest), &request[0])
	//log.Println(request)
	//log.Println(request.Jsonrpc)
	//log.Println(request.Method)
	//log.Println(request.Options)
	//log.Println("hello world")
	//log.Println(request.Options.OptionsMap)
	//log.Println(request.Options.OptionsTab)
	log.Println("DDD")
	//http.HandleFunc("/", handlerSlash)
	//log.Fatal(http.ListenAndServe("0.0.0.0:8090", nil))
}
