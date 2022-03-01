package rupicola

import (
	"bytes"
	"fmt"
	"os"
	"reflect"

	"github.com/korbank/rupicola-go/config"

	"github.com/mkocot/pwhash"

	log "github.com/rs/zerolog"

	"crypto/subtle"
	"encoding/json"

	"github.com/korbank/rupicola-go/rupicolarpc"
)

var Logger = log.Nop()

// MethodDef ...
type MethodDef struct {
	// Streamed   bool
	// Private    bool
	// Encoding   config.MethodEncoding
	// Params     map[string]config.MethodParam
	// InvokeInfo config.InvokeInfoDef
	// // Pointer because we need to know when its unsed
	// Limits *config.MethodLimits
	// // unused parameter
	// Output interface{}
	config.RawMethodDef
	logger log.Logger
}

// Config ...
type Config struct {
	Protocol config.Protocol
	Limits   config.Limits
	Log      config.LogDef
	Methods  map[string]*MethodDef
}

// CheckParams ensures that all required paramters are present and have valid type
func (m *MethodDef) CheckParams(params map[string]interface{}) error {
	for name, arg := range m.Params {
		val, ok := params[name]
		if !ok && !arg.Optional {
			m.logger.Error().Str("param", name).Msg("missing param")
			return rupicolarpc.NewStandardError(rupicolarpc.InvalidParams)
		}
		// add missing optional arguments with defined default value
		if arg.Optional && !ok {
			// Initialize if required
			if arg.DefaultVal == nil {
				continue
			}
			val = arg.DefaultVal.Raw()
			if val == nil {
				val = arg.Type.DefaultValue()
			}
			params[name] = val
			// don't check default values
			continue
		}
		providedKind := reflect.TypeOf(val).Kind()
		m.logger.Debug().Str("from", providedKind.String()).Str("to", arg.Type.String()).Msg("arg conversion")
		switch arg.Type {
		case config.String:
			_, ok = val.(string)
		case config.Int:
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
		case config.Bool:
			_, ok = val.(bool)
		default:
			ok = false
		}
		if !ok {
			m.logger.Error().Str("requested", arg.Type.String()).Str("received", providedKind.String()).Msg("invalid param")
			return rupicolarpc.NewStandardError(rupicolarpc.InvalidParams)
		}
	}
	return nil
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

// NewConfig - create configuration with default values
func NewConfig() *Config {
	var cfg Config
	cfg.Protocol.URI.RPC = "/rpc"
	cfg.Protocol.URI.Streamed = "/streaming"
	cfg.Limits = config.Limits{10000, 0, 5242880, 5242880}
	return &cfg
}

// ReadConfig from file
func ReadConfig(configFilePath string) (*Config, error) {
	x := config.NewConfig()
	if err := x.Load(configFilePath); err != nil {
		return nil, err
	}

	c := NewConfig()
	c.Limits = x.Limits
	// limitsSection := x.Limits
	// c.Limits = Limits{
	// 	limitsSection.Get("read-timeout").Duration(10000) * time.Millisecond,
	// 	limitsSection.Get("exec-timeout").Duration(0) * time.Millisecond,
	// 	limitsSection.Get("payload-size").Uint32(5242880),
	// 	limitsSection.Get("max-response").Uint32(5242880),
	// }
	c.Protocol = x.Protocol
	// var err error
	// protocolSection := x.Protocol // x.Get("protocol")
	// c.Protocol.AuthBasic.Login = protocolSection.Get("auth-basic", "login").AsString("")
	// c.Protocol.AuthBasic.Password = protocolSection.Get("auth-basic", "password").AsString("")
	// c.Protocol.URI.RPC = protocolSection.Get("uri", "rpc").AsString("/jsonrpc")
	// c.Protocol.URI.Streamed = protocolSection.Get("uri", "streamed").AsString("/streaming")
	// for _, bind := range protocolSection.Get("bind").Array(nil) {
	// 	b := new(Bind)
	// 	b.Address = bind.Get("address").AsString("") // error on empty
	// 	b.AllowPrivate = bind.Get("allow_private").Bool(false)
	// 	b.Cert = bind.Get("cert").AsString("")
	// 	b.GID = int(bind.Get("gid").Int32(int32(os.Getgid())))
	// 	b.Key = bind.Get("key").AsString("")
	// 	b.Mode = os.FileMode(bind.Get("mode").Uint32(666)) // need love...
	// 	shamefullFileModeFix(&b.Mode)

	// 	b.Port = uint16(bind.Get("port").Int32(0))
	// 	b.Type, err = parseBindType(bind.Get("type").AsString(""))
	// 	b.UID = int(bind.Get("uid").Int32(int32(os.Getuid())))
	// 	c.Protocol.Bind = append(c.Protocol.Bind, b)
	// }

	c.Methods = make(map[string]*MethodDef)
	methodsSection := x.Methods                 //.Get("methods")
	for methodName, v := range methodsSection { //}.Map() {
		meth := MethodDef {
			RawMethodDef: v,
			logger: Logger.With().Str("method", methodName).Logger(),
		}
		//meth.logger = Logger.With().Str("method", methodName).Logger()
		//meth.Limits = &v.Limits
		//meth.Encoding = v.Encoding // parseEncoding(v.Get("encoding").AsString("utf8"))
		// meth.InvokeInfo.Delay = v.InvokeInfo.Delay
		// meth.InvokeInfo.Exec = v.InvokeInfo.Exec
		// meth.InvokeInfo.RunAs = v.InvokeInfo.RunAs
		//meth.InvokeInfo = v.InvokeInfo
		// meth.InvokeInfo.Delay = v.Get("invoke", "delay").Duration(0) * time.Second
		// meth.InvokeInfo.Exec = v.Get("invoke", "exec").AsString("")
		// meth.InvokeInfo.RunAs.GID = v.Get("invoke", "run-as", "gid").Uint32(uint32(os.Getegid()))
		// meth.InvokeInfo.RunAs.UID = v.Get("invoke", "run-as", "uid").Uint32(uint32(os.Getuid()))
		// args := v.InvokeInfo.Args // v.Get("invoke", "args").Array(nil)
		// if len(args) != 0 {
		// 	meth.InvokeInfo.Args = make([]methodArgs, len(args))
		// 	for i, a := range args {
		// 		meth.InvokeInfo.Args[i] = methodArgs{
		// 			Param:  a.Param,
		// 			Skip:   a.Skip,
		// 			Static: a.Static,
		// 			Child:  a.Child,
		// 		}
		// 	}
		// }

		//meth.Output = v.Output     //.Get("output")
		//meth.Private = v.Private   //.Get("private").Bool(false)
		//meth.Streamed = v.Streamed //.Get("streamed").Bool(false)
		//meth.Params = v.Params
		// methParams := v.Params//.Get("params").Map()
		// if len(methParams) != 0 {
		// 	meth.Params = make(map[string]config.MethodParam)
		// 	for paramName, v := range methParams {
		// 		// tyype, err := parseMethodParamType(v.Get("type").AsString("")) // required
		// 		tyype := v.Type
		// 		// if err != nil {
		// 		// 	meth.logger.Error().Str("name", "type").Msg("required field missing")
		// 		// }
		// 		optional := v.Optional// v.Get("optional").Bool(false)
		// 		defaultVal := v.DefaultVal//.Get("default")
		// 		meth.logger.Trace().Str("paraName", paramName).Send()
		// 		meth.Params[paramName] = MethodParam{Optional: optional, Type: tyype, defaultVal: defaultVal}
		// 	}
		// }
		meth.logger.Info().Str("details", fmt.Sprintf("%+v", meth)).Msg("add new method")
		c.Methods[methodName] = &meth
	}
	// logsSecrion := x.Log//.Get("log")
	c.Log = x.Log
	// c.Log.Backend = logsSec err = parseBackend(logsSecrion.Get("backend").AsString(""))
	// if err != nil {
	// 	return nil, err
	// }
	// c.Log.LogLevel, err = parseLoglevel(logsSecrion.Get("level").AsString("warn"))
	// if err != nil {
	// 	return nil, err
	// }
	// c.Log.Path = logsSecrion.Get("path").AsString("")
	return c, nil
}

func evalueateArgs(m *config.MethodArgs, arguments map[string]interface{}, output *bytes.Buffer) (bool, error) {
	if m.Static {
		_, e := output.WriteString(m.Param)
		if e != nil {
			return false, e
		}
		return false, nil
	}
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
	if has || m.Compound {
		// We should skip expanding for markers only
		if !m.Skip {
			output.WriteString(value)
		}
		var skipCompound bool
		for _, arg := range m.Child {
			var skip bool
			var err error
			if skip, err = evalueateArgs(&arg, arguments, output); err != nil {
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

// SetLogging to expected values
// THIS IS NOT THREAD SAFE
func (conf *Config) SetLogging() {
	var logLevel log.Level
	switch conf.Log.LogLevel {
	case config.LLError:
		logLevel = log.ErrorLevel
	case config.LLWarn:
		logLevel = log.WarnLevel
	case config.LLInfo:
		logLevel = log.InfoLevel
	case config.LLDebug:
		logLevel = log.DebugLevel
	case config.LLOff:
		logLevel = log.Disabled
	default:
		panic("unknown log level")
	}

	log.SetGlobalLevel(logLevel)
	newLogger := Logger.Level(logLevel)

	switch conf.Log.Backend {
	case config.BackendKeep:
		// NoOp
		break
	case config.BackendStderr:
		newLogger = newLogger.Output(os.Stderr)
	case config.BackendStdout:
		newLogger = newLogger.Output(os.Stdout)
	case config.BackendSyslog:
		// newLogger = newLogger.Output(log.SyslogLevelWriter(w log.SyslogWriter))
		w, err := configureSyslog(conf.Log.Path)
		if err != nil {
			Logger.Error().Err(err).Msg("Syslog connection failed")
			os.Exit(1)
		}
		newLogger = newLogger.Output(log.SyslogLevelWriter(w))
	}
	Logger = newLogger
	rupicolarpc.Logger = Logger
}
