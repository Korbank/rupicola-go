package rupicola

import (
	"fmt"
	"io"
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
	config.RawMethodDef
	logger log.Logger
}

// Config ...
type Config struct {
	config.Config
	Methods map[string]*MethodDef
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
			val = arg.DefaultVal
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
		case config.Int, config.Number:
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

func (conf Config) isValidAuth(login string, password string) bool {
	if conf.Protocol.AuthBasic.Login != "" {
		// NOTE: Verify method is not time constant!
		passOk, _ := pwhash.Verify(password, conf.Protocol.AuthBasic.Password)
		loginOk := subtle.ConstantTimeCompare([]byte(login), []byte(conf.Protocol.AuthBasic.Login)) == 1
		return passOk && loginOk
	}
	return true
}

// ReadConfig from file
func ReadConfig(configFilePath string) (Config, error) {
	c := Config{
		Config: config.NewConfig(),
	}
	if err := c.Load(configFilePath); err != nil {
		return c, err
	}

	c.Methods = make(map[string]*MethodDef)
	for methodName := range c.Config.Methods {
		meth := MethodDef{
			RawMethodDef: c.Config.Methods[methodName],
			logger:       Logger.With().Str("method", methodName).Logger(),
		}
		meth.logger.Info().Str("details", fmt.Sprintf("%+v", meth)).Msg("add new method")
		c.Methods[methodName] = &meth
	}
	return c, nil
}

func evalueateArgs(m config.MethodArgs, arguments map[string]interface{}, output io.StringWriter) (bool, error) {
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
		value = ""
		if arguments != nil {
			var bytes []byte
			var err error
			if bytes, err = json.Marshal(arguments); err != nil {
				return false, rupicolarpc.NewStandardErrorData(rupicolarpc.InternalError, "self")
			}
			value = string(bytes)
		}
	}
	if has || m.Compound {
		// We should skip expanding for markers only
		if !m.Skip {
			if _, err := output.WriteString(value); err != nil {
				return false, err
			}
		}
		var skipCompound bool
		for _, arg := range m.Child {
			var skip bool
			var err error
			if skip, err = evalueateArgs(arg, arguments, output); err != nil {
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
func (conf Config) SetLogging() {
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
	case config.LLUndefined:
	default:
		panic("unknown log level")
	}

	log.SetGlobalLevel(logLevel)
	newLogger := Logger.Level(logLevel)

	switch conf.Log.Backend {
	case config.BackendKeep, config.BackendUndefined:
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
