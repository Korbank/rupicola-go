package rupicola

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/korbank/rupicola-go/config"

	"github.com/mkocot/pwhash"

	log "github.com/inconshreveable/log15"

	"crypto/subtle"
	"encoding/json"

	"github.com/korbank/rupicola-go/rupicolarpc"
)

// Limits ...
type Limits struct {
	ReadTimeout time.Duration
	ExecTimeout time.Duration
	PayloadSize uint32
	MaxResponse uint32
}

type methodLimits struct {
	ExecTimeout time.Duration
	MaxResponse int64
}

// MethodLimits define execution limits for method
type MethodLimits methodLimits

// LogLevel describe logging level
type LogLevel int8

// Backend define log backend
type Backend int8

const (
	// BackendStdout write to stdout
	BackendStdout Backend = 1 << iota
	// BackendSyslog write to syslog
	BackendSyslog Backend = 1 << iota
)

const (
	// LLOff Disable log
	LLOff LogLevel = iota
	// LLDebug most detailed log level (same as Trace)
	LLDebug
	// LLInfo only info and above
	LLInfo
	// LLWarn only warning or errors
	LLWarn
	// LLError only errors
	LLError
)

// LogDef holds logging definitions
type LogDef struct {
	Backend
	LogLevel
	Path string
}

// Config ...
type Config struct {
	Protocol Protocol
	Limits   Limits
	Log      LogDef
	Methods  map[string]*MethodDef
}

// MethodParam ...
type MethodParam struct {
	Type       MethodParamType
	Optional   bool
	defaultVal config.Value
}

// RunAs ...
type RunAs struct {
	UID uint32
	GID uint32
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
		RunAs RunAs
	}
	// Pointer because we need to know when its unsed
	Limits *MethodLimits
	logger log.Logger
	// unused parameter
	Output interface{}
}

// MethodEncoding ...
type MethodEncoding int

// BindType ...
type BindType int

const (
	// Utf8 - Default message encoding
	Utf8 MethodEncoding = iota
	// Base64 - Encode message as base64
	Base64
	// Base85 - Encode message as base85
	Base85
)

// MethodParamType ...
type MethodParamType int

const (
	// String - Method parameter should be string
	String MethodParamType = iota
	// Int - Method parameter should be int (not float)
	Int
	// Bool - Method parameter should be bool
	Bool
	//Number - Any number (for now this is alias)
	Number
)

func (mpt MethodParamType) String() string {
	switch mpt {
	case String:
		return "string"
	case Int:
		return "int"
	case Bool:
		return "bool"
	default:
		return fmt.Sprintf("unknown(%d)", mpt)
	}
}

type methodArgs struct {
	Param    string
	Skip     bool
	Static   bool
	compound bool
	Child    []methodArgs
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
	AllowPrivate bool
	// Only for HTTPS
	Cert string
	// Only for HTTPS
	Key string
	// Only for Unix [default=660]
	Mode os.FileMode
	UID  int
	GID  int
}

// Protocol - define bind points, auth and URI paths
type Protocol struct {
	Bind []*Bind

	AuthBasic struct {
		Login    string
		Password string
	}

	URI struct {
		Streamed string
		RPC      string
	}
}

func parseBackend(backend string) (Backend, error) {
	switch strings.ToLower(backend) {
	case "syslog":
		return BackendSyslog, nil
	case "stdout":
		fallthrough
	case "":
		return BackendStdout, nil
	default:
		return BackendStdout, fmt.Errorf("Unknown backend %s", backend)
	}
}
func parseLoglevel(level string) (LogLevel, error) {
	switch strings.ToLower(level) {
	case "off":
		return LLOff, nil
	case "trace":
		return LLDebug, nil
	case "debug":
		return LLDebug, nil
	case "info":
		return LLInfo, nil
	case "warn":
		return LLWarn, nil
	case "error":
		return LLError, nil
	default:
		return LLOff, fmt.Errorf("unknown log level: %s", level)
	}
}
func parseEncoding(value string) (MethodEncoding, error) {
	value = strings.ToLower(value)
	switch value {
	case "base64":
		return Base64, nil
	case "utf-8", "utf8":
		return Utf8, nil
	default:
		return Utf8, errors.New("Unknown output type")
	}
}

func parseBindType(bindType string) (BindType, error) {
	switch strings.ToLower(bindType) {
	case "http":
		return HTTP, nil
	case "https":
		return HTTPS, nil
	case "unix":
		return Unix, nil
	default:
		return HTTP, fmt.Errorf("Unknown bind type %v", bindType)
	}
}

func parseMethodParamType(value string) (MethodParamType, error) {
	switch strings.ToLower(value) {
	case "string":
		return String, nil
	case "integer", "int", "number":
		return Int, nil
	case "bool", "boolean":
		return Bool, nil
	default:
		return String, errors.New("Unknown type")
	}
}

func fromVal(value config.Value) (methodArgs, error) {
	var out methodArgs

	var err error
	if value.IsMap() {
		out.Param = value.Get("param").AsString("")
		out.Skip = value.Get("skip").Bool(false)
		out.Static = false
	} else if value.IsArray() {
		asArray := value.Array(nil)
		out.Child = make([]methodArgs, len(asArray))
		for i, m := range asArray {
			out.Child[i], err = fromVal(m)
		}
		out.compound = true
	} else {
		out.Param = fmt.Sprint(value.Raw())
		out.Static = true
		out.Skip = false
	}
	return out, err
}

func (conf *Config) isValidAuth(login string, password string) bool {
	if conf.Protocol.AuthBasic.Login != "" {
		// NOTE: Verify method is not time constant!
		passOk, _ := pwhash.Verify(password, conf.Protocol.AuthBasic.Password)
		loginOk := subtle.ConstantTimeCompare([]byte(login), []byte(conf.Protocol.AuthBasic.Login)) == 1
		return passOk && loginOk
	}
	return true
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
	definedParams := make(map[string]bool)
	definedArgs := make(map[string]bool)
	for _, v := range m.InvokeInfo.Args {
		aggregateArgs(v, definedArgs)
	}

	for k := range m.Params {
		definedParams[k] = true
	}

	for k := range definedArgs {
		if _, has := definedParams[k]; has {
			definedParams[k] = false
		} else {
			// Fatal, or just return error?
			m.logger.Error("undeclared param", "name", k)
			return fmt.Errorf("Undeclared param '%v' in arguments", k)
		}
	}

	for k, v := range definedParams {
		if v {
			m.logger.Warn("unused parameter defined", "name", k)
		}
	}
	return nil
}

// NewConfig - create configuration with default values
func NewConfig() *Config {
	var cfg Config
	cfg.Protocol.URI.RPC = "/rpc"
	cfg.Protocol.URI.Streamed = "/streaming"
	cfg.Limits = Limits{10000, 0, 5242880, 5242880}
	return &cfg
}

func shamefullFileModeFix(inout *os.FileMode) error {
	// Well yeah, this is ugly bug originating
	// from first version it uses DEC values insted OCT (no 0 prefix)
	// so we need to convert it...

	// 9 bits - from 0 to 0777
	mode, err := strconv.ParseUint(strconv.FormatUint(uint64(*inout), 10), 8, 9)
	if err != nil {
		return err
	}
	*inout = os.FileMode(mode)
	return nil
}

// ReadConfig from file
func ReadConfig(configFilePath string) (*Config, error) {
	x := config.NewConfig()
	if err := x.Load(configFilePath); err != nil {
		return nil, err
	}

	c := NewConfig()
	limitsSection := x.Get("limits")
	c.Limits = Limits{
		limitsSection.Get("read-timeout").Duration(10000) * time.Millisecond,
		limitsSection.Get("exec-timeout").Duration(0) * time.Millisecond,
		limitsSection.Get("payload-size").Uint32(5242880),
		limitsSection.Get("max-response").Uint32(5242880),
	}
	var err error
	protocolSection := x.Get("protocol")
	c.Protocol.AuthBasic.Login = protocolSection.Get("auth-basic", "login").AsString("")
	c.Protocol.AuthBasic.Password = protocolSection.Get("auth-basic", "password").AsString("")
	c.Protocol.URI.RPC = protocolSection.Get("uri", "rpc").AsString("/jsonrpc")
	c.Protocol.URI.Streamed = protocolSection.Get("uri", "streamed").AsString("/streaming")
	for _, bind := range protocolSection.Get("bind").Array(nil) {
		b := new(Bind)
		b.Address = bind.Get("address").AsString("") // error on empty
		b.AllowPrivate = bind.Get("allow_private").Bool(false)
		b.Cert = bind.Get("cert").AsString("")
		b.GID = int(bind.Get("gid").Int32(int32(os.Getgid())))
		b.Key = bind.Get("key").AsString("")
		b.Mode = os.FileMode(bind.Get("mode").Uint32(666)) // need love...
		shamefullFileModeFix(&b.Mode)

		b.Port = uint16(bind.Get("port").Int32(0))
		b.Type, err = parseBindType(bind.Get("type").AsString(""))
		b.UID = int(bind.Get("uid").Int32(int32(os.Getuid())))
		c.Protocol.Bind = append(c.Protocol.Bind, b)
	}

	c.Methods = make(map[string]*MethodDef)
	methodsSection := x.Get("methods")
	for methodName, v := range methodsSection.Map() {
		meth := new(MethodDef)
		meth.logger = log.New("method", methodName)
		meth.Limits = new(MethodLimits)
		meth.Limits.ExecTimeout = v.Get("limits", "exec-timeout").Duration(-1) * time.Millisecond
		meth.Limits.MaxResponse = v.Get("limits", "max-response").Int64(-1)
		meth.Encoding, err = parseEncoding(v.Get("encoding").AsString("utf8"))
		meth.InvokeInfo.Delay = v.Get("invoke", "delay").Duration(0) * time.Second
		meth.InvokeInfo.Exec = v.Get("invoke", "exec").AsString("")
		meth.InvokeInfo.RunAs.GID = v.Get("invoke", "run-as", "gid").Uint32(uint32(os.Getegid()))
		meth.InvokeInfo.RunAs.UID = v.Get("invoke", "run-as", "uid").Uint32(uint32(os.Getuid()))
		args := v.Get("invoke", "args").Array(nil)
		if len(args) != 0 {
			meth.InvokeInfo.Args = make([]methodArgs, len(args))
			for i, a := range args {
				meth.InvokeInfo.Args[i], err = fromVal(a)
			}
		}

		meth.Output = v.Get("output")
		meth.Private = v.Get("private").Bool(false)
		meth.Streamed = v.Get("streamed").Bool(false)
		methParams := v.Get("params").Map()
		if len(methParams) != 0 {
			meth.Params = make(map[string]MethodParam)
			for paramName, v := range methParams {
				tyype, err := parseMethodParamType(v.Get("type").AsString("")) // required
				if err != nil {
					meth.logger.Error("required field missing", "name", "type")
				}
				optional := v.Get("optional").Bool(false)
				defaultVal := v.Get("default")
				meth.Params[paramName] = MethodParam{Optional: optional, Type: tyype, defaultVal: defaultVal}
			}
		}
		if err = meth.Validate(); err != nil {
			return nil, err
		}
		log.Info("add new method", "name", methodName, "details", meth)
		c.Methods[methodName] = meth
	}
	logsSecrion := x.Get("log")
	c.Log.Backend, err = parseBackend(logsSecrion.Get("backend").AsString(""))
	if err != nil {
		return nil, err
	}
	c.Log.LogLevel, err = parseLoglevel(logsSecrion.Get("level").AsString("warn"))
	if err != nil {
		return nil, err
	}
	c.Log.Path = logsSecrion.Get("path").AsString("")
	return c, err
}

func (t MethodParamType) defaultValue() interface{} {
	switch t {
	case String:
		return ""
	case Number:
		fallthrough
	case Int:
		return 0
	case Bool:
		return false
	default:
		panic("Sloppy programmer")
	}
}

// CheckParams ensures that all required paramters are present and have valid type
func (m *MethodDef) CheckParams(params map[string]interface{}) error {
	for name, arg := range m.Params {
		val, ok := params[name]
		if !ok && !arg.Optional {
			m.logger.Error("invalid param")
			return rupicolarpc.NewStandardError(rupicolarpc.InvalidParams)
		}
		// add missing optional arguments with defined default value
		if arg.Optional && !ok {
			// Initialize if required
			val = arg.defaultVal.Raw()
			if val == nil {
				val = arg.Type.defaultValue()
			}
			params[name] = val
			// don't check default values
			return nil
		}
		providedKind := reflect.TypeOf(val).Kind()
		m.logger.Debug("arg conversion", "from", providedKind, "to", arg.Type)
		switch arg.Type {
		case String:
			_, ok = val.(string)
		case Int:
			// any numerical is ok
			switch providedKind {
			case reflect.Float32:
				fallthrough
			case reflect.Float64:
				fallthrough
			case reflect.Int:
				fallthrough
			case reflect.Int8:
				fallthrough
			case reflect.Int16:
				fallthrough
			case reflect.Int32:
				fallthrough
			case reflect.Int64:
				fallthrough
			case reflect.Uint:
				fallthrough
			case reflect.Uint8:
				fallthrough
			case reflect.Uint16:
				fallthrough
			case reflect.Uint32:
				fallthrough
			case reflect.Uint64:
				ok = true
			default:
				ok = false
			}
		case Bool:
			_, ok = val.(bool)
		default:
			ok = false
		}
		if !ok {
			m.logger.Error("invalid param", "requested", arg.Type, "received", providedKind)
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
		// Convert value to string and skip converting nil to <nil>
		if valueRaw != nil {
			value = fmt.Sprint(valueRaw)
		}

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

// SetLogging to expected values
func (conf *Config) SetLogging() {
	var logLevel log.Lvl
	switch conf.Log.LogLevel {
	case LLError:
		logLevel = log.LvlError
	case LLWarn:
		logLevel = log.LvlWarn
	case LLInfo:
		logLevel = log.LvlInfo
	case LLDebug:
		logLevel = log.LvlDebug
	case LLOff:
		logLevel = -1
	}
	var handler log.Handler
	switch conf.Log.Backend {
	case BackendStdout:
		handler = log.StdoutHandler
	case BackendSyslog:
		h, err := configureSyslog(conf.Log.Path)
		if err != nil {
			log.Error("Syslog connection failed", "err", err)
			os.Exit(1)
		}
		handler = h
	}
	log.Root().SetHandler(log.LvlFilterHandler(logLevel, handler))
}
