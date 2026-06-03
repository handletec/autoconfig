package autoconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	mapstructure "github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

const (
	tagConfig       = "config"
	tagMapStructure = "mapstructure"
	tagJSON         = "json"
	tagYAML         = "yaml"
)

var (
	allowedNameRegex = regexp.MustCompile(`[^a-z0-9._-]+`)
	spaceRegex       = regexp.MustCompile(`\s+`)
	timeDurationType = reflect.TypeOf(time.Duration(0))
)

// Config holds state for reading and validating configuration.
type Config struct {
	project     string
	dirname     string
	cfgBaseName string
	cfgType     ConfigType
	envPrefix   string
	v           *viper.Viper

	structFields map[reflect.Type][]fieldMeta
	present      map[string]struct{}
	mu           sync.RWMutex
}

// fieldMeta holds pre-parsed tag info for one struct field.
type fieldMeta struct {
	name       string
	index      []int
	mapTag     string
	yamlTag    string
	jsonTag    string
	required   bool
	defaultVal *string
	isStruct   bool
}

// New creates a new isolated Config instance.
func New(prefix string) *Config {
	v := viper.New()
	if prefix != "" {
		v.SetEnvPrefix(prefix)
	}

	return &Config{
		envPrefix:    prefix,
		v:            v,
		structFields: make(map[reflect.Type][]fieldMeta),
		present:      make(map[string]struct{}),
	}
}

// Create sets up config directory and Viper file settings.
func (c *Config) Create(project, baseCfgName, baseDirname string, cfgType ConfigType) error {
	if c == nil {
		return fmt.Errorf("config init: nil config receiver")
	}

	safeProject, err := sanitizeName(project)
	if err != nil {
		return fmt.Errorf("config init: invalid project name: %w", err)
	}

	safeCfgBaseName, err := sanitizeName(baseCfgName)
	if err != nil {
		return fmt.Errorf("config init: invalid config base file name: %w", err)
	}

	if !cfgType.IsValid() {
		cfgType = ConfigTypeYAML
	}

	c.cfgType = cfgType
	c.project = "." + safeProject
	c.cfgBaseName = safeCfgBaseName

	if baseDirname == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("config init: getting home dir: %w", err)
		}

		home, err = filepath.Abs(filepath.Clean(home))
		if err != nil {
			return fmt.Errorf("config init: resolving home dir: %w", err)
		}

		c.dirname = filepath.Join(home, c.project)
		if !isWithinBaseDir(home, c.dirname) {
			return fmt.Errorf("config init: resolved config directory %q escapes home directory %q", c.dirname, home)
		}
	} else {
		// Reject relative paths that contain traversal components before resolution
		// so that inputs like "../../etc" cannot silently escape the caller's intent.
		cleaned := filepath.Clean(baseDirname)
		if !filepath.IsAbs(cleaned) {
			return fmt.Errorf("config init: baseDirname %q must be an absolute path", baseDirname)
		}
		c.dirname = cleaned
	}

	if err := os.MkdirAll(c.dirname, 0o700); err != nil {
		return fmt.Errorf("config init: creating %q directory: %w", c.dirname, err)
	}

	c.v.SetConfigFile(c.configFilePath())
	c.v.SetConfigType(c.cfgType.String())
	return nil
}

// ReadFile reads a config file and unmarshals it into s.
// Unknown fields are rejected.
func (c *Config) ReadFile(s any) error {
	rv, err := structValueFromPointer(s)
	if err != nil {
		return fmt.Errorf("readfile: %w", err)
	}

	if err := c.v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		var parseErr viper.ConfigParseError

		cfgName := filepath.Base(c.configFilePath())
		switch {
		case errors.As(err, &notFound):
			return fmt.Errorf("readfile: missing %q", cfgName)
		case errors.As(err, &parseErr):
			return fmt.Errorf("readfile: invalid config file %q: %w", cfgName, err)
		default:
			return fmt.Errorf("readfile: unable to read %q: %w", cfgName, err)
		}
	}

	if err := c.v.UnmarshalExact(s, decoderOptions()...); err != nil {
		return fmt.Errorf("readfile: unable to decode %q: %w", filepath.Base(c.configFilePath()), err)
	}

	if err := c.recordPresenceFromSettings(rv.Type(), c.rootPathForType(rv.Type()), c.v.AllSettings()); err != nil {
		return fmt.Errorf("readfile: presence tracking failed: %w", err)
	}

	return nil
}

// ReadEnv binds environment variables using each field's mapstructure tag
// and then unmarshals into s.
func (c *Config) ReadEnv(s any) error {
	rv, err := structValueFromPointer(s)
	if err != nil {
		return fmt.Errorf("read environment: %w", err)
	}

	if err := c.bindEnvStruct(rv, c.rootPathForType(rv.Type())); err != nil {
		return err
	}

	if err := c.v.Unmarshal(s, decoderOptions()...); err != nil {
		return fmt.Errorf("read environment: error unmarshaling: %w", err)
	}

	return nil
}

func (c *Config) bindEnvStruct(rv reflect.Value, path []string) error {
	metas, err := c.getOrBuildFieldMeta(rv.Type())
	if err != nil {
		return err
	}

	for _, fm := range metas {
		field := rv.FieldByIndex(fm.index)
		fieldPath := appendPath(path, fm.name)

		if fm.isStruct {
			nested, ok := ensureStructValue(field)
			if ok {
				if err := c.bindEnvStruct(nested, fieldPath); err != nil {
					return fmt.Errorf("read environment: nested struct %q: %w", fm.name, err)
				}
			}
		}

		if fm.mapTag == "" || fm.mapTag == "-" {
			continue
		}

		if err := c.v.BindEnv(fm.mapTag); err != nil {
			return fmt.Errorf("read environment: bind env for %q (%s): %w", fm.name, fm.mapTag, err)
		}

		if c.v.IsSet(fm.mapTag) {
			c.recordPresence(fieldPath)
		}
	}

	return nil
}

// Check validates required fields and applies defaults.
func (c *Config) Check(s any) error {
	rv, err := structValueFromPointer(s)
	if err != nil {
		return fmt.Errorf("config check: %w", err)
	}

	return c.checkStruct(rv, c.rootPathForType(rv.Type()))
}

func (c *Config) checkStruct(rv reflect.Value, path []string) error {
	metas, err := c.getOrBuildFieldMeta(rv.Type())
	if err != nil {
		return err
	}

	for _, fm := range metas {
		fv := rv.FieldByIndex(fm.index)
		fieldPath := appendPath(path, fm.name)

		if fm.isStruct {
			nested, ok := ensureStructValue(fv)
			if ok {
				if err := c.checkStruct(nested, fieldPath); err != nil {
					return fmt.Errorf("config check: nested struct %q: %w", fm.name, err)
				}
			}

			if fm.required && !c.hasAnyPresence(fieldPath) && isZeroValue(fv) && fm.defaultVal == nil {
				return fmt.Errorf("config check: missing required %q for field %q", envFieldName(c.envPrefix, fm.mapTag), fm.name)
			}
			continue
		}

		if !c.hasPresence(fieldPath) && isZeroValue(fv) && fm.defaultVal != nil {
			if err := setFieldDefault(rv, fm, *fm.defaultVal); err != nil {
				return fmt.Errorf("config check: default for field %q: %w", fm.name, err)
			}
		}

		if fm.required && !c.hasPresence(fieldPath) && fm.defaultVal == nil && isZeroValue(fv) {
			return fmt.Errorf("config check: missing required %q for field %q", envFieldName(c.envPrefix, fm.mapTag), fm.name)
		}
	}

	return nil
}

func (c *Config) getOrBuildFieldMeta(rt reflect.Type) ([]fieldMeta, error) {
	c.mu.RLock()
	if metas, found := c.structFields[rt]; found {
		c.mu.RUnlock()
		return metas, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if metas, found := c.structFields[rt]; found {
		return metas, nil
	}

	metas := make([]fieldMeta, 0, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		sf := rt.Field(i)
		if sf.PkgPath != "" {
			continue
		}

		rawTag, ok := sf.Tag.Lookup(tagConfig)
		if !ok || rawTag == "-" {
			continue
		}

		fm := fieldMeta{
			name:    sf.Name,
			index:   []int{i},
			mapTag:  cleanTagValue(sf.Tag.Get(tagMapStructure)),
			yamlTag: cleanTagValue(sf.Tag.Get(tagYAML)),
			jsonTag: cleanTagValue(sf.Tag.Get(tagJSON)),
		}

		parts := strings.Split(rawTag, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			switch {
			case part == "":
				continue
			case strings.EqualFold(part, "struct"):
				fm.isStruct = true
			case strings.EqualFold(part, "required"):
				fm.required = true
			case strings.HasPrefix(strings.ToLower(part), "default="):
				kv := strings.SplitN(part, "=", 2)
				if len(kv) != 2 {
					return nil, fmt.Errorf("config tag on field %q: invalid default expression %q", sf.Name, part)
				}
				value := kv[1]
				fm.defaultVal = &value
			default:
				return nil, fmt.Errorf("config tag on field %q: unsupported token %q", sf.Name, part)
			}
		}

		metas = append(metas, fm)
	}

	c.structFields[rt] = metas
	return metas, nil
}

func (c *Config) recordPresenceFromSettings(rt reflect.Type, path []string, settings map[string]any) error {
	metas, err := c.getOrBuildFieldMeta(rt)
	if err != nil {
		return err
	}

	for _, fm := range metas {
		fieldPath := appendPath(path, fm.name)
		settingValue, ok := findSettingValue(settings, fm)
		if !ok {
			continue
		}

		c.recordPresence(fieldPath)

		if !fm.isStruct {
			continue
		}

		nestedSettings, ok := toStringAnyMap(settingValue)
		if !ok {
			continue
		}

		nestedType, ok := nestedStructType(rt.FieldByIndex(fm.index).Type)
		if !ok {
			continue
		}

		if err := c.recordPresenceFromSettings(nestedType, fieldPath, nestedSettings); err != nil {
			return err
		}
	}

	return nil
}

func decoderOptions() []viper.DecoderConfigOption {
	return []viper.DecoderConfigOption{
		viper.DecodeHook(mapstructure.ComposeDecodeHookFunc(
			stringToDurationHookFunc(),
			stringToStringSliceHookFunc(),
			mapstructure.StringToTimeDurationHookFunc(),
		)),
	}
}

func stringToDurationHookFunc() mapstructure.DecodeHookFuncType {
	return func(from reflect.Type, to reflect.Type, data any) (any, error) {
		if from.Kind() != reflect.String || to != timeDurationType {
			return data, nil
		}

		d, err := time.ParseDuration(strings.TrimSpace(data.(string)))
		if err != nil {
			return nil, err
		}
		return d, nil
	}
}

func stringToStringSliceHookFunc() mapstructure.DecodeHookFuncType {
	return func(from reflect.Type, to reflect.Type, data any) (any, error) {
		if from.Kind() != reflect.String || to.Kind() != reflect.Slice || to.Elem().Kind() != reflect.String {
			return data, nil
		}

		raw := strings.TrimSpace(data.(string))
		if raw == "" {
			return []string{}, nil
		}

		parts := strings.Split(raw, ",")
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			result = append(result, trimmed)
		}
		return result, nil
	}
}

func structValueFromPointer(s any) (reflect.Value, error) {
	rv := reflect.ValueOf(s)
	if !rv.IsValid() {
		return reflect.Value{}, fmt.Errorf("expected pointer to struct, got <nil>")
	}
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return reflect.Value{}, fmt.Errorf("expected pointer to struct, got %s", rv.Kind())
	}

	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("expected pointer to struct, got pointer to %s", rv.Kind())
	}

	return rv, nil
}

func ensureStructValue(v reflect.Value) (reflect.Value, bool) {
	for v.Kind() == reflect.Ptr {
		if v.Type().Elem().Kind() != reflect.Struct {
			return reflect.Value{}, false
		}
		if v.IsNil() {
			if !v.CanSet() {
				return reflect.Value{}, false
			}
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}

	return v, true
}

func nestedStructType(t reflect.Type) (reflect.Type, bool) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, false
	}
	return t, true
}

func sanitizeName(input string) (string, error) {
	value := strings.TrimSpace(strings.ToLower(input))
	value = spaceRegex.ReplaceAllString(value, "-")
	value = allowedNameRegex.ReplaceAllString(value, "")
	value = strings.Trim(value, ".-_")
	if value == "" {
		return "", fmt.Errorf("value is empty after sanitization")
	}
	return value, nil
}

func isWithinBaseDir(baseDir, target string) bool {
	rel, err := filepath.Rel(baseDir, target)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && rel != "")
}

func (c *Config) configFilePath() string {
	return filepath.Join(c.dirname, c.cfgBaseName+"."+c.cfgType.String())
}

func appendPath(path []string, elem string) []string {
	out := make([]string, 0, len(path)+1)
	out = append(out, path...)
	out = append(out, elem)
	return out
}

func pathKey(path []string) string {
	return strings.Join(path, ".")
}

func (c *Config) rootPathForType(rt reflect.Type) []string {
	return []string{rt.PkgPath() + "." + rt.Name()}
}

func (c *Config) recordPresence(path []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.present[pathKey(path)] = struct{}{}
}

func (c *Config) hasPresence(path []string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.present[pathKey(path)]
	return ok
}

func (c *Config) hasAnyPresence(path []string) bool {
	prefix := pathKey(path)
	c.mu.RLock()
	defer c.mu.RUnlock()

	if _, ok := c.present[prefix]; ok {
		return true
	}

	prefix += "."
	for key := range c.present {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func findSettingValue(settings map[string]any, fm fieldMeta) (any, bool) {
	for _, key := range settingKeysForField(fm) {
		value, ok := settings[key]
		if ok {
			return value, true
		}
	}
	return nil, false
}

func settingKeysForField(fm fieldMeta) []string {
	keys := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)

	add := func(key string) {
		key = strings.TrimSpace(strings.ToLower(key))
		if key == "" || key == "-" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}

	add(fm.yamlTag)
	add(fm.jsonTag)
	add(toSnakeCase(fm.name))
	add(strings.ToLower(fm.name))
	add(strings.ToLower(fm.mapTag))

	return keys
}

func toSnakeCase(value string) string {
	if value == "" {
		return value
	}

	var b strings.Builder
	b.Grow(len(value) + len(value)/3)

	for i, r := range value {
		if i > 0 && r >= 'A' && r <= 'Z' {
			prev := rune(value[i-1])
			if (prev >= 'a' && prev <= 'z') || (i+1 < len(value) && rune(value[i+1]) >= 'a' && rune(value[i+1]) <= 'z') {
				b.WriteByte('_')
			}
		}
		b.WriteRune(r)
	}

	return strings.ToLower(b.String())
}

func toStringAnyMap(value any) (map[string]any, bool) {
	typed, ok := value.(map[string]any)
	return typed, ok
}

func cleanTagValue(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}
	parts := strings.SplitN(tag, ",", 2)
	return strings.TrimSpace(parts[0])
}

func envFieldName(prefix, key string) string {
	key = strings.TrimSpace(key)
	prefix = strings.TrimSpace(prefix)
	if key == "" {
		return prefix
	}
	if prefix == "" {
		return key
	}
	return prefix + "_" + key
}

func isZeroValue(v reflect.Value) bool {
	return v.IsZero()
}

func setFieldDefault(parent reflect.Value, fm fieldMeta, defaultStr string) error {
	fv := parent.FieldByIndex(fm.index)
	if !fv.CanSet() {
		return fmt.Errorf("cannot set default on unaddressable field %q", fm.name)
	}

	for fv.Kind() == reflect.Ptr {
		if fv.IsNil() {
			fv.Set(reflect.New(fv.Type().Elem()))
		}
		fv = fv.Elem()
	}

	switch fv.Kind() {
	case reflect.Bool:
		b, err := strconv.ParseBool(defaultStr)
		if err != nil {
			return fmt.Errorf("invalid bool default for field %q: %w", fm.name, err)
		}
		fv.SetBool(b)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if fv.Type() == timeDurationType {
			d, err := time.ParseDuration(defaultStr)
			if err != nil {
				return fmt.Errorf("invalid duration default for field %q: %w", fm.name, err)
			}
			fv.SetInt(int64(d))
			return nil
		}
		i, err := strconv.ParseInt(defaultStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid int default for field %q: %w", fm.name, err)
		}
		fv.SetInt(i)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(defaultStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid uint default for field %q: %w", fm.name, err)
		}
		fv.SetUint(u)

	case reflect.Float32, reflect.Float64:
		fl, err := strconv.ParseFloat(defaultStr, 64)
		if err != nil {
			return fmt.Errorf("invalid float default for field %q: %w", fm.name, err)
		}
		fv.SetFloat(fl)

	case reflect.String:
		fv.SetString(defaultStr)

	case reflect.Slice:
		if fv.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("unsupported slice type %s for default on %q", fv.Type(), fm.name)
		}
		parts := strings.Split(defaultStr, ",")
		items := make([]string, 0, len(parts))
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			items = append(items, trimmed)
		}
		fv.Set(reflect.ValueOf(items))

	default:
		return fmt.Errorf("unsupported kind %s for default on %q", fv.Kind(), fm.name)
	}
	return nil
}
