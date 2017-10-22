package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"path/filepath"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v2"
)

type Limits struct {
	ReadTimeout uint32 `yaml:"read-timeout"`
	ExecTimeout uint32 `yaml:"exec-timeout"`
	PayloadSize uint32 `yaml:"payload-size"`
	MaxResponse uint32 `yaml:"max-response"`
}

type RupicolaConfig struct {
	//Include  []IncludeConfig
	Protocol Protocol
	Limits   Limits
	Log      struct {
		Level string
	}
	Methods map[string]MethodDef
}
type MethodParam struct {
	Type     MethodParamType
	Optional bool
}
type MethodDef struct {
	Streamed bool
	Private  bool
	Encoding MethodEncoding
	Params   map[string]MethodParam
	Invoke   struct {
		Exec  string
		Args  []MethodArgs
		RunAs struct {
			Gid uint32
			Uid uint32
		}
	}
}

type MethodParamType int
type MethodEncoding int

const (
	Utf8   MethodEncoding = 0
	Base64                = 1
)
const (
	String MethodParamType = 0
	Int                    = 1
	Bool                   = 2
)

type MethodArgs struct {
	Param    string
	Skip     bool
	Static   bool
	compound bool
	Child    []MethodArgs
}

func (w *MethodEncoding) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var value string
	if err := unmarshal(&value); err != nil {
		return err
	}
	value = strings.ToLower(value)
	switch value {
	case "base64":
		*w = Base64
	case "utf-8":
	case "utf8":
		*w = Utf8
	default:
		return errors.New("Unknown output type")
	}
	return nil
}

func (w *MethodParam) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var tempStruct struct {
		Type     string
		Optional bool
	}
	err := unmarshal(&tempStruct)
	if err != nil {
		return err
	}

	switch strings.ToLower(tempStruct.Type) {
	case "string":
		w.Type = String
	case "integer":
		w.Type = Int
	case "bool":
		w.Type = Bool
	default:
		return errors.New("Unknown type")
	}
	w.Optional = tempStruct.Optional
	return nil
}

func (w *MethodArgs) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var plainText string
	var structure struct {
		Param string
		// Check what was it
		Skip bool
	}
	var array []MethodArgs

	if unmarshal(&plainText) == nil {
		w.Param = plainText
		w.Static = true
		w.Skip = false
		return nil
	}
	var err error
	if err = unmarshal(&structure); err == nil {
		w.Param = structure.Param
		w.Skip = structure.Skip
		w.Static = false
		return nil
	}
	if err = unmarshal(&array); err == nil {
		w.Child = array
		w.compound = true
		return nil
	}
	log.Panic(err)
	return nil
}

type IncludeConfig struct {
	Required bool
	Name     string
}
type Bind struct {
	Type         string
	Address      string
	Port         uint16
	AllowPrivate bool `yaml:"allow_private"`
}
type Protocol struct {
	Bind []Bind

	AuthBasic struct {
		Login    string
		password string
	}

	Uri struct {
		Streamed string
		Rpc      string
	}
}

type includeFile struct {
	name     string
	required bool
	conf     *viper.Viper
}

func includesFromConf(conf *viper.Viper) []includeFile {
	includes := make([]includeFile, 0)
	if conf.InConfig("include") {
		v, ok := conf.Get("include").([]interface{})
		if ok {
			for _, vv := range v {
				info := includeFile{}
				switch vvv := vv.(type) {
				case map[interface{}]interface{}:
					for k, vvvv := range vvv {
						ks := fmt.Sprint(k)
						isRequired, _ := vvvv.(bool)
						info.required = isRequired
						info.name = ks
					}
					log.Println(vvv)
				case string:
					info.name = vvv
					log.Println(vvv)
				}

				fileInfo, err := os.Stat(info.name)

				if !os.IsNotExist(err) {
					if fileInfo.IsDir() {
						if entries, err := ioutil.ReadDir(info.name); err == nil {
							for _, finfo := range entries {
								if !finfo.IsDir() && filepath.Ext(finfo.Name()) == ".conf" {
									vip := viper.New()
									vip.SetConfigFile(finfo.Name())
									vip.SetConfigType("yaml")
									vip.ReadInConfig()
									aaaa := includesFromConf(vip)
									for _, aaa := range aaaa {
										includes = append(includes, aaa)
									}
								}
							}
							//don't add dirs
							continue
						} else {
							info.conf = viper.New()
							info.conf.SetConfigFile(info.name)
						}
					}
				}

				includes = append(includes, info)
			}
		}
	}
	return includes
}

func x(a MethodArgs, b map[string]bool) {
	if !a.Static && !a.compound && a.Param != "self" {
		b[a.Param] = true
	}
	for _, v := range a.Child {
		x(v, b)
	}
}
func (w *MethodDef) Validate() error {
	definedParams := w.Params
	definedArgs := make(map[string]bool)
	for _, v := range w.Invoke.Args {
		x(v, definedArgs)
	}

	for k := range definedArgs {
		if _, has := definedParams[k]; has {
			delete(definedParams, k)
		} else {
			// Fatal, or just return error?
			return fmt.Errorf("Undeclared param '%v' in arguments", k)
		}
	}

	for k := range definedParams {
		log.Printf("Unused parameter '%v' defined\n", k)
	}
	return nil
}
func ParseConfig(configFilePath string) (*RupicolaConfig, error) {
	var cfg RupicolaConfig
	//log.Println(baseConfig.Unmarshal(&cfg))
	by, err := ioutil.ReadFile(configFilePath)
	//log.Println("----")
	log.Println(yaml.Unmarshal(by, &cfg))
	for _, v := range cfg.Methods {
		if err := v.Validate(); err != nil {
			return nil, err
		}
	}
	//log.Printf("%+v\n", cfg.Methods["upgrade"].Invoke.Args)
	return &cfg, err
}
