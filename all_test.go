package autoconfig_test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/handletec/autoconfig"
)

const EnvPrefix = "AUTOCONFIG"

type AppConfig struct {
	Address string   `mapstructure:"ADDRESS" config:"default=127.0.0.1"`
	Port    int      `mapstructure:"PORT" config:"default=8000"`
	Origin  []string `mapstructure:"ORIGIN"`
	Enabled bool     `mapstructure:"ENABLED"`
}

func (appConfig *AppConfig) String() (str string) {
	bytes, _ := json.Marshal(appConfig)
	return string(bytes)
}

// Setup - set values for fields that depend on other values
func (appConfig *AppConfig) Setup() (err error) {
	// do any custom checks
	return
}

func TestAutoConfig(t *testing.T) {

	t.Setenv(EnvPrefix+"_ADDRESS", "::1")
	t.Setenv(EnvPrefix+"_PORT", "9000")
	t.Setenv(EnvPrefix+"_ORIGIN", "localhost, ::1, 127.0.0.1")
	t.Setenv(EnvPrefix+"_ENABLED", "true")

	cfg := autoconfig.New(EnvPrefix) // create a new instance of `autoconfig`
	appConfig := new(AppConfig)      // create new instance of `AppConfig`

	//cfg.SetEnvPrefix(EnvPrefix)   // pass the base environment variable for `Viper`. This helps distinguish variable names for specific applications
	err := cfg.ReadEnv(appConfig) // read the environment variables and populates the given structure
	if nil != err {
		fmt.Println(err)
		os.Exit(1)
	}

	err = cfg.Check(appConfig) // check the rules specified in the `config` tag
	if nil != err {
		fmt.Println(err)
		os.Exit(1)
	}

	err = appConfig.Setup() // run any custom checks
	if nil != err {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Println(appConfig)
}
