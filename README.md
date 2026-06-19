# Golang auto-config library

`autoconfig` populates a Go struct from environment variables and optional config files with minimal boilerplate.

This version is hardened to avoid shared global state, reject unexpected file keys, and preserve explicit zero values such as `false` and `0` for required fields.

## v1.1.0 upgrade notice

**Breaking change**: in v1.1.0, a field with a `mapstructure` tag is automatically eligible for environment binding even if it has no `config` tag. In v1.0.x, a `config` tag was required.

**Action required if upgrading from v1.0.x**: review every exported struct field that has a `mapstructure` tag but no `config` tag. Those fields will now receive values from matching environment variables. Add `config:"-"` to any field that must not be bound.

See [CHANGELOG.md](CHANGELOG.md) for the full migration guide.

## Key behavior

- Each `Config` instance owns its own Viper instance.
- File decoding is strict: unknown keys cause an error.
- Required checks use presence tracking, not only zero-value checks.
- Defaults are applied only when a value was not provided.
- Nested structs support combined tags such as `config:"struct,required"`.
- Comma-separated environment variables are decoded into `[]string` with whitespace trimming.

The `mapstructure` tag on an exported field determines the environment variable key that Viper binds to. Any exported field with a `mapstructure` tag is automatically eligible for environment binding. Use `config:"-"` to explicitly exclude a field.

The `config` tag is optional and controls validation policy only. Fields without a `config` tag are treated as optional — they receive no default and require no value.

## Supported `config` options

| option | description |
| :-- | :-- |
| *(absent)* | optional field — env binding proceeds if a `mapstructure` tag is present; no default, no required check |
| `default=<value>` | default value to use when the field was not provided; must be the last option |
| `required` | the value must be provided unless a default exists |
| `struct` | recurse into a nested struct |
| `-` | explicitly exclude this field from all autoconfig processing |

### `default=` is terminal

`default=` must be the last option in a `config` tag. Everything after `=` up to the end of the tag value — including any commas — is treated as the default value. This allows multi-value defaults:

```go
// Valid: required appears before default=
Origins []string `mapstructure:"ORIGINS" config:"required,default=localhost,127.0.0.1"`

// Invalid: required after default= — returns a parse error
Host string `mapstructure:"HOST" config:"default=localhost,required"`
```

## Known limitations

- **`time.Time` is not env-bindable.** `time.Time` is a struct type. The library does not bind struct-typed fields to a single flat env key. Set `time.Time` fields from config files or application logic, not from environment variables. `time.Duration` works correctly because it is an `int64` underneath.
- **Nested struct env binding uses flat keys.** Viper's flat-key model has limits with deeply nested structures. If nested struct env binding is not working as expected, use a flat struct with explicit `mapstructure` keys.
- **Empty env vars are treated as absent.** Setting `FOO=""` applies the `default=` value or leaves the field at its zero value (`AllowEmptyEnv=false`, the Viper default).

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
    // Optional — no config tag needed. Populated if MYAPP_NAME is set; zero otherwise.
    Name    string        `yaml:"name" json:"name" mapstructure:"NAME"`

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
