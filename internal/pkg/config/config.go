package config

import (
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

var (
	emptyValue = value{}
)

// Value represents any underlying value
type Value interface {
	Int32(def int32) int32
	Uint32(def uint32) uint32
	Int64(def int64) int64
	Duration(def time.Duration) time.Duration
	Map() map[string]Value
	Array(def []interface{}) []Value
	AsString(def string) string
	Bool(def bool) bool
	Get(key ...string) Value
	get(key string) Value
	IsValid() bool
	IsArray() bool
	IsMap() bool
	Raw() interface{}
}
type mapValue struct {
	Mapa map[string]Value
}
type listValue struct {
	Lista []interface{}
}
type scalarValue struct {
	Other interface{}
}

type value struct {
	Mapa  map[string]Value
	Lista []Value
	Other interface{}
}

func (v *value) String() string {
	if !v.IsValid() {
		return "invalid"
	}
	if v.IsArray() {
		return "array"
	}
	if v.IsMap() {
		b := &strings.Builder{}
		b.WriteString("{")

		for k, v := range v.Mapa {
			b.WriteString(k + ":" + fmt.Sprintf("%v", v))
		}

		b.WriteString("}")
		return b.String()
	}
	return fmt.Sprintf("%v", v.Other)
}

func (v *value) IsArray() bool {
	return v.Lista != nil
}

func (v *value) IsMap() bool {
	return v.Mapa != nil
}

func (v *value) Raw() interface{} {
	return v.Other
}

func (v *value) Uint32(def uint32) uint32 {
	return uint32(v.Int64(int64(def)))
}

func (v *value) IsValid() bool {
	return v.Mapa != nil || v.Lista != nil || v.Other != nil
}

func (v *value) Duration(def time.Duration) time.Duration {
	return time.Duration(v.Int64(int64(def)))
}
func (v *value) AsString(def string) string {
	if v.Other == nil {
		return def
	}
	str, ok := v.Other.(string)
	if ok {
		return str
	}
	return def
}

func (v *value) Bool(def bool) bool {
	if v.Other == nil {
		return def
	}
	str, ok := v.Other.(bool)
	if ok {
		return str
	}
	return def
}

func (v *value) Map() map[string]Value {
	return v.Mapa
}

func arrayFrom(def []interface{}) []Value {
	var result []Value
	for _, x := range def {
		val := valFrom(x)
		result = append(result, val)
	}
	return result
}

func (v *value) Array(def []interface{}) []Value {
	// Yeeees we cast, loop and do other bad things
	// but who cares
	if v.Lista != nil {
		return v.Lista
	}
	return arrayFrom(def)
}

func mapFrom2(this map[string]interface{}) map[string]Value {
	mapa := make(map[string]Value)
	for k, v := range this {
		mapa[k] = valFrom(v)
	}
	return mapa
}
func mapFrom(this map[interface{}]interface{}) map[string]Value {
	mapa := make(map[string]Value)
	for k, v := range this {
		var key string
		switch cast := k.(type) {
		case string:
			key = cast
			break
		default:
			key = fmt.Sprint(k)
		}
		mapa[key] = valFrom(v)
	}
	return mapa
}

// ValFrom wrapes provided value inside Value
func ValFrom(this interface{}) Value {
	return valFrom(this)
}

func valFrom(this interface{}) Value {
	var val = new(value)
	switch cast := this.(type) {
	case Value:
		return cast
	case []Value:
		val.Lista = cast
	case map[string]Value:
		val.Mapa = cast
	case map[string]interface{}:
		val.Mapa = mapFrom2(cast)
	case map[interface{}]interface{}:
		val.Mapa = mapFrom(cast)
	case []interface{}:
		val.Lista = arrayFrom(cast)
	default:
		val.Other = this
	}
	return val
}
func (v *value) get(key string) Value {
	if v.Mapa == nil {
		return &emptyValue
	}
	val, has := v.Mapa[key]
	if !has {
		return &emptyValue
	}
	return val
}
func (v *value) Get(keys ...string) Value {
	current := Value(v)
	for _, key := range keys {
		current = current.get(key)
	}
	return current
}

func (v *value) Int64(def int64) int64 {
	if v.Other == nil {
		return def
	}
	switch cast := v.Other.(type) {
	case int:
		return int64(cast)
	case int32:
		return int64(cast)
	case int64:
		return cast
	default:
		return def
	}
}

func (v *value) Int32(def int32) int32 {
	// decode using bigger format
	big := v.Int64(int64(def))
	if big == int64(def) {
		return def
	}
	// got some value, but can we cast it?

	if big > math.MaxInt32 {
		return def
	}
	return int32(big)
}

// Config uration
type Config interface {
	Get(key ...string) Value
	Load(path ...string) error
}
type config struct {
	root value
	def  value
}

// NewConfig returns empty configuration
func NewConfig() Config {
	return new(config)
}

func (c *config) Get(key ...string) Value {
	var currentValue Value
	currentValue = &c.root
	for _, k := range key {
		currentValue = currentValue.Get(k)
	}
	return currentValue
}

func searchFiles(path string, required bool) (out []string, err error) {
	fileInfo, err := os.Stat(path)
	if os.IsNotExist(err) {
		if required {
			return nil, err
		}
		//log.Warn("Optional config not found", "path", info.Name)
		return nil, nil
	}

	if fileInfo.IsDir() {
		var entries []os.FileInfo
		if entries, err = ioutil.ReadDir(path); err == nil {
			for _, finfo := range entries {
				if !finfo.IsDir() && filepath.Ext(finfo.Name()) == ".conf" {
					// first field doesn't matter - we are including files from directory so
					// they should exists
					out = append(out, filepath.Join(path, finfo.Name()))
				}
			}

		}
	} else {
		out = []string{path}
	}
	return
}

func (c *config) Load(paths ...string) error {
	pathToVisit := new(stack)
	for _, path := range paths {
		pathToVisit.push(path)
	}
	for pathToVisit.len() != 0 {
		path := pathToVisit.pop()
		log.Println("loading", path)
		bytes, e := ioutil.ReadFile(path)
		if e != nil {
			return e
		}
		val := new(value)
		var specialOne map[string]interface{}
		if err := yaml.Unmarshal(bytes, &specialOne); err != nil {
			if err := yaml.Unmarshal(bytes, &val.Lista); err != nil {
				if err := yaml.Unmarshal(bytes, &val.Other); err != nil {
					return err
				}
			}
		} else {
			val.Mapa = mapFrom2(specialOne)
		}
		// do we have any includes
		includesr := val.Get("include")
		includes := includesr.Array(nil)
		if len(includes) != 0 {
			for _, v := range includes {
				path := v.AsString("")
				req := false
				if path == "" {
					compound := v.Map()
					for k, v := range compound {
						path = k
						req = v.Bool(false)
						break
					}
				}
				fs, e := searchFiles(path, req)
				if e != nil {
					return e
				}
				for _, f := range fs {
					klon := f
					pathToVisit.push(klon)
				}
			}
		}
		c.root.Mapa = mergeMap(c.root.Mapa, val.Mapa)
	}
	return nil
}

func pp(a Value, lvl int) {
	if a.IsMap() {
		for k, v := range a.Map() {
			fmt.Printf("%s%s:\n", strings.Repeat(" ", lvl), k)
			pp(v, lvl+1)
		}
	} else if a.IsArray() {
		for _, v := range a.Array(nil) {
			pp(v, lvl+1)
		}
	} else {
		fmt.Printf("%s%v\n", strings.Repeat(" ", lvl), a.Raw())
	}
}

func mergeValue(a, b Value) Value {
	if a.IsMap() && b.IsMap() {
		return valFrom(mergeMap(a.Map(), b.Map()))
	} else if a.IsArray() && b.IsArray() {
		return valFrom(append(a.Array(nil), b.Array(nil)...))
	}
	return b
}

func mergeMap(a, b map[string]Value) map[string]Value {
	c := make(map[string]Value)
	for k, v := range a {
		c[k] = v
	}

	for k, v := range b {
		av, has := a[k]
		if has {
			c[k] = mergeValue(av, v)
		} else {
			c[k] = v
		}
	}
	return c
}
