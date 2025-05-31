package autoconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/viper"
)

const (
	tagConfig       = "config"
	tagMapStructure = "mapstructure" // viper uses this to read environment variables
)

var allowedRegex = regexp.MustCompile(`[^a-z0-9 ]+`)

// ----------------------------------------------------------------
// Config holds state for reading & validating configuration.
// ----------------------------------------------------------------

type Config struct {
	project, dirname, cfgBaseName string
	cfgType                       ConfigType
	envPrefix                     string
	msTag                         map[string]string

	// For caching field metadata per‐type:
	structFields map[reflect.Type][]fieldMeta
	mu           sync.RWMutex
}

// fieldMeta holds pre‐parsed tag info for one struct field.
type fieldMeta struct {
	name       string  // Go field name
	index      []int   // reflect index path (always [i] for top‐level fields)
	mapTag     string  // contents of `mapstructure:"..."`
	required   bool    // did tag include "required"?
	defaultVal *string // default value string if tag included `default=...`
	isStruct   bool    // did tagConfig have exactly "struct"?
}

// New creates a new Config, setting up maps & env prefix.
func New(prefix string) *Config {
	c := &Config{
		msTag:        make(map[string]string),
		structFields: make(map[reflect.Type][]fieldMeta),
		envPrefix:    prefix,
	}
	viper.SetEnvPrefix(prefix)
	return c
}

// Create sets up config directory & viper file settings.
func (c *Config) Create(
	project, baseCfgName, baseDirname string,
	cfgType ConfigType,
) error {
	if project == "" {
		return fmt.Errorf("config init: missing project name")
	}
	if baseCfgName == "" {
		return fmt.Errorf("config init: missing config base file name")
	}
	if !cfgType.IsValid() {
		cfgType = ConfigTypeYAML
	}
	c.cfgType = cfgType
	c.project = "." + allowedRegex.ReplaceAllString(strings.ToLower(project), "")
	c.cfgBaseName = allowedRegex.ReplaceAllString(strings.ToLower(baseCfgName), "")

	var err error
	if baseDirname == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			return fmt.Errorf("config init: getting home dir → %w", e)
		}
		c.dirname = filepath.Join(home, project)
	} else {
		c.dirname, err = filepath.Abs(filepath.Clean(baseDirname))
		if err != nil {
			return fmt.Errorf("config init: setting dirname → %w", err)
		}
	}

	if err := os.MkdirAll(c.dirname, 0700); err != nil {
		return fmt.Errorf("config init: error creating '%s' directory → %w", c.dirname, err)
	}

	viper.AddConfigPath(c.dirname)
	viper.SetConfigType(c.cfgType.String())
	viper.SetConfigName(c.cfgBaseName)
	return nil
}

// ReadFile instructs viper to read a config file and unmarshal into `s`.
// `s` must be a pointer to a struct.
func (c *Config) ReadFile(s any) error {
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf(
			"readfile: missing '%s.%s' config in '%s'",
			c.cfgBaseName, c.cfgType.String(), c.dirname,
		)
	}
	if err := viper.Unmarshal(s); err != nil {
		return fmt.Errorf(
			"readfile: error reading '%s.%s' config in '%s' → %w",
			c.cfgBaseName, c.cfgType.String(), c.dirname, err,
		)
	}
	return nil
}

// ReadEnv binds environment variables using each field’s `mapstructure` tag
// and then unmarshals into `s`.  It also records `msTag` for later validation.
func (c *Config) ReadEnv(s any) error {
	// 1) Must be a non‐nil pointer to struct
	rv := reflect.ValueOf(s)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf(
			"read environment: expected pointer, got %s",
			rv.Kind(),
		)
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf(
			"read environment: expected pointer to struct, got pointer to %s",
			rv.Kind(),
		)
	}

	// 2) Iterate top‐level fields, bind env if mapstructure tag is present
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		sf := rt.Field(i)
		if sf.PkgPath != "" {
			continue // skip unexported
		}

		// 2a) If tagConfig:"struct", recurse into nested struct
		if rawTag, ok := sf.Tag.Lookup(tagConfig); ok && strings.EqualFold(rawTag, "struct") {
			fv := rv.Field(i)
			if fv.CanAddr() {
				if err := c.ReadEnv(fv.Addr().Interface()); err != nil {
					return fmt.Errorf(
						"read environment: nested struct '%s' → %w",
						sf.Name, err,
					)
				}
			}
		}

		// 2b) Bind env by mapstructure tag
		if mapTag, ok := sf.Tag.Lookup(tagMapStructure); ok && mapTag != "-" {
			_ = viper.BindEnv(mapTag)
			c.msTag[sf.Name] = mapTag
		}
	}

	// 3) Unmarshal all bound env vars at once
	if err := viper.Unmarshal(s); err != nil {
		return fmt.Errorf("read environment: error unmarshaling → %w", err)
	}
	return nil
}

// getOrBuildFieldMeta returns a slice of fieldMeta for type `rt`.
// It caches the result so that each struct’s tags are parsed once.
// Returns an error if any `config:"..."` tag is malformed.
func (c *Config) getOrBuildFieldMeta(rt reflect.Type) ([]fieldMeta, error) {
	// 1) Fast path: check if already cached
	c.mu.RLock()
	if metas, found := c.structFields[rt]; found {
		c.mu.RUnlock()
		return metas, nil
	}
	c.mu.RUnlock()

	// 2) Build under write‐lock
	c.mu.Lock()
	defer c.mu.Unlock()

	// Another goroutine may have cached meanwhile
	if metas, found := c.structFields[rt]; found {
		return metas, nil
	}

	var list []fieldMeta
	numFields := rt.NumField()

	for i := 0; i < numFields; i++ {
		sf := rt.Field(i)
		if sf.PkgPath != "" {
			continue // skip unexported
		}

		rawTag, ok := sf.Tag.Lookup(tagConfig)
		if !ok || rawTag == "-" {
			continue
		}

		// Split “config” tag on commas once
		parts := strings.Split(rawTag, ",")
		var fm fieldMeta
		fm.name = sf.Name
		fm.index = []int{i}

		for _, part := range parts {
			part = strings.TrimSpace(part)
			switch {
			case strings.EqualFold(part, "struct"):
				fm.isStruct = true
			case strings.EqualFold(part, "required"):
				fm.required = true
			case strings.HasPrefix(part, "default="):
				kv := strings.SplitN(part, "=", 2)
				if len(kv) == 2 {
					val := kv[1]
					fm.defaultVal = &val
				}
				// ignore unrecognized tokens silently
			}
		}

		// Record mapstructure tag (for error messaging)
		if mapTag, ok := sf.Tag.Lookup(tagMapStructure); ok && mapTag != "-" {
			fm.mapTag = mapTag
		}

		list = append(list, fm)
	}

	c.structFields[rt] = list
	return list, nil
}

// Check validates “required” fields and applies “default=” defaults.
// It recurses into nested structs when tagConfig:"struct" is present.
// Uses cached fieldMeta for speed.
// `s` must be pointer to struct.
func (c *Config) Check(s any) error {
	// 1) Must be a pointer to a non‐nil struct
	rv := reflect.ValueOf(s)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("config check: expected pointer to struct, got %s", rv.Kind())
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("config check: expected pointer to struct, got pointer to %s", rv.Kind())
	}

	// 2) Look up (or build) metadata for this struct type
	rt := rv.Type()
	metas, err := c.getOrBuildFieldMeta(rt)
	if err != nil {
		return err
	}

	// 3) Iterate only over those fields with a “config” tag
	for _, fm := range metas {
		fv := rv.FieldByIndex(fm.index)

		// 3a) If this field is itself a nested struct marker, recurse
		if fm.isStruct {
			if fv.CanAddr() {
				if err := c.Check(fv.Addr().Interface()); err != nil {
					return fmt.Errorf("config check: nested struct %q → %w", fm.name, err)
				}
			}
		}

		// 3b) Required check
		if fm.required && isZeroValue(fv) {
			return fmt.Errorf(
				"config check: missing required '%s_%s' for field '%s'",
				c.envPrefix, fm.mapTag, fm.name,
			)
		}

		// 3c) Default check (only if zero and defaultVal is set)
		if isZeroValue(fv) && fm.defaultVal != nil {
			if err := setFieldDefault(rv, fm, *fm.defaultVal); err != nil {
				return fmt.Errorf("config check: default for field %q → %w", fm.name, err)
			}
		}
	}

	return nil
}

// isZeroValue returns true if v is the zero value for its type.
func isZeroValue(v reflect.Value) bool {
	return v.IsZero()
}

// setFieldDefault writes the literal defaultStr into the field described by fm.
func setFieldDefault(parent reflect.Value, fm fieldMeta, defaultStr string) error {
	fv := parent.FieldByIndex(fm.index)
	if !fv.CanSet() {
		return fmt.Errorf("cannot set default on unaddressable field %q", fm.name)
	}

	switch fv.Kind() {
	case reflect.Bool:
		b, err := strconv.ParseBool(defaultStr)
		if err != nil {
			return fmt.Errorf("invalid bool default %q: %w", defaultStr, err)
		}
		fv.SetBool(b)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.ParseInt(defaultStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid int default %q: %w", defaultStr, err)
		}
		fv.SetInt(i)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(defaultStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid uint default %q: %w", defaultStr, err)
		}
		fv.SetUint(u)

	case reflect.Float32, reflect.Float64:
		fvlt, err := strconv.ParseFloat(defaultStr, 64)
		if err != nil {
			return fmt.Errorf("invalid float default %q: %w", defaultStr, err)
		}
		fv.SetFloat(fvlt)

	case reflect.String:
		fv.SetString(defaultStr)

	default:
		return fmt.Errorf("unsupported kind %s for default on %q", fv.Kind(), fm.name)
	}
	return nil
}
