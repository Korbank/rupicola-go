package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	log "github.com/inconshreveable/log15"

	"crypto/subtle"
	"encoding/json"
	"path/filepath"

	"rupicolarpc"
	"gopkg.in/yaml.v2"
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
	Include  []includeConfig
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

// RunAs ...
type RunAs struct {
	UID *uint32
	GID *uint32
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
		Args  []methodArgs
		RunAs RunAs `yaml:",omitempty"`
	} `yaml:"invoke"`
	logger log.Logger
}

// MethodParamType ...
type MethodParamType int

// MethodEncoding ...
type MethodEncoding int

// BindType ...
type BindType int

const (
	// Utf8 - Default message encoding
	Utf8 MethodEncoding = 0
	// Base64 - Encode message as base64
	Base64 = 1
	// Base85 - Encode message as base85
	Base85 = 2
)
const (
	// String - Method parameter should be string
	String MethodParamType = 0
	// Int - Method parameter should be int
	Int = 1
	// Bool - Method parameter should be bool
	Bool = 2
)

type methodArgs struct {
	Param    string
	Skip     bool
	Static   bool
	compound bool
	Child    []methodArgs
}

type includeConfig struct {
	Required bool
	Name     string
}

const (
	// HTTP - HTTP transport over TCP
	HTTP BindType = 0
	// HTTPS - HTTPS transport over TCP
	HTTPS = 1
	// Unix - HTTP transport over unix socket
	Unix = 2
)

// Bind - describe listening address binding
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

// Protocol - define bind points, auth and URI paths
type Protocol struct {
	Bind []*Bind

	AuthBasic struct {
		Login    string
		Password string
	} `yaml:"auth-basic"`

	URI struct {
		Streamed string
		RPC      string
	}
}

// UnmarshalYAML ignore
func (w *includeConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
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
		*w = HTTP
	case "https":
		*w = HTTPS
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
func (m *methodArgs) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var plainText string
	var structure struct {
		Param string
		// Check what was it
		Skip bool
	}
	var array []methodArgs

	if unmarshal(&plainText) == nil {
		m.Param = plainText
		m.Static = true
		m.Skip = false
		return nil
	}
	var err error
	if err = unmarshal(&structure); err == nil {
		m.Param = structure.Param
		m.Skip = structure.Skip
		m.Static = false
		return nil
	}
	if err = unmarshal(&array); err == nil {
		m.Child = array
		m.compound = true
		return nil
	}
	log.Crit("err", "err", err)
	panic(err)
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

func (conf *RupicolaConfig) includesFromConf(info includeConfig) error {
	fileInfo, err := os.Stat(info.Name)

	if os.IsNotExist(err) {
		if info.Required {
			return err
		}
		log.Warn("Optional config not found", "path", info.Name)
		return nil
	}

	if fileInfo.IsDir() {
		if entries, err := ioutil.ReadDir(info.Name); err == nil {
			for _, finfo := range entries {
				if !finfo.IsDir() && filepath.Ext(finfo.Name()) == ".conf" {
					// first field doesn't matter - we are including files from directory so
					// they should exists
					if err := conf.includesFromConf(includeConfig{true, filepath.Join(info.Name, finfo.Name())}); err != nil {
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

		if konfig.Protocol.URI.RPC != "" {
			conf.Protocol.URI.RPC = konfig.Protocol.URI.RPC
		}
		if konfig.Protocol.URI.Streamed != "" {
			conf.Protocol.URI.Streamed = konfig.Protocol.URI.Streamed
		}

		// If anything is change in AuthBasic - replace current definition
		if konfig.Protocol.AuthBasic.Login != "" {
			conf.Protocol.AuthBasic = konfig.Protocol.AuthBasic
		}
	}
	return nil
}

func aggregateArgs(a methodArgs, b map[string]bool) {
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
			m.logger.Error("undeclared param", "name", k)
			return fmt.Errorf("Undeclared param '%v' in arguments", k)
		}
	}

	for k := range definedParams {
		m.logger.Warn("unused parameter defined", "name", k)
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

// NewConfig - create configuration with default values
func NewConfig() *RupicolaConfig {
	var cfg RupicolaConfig
	cfg.Protocol.URI.RPC = "/rpc"
	cfg.Protocol.URI.Streamed = "/streaming"
	cfg.Limits = Limits{10000, 0, 5242880, 5242880}
	return &cfg
}

// ParseConfig from file
func ParseConfig(configFilePath string) (*RupicolaConfig, error) {
	cfg := NewConfig()
	err := cfg.readConfig(configFilePath, true)

	if err != nil {
		return cfg, err
	}
	for k, v := range cfg.Methods {
		v.logger = log.New("method", k)
		v.logger.Debug("method info", "streamed", v.Streamed)
		if err := v.Validate(); err != nil {
			return nil, err
		}

		// If Gid or Uid is empty assign it from current process
		// We cant use 0 as empty (this is root on unix, and someone could set it)
		if v.InvokeInfo.RunAs.GID == nil {
			tmp := uint32(os.Getgid())
			v.InvokeInfo.RunAs.GID = &tmp
		}

		if v.InvokeInfo.RunAs.UID == nil {
			tmp := uint32(os.Getuid())
			v.InvokeInfo.RunAs.UID = &tmp
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
			m.logger.Error("invalid param")
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
			m.logger.Error("invalid param")
			return rupicolarpc.NewStandardError(rupicolarpc.InvalidParams)
		}
	}
	return nil
}

func (m *methodArgs) evalueateArgs(arguments map[string]interface{}, output *bytes.Buffer) (bool, error) {
	if m.Static {
		_, e := output.WriteString(m.Param)
		if e != nil {
			return false, e
		}
	} else {
		var value string
		valueRaw, has := arguments[m.Param]
		// Convert value to string
		value = fmt.Sprint(valueRaw)

		if m.Param == "self" {
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
		if has || m.compound {
			// We should skip expanding for markers only
			if !m.Skip {
				output.WriteString(value)
			}
			var skipCompound bool
			for _, arg := range m.Child {
				var skip bool
				var err error
				if skip, err = arg.evalueateArgs(arguments, output); err != nil {
					return skip, err
				}
				skipCompound = skipCompound || skip
			}
			return skipCompound, nil
		}

		// All arguments in param are filtered
		// so we are sure that we don't have "wild" param in args
		return m.Skip, nil
	}

	return false, nil
}
