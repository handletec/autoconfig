# Changelog

All notable changes to this project will be documented in this file.

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
