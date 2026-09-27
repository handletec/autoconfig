package autoconfig

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

// --- _FILE (secrets-from-file) fixtures ---------------------------------

// fileSecretConfig exercises the basic _FILE resolution paths: an optional
// string secret, a required string secret, and a string with a default,
// alongside a plain non-string field to prove non-string fields are
// unaffected by any of this.
type fileSecretConfig struct {
	Secret        string  `mapstructure:"SECRET"`
	RequiredValue string  `mapstructure:"REQUIRED_VALUE" config:"required"`
	WithDefault   string  `mapstructure:"WITH_DEFAULT" config:"default=fallback-value"`
	Port          int     `mapstructure:"PORT" config:"default=8080"`
	PointerSecret *string `mapstructure:"POINTER_SECRET"`
}

// fileSecretExcludedConfig exercises config:"-" excluding both the direct
// env var and its _FILE sibling.
type fileSecretExcludedConfig struct {
	Internal string `mapstructure:"INTERNAL" config:"-"`
}

// fileSecretNestedConfig exercises _FILE through a config:"struct" nested
// field, matching the documented flat OS env-var naming convention.
type fileSecretNestedSecret struct {
	APIKey string `mapstructure:"API_KEY"`
}

type fileSecretNestedConfig struct {
	Secret fileSecretNestedSecret `config:"struct"`
}

// repeatedReadEnvConfig is a minimal single-field fixture for the
// repeated-ReadEnv persistent-state regression test.
type repeatedReadEnvConfig struct {
	Secret string `mapstructure:"SECRET"`
}

// --- Nested env-binding regression fixtures -----------------------------
//
// bindEnvStruct previously bound every field's env var using only its leaf
// mapstructure tag (c.v.BindEnv(fm.mapTag)), while presence tracking used
// the hierarchical Go field path. Viper's Unmarshal decodes from a nested
// map built by splitting bound keys on its key delimiter, so a flat leaf
// key registered for a nested field never lined up with the struct's
// nesting: the env var was detected (so presence was recorded and the
// configured default was skipped), but the decoded value never reached the
// nested field, which stayed at its Go zero value. These fixtures exercise
// realistic nested listener/logging/TLS/secret style config shapes to
// cover that fix generally, without any field-name-specific handling in
// the library itself.

type nestedListenerConfig struct {
	Host string `mapstructure:"LISTENER_HOST" config:"default=0.0.0.0"`
	Port int    `mapstructure:"LISTENER_PORT" config:"default=8080"`
}

type nestedLoggingConfig struct {
	Level   string `mapstructure:"LOG_LEVEL" config:"default=info"`
	Verbose bool   `mapstructure:"LOG_VERBOSE" config:"default=false"`
}

type nestedTLSConfig struct {
	Enabled  bool   `mapstructure:"TLS_ENABLED" config:"default=false"`
	CertFile string `mapstructure:"TLS_CERT_FILE"`
}

type nestedSecretConfig struct {
	APIKey string `mapstructure:"SECRET_API_KEY"`
}

type nestedServiceConfig struct {
	Listener nestedListenerConfig `config:"struct"`
	Logging  nestedLoggingConfig  `config:"struct"`
	TLS      nestedTLSConfig      `config:"struct"`
	Secret   nestedSecretConfig   `config:"struct"`
}

// Two levels of config:"struct" recursion, to cover deeper/recursive nesting.
type deepInnerConfig struct {
	Value string `mapstructure:"DEEP_INNER_VALUE" config:"default=inner-default"`
}

type deepMiddleConfig struct {
	Inner deepInnerConfig `config:"struct"`
}

type deepOuterConfig struct {
	Middle deepMiddleConfig `config:"struct"`
}

// Two different nested structs deliberately reusing the identical literal
// mapstructure leaf tag "HOST". Both therefore read from the same flat
// PREFIX_HOST OS environment variable (see README "Known limitations" —
// this is the documented, unchanged env-var-naming contract). The defect
// under test is not the shared OS env var name; it is that the previous
// flat-key BindEnv implementation used the identical internal Viper key
// "host" for both fields, which could corrupt presence tracking and value
// decoding across the two sibling struct fields.
type sharedLeafNestedA struct {
	Host string `mapstructure:"HOST" config:"default=a-default"`
}

type sharedLeafNestedB struct {
	Host string `mapstructure:"HOST" config:"default=b-default"`
}

type sharedLeafAppConfig struct {
	ServiceA sharedLeafNestedA `config:"struct"`
	ServiceB sharedLeafNestedB `config:"struct"`
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

func TestNestedStringFieldEnvOverride(t *testing.T) {
	t.Setenv("NESTSTR_LISTENER_HOST", "10.0.0.5")
	c := New("NESTSTR")
	app := new(nestedServiceConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Listener.Host != "10.0.0.5" {
		t.Fatalf("expected nested string env override 10.0.0.5, got %q", app.Listener.Host)
	}
}

func TestNestedIntFieldEnvOverride(t *testing.T) {
	t.Setenv("NESTINT_LISTENER_PORT", "9443")
	c := New("NESTINT")
	app := new(nestedServiceConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Listener.Port != 9443 {
		t.Fatalf("expected nested int env override 9443, got %d", app.Listener.Port)
	}
}

func TestNestedBoolFieldEnvOverride(t *testing.T) {
	t.Setenv("NESTBOOL_TLS_ENABLED", "true")
	c := New("NESTBOOL")
	app := new(nestedServiceConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if !app.TLS.Enabled {
		t.Fatalf("expected nested bool env override true, got false")
	}
}

func TestNestedSecretFieldEnvOverride(t *testing.T) {
	t.Setenv("NESTSECRET_SECRET_API_KEY", "sk-test-1234")
	c := New("NESTSECRET")
	app := new(nestedServiceConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Secret.APIKey != "sk-test-1234" {
		t.Fatalf("expected nested secret env override, got %q", app.Secret.APIKey)
	}
}

func TestNestedDefaultAppliedWhenEnvAbsent(t *testing.T) {
	c := New("NESTDEF")
	app := new(nestedServiceConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Listener.Host != "0.0.0.0" {
		t.Fatalf("expected nested default 0.0.0.0, got %q", app.Listener.Host)
	}
	if app.Listener.Port != 8080 {
		t.Fatalf("expected nested default 8080, got %d", app.Listener.Port)
	}
	if app.Logging.Level != "info" {
		t.Fatalf("expected nested default info, got %q", app.Logging.Level)
	}
}

func TestNestedDefaultOverriddenByEnv(t *testing.T) {
	t.Setenv("NESTDEFOVR_LOG_LEVEL", "debug")
	c := New("NESTDEFOVR")
	app := new(nestedServiceConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Logging.Level != "debug" {
		t.Fatalf("expected env override debug, got %q", app.Logging.Level)
	}
}

func TestTwoNestedStructsWithIdenticalLeafFieldNameDoNotCollide(t *testing.T) {
	t.Setenv("SHAREDLEAF_HOST", "shared-value")
	c := New("SHAREDLEAF")
	app := new(sharedLeafAppConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.ServiceA.Host != "shared-value" {
		t.Fatalf("expected ServiceA.Host=shared-value, got %q", app.ServiceA.Host)
	}
	if app.ServiceB.Host != "shared-value" {
		t.Fatalf("expected ServiceB.Host=shared-value, got %q", app.ServiceB.Host)
	}
}

func TestTwoNestedStructsWithIdenticalLeafFieldNameApplyIndependentDefaultsWhenUnset(t *testing.T) {
	c := New("SHAREDLEAF2")
	app := new(sharedLeafAppConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.ServiceA.Host != "a-default" {
		t.Fatalf("expected ServiceA.Host default a-default, got %q", app.ServiceA.Host)
	}
	if app.ServiceB.Host != "b-default" {
		t.Fatalf("expected ServiceB.Host default b-default, got %q", app.ServiceB.Host)
	}
}

func TestDeeplyNestedFieldEnvOverride(t *testing.T) {
	t.Setenv("DEEPNEST_DEEP_INNER_VALUE", "deep-env-value")
	c := New("DEEPNEST")
	app := new(deepOuterConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Middle.Inner.Value != "deep-env-value" {
		t.Fatalf("expected deep-env-value, got %q", app.Middle.Inner.Value)
	}
}

func TestDeeplyNestedFieldDefaultWhenEnvAbsent(t *testing.T) {
	c := New("DEEPNEST2")
	app := new(deepOuterConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Middle.Inner.Value != "inner-default" {
		t.Fatalf("expected inner-default, got %q", app.Middle.Inner.Value)
	}
}

// TestNestedEnvValuePreservedNotOverwrittenByDefault directly reproduces the
// previously-confirmed failure mode: a nested field has an env value set,
// ReadEnv must populate the nested struct correctly, and Check must not
// overwrite the env-supplied value with the field's configured default.
func TestNestedEnvValuePreservedNotOverwrittenByDefault(t *testing.T) {
	t.Setenv("NESTPRESERVE_LISTENER_HOST", "override-host")
	c := New("NESTPRESERVE")
	app := new(nestedServiceConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if app.Listener.Host != "override-host" {
		t.Fatalf("ReadEnv did not populate nested field from env: got %q", app.Listener.Host)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Listener.Host != "override-host" {
		t.Fatalf("Check overwrote env-supplied nested value with default: got %q", app.Listener.Host)
	}
}

// TestNestedEnvAbsentAppliesDefaultInverse proves the inverse of the above:
// when a nested field has no env value, ReadEnv must leave it unset (zero
// value) and Check must apply the configured default.
func TestNestedEnvAbsentAppliesDefaultInverse(t *testing.T) {
	c := New("NESTINVERSE")
	app := new(nestedServiceConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if app.Listener.Host != "" {
		t.Fatalf("expected ReadEnv to leave nested field unset (zero) when env absent, got %q", app.Listener.Host)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Listener.Host != "0.0.0.0" {
		t.Fatalf("expected Check to apply configured default 0.0.0.0, got %q", app.Listener.Host)
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

// --- _FILE (secrets-from-file) regression tests -------------------------

// writeSecretFile writes content to a new file inside a fresh t.TempDir()
// (which honors $TMPDIR) and returns its path, for use as a _FILE target.
func writeSecretFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	return path
}

func TestFileEnvVarPopulatesStringField(t *testing.T) {
	path := writeSecretFile(t, "super-secret")
	t.Setenv("FILEBASIC_SECRET_FILE", path)
	t.Setenv("FILEBASIC_REQUIRED_VALUE", "req-ok")

	c := New("FILEBASIC")
	app := new(fileSecretConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Secret != "super-secret" {
		t.Fatalf("expected super-secret, got %q", app.Secret)
	}
}

func TestFileEnvVarSatisfiesRequired(t *testing.T) {
	path := writeSecretFile(t, "req-secret")
	t.Setenv("FILEREQ_REQUIRED_VALUE_FILE", path)

	c := New("FILEREQ")
	app := new(fileSecretConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed (required should be satisfied by _FILE): %v", err)
	}
	if app.RequiredValue != "req-secret" {
		t.Fatalf("expected req-secret, got %q", app.RequiredValue)
	}
}

func TestFileEnvVarOverridesFileConfigValue(t *testing.T) {
	dir := t.TempDir()
	cfg := New("FILEOVERRIDE")
	if err := cfg.Create("app", "config", dir, ConfigTypeYAML); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	content := "address: file-address\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	path := writeSecretFile(t, "file-secret-address")
	t.Setenv("FILEOVERRIDE_ADDRESS_FILE", path)

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
	if app.Address != "file-secret-address" {
		t.Fatalf("expected _FILE value to override file config value, got %q", app.Address)
	}
}

func TestFileEnvVarPreventsDefaultOverride(t *testing.T) {
	path := writeSecretFile(t, "custom-value")
	t.Setenv("FILEDEFAULT_WITH_DEFAULT_FILE", path)
	t.Setenv("FILEDEFAULT_REQUIRED_VALUE", "req-ok")

	c := New("FILEDEFAULT")
	app := new(fileSecretConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.WithDefault != "custom-value" {
		t.Fatalf("expected _FILE value to survive default application, got %q", app.WithDefault)
	}
}

func TestDirectEnvAndFileEnvVarConflict(t *testing.T) {
	path := writeSecretFile(t, "file-secret")
	t.Setenv("FILECONFLICT_SECRET", "direct-secret")
	t.Setenv("FILECONFLICT_SECRET_FILE", path)

	c := New("FILECONFLICT")
	app := new(fileSecretConfig)
	err := c.ReadEnv(app)
	if err == nil {
		t.Fatalf("expected conflict error, got none")
	}
	msg := err.Error()
	if !strings.Contains(msg, "FILECONFLICT_SECRET_FILE") || !strings.Contains(msg, "FILECONFLICT_SECRET") {
		t.Fatalf("expected error to name both conflicting env vars, got: %v", err)
	}
	if strings.Contains(msg, "direct-secret") || strings.Contains(msg, "file-secret") {
		t.Fatalf("error must never contain secret values, got: %v", err)
	}
}

func TestEmptyDirectEnvUsesFileEnvVar(t *testing.T) {
	path := writeSecretFile(t, "from-file")
	t.Setenv("FILEEMPTYDIRECT_SECRET", "")
	t.Setenv("FILEEMPTYDIRECT_SECRET_FILE", path)
	t.Setenv("FILEEMPTYDIRECT_REQUIRED_VALUE", "req-ok")

	c := New("FILEEMPTYDIRECT")
	app := new(fileSecretConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Secret != "from-file" {
		t.Fatalf("expected from-file (empty direct env is absent), got %q", app.Secret)
	}
}

func TestEmptyFileEnvVarTreatedAsAbsent(t *testing.T) {
	t.Setenv("FILEEMPTYFILEVAR_SECRET_FILE", "")
	t.Setenv("FILEEMPTYFILEVAR_REQUIRED_VALUE", "req-ok")

	c := New("FILEEMPTYFILEVAR")
	app := new(fileSecretConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Secret != "" {
		t.Fatalf("expected empty Secret (empty _FILE is absent), got %q", app.Secret)
	}
}

func TestFileEnvVarMissingFileReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist")
	t.Setenv("FILEMISSING_SECRET_FILE", path)

	c := New("FILEMISSING")
	app := new(fileSecretConfig)
	err := c.ReadEnv(app)
	if err == nil {
		t.Fatalf("expected error for missing _FILE target, got none")
	}
	msg := err.Error()
	if !strings.Contains(msg, "FILEMISSING_SECRET_FILE") || !strings.Contains(msg, path) {
		t.Fatalf("expected error to mention env var and path, got: %v", err)
	}
}

func TestFileEnvVarUnreadableFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	// A directory cannot be read as a file; os.ReadFile fails portably
	// without needing platform-specific permission bit manipulation.
	subdir := filepath.Join(dir, "not-a-file")
	if err := os.Mkdir(subdir, 0o700); err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}
	t.Setenv("FILEUNREADABLE_SECRET_FILE", subdir)

	c := New("FILEUNREADABLE")
	app := new(fileSecretConfig)
	err := c.ReadEnv(app)
	if err == nil {
		t.Fatalf("expected error for unreadable _FILE target, got none")
	}
	if !strings.Contains(err.Error(), "FILEUNREADABLE_SECRET_FILE") {
		t.Fatalf("expected error to mention env var, got: %v", err)
	}
}

func TestFileEnvVarTrailingLFStripped(t *testing.T) {
	path := writeSecretFile(t, "secret\n")
	t.Setenv("FILELFSTRIP_SECRET_FILE", path)
	t.Setenv("FILELFSTRIP_REQUIRED_VALUE", "req-ok")

	c := New("FILELFSTRIP")
	app := new(fileSecretConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if app.Secret != "secret" {
		t.Fatalf("expected trailing LF stripped, got %q", app.Secret)
	}
}

func TestFileEnvVarTrailingCRLFStripped(t *testing.T) {
	path := writeSecretFile(t, "secret\r\n")
	t.Setenv("FILECRLFSTRIP_SECRET_FILE", path)
	t.Setenv("FILECRLFSTRIP_REQUIRED_VALUE", "req-ok")

	c := New("FILECRLFSTRIP")
	app := new(fileSecretConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if app.Secret != "secret" {
		t.Fatalf("expected trailing CRLF stripped, got %q", app.Secret)
	}
}

func TestFileEnvVarSurroundingSpacesPreserved(t *testing.T) {
	path := writeSecretFile(t, " secret \n")
	t.Setenv("FILESPACES_SECRET_FILE", path)
	t.Setenv("FILESPACES_REQUIRED_VALUE", "req-ok")

	c := New("FILESPACES")
	app := new(fileSecretConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if app.Secret != " secret " {
		t.Fatalf("expected surrounding spaces preserved, got %q", app.Secret)
	}
}

func TestFileEnvVarInternalNewlinesPreserved(t *testing.T) {
	path := writeSecretFile(t, "line1\nline2")
	t.Setenv("FILEINTERNALNL_SECRET_FILE", path)
	t.Setenv("FILEINTERNALNL_REQUIRED_VALUE", "req-ok")

	c := New("FILEINTERNALNL")
	app := new(fileSecretConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if app.Secret != "line1\nline2" {
		t.Fatalf("expected internal newline preserved, got %q", app.Secret)
	}
}

func TestFileEnvVarEmptyFileTreatedAsAbsent(t *testing.T) {
	path := writeSecretFile(t, "")
	t.Setenv("FILEEMPTYCONTENT_WITH_DEFAULT_FILE", path)
	t.Setenv("FILEEMPTYCONTENT_REQUIRED_VALUE", "req-ok")

	c := New("FILEEMPTYCONTENT")
	app := new(fileSecretConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.WithDefault != "fallback-value" {
		t.Fatalf("expected default to apply for empty _FILE content, got %q", app.WithDefault)
	}
}

func TestFileEnvVarContentOnlyLFTreatedAsAbsent(t *testing.T) {
	path := writeSecretFile(t, "\n")
	t.Setenv("FILEONLYLF_WITH_DEFAULT_FILE", path)
	t.Setenv("FILEONLYLF_REQUIRED_VALUE", "req-ok")

	c := New("FILEONLYLF")
	app := new(fileSecretConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.WithDefault != "fallback-value" {
		t.Fatalf("expected default to apply for LF-only _FILE content, got %q", app.WithDefault)
	}
}

func TestFileEnvVarContentOnlyCRLFTreatedAsAbsent(t *testing.T) {
	path := writeSecretFile(t, "\r\n")
	t.Setenv("FILEONLYCRLF_WITH_DEFAULT_FILE", path)
	t.Setenv("FILEONLYCRLF_REQUIRED_VALUE", "req-ok")

	c := New("FILEONLYCRLF")
	app := new(fileSecretConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.WithDefault != "fallback-value" {
		t.Fatalf("expected default to apply for CRLF-only _FILE content, got %q", app.WithDefault)
	}
}

func TestFileEnvVarEmptyAfterTrimFailsRequiredValidation(t *testing.T) {
	path := writeSecretFile(t, "\n")
	t.Setenv("FILEREQEMPTY_REQUIRED_VALUE_FILE", path)

	c := New("FILEREQEMPTY")
	app := new(fileSecretConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed (empty-after-trim _FILE must not itself error): %v", err)
	}
	if err := c.Check(app); err == nil {
		t.Fatalf("expected Check to fail: required field with no default and empty-after-trim _FILE must be treated as absent")
	}
}

func TestConfigDashExcludesFileEnvVarToo(t *testing.T) {
	path := writeSecretFile(t, "leak")
	t.Setenv("FILEEXCLUDED_INTERNAL_FILE", path)

	c := New("FILEEXCLUDED")
	app := new(fileSecretExcludedConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if err := c.Check(app); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if app.Internal != "" {
		t.Fatalf("expected config:\"-\" to exclude the _FILE sibling too, got %q", app.Internal)
	}
}

func TestNestedFileEnvVarPopulatesStringField(t *testing.T) {
	path := writeSecretFile(t, "sk-nested-secret")
	t.Setenv("FILENESTED_API_KEY_FILE", path)

	c := New("FILENESTED")
	app := new(fileSecretNestedConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if app.Secret.APIKey != "sk-nested-secret" {
		t.Fatalf("expected nested _FILE value, got %q", app.Secret.APIKey)
	}
}

func TestFileEnvVarOnNonStringFieldReturnsUnsupportedTypeError(t *testing.T) {
	path := writeSecretFile(t, "9090")
	t.Setenv("FILEUNSUPPORTED_PORT_FILE", path)

	c := New("FILEUNSUPPORTED")
	app := new(fileSecretConfig)
	err := c.ReadEnv(app)
	if err == nil {
		t.Fatalf("expected unsupported-type error for _FILE on an int field, got none")
	}
	if !strings.Contains(err.Error(), "FILEUNSUPPORTED_PORT_FILE") {
		t.Fatalf("expected error to name the _FILE env var, got: %v", err)
	}
}

func TestFileEnvVarOnPointerStringFieldReturnsUnsupportedTypeError(t *testing.T) {
	// _FILE is intentionally scoped to bare (non-pointer) string fields only.
	// A *string field is rejected the same way as any other unsupported
	// kind rather than being speculatively dereferenced/allocated.
	path := writeSecretFile(t, "sk-pointer-secret")
	t.Setenv("FILEPOINTERUNSUPPORTED_POINTER_SECRET_FILE", path)

	c := New("FILEPOINTERUNSUPPORTED")
	app := new(fileSecretConfig)
	err := c.ReadEnv(app)
	if err == nil {
		t.Fatalf("expected unsupported-type error for _FILE on a *string field, got none")
	}
	if !strings.Contains(err.Error(), "FILEPOINTERUNSUPPORTED_POINTER_SECRET_FILE") {
		t.Fatalf("expected error to name the _FILE env var, got: %v", err)
	}
	if app.PointerSecret != nil {
		t.Fatalf("expected PointerSecret to remain nil (no speculative allocation), got %v", *app.PointerSecret)
	}
}

func TestFileValueIsNotCopiedIntoEnvironment(t *testing.T) {
	path := writeSecretFile(t, "hidden-secret")
	t.Setenv("FILENOLEAK_SECRET_FILE", path)
	t.Setenv("FILENOLEAK_REQUIRED_VALUE", "req-ok")

	c := New("FILENOLEAK")
	app := new(fileSecretConfig)
	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("ReadEnv failed: %v", err)
	}
	if app.Secret != "hidden-secret" {
		t.Fatalf("expected hidden-secret, got %q", app.Secret)
	}

	if _, ok := os.LookupEnv("FILENOLEAK_SECRET"); ok {
		t.Fatalf("_FILE contents must never be copied into the sibling environment variable")
	}
}

func TestRepeatedReadEnvDoesNotLeakFileBackedValue(t *testing.T) {
	alphaPath := writeSecretFile(t, "alpha")

	const fileEnvName = "REPEATTEST_SECRET_FILE"
	const directEnvName = "REPEATTEST_SECRET"

	t.Setenv(fileEnvName, alphaPath)

	c := New("REPEATTEST")
	app := new(repeatedReadEnvConfig)

	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("first ReadEnv failed: %v", err)
	}
	if app.Secret != "alpha" {
		t.Fatalf("expected alpha from _FILE on first ReadEnv, got %q", app.Secret)
	}

	// t.Setenv cannot unset a variable mid-test, so os.Unsetenv is used
	// directly here to simulate the secret file being removed before a
	// second ReadEnv call on the SAME Config/struct. Prior state is
	// captured and restored via t.Cleanup so this does not leak into any
	// other test.
	priorFileVal, priorFileSet := os.LookupEnv(fileEnvName)
	if err := os.Unsetenv(fileEnvName); err != nil {
		t.Fatalf("Unsetenv failed: %v", err)
	}
	t.Cleanup(func() {
		if priorFileSet {
			_ = os.Setenv(fileEnvName, priorFileVal)
		} else {
			_ = os.Unsetenv(fileEnvName)
		}
	})

	t.Setenv(directEnvName, "bravo")

	if err := c.ReadEnv(app); err != nil {
		t.Fatalf("second ReadEnv failed: %v", err)
	}
	if app.Secret != "bravo" {
		t.Fatalf("expected bravo on second ReadEnv (no leaked _FILE value from the first call), got %q", app.Secret)
	}
}
