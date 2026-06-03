package autoconfig

type ConfigType uint8

const (
	ConfigTypeNone ConfigType = iota
	ConfigTypeYAML
	ConfigTypeJSON
)

func (ct ConfigType) IsValid() bool {
	return ct == ConfigTypeYAML || ct == ConfigTypeJSON
}

func (ct ConfigType) String() string {
	switch ct {
	case ConfigTypeYAML:
		return "yaml"
	case ConfigTypeJSON:
		return "json"
	default:
		return "none"
	}
}
