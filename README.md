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
- Any environment-bindable **string** field also supports a sibling `_FILE` variable (e.g. `MYAPP_DB_PASSWORD_FILE`) to load the value from a file instead — see "Loading secrets from files" below.

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

## Loading secrets from files (`_FILE`)

Any exported field that is env-bindable and whose effective Go type is
`string` (passwords, tokens, API keys, DSNs, signing secrets, and similar)
also accepts its value from a file, using the Docker/Kubernetes
secrets-file convention. No new struct tag is required — the existing
`mapstructure` tag is sufficient.

Given:

```go
type Config struct {
    Password string `mapstructure:"DB_PASSWORD" config:"required"`
}
```

and `cfg := autoconfig.New("MYAPP")`:

- `MYAPP_DB_PASSWORD=super-secret` — existing behaviour, unchanged.
- `MYAPP_DB_PASSWORD_FILE=/run/secrets/db_password` (file contains
  `super-secret`) — after `cfg.ReadEnv(appConfig)`, `appConfig.Password ==
  "super-secret"`.

autoconfig (1) reads the path from `..._FILE`, (2) reads the file's contents
directly, (3) places the resulting value into the config struct field —
**it never creates, updates, or reads back a value into the corresponding
plain environment variable.** After loading, `MYAPP_DB_PASSWORD_FILE` may
exist in the environment, but `MYAPP_DB_PASSWORD` remains absent unless the
caller separately supplied it.

### Rules

- **Direct value only:** unchanged existing behaviour.
- **`_FILE` only:** the file is read and its value is applied to the field.
  A successfully resolved `_FILE` value participates in presence tracking,
  so `config:"required"` is satisfied by it, and a `default=` value never
  overwrites it.
- **Neither supplied:** unchanged existing behaviour (config-file value,
  default, optional zero value, or a `required` validation failure).
- **Both `MYAPP_DB_PASSWORD` and `MYAPP_DB_PASSWORD_FILE` set to non-empty
  values:** `ReadEnv` returns an error naming both environment variable
  names. Neither value is silently preferred, and neither value appears in
  the error.
- **Empty values:** `MYAPP_DB_PASSWORD=""` with a valid `_FILE` uses the
  `_FILE` value. `MYAPP_DB_PASSWORD_FILE=""` is treated as though `_FILE`
  were not supplied at all.
- **Empty file content:** if the file's content, after removing exactly one
  trailing line ending (see below), is empty, `_FILE` is treated as though
  it were not supplied — no presence is recorded and no value is applied.
  An optional field keeps its existing value, a field with `default=`
  receives that default, and a `required` field with no default fails
  required validation exactly as if `_FILE` had never been set.
- **Unreadable or missing file:** `ReadEnv` returns a hard error identifying
  the `_FILE` variable name and the file path, wrapping the underlying
  filesystem error. There is no fallback to a default — a configured but
  unreadable secret is a configuration failure.
- **Non-string fields:** `_FILE` is only supported for a bare (non-pointer)
  `string` field. Using it against an int, bool, duration, slice, struct,
  `*string`, or any other pointer or type returns a clear "unsupported
  type" error rather than being silently ignored or dereferenced.
- **`config:"-"`** excludes a field, and its `_FILE` sibling, entirely.
- **Nested structs:** works through `config:"struct"` nesting using the
  existing flat OS environment-variable naming convention — a nested field
  bound to `MYAPP_API_KEY` also accepts `MYAPP_API_KEY_FILE`.

### Trailing newline convention

Exactly one conventional trailing line ending is removed if present:
`"secret\n"` → `"secret"`, `"secret\r\n"` → `"secret"`. `strings.TrimSpace`
is never applied — surrounding spaces and internal newlines may be part of
the actual secret and are preserved exactly: `" secret "` stays `" secret
"`, and `"line1\nline2"` stays `"line1\nline2"`.

### Security notes

`_FILE` reduces accidental exposure through environment-variable inspection
and logging. **It is not a full secret-management system.** Once loaded,
the application necessarily holds the secret in memory like any other
configuration value.

**Autoconfig does not redact configuration structs.** Applications must not
log or print configuration structs that contain secrets.

### Rotation

`_FILE` is read once, when `ReadEnv()` executes. Changing the file's
contents afterward does not automatically update an already-loaded
configuration value. Applications that require rotation should implement
their own reload flow (e.g. calling `ReadEnv()` again in response to a
signal) or use a dedicated secret-management mechanism.

## Known limitations

- **`time.Time` is not env-bindable.** `time.Time` is a struct type. The library does not bind struct-typed fields to a single flat env key. Set `time.Time` fields from config files or application logic, not from environment variables. `time.Duration` works correctly because it is an `int64` underneath.
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

    // Print only a known non-secret field. Do not print or log the whole
    // config struct — autoconfig does not redact secret-bearing fields
    // (see "Loading secrets from files" above).
    fmt.Printf("listening on %s:%d\n", appConfig.Address, appConfig.Port)
}
```

## Testing

Run both of these in CI:

```bash
go test ./...
go test -race ./...
```
