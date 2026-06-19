package autoconfig

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/spf13/viper"
)

const testEnvPrefix = "AUTOCONFIG"

type envNestedConfig struct {
	Enabled bool `mapstructure:"ENABLED" config:"required"`
}

type envAppConfig struct {
	Address  string          `mapstructure:"ADDRESS" config:"default=127.0.0.1"`
	Port     int             `mapstructure:"PORT" config:"default=8000"`
	Origin   []string        `mapstructure:"ORIGIN" config:"default=localhost,127.0.0.1"`
	Enabled  bool            `mapstructure:"ENABLED" config:"required"`
	Timeout  time.Duration   `mapstructure:"TIMEOUT" config:"default=5s"`
	Features envNestedConfig `config:"struct,required"`
}

type fileNestedConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled" mapstructure:"ENABLED" config:"required"`
}

type fileAppConfig struct {
	Address  string           `yaml:"address" json:"address" mapstructure:"ADDRESS" config:"required"`
	Enabled  bool             `yaml:"enabled" json:"enabled" mapstructure:"ENABLED" config:"required"`
	Features fileNestedConfig `yaml:"features" json:"features" config:"struct,required"`
}

type isolatedConfig struct {
	Value string `mapstructure:"VALUE" config:"required"`
}

// optionalEnvConfig has a field with mapstructure but no config tag (optional).
type optionalEnvConfig struct {
	Host string `mapstructure:"HOST"`
	Port int    `mapstructure:"PORT" config:"default=8080"`
}

// excludedFieldConfig tests config:"-" explicit exclusion.
type excludedFieldConfig struct {
	Host     string `mapstructure:"HOST"`
	Internal string `mapstructure:"INTERNAL" config:"-"`
}

// noMapTagEnvConfig tests that a field without mapstructure is not env-bound.
type noMapTagEnvConfig struct {
	Bound   string `mapstructure:"BOUND" config:"required"`
	Unbound string `yaml:"unbound" config:"default=fallback"`
}

// envOverridesDefaultConfig tests env overriding a declared default.
type envOverridesDefaultConfig struct {
	Host string `mapstructure:"HOST" config:"default=localhost"`
}

// envOverridesFileConfig tests env overriding a file-supplied value.
type envOverridesFileConfig struct {
	Address string `yaml:"address" json:"address" mapstructure:"ADDRESS" config:"required"`
}

// emptyEnvVarConfig is used to document empty-string env var behaviour.
type emptyEnvVarConfig struct {
	Host string `mapstructure:"HOST" config:"default=localhost"`
}

// requiredBeforeDefaultConfig tests config:"required,default=..." (valid order).
type requiredBeforeDefaultConfig struct {
	Origins []string `mapstructure:"ORIGINS" config:"required,default=localhost,127.0.0.1"`
}

// invalidDefaultOrderConfig tests that config:"default=...,required" is rejected.
type invalidDefaultOrderConfig struct {
	Host string `mapstructure:"HOST" config:"default=localhost,required"`
}

// timeSupportConfig documents which scalar time types are env-bindable.
type timeSupportConfig struct {
	Timeout   time.Duration `mapstructure:"TIMEOUT"`
	StartTime time.Time     `mapstructure:"START_TIME"`
}

func TestReadEnvAppliesDefaultsAndSupportsExplicitZeroValues(t *testing.T) {
	t.Setenv(testEnvPrefix+"_ENABLED", "false")
	t.Setenv(testEnvPrefix+"_FEATURES_ENABLED", "false")
	t.Setenv(testEnvPrefix+"_ORIGIN", "localhost, ::1, 127.0.0.1")

	cfg := New(testEnvPrefix)
	app := new(envAppConfig)

	if err := cfg.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := cfg.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	if app.Address != "127.0.0.1" {
		t.Fatalf("unexpected default address: %q", app.Address)
	}
	if app.Port != 8000 {
		t.Fatalf("unexpected default port: %d", app.Port)
	}
	if !reflect.DeepEqual(app.Origin, []string{"localhost", "::1", "127.0.0.1"}) {
		t.Fatalf("unexpected origin slice: %#v", app.Origin)
	}
	if app.Enabled {
		t.Fatalf("expected explicit false env value to be preserved")
	}
	if app.Features.Enabled {
		t.Fatalf("expected explicit false nested env value to be preserved")
	}
	if app.Timeout != 5*time.Second {
		t.Fatalf("unexpected default timeout: %s", app.Timeout)
	}
}

func TestInstancesRemainIsolated(t *testing.T) {
	t.Setenv("APPA_VALUE", "alpha")
	t.Setenv("APPB_VALUE", "bravo")

	cfgA := New("APPA")
	cfgB := New("APPB")

	a := new(isolatedConfig)
	b := new(isolatedConfig)

	if err := cfgA.ReadEnv(a); err != nil {
		t.Fatalf("cfgA.ReadEnv failed: %v", err)
	}
	if err := cfgB.ReadEnv(b); err != nil {
		t.Fatalf("cfgB.ReadEnv failed: %v", err)
	}
	if err := cfgA.Check(a); err != nil {
		t.Fatalf("cfgA.Check failed: %v", err)
	}
	if err := cfgB.Check(b); err != nil {
		t.Fatalf("cfgB.Check failed: %v", err)
	}

	if a.Value != "alpha" {
		t.Fatalf("cfgA loaded %q, expected alpha", a.Value)
	}
	if b.Value != "bravo" {
		t.Fatalf("cfgB loaded %q, expected bravo", b.Value)
	}
}

func TestCreateSanitizesDefaultDirectoryWithinHome(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	cfg := New("APP")
	if err := cfg.Create("../../Escape App", "../Config Name", "", ConfigTypeYAML); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if !isWithinBaseDir(homeDir, cfg.dirname) {
		t.Fatalf("config dir escaped home: home=%q dir=%q", homeDir, cfg.dirname)
	}
	if cfg.project != ".escape-app" {
		t.Fatalf("unexpected sanitized project: %q", cfg.project)
	}
	if cfg.cfgBaseName != "config-name" {
		t.Fatalf("unexpected sanitized config name: %q", cfg.cfgBaseName)
	}
	if info, err := os.Stat(cfg.dirname); err != nil || !info.IsDir() {
		t.Fatalf("config directory was not created correctly: %v", err)
	}
}

func TestReadFileRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	cfg := New("APP")
	if err := cfg.Create("app", "config", dir, ConfigTypeYAML); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	content := "address: 127.0.0.1\nenabled: false\nfeatures:\n  enabled: false\nunknown_key: nope\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	app := new(fileAppConfig)
	err := cfg.ReadFile(app)
	if err == nil {
		t.Fatalf("ReadFile succeeded unexpectedly")
	}
}

func TestReadFilePreservesExplicitFalseValues(t *testing.T) {
	dir := t.TempDir()
	cfg := New("APP")
	if err := cfg.Create("app", "config", dir, ConfigTypeYAML); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	content := "address: 127.0.0.1\nenabled: false\nfeatures:\n  enabled: false\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	app := new(fileAppConfig)
	if err := cfg.ReadFile(app); err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if err := cfg.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Enabled {
		t.Fatalf("expected explicit false file value to be preserved")
	}
	if app.Features.Enabled {
		t.Fatalf("expected explicit nested false file value to be preserved")
	}
}

func TestOptionalFieldWithoutConfigTagIsPopulatedFromEnv(t *testing.T) {
	t.Setenv("OPTTEST_HOST", "db.example.com")
	c := New("OPTTEST")
	app := new(optionalEnvConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Host != "db.example.com" {
		t.Fatalf("expected db.example.com, got %q", app.Host)
	}
	if app.Port != 8080 {
		t.Fatalf("expected default port 8080, got %d", app.Port)
	}
}

func TestOptionalFieldWithoutConfigTagStaysZeroWhenEnvAbsent(t *testing.T) {
	c := New("OPTTEST2")
	app := new(optionalEnvConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Host != "" {
		t.Fatalf("expected empty host when env absent, got %q", app.Host)
	}
	if app.Port != 8080 {
		t.Fatalf("expected default port 8080, got %d", app.Port)
	}
}

func TestEnvValueOverridesDefault(t *testing.T) {
	t.Setenv("DEFTEST_HOST", "env-host")
	c := New("DEFTEST")
	app := new(envOverridesDefaultConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Host != "env-host" {
		t.Fatalf("expected env-host (env overrides default), got %q", app.Host)
	}
}

func TestEnvValueOverridesFileValue(t *testing.T) {
	dir := t.TempDir()
	cfg := New("FILETEST")
	if err := cfg.Create("app", "config", dir, ConfigTypeYAML); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	content := "address: file-address\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	t.Setenv("FILETEST_ADDRESS", "env-address")
	app := new(envOverridesFileConfig)
	if err := cfg.ReadFile(app); err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if err := cfg.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := cfg.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Address != "env-address" {
		t.Fatalf("expected env-address (env overrides file), got %q", app.Address)
	}
}

func TestRequiredValidationRunsAfterEnvLoading(t *testing.T) {
	c := New("REQTEST")
	app := new(isolatedConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err == nil {
		t.Fatalf("expected Check to fail for missing required field")
	}
}

func TestConfigDashExcludesFieldFromEnvBinding(t *testing.T) {
	t.Setenv("EXCTEST_HOST", "bound-value")
	t.Setenv("EXCTEST_INTERNAL", "should-be-ignored")
	c := New("EXCTEST")
	app := new(excludedFieldConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Host != "bound-value" {
		t.Fatalf("expected bound-value for Host, got %q", app.Host)
	}
	if app.Internal != "" {
		t.Fatalf("expected empty Internal (excluded by config:\"-\"), got %q", app.Internal)
	}
}

func TestNoMapstructureTagPreventsEnvBinding(t *testing.T) {
	t.Setenv("NOMAPTEST_BOUND", "hello")
	t.Setenv("NOMAPTEST_UNBOUND", "from-env-ignored")
	c := New("NOMAPTEST")
	app := new(noMapTagEnvConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Bound != "hello" {
		t.Fatalf("expected hello for Bound, got %q", app.Bound)
	}
	if app.Unbound != "fallback" {
		t.Fatalf("expected fallback default for Unbound (no mapstructure tag), got %q", app.Unbound)
	}
}

func TestNestedStructRetainsExistingBehaviour(t *testing.T) {
	// Verify that config:"struct" nested fields still participate in required
	// validation: Check must pass when the bound env key is set.
	t.Setenv(testEnvPrefix+"_ENABLED", "false")
	t.Setenv(testEnvPrefix+"_FEATURES_ENABLED", "false")
	c := New(testEnvPrefix)
	app := new(envAppConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed (nested struct required validation broken): %v", err)
	}
}

// TestEmptyEnvVarIsIgnoredAndDefaultApplied documents Viper's default behaviour:
// with AllowEmptyEnv=false (the default), an env var set to an empty string is
// treated as absent and the declared default is applied.
func TestEmptyEnvVarIsIgnoredAndDefaultApplied(t *testing.T) {
	t.Setenv("EMPTYTEST_HOST", "")
	c := New("EMPTYTEST")
	app := new(emptyEnvVarConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Host != "localhost" {
		t.Fatalf("expected default localhost when env is empty string, got %q", app.Host)
	}
}

func TestDefaultTagMustBeFinal_RequiredComesFirstEnvOverride(t *testing.T) {
	t.Setenv("GRAMTEST_ORIGINS", "a.example.com, b.example.com")
	c := New("GRAMTEST")
	app := new(requiredBeforeDefaultConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	want := []string{"a.example.com", "b.example.com"}
	if !reflect.DeepEqual(app.Origins, want) {
		t.Fatalf("expected %v, got %v", want, app.Origins)
	}
}

func TestDefaultTagMustBeFinal_RequiredComesFirstUsesDefault(t *testing.T) {
	c := New("GRAMTEST2")
	app := new(requiredBeforeDefaultConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	want := []string{"localhost", "127.0.0.1"}
	if !reflect.DeepEqual(app.Origins, want) {
		t.Fatalf("expected %v, got %v", want, app.Origins)
	}
}

func TestDefaultTagMustBeFinal_RequiredAfterDefaultIsRejected(t *testing.T) {
	c := New("GRAMTEST3")
	app := new(invalidDefaultOrderConfig)
	err := c.ReadEnv(app)
	if err == nil {
		t.Fatalf("expected error for config:\"default=...,required\" but got none")
	}
}

// TestStructBackedScalarTypesNotEnvBound documents the struct-kind guard:
// time.Duration (int64 kind) is supported; time.Time (struct kind) is not.
func TestStructBackedScalarTypesNotEnvBound(t *testing.T) {
	t.Setenv("STRUCTTEST_TIMEOUT", "10s")
	t.Setenv("STRUCTTEST_START_TIME", "2024-01-01T00:00:00Z")
	c := New("STRUCTTEST")
	app := new(timeSupportConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Timeout != 10*time.Second {
		t.Fatalf("expected 10s for Timeout, got %v", app.Timeout)
	}
	// time.Time is a struct type. The struct-kind guard prevents it from being
	// bound as a scalar env key; it stays at its zero value.
	if !app.StartTime.IsZero() {
		t.Fatalf("expected zero time.Time (struct kind not env-bound), got %v", app.StartTime)
	}
}

func TestReadFileMalformedConfigReturnsSpecificError(t *testing.T) {
	dir := t.TempDir()
	cfg := New("APP")
	if err := cfg.Create("app", "config", dir, ConfigTypeYAML); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	content := "address: [unterminated\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	app := new(fileAppConfig)
	err := cfg.ReadFile(app)
	if err == nil {
		t.Fatalf("ReadFile succeeded unexpectedly")
	}

	var parseErr viper.ConfigParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("expected ConfigParseError, got %T: %v", err, err)
	}
}
