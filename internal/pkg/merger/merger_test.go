package merger

import (
	"reflect"
	"testing"
)

func Test_merge(t *testing.T) {
	// TODO: Add test cases.
	type booya struct {
		Number int
	}

	type annotated struct {
		Eeno   booya
		Normal int
		Anno   int
		Beno   int
		Ceno   []*booya `merger:""`
		Deno   *booya
	}

	type args struct {
		a interface{}
		b interface{}
	}

	tests := []struct {
		name    string
		args    args
		want    interface{}
		wantErr bool
	}{
		// TODO: Add test cases.
		//{"sgat", args{annotated{Eeno: booya{0}}, annotated{Eeno: booya{4}}}, annotated{Eeno: booya{4}}, false},
		{"sgat", args{annotated{Ceno: []*booya{&booya{1}}}, annotated{}}, annotated{Ceno: []*booya{&booya{1}}}, false},
		{"tags", args{&annotated{}, &annotated{Ceno: []*booya{&booya{1}}}}, &annotated{Ceno: []*booya{&booya{1}}}, false},
		{"arrs", args{[]int{1, 1}, []int{2, 2}}, []int{1, 1, 2, 2}, false},
		{"maps", args{map[string]string{"b": "", "a": "a"}, map[string]string{"b": "b", "c": "c"}}, map[string]string{"b": "b", "a": "a", "c": "c"}, false},
		{"maps", args{&map[string]string{"b": "", "a": "a"}, &map[string]string{"b": "b", "c": "c"}}, map[string]string{"b": "b", "a": "a", "c": "c"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Merge(tt.args.a, tt.args.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("merge() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("merge() = %v, want %v", got, tt.want)
			}
		})
	}
}
