package merger

import (
	"errors"
	"log"
	"reflect"
	"strings"
)

var (
	ErrUnmergable = errors.New("Unmergable")
)

func merge_tag(a interface{}, b interface{}) {
	mirrora := reflect.TypeOf(a)
	mirrorb := reflect.TypeOf(b)
	if mirrora != mirrorb {
		return
	}
	for i := 0; i < mirrora.NumField(); i++ {
		f := mirrora.Field(i)
		t, ok := f.Tag.Lookup("merger")
		if !ok {
			continue
		}
		print(t)
	}
}

// Merge given arguments
// Treating 0 as literal value! To change this, mark fields as "zero-as-empty"
// Arguments are read-only
// tag: merger
// options:
// * zero-as-empty - treat empty (default) values as unset, and keep previous.
func Merge(a, b interface{}) (interface{}, error) {
	v, err := merge(reflect.ValueOf(a), reflect.ValueOf(b))
	if err != nil {
		return nil, err
	}
	return v.Interface(), nil
}

func merge(atype, btype reflect.Value) (out reflect.Value, err error) {
	// NOTE: We are creating new object of given type each time and not
	// using inplace replace. This is by design.

	//if atype.Kind() != reflect.Ptr {
	//	return reflect.Value{}, ErrUnmergable
	//}

	// Treat 0 as variable yet to be set
	zeroAsEmpty := false
	if atype.IsValid() && btype.IsValid() && atype.Type() != btype.Type() {
		return reflect.Value{}, ErrUnmergable
	}
	//if atype.Kind() == reflect.Ptr && atype.Elem().Kind() == reflect.Map {
	//	return reflect.Value{}, ErrUnmergable
	//}
	if atype.Kind() == reflect.Ptr {
		// If we do unwrap then pack again
		out, err = merge(atype.Elem(), btype.Elem())
		if err == nil && out.IsValid() && out.Kind() != reflect.Map {
			out = out.Addr()
		}
		return
	} else if atype.Kind() == reflect.Struct {
		// make type - this ensures we use modifable value later
		ctypePtr := reflect.New(atype.Type())
		ctype := ctypePtr.Elem()

		for i := 0; i < atype.NumField(); i++ {
			f := atype.Type().Field(i)
			tag, ok := f.Tag.Lookup("merger")
			if ok {
				args := strings.Split(tag, ",")
				switch args[0] {
				case "zero-as-empty":
					zeroAsEmpty = true
				}
			}
			// great!
			afield := atype.Field(i)
			cfield := ctype.Field(i)

			if !btype.IsValid() {
				cfield.Set(afield)
			} else {
				bfield := btype.Field(i)
				merged, _ := merge(afield, bfield)
				if merged.IsValid() {
					cfield.Set(merged)
				}
			}
		}
		return ctype, nil
	} else if atype.Kind() == reflect.Slice {
		alen := atype.Len()
		blen := btype.Len()
		carray := reflect.MakeSlice(reflect.SliceOf(atype.Type().Elem()), alen, alen+blen)
		reflect.Copy(carray, atype)
		return reflect.AppendSlice(carray, btype), nil
	} else if atype.Kind() == reflect.Map {
		amap := atype.MapKeys()
		bmap := btype.MapKeys()
		cmap := reflect.MakeMap(reflect.MapOf(atype.Type().Key(), atype.Type().Elem()))
		for _, akey := range amap {
			cmap.SetMapIndex(akey, atype.MapIndex(akey))
		}
		for _, bkey := range bmap {
			bval := btype.MapIndex(bkey)
			i := atype.MapIndex(bkey)
			has := i.IsValid()
			if has {
				ix, err := merge(i, bval)
				if err != nil {
					return reflect.Value{}, err
				}
				i = ix
			} else {
				i = bval
			}
			cmap.SetMapIndex(bkey, i)
		}
		return cmap, nil
	} else {
		err = nil
		if zeroAsEmpty {
			switch btype.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				if btype.Int() == 0 {
					log.Println("int 0, keep previous", atype.Int())
					out = atype
					return
				}
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				if btype.Uint() == 0 {
					log.Println("uint 0, keep previouse", atype.Uint())
					out = atype
					return
				}
			case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
				if btype.IsNil() {
					log.Println("ptr 0, keep previouse")
					out = atype
					return
				}
			case reflect.String:
				if btype.Len() == 0 {
					log.Println("str 0, keep previouse", atype.String())
					out = atype
					return
				}
				//add more cases for Float, Bool, String, etc (and anything else listed http://golang.org/pkg/reflect/#Kind )
			}
		}
		out = btype
		return
	}
}
