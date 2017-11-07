package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"

	"./rupicolarpc"
	"crypto/subtle"
	"encoding/json"
	"gopkg.in/yaml.v2"
	"path/filepath"
)

// Limits ...
type Limits struct {
	ReadTimeout time.Duration `yaml:"read-timeout"`
	ExecTimeout time.Duration `yaml:"exec-timeout"`
	PayloadSize uint32        `yaml:"payload-size"`
	MaxResponse uint32        `yaml:"max-response"`
}

// RupicolaConfig ...
type RupicolaConfig struct {
	Include  []IncludeConfig
	Protocol Protocol
	Limits   Limits
	Log      struct {
		Level string
	}
	Methods map[string]*MethodDef
}

// MethodParam ...
type MethodParam struct {
	Type     MethodParamType
	Optional bool
}

type RunAs struct {
	Uid *uint32
	Gid *uint32
}

// MethodDef ...
type MethodDef struct {
	Streamed   bool
	Private    bool
	Encoding   MethodEncoding
	Params     map[string]MethodParam
	InvokeInfo struct {
		Exec  string
		Delay time.Duration
		Args  []MethodArgs
		RunAs RunAs `yaml:",omitempty"`
	} `yaml:"invoke"`
}

// MethodParamType ...
type MethodParamType int

// MethodEncoding ...
type MethodEncoding int
type BindType int

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

type IncludeConfig struct {
	Required bool
	Name     string
}

const (
	Http  BindType = 0
	Https          = 1
	Unix           = 2
)

type Bind struct {
	Type         BindType
	Address      string
	Port         uint16
	AllowPrivate bool `yaml:"allow_private"`
	// Only for HTTPS
	Cert string
	// Only for HTTPS
	Key string
	// Only for Unix [default=660]
	Mode uint32
}
type Protocol struct {
	Bind []*Bind

	AuthBasic struct {
		Login    string
		Password string
	} `yaml:"auth-basic"`

	Uri struct {
		Streamed string
		Rpc      string
	}
}

// UnmarshalYAML ignore
func (w *IncludeConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var mapType map[string]bool
	var stringType string

	if err := unmarshal(&mapType); err == nil {
		if len(mapType) != 1 {
			return errors.New("Invalid include definition")
		}
		for k, v := range mapType {
			w.Name = k
			w.Required = v
		}
		return nil
	}

	if err := unmarshal(&stringType); err != nil {
		return err
	}
	w.Required = true
	w.Name = stringType
	return nil
}

// UnmarshalYAML ignore
func (w *BindType) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var bindType string
	if err := unmarshal(&bindType); err != nil {
		return err
	}
	switch strings.ToLower(bindType) {
	case "http":
		*w = Http
	case "https":
		*w = Https
	case "unix":
		*w = Unix
	default:
		return fmt.Errorf("Unknown bind type %v", bindType)
	}
	return nil
}

// UnmarshalYAML ignore
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

// UnmarshalYAML ignore
func (w *MethodParamType) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var value string

	if err := unmarshal(&value); err != nil {
		return err
	}

	switch strings.ToLower(value) {
	case "string":
		*w = String
	case "integer":
	case "int":
		*w = Int
	case "bool":
	case "boolean":
		*w = Bool
	default:
		return errors.New("Unknown type")
	}

	return nil
}

// UnmarshalYAML ignore
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

func (conf *RupicolaConfig) isValidAuth(login string, password string) bool {
	if conf.Protocol.AuthBasic.Login != "" {
		// NOTE: Verify method is not time constant!
		passOk, _ := pwVerify(password, conf.Protocol.AuthBasic.Password)
		loginOk := subtle.ConstantTimeCompare([]byte(login), []byte(conf.Protocol.AuthBasic.Login)) == 1
		return passOk && loginOk
	}
	return true
}

func (conf *RupicolaConfig) includesFromConf(info IncludeConfig) error {
	fileInfo, err := os.Stat(info.Name)

	if os.IsNotExist(err) {
		if info.Required {
			return err
		}
		log.Printf("Optional config path \"%v\" not found\n", info.Name)
		return nil
	}

	if fileInfo.IsDir() {
		if entries, err := ioutil.ReadDir(info.Name); err == nil {
			for _, finfo := range entries {
				if !finfo.IsDir() && filepath.Ext(finfo.Name()) == ".conf" {
					// first field doesn't matter - we are including files from directory so
					// they should exists
					if err := conf.includesFromConf(IncludeConfig{true, filepath.Join(info.Name, finfo.Name())}); err != nil {
						return err
					}
				}
			}
		} else {
			return err
		}
	} else {
		konfig := new(RupicolaConfig)
		err := konfig.readConfig(info.Name, true)
		if err != nil {
			return err
		}

		if konfig.Methods != nil && conf.Methods == nil {
			conf.Methods = make(map[string]*MethodDef)
		}

		for methodName, methodDef := range konfig.Methods {
			conf.Methods[methodName] = methodDef
		}

		conf.Protocol.Bind = append(conf.Protocol.Bind, konfig.Protocol.Bind...)

		if konfig.Protocol.Uri.Rpc != "" {
			conf.Protocol.Uri.Rpc = konfig.Protocol.Uri.Rpc
		}
		if konfig.Protocol.Uri.Streamed != "" {
			conf.Protocol.Uri.Streamed = konfig.Protocol.Uri.Streamed
		}

		// If anything is change in AuthBasic - replace current definition
		if konfig.Protocol.AuthBasic.Login != "" {
			conf.Protocol.AuthBasic = konfig.Protocol.AuthBasic
		}
	}
	return nil
}

func aggregateArgs(a MethodArgs, b map[string]bool) {
	if !a.Static && !a.compound && a.Param != "self" {
		b[a.Param] = true
	}
	for _, v := range a.Child {
		aggregateArgs(v, b)
	}
}

// Validate ensure correct method definition
func (m *MethodDef) Validate() error {
	definedParams := m.Params
	definedArgs := make(map[string]bool)
	for _, v := range m.InvokeInfo.Args {
		aggregateArgs(v, definedArgs)
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

func (conf *RupicolaConfig) readConfig(configFilePath string, recursive bool) error {
	by, err := ioutil.ReadFile(configFilePath)
	if err := yaml.Unmarshal(by, conf); err != nil {
		return err
	}

	if recursive {
		for _, inc := range conf.Include {
			if err := conf.includesFromConf(inc); err != nil {
				return err
			}
		}
	}

	return err
}

func NewConfig() *RupicolaConfig {
	var cfg RupicolaConfig
	cfg.Protocol.Uri.Rpc = "/rpc"
	cfg.Protocol.Uri.Streamed = "/streaming"
	cfg.Limits = Limits{10000, 0, 5242880, 5242880}
	return &cfg
}

func ParseConfig(configFilePath string) (*RupicolaConfig, error) {
	cfg := NewConfig()
	err := cfg.readConfig(configFilePath, true)

	if err != nil {
		return cfg, err
	}
	for k, v := range cfg.Methods {
		log.Printf("[%s]: Streamed -> %v\n", k, v.Streamed)
		if err := v.Validate(); err != nil {
			return nil, err
		}

		// If Gid or Uid is empty assign it from current process
		if v.InvokeInfo.RunAs.Gid == nil {
			tmp := uint32(os.Getgid())
			v.InvokeInfo.RunAs.Gid = &tmp
		}

		if v.InvokeInfo.RunAs.Uid == nil {
			tmp := uint32(os.Getuid())
			v.InvokeInfo.RunAs.Uid = &tmp
		}

		v.InvokeInfo.Delay *= time.Second
	}
	cfg.Limits.ExecTimeout *= time.Millisecond
	cfg.Limits.ReadTimeout *= time.Millisecond
	return cfg, err
}

// CheckParams ensures that all required paramters are present and have valid type
func (m *MethodDef) CheckParams(req *rupicolarpc.JsonRpcRequest) error {
	// Check if required arguments are present
	for name, arg := range m.Params {
		val, ok := req.Params[name]
		if !ok && !arg.Optional {
			log.Println("invalid param")
			return rupicolarpc.NewStandardError(rupicolarpc.InvalidParams)
		}

		switch arg.Type {
		case String:
			_, ok = val.(string)
		case Int:
			_, ok = val.(int)
		case Bool:
			_, ok = val.(bool)
		default:
			ok = false
		}
		if !ok {
			log.Println("invalid param")
			return rupicolarpc.NewStandardError(rupicolarpc.InvalidParams)
		}
	}
	return nil
}
func (arg *MethodArgs) _evalueateArgs(arguments map[string]interface{}, output *bytes.Buffer) (bool, error) {
	if arg.Static {
		_, e := output.WriteString(arg.Param)
		if e != nil {
			return false, e
		}
	} else {
		var value string
		valueRaw, has := arguments[arg.Param]
		// Convert value to string
		value = fmt.Sprint(valueRaw)

		if arg.compound != (len(arg.Child) != 0) {
			log.Panicln("Oooh..")
		}
		if arg.Param == "self" {
			has = true
			if arguments == nil {
				value = ""
			} else {
				bytes, ok := json.Marshal(arguments)
				if ok == nil {
					value = string(bytes)
				} else {
					return false, rupicolarpc.NewStandardErrorData(rupicolarpc.InternalError, "self")
				}
			}
		}
		if has || arg.compound {
			// We should skip expanding for markers only
			if !arg.Skip {
				output.WriteString(value)
			}
			var skipCompound bool
			for _, arg := range arg.Child {
				var skip bool
				var err error
				if skip, err = arg._evalueateArgs(arguments, output); err != nil {
					return skip, err
				}
				skipCompound = skipCompound || skip
			}
			return skipCompound, nil
		}

		// All arguments in param are filtered
		// so we are sure that we don't have "wild" param in args
		return arg.Skip, nil
	}

	return false, nil
}
