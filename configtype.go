package autoconfig

import "github.com/svicknesh/enum2str"

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
	return enum2str.String(ct, "none", "yaml", "json")
}
