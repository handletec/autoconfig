# Changelog

All notable changes to this project will be documented in this file.

## [v1.2.0] — 2026-09-27

### Added

- Added `_FILE` support for string environment-backed configuration values. Any environment-bindable string field (passwords, tokens, API keys, DSNs, signing secrets) now also accepts a sibling `<ENV>_FILE` environment variable (e.g. `MYAPP_DB_PASSWORD_FILE`) whose value is a path to a file containing the actual value — the Docker/Kubernetes secrets-file pattern. No new struct tag is required. The secret's contents are never written back into the process environment, never persisted on `Config`, and never stored in the internal Viper instance; they are resolved fresh on each `ReadEnv()` call and applied directly to the destination struct field. See the README's "Loading secrets from files" section for the full behaviour, including the direct-value/`_FILE` conflict error, the string-only scope, the empty-content-is-absent rule, and the trailing-newline convention.

This is a backward-compatible, additive feature. Existing direct environment-variable behaviour, the `config` tag syntax, and the public API are all unchanged.

## [v1.1.1] — 2026-08-18

### Fixes

- **Nested environment-variable binding now correctly populates nested struct fields.** In v1.1.0 and earlier, environment variables for fields inside a `config:"struct"` nested struct were detected as present but never decoded into the nested Go field: `ReadEnv` recorded the field as explicitly supplied, so `Check()` skipped applying the configured default, but the field itself was left at its Go zero value. Environment overrides for nested string, integer, boolean, secret, listener, logging, and TLS configuration fields — including deeper recursive nesting — now populate the intended field correctly. Two nested structs sharing an identical leaf `mapstructure` field name no longer risk internal collision either.

This is a correctness fix, not a new feature: nested `config:"struct"` env binding was always intended to work this way. Environment-variable names (`PREFIX_<mapstructure tag>`), the `config` tag syntax, the public API, and default behaviour when no environment value is supplied are all unchanged.

### Upgrade note

If an existing application already sets environment variables for fields inside a `config:"struct"` nested struct, those values were silently ignored under v1.1.0 and earlier — the field stayed at its zero value. After upgrading to v1.1.1, those environment variables will now take effect. This is corrected behaviour, not an API break, but verify your effective runtime configuration after upgrading if you rely on nested struct env binding.

---

## [v1.1.0] — 2026-06-19

### Breaking change

**`config` tag is no longer required for environment binding.**

In v1.0.x, a field was only eligible for environment binding if it carried a `config` tag. Starting in v1.1.0, any exported field with a valid `mapstructure` tag is automatically eligible for environment binding. The `config` tag is now optional and controls validation policy only (`required`, `default=`, `struct`). Fields without a `config` tag are treated as optional.

This is a breaking change for consumers that unintentionally omitted `config` tags on fields that should not be bound. Those fields will now receive values from environment variables if a matching `MAPSTRUCTURE`-keyed environment variable is set. To explicitly exclude a field, add `config:"-"`.

**Affected consumers (identified at v1.1.0 audit):**
- `sshgatekeepersrv` — 3 fields require `config:"-"` or a `config` tag review.
- `sshgatekeepersvc` — 4 fields require `config:"-"` or a `config` tag review.
- `ncauth` — no breaking impact identified.

### Migration guide for v1.0.x consumers

1. Audit every exported struct field that has a `mapstructure` tag but no `config` tag. These fields are now env-bound by default.
2. Add `config:"-"` to any field that must not receive a value from environment variables.
3. If a field was already working as intended (optional, populated from env), no change is required.
4. Run your test suite with representative environment variables to confirm behaviour.

### New behaviour

- Fields with only a `mapstructure` tag (no `config` tag) receive env values when the corresponding environment variable is set.
- Fields with only a `mapstructure` tag stay at their zero value when the environment variable is absent.
- `config:"-"` explicitly excludes a field from all autoconfig processing.

### Grammar: `default=` is terminal

The `default=` option must be the last option in a `config` tag. Everything after the `=` and up to the end of the tag value (including any commas) is treated as the default value. This enables multi-value defaults such as `config:"default=localhost,127.0.0.1"`.

Policy options (`required`, `struct`) must appear before `default=`. The parser returns an error if a policy option follows `default=`.

Valid: `config:"required,default=localhost,127.0.0.1"`
Invalid: `config:"default=localhost,required"` — returns a parse error.

### Known limitations

- **`time.Time` is not env-bindable.** `time.Time` is a struct type. The library guards against binding struct-typed fields to a single flat env key. Set `time.Time` fields from config files or application code, not from environment variables.
- **`time.Duration` is supported.** It is backed by `int64` (not a struct), and Viper's decode hook handles the `"5s"` → `time.Duration` conversion correctly.
- **Nested struct env binding uses flat keys.** A nested struct field with `mapstructure:"FEATURES_ENABLED"` competes with a sibling scalar field at the same key. Struct env binding via the flat-key Viper model has known limits with deeply nested structures.
- **Empty env vars are treated as absent.** Setting `FOO=""` applies the `default=` value or leaves the field at its zero value. This is Viper's default `AllowEmptyEnv=false` behaviour and is unchanged from v1.0.x.

### Fixes

- Fixed a pre-existing parser bug where a comma-separated default value such as `config:"default=localhost,127.0.0.1"` was incorrectly split on the internal comma, causing an unsupported token error. The parser now treats `default=` as a terminal option and rejoins all subsequent comma-separated parts as the value.

---

## [v1.0.5] and earlier

See git log for prior changes.
