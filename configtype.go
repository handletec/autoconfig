package autoconfig

type ConfigType uint8

const (
	ConfigTypeNone ConfigType = iota
	ConfigTypeYAML
	ConfigTypeJSON
)

func (ct ConfigType) IsValid() (valid bool) {
	return ct.String() != "none"
}

func (ct ConfigType) String() (str string) {

	ctName := []string{"none", "yaml", "json"}
	ctInt := int(ct)

	if ctInt < 0 || ctInt > len(ctName) {
		ctInt = 0 // if an unknown config type is given, set to none as default
	}

	return ctName[ctInt]
}
