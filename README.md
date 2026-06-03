# Golang auto-config library

`autoconfig` populates a Go struct from environment variables and optional config files with minimal boilerplate.

This version is hardened to avoid shared global state, reject unexpected file keys, and preserve explicit zero values such as `false` and `0` for required fields.

## Key behavior

- Each `Config` instance owns its own Viper instance.
- File decoding is strict: unknown keys cause an error.
- Required checks use presence tracking, not only zero-value checks.
- Defaults are applied only when a value was not provided.
- Nested structs support combined tags such as `config:"struct,required"`.
- Comma-separated environment variables are decoded into `[]string` with whitespace trimming.

The are 2 tags that **MUST** exist for auto-populating are `config` and `mapstructure`. The `mapstructure` tag is used by `Viper` itself to populate the values read from the environment variable name whereas the `config` tag sets the rules for the values.

## Supported `config` options

| option | description |
| :-- | :-- |
| `default=<value>` | default value to use when the field was not provided |
| `required` | the value must be provided unless a default exists |
| `struct` | recurse into a nested struct |

## Example

```go
package main

import (
    "fmt"
    "log"
    "time"

    "github.com/handletec/autoconfig"
)

type AppConfig struct {
    Address string        `yaml:"address" json:"address" mapstructure:"ADDRESS" config:"default=127.0.0.1"`
    Port    int           `yaml:"port" json:"port" mapstructure:"PORT" config:"default=8080"`
    Origin  []string      `yaml:"origin" json:"origin" mapstructure:"ORIGIN" config:"default=localhost,127.0.0.1"`
    Timeout time.Duration `yaml:"timeout" json:"timeout" mapstructure:"TIMEOUT" config:"default=5s"`

    Features struct {
        Enabled bool `yaml:"enabled" json:"enabled" mapstructure:"FEATURES_ENABLED" config:"required"`
    } `yaml:"features" json:"features" config:"struct,required"`
}

func main() {
    cfg := autoconfig.New("MYAPP")

    if err := cfg.Create("myapp", "config", "", autoconfig.ConfigTypeYAML); err != nil {
        log.Fatal(err)
    }

    appConfig := new(AppConfig)

    // File is optional. Handle the error according to your application needs.
    _ = cfg.ReadFile(appConfig)

    if err := cfg.ReadEnv(appConfig); err != nil {
        log.Fatal(err)
    }

    if err := cfg.Check(appConfig); err != nil {
        log.Fatal(err)
    }

    fmt.Printf("%+v\n", appConfig)
}
```

## Testing

Run both of these in CI:

```bash
go test ./...
go test -race ./...
```
