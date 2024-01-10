package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func (e *ExecType) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var name string
	if err := unmarshal(&name); err != nil {
		return err
	}

	et, err := parseExecType(name)
	if err != nil {
		return err
	}

	*e = et

	return nil
}

func (e *Exec) UnmarshalYAML(unmarshal func(interface{}) error) error {
	if err := unmarshal(&e.Path); err == nil {
		e.Mode = ExecTypeDefault
		// Done, this is default exec
		return nil
	}

	type yamlFix Exec

	e.Path = "sh"
	wrap := yamlFix(*e)

	if err := unmarshal(&wrap); err != nil {
		return err
	}

	*e = Exec(wrap)

	return nil
}

func (m *MethodEncoding) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var name string
	if err := unmarshal(&name); err != nil {
		return err
	}

	me, err := parseEncoding(name)
	if err != nil {
		return err
	}

	*m = me

	return nil
}

func (m *RawMethodDef) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Use alias, so we dont hit stack overflow invoking self over and over
	type yamlFix RawMethodDef

	m.Limits = MethodLimits{
		ExecTimeout: -1,
		MaxResponse: -1,
	}
	m.InvokeInfo.RunAs = RunAs{
		UID: os.Getuid(),
		GID: os.Getgid(),
	}
	m.AllowRPC = true

	wrap := yamlFix(*m)
	if err := unmarshal(&wrap); err != nil {
		return err
	}
	// NOTE: this is valid, as config uses values in ms not in time.Duration
	wrap.Limits.ExecTimeout *= time.Millisecond
	wrap.InvokeInfo.Delay *= time.Millisecond
	*m = RawMethodDef(wrap)

	return nil
}

func (b *Bind) UnmarshalYAML(unmarshal func(interface{}) error) error {
	b.UID = -1
	b.GID = -1

	type yamlFix Bind

	v := yamlFix(*b)
	if err := unmarshal(&v); err != nil {
		return err
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
	// 8 - octal number
	// 9 - use only 9 bits
	val, err := strconv.ParseUint(name, 8, 9)
	if err != nil {
		return fmt.Errorf("%v: %w", ErrInvalidConfig, err)
	}

	*fm = FileMode(val)

	return nil
}

func fromInterParams(param interParams) (MethodArgs, error) {
	switch param.Type {
	case Scalar:
		return MethodArgs{
			Static: true,
			Param:  param.Scalar,
		}, nil
	case Map:
		return MethodArgs{
			Param: param.Map.Param,
			Skip:  param.Map.Skip,
		}, nil
	case Array:
		ma := MethodArgs{
			Compound: true,
			Child:    make([]MethodArgs, len(param.Array)),
		}

		var err error
		for i := range param.Array {
			if ma.Child[i], err = fromInterParams(param.Array[i]); err != nil {
				return ma, err
			}
		}

		return ma, nil
	case BORKED:
		fallthrough
	default:
		return MethodArgs{}, ErrInvalidConfig
	}
}

type intermediateParamsType int

const (
	BORKED intermediateParamsType = iota
	Scalar
	Map
	Array
)

func (i intermediateParamsType) String() string {
	switch i {
	case BORKED:
		return "- error -"
	case Scalar:
		return "Scalar"
	case Map:
		return "Map"
	case Array:
		return "Array"
	}

	panic("not reachable")
}

type interParams struct {
	Scalar string
	Map    struct {
		Param string
		Skip  bool
	}
	Array []interParams
	Type  intermediateParamsType
}

func (i *interParams) UnmarshalYAML(unmarshal func(interface{}) error) error {
	if err := unmarshal(&i.Scalar); err == nil {
		i.Type = Scalar

		return nil
	}

	if err := unmarshal(&i.Map); err == nil {
		i.Type = Map

		return nil
	}

	if err := unmarshal(&i.Array); err != nil {
		return err
	}
	// not sure if this is only exception
	if len(i.Array) == 1 && i.Array[0].Type == BORKED {
		i.Type = Scalar
		i.Array = nil
		i.Scalar = "-"

		return nil
	}

	i.Type = Array
	return nil
}

func (ma *MethodArgs) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var ip interParams
	if err := unmarshal(&ip); err != nil {
		return err
	}

	args, err := fromInterParams(ip)
	if err != nil {
		return err
	}

	*ma = args
	return nil
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
	var compound map[string]bool
	if err := unmarshal(&compound); err == nil {
		for k, v := range compound {
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