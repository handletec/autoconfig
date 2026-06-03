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
