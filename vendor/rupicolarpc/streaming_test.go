package rupicolarpc

import (
	"bytes"
	"reflect"
	"testing"
)

func Test_newStreamingResponse(t *testing.T) {
	type args struct {
		n int
	}
	tests := []struct {
		name       string
		args       args
		want       rpcResponserPriv
		wantWriter string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := &bytes.Buffer{}
			if got := newStreamingResponse(writer, tt.args.n); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("newStreamingResponse() = %v, want %v", got, tt.want)
			}
			if gotWriter := writer.String(); gotWriter != tt.wantWriter {
				t.Errorf("newStreamingResponse() = %v, want %v", gotWriter, tt.wantWriter)
			}
		})
	}
}

func Test_streamingResponse_MaxResponse(t *testing.T) {
	type args struct {
		max int64
	}
	tests := []struct {
		name string
		b    *streamingResponse
		args args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.b.MaxResponse(tt.args.max)
		})
	}
}

/*func Test_streamingResponse_Writer(t *testing.T) {
	tests := []struct {
		name string
		b    *streamingResponse
		want io.Writer
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := in0.String(); got != tt.want {
				t.Errorf("streamingResponse.Writer() = %v, want %v", got, tt.want)
			}
		})
	}
}*/

func Test_streamingResponse_Close(t *testing.T) {
	tests := []struct {
		name    string
		b       *streamingResponse
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.b.Close(); (err != nil) != tt.wantErr {
				t.Errorf("streamingResponse.Close() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_streamingResponse_commit(t *testing.T) {
	tests := []struct {
		name    string
		b       *streamingResponse
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.b.commit(); (err != nil) != tt.wantErr {
				t.Errorf("streamingResponse.commit() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_streamingResponse_SetID(t *testing.T) {
	type args struct {
		id *interface{}
	}
	tests := []struct {
		name string
		b    *streamingResponse
		args args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.b.SetID(tt.args.id)
		})
	}
}

func Test_streamingResponse_Write(t *testing.T) {
	type args struct {
		p []byte
	}
	tests := []struct {
		name    string
		b       *streamingResponse
		args    args
		wantN   int
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotN, err := tt.b.Write(tt.args.p)
			if (err != nil) != tt.wantErr {
				t.Errorf("streamingResponse.Write() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotN != tt.wantN {
				t.Errorf("streamingResponse.Write() = %v, want %v", gotN, tt.wantN)
			}
		})
	}
}

func Test_streamingResponse_SetResponseError(t *testing.T) {
	type args struct {
		e error
	}
	tests := []struct {
		name    string
		b       *streamingResponse
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.b.SetResponseError(tt.args.e); (err != nil) != tt.wantErr {
				t.Errorf("streamingResponse.SetResponseError() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_streamingResponse_SetResponseResult(t *testing.T) {
	type args struct {
		r interface{}
	}
	tests := []struct {
		name    string
		b       *streamingResponse
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.b.SetResponseResult(tt.args.r); (err != nil) != tt.wantErr {
				t.Errorf("streamingResponse.SetResponseResult() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
