package config

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"os"
	"path/filepath"
	"time"

	"bitbucket.org/kociolek/rupicola-ng/internal/pkg/merger"
	"gopkg.in/yaml.v2"
)

var (
	ErrStub    = errors.New("X")
	EmptyValue = value{}
)

type Value interface {
	Int32(def int32) int32
	Uint32(def uint32) uint32
	Int64(def int64) int64
	Duration(def time.Duration) time.Duration
	Map(def map[interface{}]interface{}) map[string]Value
	Array(def []interface{}) []Value
	String(def string) string
	Bool(def bool) bool
	Get(key string) Value
	IsValid() bool
}

type value struct {
	Mapa  map[string]Value
	Lista []interface{}
	Other interface{}
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
func (v *value) String(def string) string {
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

func (v *value) Map(def map[interface{}]interface{}) map[string]Value {
	if v.Mapa == nil {
		// saaad
		return mapFrom(def)
	}
	return v.Mapa
}
func (v *value) Array(def []interface{}) []Value {
	// Yeeees we cast, loop and do other bad things
	// but who cares
	var result []Value
	convertFrom := v.Lista
	if v.Lista == nil {
		convertFrom = def
	}

	for _, x := range convertFrom {
		val := valFrom(x)
		result = append(result, val)
	}

	return result
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
func valFrom(this interface{}) Value {
	var val = new(value)
	switch cast := this.(type) {
	case map[interface{}]interface{}:
		log.Println("MAP")
		val.Mapa = mapFrom(cast)
		break
	case []interface{}:
		log.Println("Array")
		val.Lista = cast
		break
	default:
		log.Println("End is nigh")
		val.Other = this
		break
	}
	return val
}
func (v *value) Get(key string) Value {
	if v.Mapa == nil {
		return &EmptyValue
	}
	val, has := v.Mapa[key]
	if !has {
		return &EmptyValue
	}
	return val
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

type Config struct {
	root value
	def  value
}

func (c *Config) Get(key ...string) Value {
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
func (c *Config) Load(paths ...string) error {
	pathToVisit := new(Stack)
	for _, path := range paths {
		pathToVisit.Push(path)
	}
	for pathToVisit.Len() != 0 {
		path := pathToVisit.Pop()
		log.Println("loading", path)
		bytes, e := ioutil.ReadFile(path)
		if e != nil {
			return e
		}
		val := new(value)
		var specialOne map[interface{}]interface{}
		if err := yaml.Unmarshal(bytes, &specialOne); err != nil {
			if err := yaml.Unmarshal(bytes, &val.Lista); err != nil {
				if err := yaml.Unmarshal(bytes, &val.Other); err != nil {
					return err
				}
			}
		} else {
			val.Mapa = mapFrom(specialOne)
		}
		// do we have any includes
		includesr := val.Get("include")
		includes := includesr.Array(nil)
		if len(includes) != 0 {
			for _, v := range includes {
				path := v.String("")
				req := false
				if path == "" {
					compound := v.Map(nil)
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
					pathToVisit.Push(klon)
				}
			}
		}

		m, _ := merger.Merge(c.root.Mapa, val.Mapa)
		c.root.Mapa = m.(map[string]Value)
	}
	return nil
}
