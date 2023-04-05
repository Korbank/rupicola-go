package config

import (
	"container/list"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"text/template/parse"
	"time"

	log "github.com/rs/zerolog"
	"gopkg.in/yaml.v2"
)

type config = Config

var (
	ErrInvalidConfig     = errors.New("invalid config")
	ErrInvalidDefinition = fmt.Errorf("%w: invalid method definition", ErrInvalidConfig)
)

// NewConfig returns empty configuration.
func NewConfig() Config {
	return config{
		Limits: DefaultLimits(),
		Protocol: Protocol{
			URI: struct {
				Streamed string
				RPC      string
			}{
				Streamed: "/streaming",
				RPC:      "/jsonrpc",
			},
		},
	}
}

func searchFiles(path string, required bool) ([]string, error) {
	fileInfo, err := os.Stat(path)
	if os.IsNotExist(err) {
		if required {
			return nil, err
		}

		logger.Warn().Str("path", path).Msg("Optional config not found")

		return nil, nil
	}

	out := make([]string, 0)

	if !fileInfo.IsDir() {
		return []string{path}, nil
	}

	entries, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, err
	}

	for _, finfo := range entries {
		if !finfo.IsDir() && filepath.Ext(finfo.Name()) == ".conf" {
			// first field doesn't matter - we are including files from directory so
			// they should exists
			out = append(out, filepath.Join(path, finfo.Name()))
		}
	}

	return out, nil
}

// BindType ...
type BindType int

const (
	BindTypeUnknown BindType = iota
	// HTTP - HTTP transport over TCP.
	HTTP
	// HTTPS - HTTPS transport over TCP.
	HTTPS
	// Unix - HTTP transport over unix socket.
	Unix
)

// LogLevel describe logging level.
type LogLevel int8

const (
	LLUndefined LogLevel = iota
	// LLOff Disable log.
	LLOff
	// LLDebug most detailed log level (same as Trace).
	LLDebug
	// LLInfo only info and above.
	LLInfo
	// LLWarn only warning or errors.
	LLWarn
	// LLError only errors.
	LLError
)

// LogDef holds logging definitions.
type LogDef struct {
	Backend
	LogLevel `yaml:"level"`
	Path     string
}

// Backend define log backend.
type Backend int8

const (
	// BackendStdout write to stdout.
	BackendStdout Backend = 1 << iota
	// BackendSyslog write to syslog.
	BackendSyslog Backend = 1 << iota
	// BackendStderr write to stderr (default).
	BackendStderr Backend = 1 << iota
	// BackendKeep keep current output.
	BackendKeep Backend = 1 << iota
	// BackendUndefined is used when no value is defined in config.
	BackendUndefined Backend = 0
)

func parseBackend(backend string) (Backend, error) {
	switch strings.ToLower(backend) {
	case "syslog":
		return BackendSyslog, nil
	case "stdout":
		return BackendStdout, nil
	case "stderr":
		fallthrough
	case "":
		return BackendStderr, nil
	default:
		return BackendStdout, fmt.Errorf("%w: unknown backend %s", ErrInvalidConfig, backend)
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
		return LLOff, fmt.Errorf("%w: unknown log level: %s", ErrInvalidConfig, level)
	}
}

// MethodEncoding ...
type MethodEncoding int

const (
	// Utf8 - Default message encoding.
	Utf8 MethodEncoding = iota
	// Base64 - Encode message as base64 (STD).
	Base64
	// Base64Url - Encode message as base64 (URL).
	Base64Url
	// Base85 - Encode message as base85.
	Base85
)

// MethodParamType ...
type MethodParamType int

const (
	// String - Method parameter should be string.
	String MethodParamType = iota
	// Int - Method parameter should be int (not float).
	Int
	// Bool - Method parameter should be bool.
	Bool
	// Number - Any number (for now this is alias).
	Number
)

func (mpt MethodParamType) String() string {
	switch mpt {
	case String:
		return "string"
	case Int, Number:
		return "int"
	case Bool:
		return "bool"
	default:
		return fmt.Sprintf("unknown(%d)", mpt)
	}
}

func (mpt MethodParamType) DefaultValue() interface{} {
	switch mpt {
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

func parseEncoding(value string) (MethodEncoding, error) {
	value = strings.ToLower(value)
	switch value {
	case "base64":
		return Base64, nil
	case "base64-url":
		return Base64Url, nil
	case "utf-8", "utf8":
		return Utf8, nil
	case "base85", "ascii85":
		return Base85, nil
	default:
		return Utf8, fmt.Errorf("%w: unknown encoding: %s", ErrInvalidConfig, value)
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
		return HTTP, fmt.Errorf("%w: unknown bind type %v", ErrInvalidConfig, bindType)
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
		return String, fmt.Errorf("%w: unknown type", ErrInvalidConfig)
	}
}

type FileMode os.FileMode

// Bind - describe listening address binding.
type Bind struct {
	Type         BindType
	Address      string
	Port         uint16
	AllowPrivate bool `yaml:"allow_private"`
	// Only for Unix [default=660]
	Mode FileMode
	UID  int
	GID  int
	// Only for HTTPS
	Cert string
	// Only for HTTPS
	Key string
}

// Protocol - define bind points, auth and URI paths.
type Protocol struct {
	Bind []Bind

	AuthBasic *struct {
		Login    string
		Password string
	} `yaml:"auth-basic"`

	URI struct {
		Streamed string
		RPC      string
	}
}

type Limits struct {
	ReadTimeout time.Duration `yaml:"read-timeout"`
	ExecTimeout time.Duration `yaml:"exec-timeout"`
	PayloadSize int64         `yaml:"payload-size"`
	MaxResponse int64         `yaml:"max-response"`
}

func DefaultLimits() Limits {
	return Limits{
		10000 * time.Millisecond,
		time.Duration(0),
		5242880,
		5242880,
	}
}

type rawInclude struct {
	Path     string
	Required bool
}

// MethodParam ...
type MethodParam struct {
	Type       MethodParamType
	Optional   bool
	DefaultVal interface{} `yaml:"default"`
}

// RunAs ...
type RunAs struct {
	UID int
	GID int
}

type ExecType int

const (
	ExecTypeDefault      ExecType = iota
	ExecTypeShellWrapper          = iota
)

var logger = log.Nop()

func SetTemporaryLog(log log.Logger) {
	logger = log
}

func parseExecType(val string) (ExecType, error) {
	switch strings.ToLower(val) {
	case "", "default":
		return ExecTypeDefault, nil
	case "shell_wrapper":
		return ExecTypeShellWrapper, nil
	}

	return 0, fmt.Errorf("%w: invalid exec type: %s", ErrInvalidConfig, val)
}

type Exec struct {
	Mode ExecType `yaml:"type"`
	Path string
}

func (e Exec) String() string {
	return fmt.Sprintf("Exec{Mode:%v Path:%s}", e.Mode, e.Path)
}

var _ fmt.Stringer = (*Exec)(nil)

type InvokeInfoDef struct {
	Exec  Exec
	Delay time.Duration
	Args  []MethodArgs
	RunAs RunAs `yaml:"run-as"`
}

// MethodDef ...
type RawMethodDef struct {
	AllowStreamed bool `yaml:"streamed"`
	AllowRPC      bool `yaml:"rpc"`
	Private       bool
	IncludeStderr bool `yaml:"include-stderr"`
	Encoding      MethodEncoding
	Params        map[string]MethodParam
	InvokeInfo    InvokeInfoDef `yaml:"invoke"`
	Limits        MethodLimits
	// unused parameter
	Output interface{}
}

// Limits ...
type MethodLimits Limits

type MethodArgs struct {
	Param    string
	Skip     bool
	Static   bool
	Compound bool
	Child    []MethodArgs
}

// MethodLimits define execution limits for method
// type MethodLimits methodLimits

func aggregateArgs(a MethodArgs, b map[string]bool) {
	if !a.Static && !a.Compound && a.Param != "self" {
		b[a.Param] = true
	}

	for _, v := range a.Child {
		aggregateArgs(v, b)
	}
}

// Validate ensure correct method definition.
func (m RawMethodDef) Validate() error {
	if !m.AllowRPC && !m.AllowStreamed {
		return fmt.Errorf("%w: method has disabled streamed and RPC response", ErrInvalidDefinition)
	}

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
			return fmt.Errorf("%w: undeclared param '%v' in arguments", ErrInvalidDefinition, k)
		}
	}

	for k, v := range definedParams {
		if v {
			logger.Warn().Str("name", k).Msg("unused parameter defined")
		}
	}

	return nil
}

type Config struct {
	Include  []rawInclude
	Log      LogDef
	Protocol Protocol
	Limits   Limits
	Methods  map[string]RawMethodDef
}

func listNodeFieldsV2(node parse.Node) []string {
	var res []string
	if node.Type() == parse.NodeAction {
		res = append(res, node.String())
	}

	if ln, ok := node.(*parse.ListNode); ok {
		for _, n := range ln.Nodes {
			res = append(res, listNodeFieldsV2(n)...)
		}
	}

	return res
}

func listUsedVariables(nodesName []string) []string {
	names := make([]string, len(nodesName))
	for i := range nodesName {
		names[i] = strings.TrimPrefix(strings.TrimSuffix(nodesName[i], "}}"), "{{.")
	}
	return names
}

func (c *config) Load(paths ...string) error {
	pathToVisit := list.New()
	for _, path := range paths {
		pathToVisit.PushBack(path)
	}

	for pathToVisit.Len() != 0 {
		path := pathToVisit.Remove(pathToVisit.Front()).(string)
		logger.Trace().Str("loading", path).Send()

		bytes, e := os.Open(path)
		if e != nil {
			return e
		}
		defer bytes.Close()

		var specialOne config

		decoder := yaml.NewDecoder(bytes)
		decoder.SetStrict(true)

		if err := decoder.Decode(&specialOne); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		// do we have any includes
		for _, v := range specialOne.Include {
			path := v.Path
			req := v.Required
			files, e := searchFiles(path, req)

			if e != nil {
				return e
			}

			for _, f := range files {
				klon := f
				pathToVisit.PushBack(klon)
			}
		}
		// ensure default Mode parameter
		const defaultMode = FileMode(0o666)

		for i := range specialOne.Protocol.Bind {
			b := specialOne.Protocol.Bind[i]
			if b.Mode == FileMode(0) {
				b.Mode = defaultMode
			}
		}

		mergeConfig(c, specialOne)
	}
	// ensure empty method limits are now filled with proper values
	// for k, v := range c.Methods {
	// 	l := &v.Limits
	// 	if l.ExecTimeout < 0 {
	// 		l.ExecTimeout = c.Limits.ExecTimeout
	// 	}
	// 	if l.MaxResponse < 0 {
	// 		l.MaxResponse = int64(c.Limits.MaxResponse)
	// 	}
	// 	c.Methods[k] = v
	// }
	for i := range c.Methods {
		if err := c.Methods[i].Validate(); err != nil {
			return fmt.Errorf("%s: %w", i, err)
		}
	}

	return nil
}

func mergeProtocol(a *Protocol, b Protocol) {
	if b.AuthBasic != nil {
		a.AuthBasic = b.AuthBasic
	}

	aBindLen := len(a.Bind)
	if aBindLen == 0 {
		a.Bind = append(a.Bind, b.Bind...)

		return
	}

	// add UNIQUE bind points
	for aIndex := 0; aIndex < aBindLen; aIndex++ {
		aBind := a.Bind[aIndex]

		for bIndex := range b.Bind {
			bBind := b.Bind[bIndex]
			if aBind == bBind {
				logger.Warn().Msg("duplicated bind point")

				continue
			}

			a.Bind = append(a.Bind, bBind)
		}
	}
}

func mergeLog(a *LogDef, b LogDef) {
	if b.Backend != BackendUndefined {
		a.Backend = b.Backend
	}

	if b.LogLevel != LLUndefined {
		a.LogLevel = b.LogLevel
	}

	if b.Path != "" {
		a.Path = b.Path
	}
}

func mergeConfig(confA *config, confB config) {
	confA.Include = append(confA.Include, confB.Include...)
	mergeLog(&confA.Log, confB.Log)

	if confA.Methods == nil {
		confA.Methods = make(map[string]RawMethodDef)
	}

	for k := range confB.Methods {
		confA.Methods[k] = confB.Methods[k]
	}

	mergeProtocol(&confA.Protocol, confB.Protocol)
}
