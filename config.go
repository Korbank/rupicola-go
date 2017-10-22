package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"

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
	Encoding string
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

func (w *MethodParam) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var tempStruct struct {
		Type     string
		Optional bool
	}
	err := unmarshal(&tempStruct)
	if err != nil {
		return err
	}

	switch tempStruct.Type {
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

func Fuu() RupicolaConfig {

	configFilePath := "/home/nfinity/git/rupicola/examples/go.yaml"
	baseConfig := viper.New()

	baseConfig.SetConfigFile(configFilePath)
	log.Println(baseConfig.ReadInConfig())

	for _, inc := range includesFromConf(baseConfig) {
		log.Println(inc)
		//baseConfig.MergeInConfig()
	}
	var cfg RupicolaConfig
	//log.Println(baseConfig.Unmarshal(&cfg))
	by, _ := ioutil.ReadFile(configFilePath)
	//log.Println("----")
	log.Println(yaml.Unmarshal(by, &cfg))
	//log.Printf("%+v\n", cfg.Methods["upgrade"].Invoke.Args)
	return cfg
}
