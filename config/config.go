package config

import (
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	log "github.com/rs/zerolog"

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

type MapOfValue map[string]Value
type ListOfValue []Value

type value struct {
	Mapa  MapOfValue
	Lista ListOfValue
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

// // Config uration
// type Config interface {
// 	// Get(key ...string) Value
// 	Load(path ...string) error
// }
type Config = config
type config struct {
	//root value
	rawConfig
	def value
}

// NewConfig returns empty configuration
func NewConfig() Config {
	return config{
		rawConfig: rawConfig{
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
		},
	}
}

// func (c *config) Get(key ...string) Value {
// 	var currentValue Value
// 	currentValue = &c.root
// 	for _, k := range key {
// 		currentValue = currentValue.Get(k)
// 	}
// 	return currentValue
// }

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

// BindType ...
type BindType int

const (
	BindTypeUnknown BindType = iota
	// HTTP - HTTP transport over TCP
	HTTP
	// HTTPS - HTTPS transport over TCP
	HTTPS
	// Unix - HTTP transport over unix socket
	Unix
)

// LogLevel describe logging level
type LogLevel int8

const (
	LLUndefined LogLevel = iota
	// LLOff Disable log
	LLOff
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
	LogLevel `yaml:"level"`
	Path     string
}

// Backend define log backend
type Backend int8

const (
	// BackendStdout write to stdout
	BackendStdout Backend = 1 << iota
	// BackendSyslog write to syslog
	BackendSyslog Backend = 1 << iota
	// BackendStderr write to stderr (default)
	BackendStderr Backend = 1 << iota
	// BackendKeep keep current output
	BackendKeep Backend = 1 << iota
	// BackendUndefined is used when no value is defined in config
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

// MethodEncoding ...
type MethodEncoding int

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

func (t MethodParamType) DefaultValue() interface{} {
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

type FileMode os.FileMode

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
	Mode FileMode
	UID  int
	GID  int
}

// Protocol - define bind points, auth and URI paths
type Protocol struct {
	Bind []*Bind

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
	PayloadSize uint32        `yaml:"payload-size"`
	MaxResponse uint32        `yaml:"max-response"`
}

func DefaultLimits() Limits {
	return Limits{
		time.Duration(10000 * time.Millisecond),
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
	DefaultVal *value `yaml:"default"`
}

// RunAs ...
type RunAs struct {
	UID uint32
	GID uint32
}
type InvokeInfoDef struct {
	Exec  string
	Delay time.Duration
	Args  []MethodArgs
	RunAs RunAs
}

// MethodDef ...
type RawMethodDef struct {
	Streamed   bool
	Private    bool
	Encoding   MethodEncoding
	Params     map[string]MethodParam
	InvokeInfo InvokeInfoDef `yaml:"invoke"`
	// Pointer because we need to know when its unsed
	Limits MethodLimits
	logger log.Logger
	// unused parameter
	Output interface{}
}

// Limits ...
type MethodLimits struct {
	ExecTimeout time.Duration
	MaxResponse int64
}

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

// Validate ensure correct method definition
func (m RawMethodDef) Validate() error {
	definedParams := make(map[string]bool)
	definedArgs := make(map[string]bool)
	for _, v := range m.InvokeInfo.Args {
		aggregateArgs(v, definedArgs)
	}

	for k := range m.Params {
		m.logger.Trace().Str("param", k).Send()
		definedParams[k] = true
	}

	for k := range definedArgs {
		if _, has := definedParams[k]; has {
			definedParams[k] = false
		} else {
			// Fatal, or just return error?
			m.logger.Error().Str("name", k).Msg("undeclared param")
			return fmt.Errorf("Undeclared param '%v' in arguments", k)
		}
	}

	for k, v := range definedParams {
		if v {
			m.logger.Warn().Str("name", k).Msg("unused parameter defined")
		}
	}
	return nil
}

type rawConfig struct {
	Include  []rawInclude
	Log      LogDef
	Protocol Protocol
	Limits   Limits
	Methods  map[string]RawMethodDef
}

func fromVal(value Value) (MethodArgs, error) {
	var out MethodArgs

	var err error
	if value.IsMap() {
		x := value.Get("template").AsString("")
		if x != "" {
			t, err := template.New("new").Parse(x)
			if err != nil {
				println(err.Error())
			}
			a := make(map[string]string)
			a["ip"] = "1000"
			t.Execute(os.Stdout, a)
			println(t)
		}
		out.Param = value.Get("param").AsString("")
		out.Skip = value.Get("skip").Bool(false)
		out.Static = false
	} else if value.IsArray() {
		asArray := value.Array(nil)
		out.Child = make([]MethodArgs, len(asArray))
		for i, m := range asArray {
			out.Child[i], err = fromVal(m)
		}
		out.Compound = true
	} else {
		out.Param = fmt.Sprint(value.Raw())
		out.Static = true
		out.Skip = false
	}
	return out, err
}

func (m *MethodLimits) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var limits struct {
		ExecTimeout uint64
		MaxResponse int64
	}
	if err := unmarshal(&limits); err != nil {
		return err
	}
	m.MaxResponse = limits.MaxResponse
	m.ExecTimeout = time.Duration(limits.ExecTimeout * uint64(time.Millisecond))
	return nil
}

func (m *RawMethodDef) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// ensure we have required defaults sets before
	m.Limits = MethodLimits{
		ExecTimeout: -1,
		MaxResponse: -1,
	}
	m.InvokeInfo.RunAs = RunAs{
		UID: uint32(os.Getuid()),
		GID: uint32(os.Getgid()),
	}
	// Use alias, so we dont hit stack overflow invoking self over and over
	type yamlFix RawMethodDef
	v := yamlFix(*m)
	if err := unmarshal(&v); err != nil {
		return err
	}
	*m = RawMethodDef(v)
	return nil
}
func (b *Bind) UnmarshalYAML(unmarshal func(interface{}) error) error {
	b.UID = -1
	b.GID = -1

	type yamlFix Bind
	v := yamlFix(*b)
	if err := unmarshal(&v); err != nil {
		return nil
	}
	*b = Bind(v)
	return nil
}

func (fm *FileMode) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var name string
	if err := unmarshal(&name); err != nil {
		return err
	}
	name = strings.ToLower(name)
	name = strings.TrimPrefix(name, "0o")
	val, err := strconv.ParseUint(name, 8, 9)
	if err != nil {
		return err
	}
	*fm = FileMode(val)
	return nil
}

func (mv *MapOfValue) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var tmp map[string]*value
	if err := unmarshal(&tmp); err != nil {
		return err
	}
	mav := make(MapOfValue)
	for k, v := range tmp {
		mav[k] = v
	}
	*mv = mav
	return nil
}
func (lv *ListOfValue) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var tmp []*value
	if err := unmarshal(&tmp); err != nil {
		return err
	}
	lav := make(ListOfValue, len(tmp))
	for i := range tmp {
		lav[i] = tmp[i]
	}
	*lv = lav
	return nil
}

func (ma *MethodArgs) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var v value
	if err := unmarshal(&v); err != nil {
		return err
	}
	a, err := fromVal(&v)
	if err != nil {
		return err
	}
	*ma = a
	return nil
}
func (v *value) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var param struct {
		Param string
		Skip  bool
	}
	if err := unmarshal(&param); err == nil {
		v.Mapa = make(MapOfValue)
		v.Mapa["param"] = &value{Other: param.Param}
		v.Mapa["skip"] = &value{Other: param.Skip}
		return nil
	}
	if err := unmarshal(&v.Lista); err == nil {
		return nil
	}
	if err := unmarshal(&v.Mapa); err == nil {
		return nil
	}
	return unmarshal(&v.Other)
}
func (mpt *MethodParamType) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var name string
	if err := unmarshal(&name); err != nil {
		return err
	}
	pt, err := parseMethodParamType(name)
	if err != nil {
		return err
	}
	*mpt = pt
	return nil
}

func (bt *BindType) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var name string
	if err := unmarshal(&name); err != nil {
		return err
	}
	b, err := parseBindType(name)
	if err != nil {
		return err
	}
	*bt = b
	return nil
}

func (i *rawInclude) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// this can be `string` or map[string]bool
	var complex map[string]bool
	if err := unmarshal(&complex); err == nil {
		for k, v := range complex {
			i.Path = k
			i.Required = v
			break
		}
		return nil
	}

	if err := unmarshal(&i.Path); err != nil {
		return err
	}
	return nil
}

func (ll *LogLevel) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var name string
	if err := unmarshal(&name); err != nil {
		return err
	}
	l, err := parseLoglevel(name)
	if err != nil {
		return err
	}
	*ll = l
	return nil
}

// Implements the Unmarshaler interface of the yaml pkg.
func (b *Backend) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var name string
	if err := unmarshal(&name); err != nil {
		return err
	}

	be, err := parseBackend(name)
	if err != nil {
		return err
	}
	*b = be

	return nil
}

func (c *config) Load(paths ...string) error {
	pathToVisit := new(stack)
	for _, path := range paths {
		pathToVisit.push(path)
	}
	for pathToVisit.len() != 0 {
		path := pathToVisit.pop()
		// log.Logger. .Println("loading", path)
		bytes, e := ioutil.ReadFile(path)
		if e != nil {
			return e
		}
		var specialOne rawConfig
		if err := yaml.UnmarshalStrict(bytes, &specialOne); err != nil {
			return err
		}
		// do we have any includes
		for _, v := range specialOne.Include {
			path := v.Path //.AsString("")
			req := v.Required
			fs, e := searchFiles(path, req)
			if e != nil {
				return e
			}
			for _, f := range fs {
				klon := f
				pathToVisit.push(klon)
			}
		}
		// ensure default Mode parameter
		for i := range specialOne.Protocol.Bind {
			b := specialOne.Protocol.Bind[i]
			if b.Mode == FileMode(0) {
				b.Mode = FileMode(0666)
			}
		}
		// c.root.Mapa = mergeMap(c.root.Mapa, val.Mapa)
		mergeConfig(&c.rawConfig, specialOne)
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
			return err
		}
	}
	return nil
}

func mergeProtocol(a *Protocol, b Protocol) {
	if b.AuthBasic != nil {
		a.AuthBasic = b.AuthBasic
	}
	a.Bind = append(a.Bind, b.Bind...)
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

func mergeConfig(a *rawConfig, b rawConfig) {
	a.Include = append(a.Include, b.Include...)
	mergeLog(&a.Log, b.Log)
	if a.Methods == nil {
		a.Methods = make(map[string]RawMethodDef)
	}
	for k := range b.Methods {
		a.Methods[k] = b.Methods[k]
	}
	mergeProtocol(&a.Protocol, b.Protocol)
}
