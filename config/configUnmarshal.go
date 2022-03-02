package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

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
	v := yamlFix(*m)
	if err := unmarshal(&v); err != nil {
		return err
	}
	v.Limits.ExecTimeout = time.Duration(v.Limits.ExecTimeout) * time.Millisecond
	v.InvokeInfo.Delay = time.Duration(v.InvokeInfo.Delay) * time.Millisecond
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
