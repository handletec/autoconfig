# Golang auto-config library

This library populates environment variables a structure without having to repeat boilerplate code.

# Details

When creating a `struct`, add the tags below to read the environment variables and populate the struct values. Default values can also be specified if no values are found. There are 2 tags that **MUST** exist for auto-populating are `config` and `mapstructure`. The `mapstructure` tag is used by `Viper` itself to populate the values read from the environment variable name whereas the `config` tag sets the rules for the values.

The name in the `mapstructure` tag represents the environment variable name to be read.

The possible parameters for the `config` config tag are

| name | description | 
| :-- | :-- |
| `default` | default value to use if no environment variable value exists. |
| `required` | an environment variable value **MUST** be provided. |
| `struct` | if the variable type is a `struct`, use this tag, which allows it to delve into to populate the fields. |


## Example

```go

const (
	// EnvPrefix - environment variable prefix
	EnvPrefix = "TRAEFIK_ADGUARDHOME"
)

// AppConfig - application configuration from config file or environment variables
type AppConfig struct {

		Logger struct {
			Format      string `yaml:"format" json:"format" mapstructure:"LOGGER_FORMAT" config:"default=kv"`                    // 'kv' or 'json'
			MinLevel    string `yaml:"minLevel" json:"minLevel" mapstructure:"LOGGER_MIN_LEVEL" config:"default=info"`           // minimum level of logging - supported values are 'debug', 'info', 'warn', 'error'
			Output      string `yaml:"output" json:"output" mapstructure:"LOGGER_OUTPUT" config:"default=stdout"`                // output for logging - 'stdout' or 'file'
			Destination string `yaml:"destination" json:"destination" mapstructure:"LOGGER_DESTINATION" config:"default=stdout"` // destination for logging, only for 'file'		
            handler  *slog.Logger
		} `yaml:"logger" json:"logger" config:"struct"`


	ServerIP string `yaml:"server_ip" json:"server_ip" mapstructure:"SERVER_IP" config:"required"`

	Docker struct {
		Address string `yaml:"value" json:"value" mapstructure:"DOCKER_ADDRESS" config:"default=unix:///var/run/docker.sock"`
	} `yaml:"docker" json:"docker" config:"struct"`

	AdguardHome struct {
		Address  string `yaml:"address" json:"address" mapstructure:"ADDRESS" config:"required"`
		Username string `yaml:"value" json:"value" mapstructure:"USERNAME" config:"required"`
		Password string `yaml:"file" json:"file" mapstructure:"PASSWORD" config:"required"`
	} `yaml:"adguardhome" json:"adguardhome" config:"struct"`
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

cfg := autoconfig.New() // create a new instance of `autoconfig`
appConfig = new(AppConfig) // create new instance of `AppConfig`

cfg.SetEnvPrefix(EnvPrefix) // pass the base environment variable for `Viper`. This helps distinguish variable names for specific applications
err := cfg.ReadEnv(appConfig) // read the environment variables and populates the given structure
cobra.CheckErr(err) // check for any error that occured

err = cfg.Check(appConfig) // check the rules specified in the `config` tag
cobra.CheckErr(err) // check for any error that occured

err = appConfig.Setup() // run any custom checks
cobra.CheckErr(err) // check for any error that occured

```
