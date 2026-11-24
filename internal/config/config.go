// Package config loads service settings from environment variables declared
// with struct tags, and exposes the same declarations for documentation.
package config

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Var describes one documented setting.
type Var struct {
	Service string
	Name    string
	Default string
	Doc     string
}

var durationType = reflect.TypeOf(time.Duration(0))

// Load fills the struct pointed to by dst from `env`/`default` tags.
// Fields without an `env` tag are left alone.
func Load(dst any) error {
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("config: Load needs a pointer to a struct")
	}
	v = v.Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		name := t.Field(i).Tag.Get("env")
		if name == "" {
			continue
		}
		raw, ok := os.LookupEnv(name)
		if !ok || raw == "" {
			raw = t.Field(i).Tag.Get("default")
		}
		if err := set(v.Field(i), raw); err != nil {
			return fmt.Errorf("config: %s=%q: %w", name, raw, err)
		}
	}
	return nil
}

func set(f reflect.Value, raw string) error {
	if f.Type() == durationType {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return err
		}
		f.SetInt(int64(d))
		return nil
	}
	switch f.Kind() {
	case reflect.String:
		f.SetString(raw)
	case reflect.Int, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return err
		}
		f.SetInt(n)
	case reflect.Float64:
		x, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return err
		}
		f.SetFloat(x)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		f.SetBool(b)
	default:
		return fmt.Errorf("unsupported type %s", f.Type())
	}
	return nil
}

// Docs lists the tagged settings of each struct, in field order. The service
// name is the struct's type name.
func Docs(dsts ...any) []Var {
	var out []Var
	for _, d := range dsts {
		t := reflect.TypeOf(d)
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.Tag.Get("env") == "" {
				continue
			}
			out = append(out, Var{
				Service: strings.TrimSuffix(t.Name(), "Config"),
				Name:    f.Tag.Get("env"),
				Default: f.Tag.Get("default"),
				Doc:     f.Tag.Get("doc"),
			})
		}
	}
	return out
}
