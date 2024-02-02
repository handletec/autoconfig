package autoconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

const (
	tagConfig       = "config"
	tagMapStructure = "mapstructure" // viper uses this to read environment variables
)

// Config - configuration structure
type Config struct {
	project, dirname, cfgBaseName string
	cfgType                       ConfigType
	envPrefix                     string
	msTag                         map[string]string
}

var allowedRegex = regexp.MustCompile(`[^a-z0-9 ]+`)

// New - creates a new configuration
func New() (c *Config) {
	c = new(Config)
	c.msTag = make(map[string]string)

	return
}

// Create - creates the directory for any application specific configuration files
func (c *Config) Create(project, baseCfgName, baseDirname string, cfgType ConfigType) (err error) {

	if len(project) == 0 {
		return fmt.Errorf("config init: missing project name")
	}

	if len(baseCfgName) == 0 {
		return fmt.Errorf("config init: missing config base file name")
	}

	if !cfgType.IsValid() {
		cfgType = ConfigTypeYAML // if an unknown type is given, default to YAML
	}
	c.cfgType = cfgType

	c.project = "." + allowedRegex.ReplaceAllString(strings.ToLower(project), "")
	c.cfgBaseName = allowedRegex.ReplaceAllString(strings.ToLower(baseCfgName), "")

	if len(baseDirname) == 0 {
		// Find home directory.
		home, err := os.UserHomeDir()
		if nil != err {
			return fmt.Errorf("config init: getting home dir -> %w", err)
		}

		c.dirname = filepath.Join(home, project)

	} else {
		c.dirname, err = filepath.Abs(filepath.Clean(baseDirname))
		if nil != err {
			return fmt.Errorf("config init: setting dirname -> %w", err)
		}
	}

	err = os.MkdirAll(c.dirname, 0600) // create the directory with permission for user only
	if nil != err {
		return fmt.Errorf("config init: error creating '%s' directory -> %w", c.dirname, err)
	}

	viper.AddConfigPath(c.dirname)
	viper.SetConfigType(c.cfgType.String())
	viper.SetConfigName(c.cfgBaseName)

	return
}

// ReadFile - reads the configuration parameters from file and populates the given structure
func (c *Config) ReadFile(s any) (err error) {

	err = viper.ReadInConfig()
	if nil != err {
		return fmt.Errorf("readfile: missing '%s.%s' config file in '%s'", c.cfgBaseName, c.cfgType.String(), c.dirname)
	}

	err = viper.Unmarshal(s)
	if nil != err {
		return fmt.Errorf("readfile: error reading '%s.%s' config file in '%s' -> %w", c.cfgBaseName, c.cfgType.String(), c.dirname, err)
	}

	return
}

// SetEnvPrefix - sets the environment prefix unique to the application to read environment variables
func (c *Config) SetEnvPrefix(prefix string) {
	c.envPrefix = prefix
	viper.SetEnvPrefix(c.envPrefix)
}

// ReadEnv - reads the configuration parameters from env and populates the given structure
func (c *Config) ReadEnv(s any) (err error) {

	// read the tags we need and populate the structure after if it passes
	val := reflect.ValueOf(s)

	if val.Kind().String() != "ptr" {
		return fmt.Errorf("read environment: pass a pointer instead of '%s'", val.Kind().String())
	}

	// de-reference the given object to get its underlying type
	val = reflect.Indirect(reflect.ValueOf(s))

	if val.Kind().String() != "struct" {
		// we expect a struct to be given, otherwise return an error
		return fmt.Errorf("read environment: underlying pointer type struct expected to populate, '%s' provided", val.Kind().String())
	}

	var structField reflect.StructField
	var tags string
	// parse the 'tagMapStructure' tag to bind to viper environment variables

	for i := 0; i < val.NumField(); i++ {
		structField = val.Type().Field(i)

		// we parse the config tag to see if a structure is given for us to delve further
		tags = structField.Tag.Get(tagConfig)
		if len(tags) != 0 {
			switch strings.ToLower(tags) {
			case "struct":
				// a structure is given, we need to delve further
				//fmt.Println("delving into", structField.Name)
				// err = c.ReadEnv(val.FieldByName(structField.Name).Addr().Interface())
				if val.CanAddr() {
					err = c.ReadEnv(val.FieldByName(structField.Name).Addr().Interface())
					if nil != err {
						return fmt.Errorf("read enviroment: error delving into struct '%s' -> %w", structField.Name, err)
					}
				}
			}
		}

		tags = structField.Tag.Get(tagMapStructure)
		//tags := val.Type().Field(i).Tag.Get(tagConfig)
		//fmt.Printf("Field: %s, Tags: %s\n", val.Type().Field(i).Name, tags)

		// only condition to bind the env
		if len(tags) != 0 && tags != "-" {
			//fmt.Printf("setting environment variable %s_%s\n", c.envPrefix, tags)
			viper.BindEnv(tags)
			c.msTag[structField.Name] = tags
		}

	}

	viper.AutomaticEnv() // read in environment variables that match

	err = viper.Unmarshal(s) // read the environment variables into this struct
	if nil != err {
		return fmt.Errorf("read environment: error unmarshaling to struct -> %w", err)
	}

	return
}

// Check - processes the struct to make sure everthing is valid
func (c *Config) Check(s any) (err error) {

	//fmt.Println(c.msTag)

	// read the tags we need and populate the structure after if it passes
	val := reflect.ValueOf(s)
	//fmt.Println(val.Kind().String(), val.CanSet(), val.CanAddr(), val.Interface())

	if val.Kind().String() != "ptr" {
		return fmt.Errorf("config check: pass a pointer instead of '%s'", val.Kind().String())
	}

	// de-reference the given object to get its underlying type
	val = reflect.Indirect(reflect.ValueOf(s))

	if val.Kind().String() != "struct" {
		// we expect a struct to be given, otherwise return an error
		return fmt.Errorf("config check: underlying pointer type struct expected to populate, '%s' provided", val.Kind().String())
	}

	var structField reflect.StructField
	var tags string

	var fieldValue reflect.Value
	var msTag string
	//var ok bool

	// parse the 'tagConfig' tag to process the necessary rules

	for i := 0; i < val.NumField(); i++ {
		structField = val.Type().Field(i)
		tags = structField.Tag.Get(tagConfig)
		//tags := val.Type().Field(i).Tag.Get(tagConfig)
		//fmt.Printf("Field: %s, Tags: %s\n", val.Type().Field(i).Name, tags)

		// we don't skip this because it could be a `struct` tag which will drill deeper
		/*
			// only continue if this field was set in the map for viper, otherwise it is a waste of time
			if msTag, ok = c.msTag[structField.Name]; !ok {
				continue // move to next item since this field is not processed for viper
			}
		*/

		msTag = c.msTag[structField.Name]

		if len(tags) == 0 || tags == "-" {
			continue // move on to next item, nothing for this library to do
		}

		tagParts := strings.Split(tags, ",")
		//fmt.Println(tagParts)

		fieldValue = val.Field(i)

		for _, t := range tagParts {
			kv := strings.Split(t, "=")
			//fmt.Println(kv)

			switch strings.ToLower(kv[0]) {

			case "struct":
				// a structure is given, we need to delve further
				//fmt.Println("delving into", structField.Name)
				// err = c.ReadEnv(val.FieldByName(structField.Name).Addr().Interface())
				if val.CanAddr() {
					err = c.Check(val.FieldByName(structField.Name).Addr().Interface())
					if nil != err {
						return fmt.Errorf("read enviroment: error delving into struct '%s' -> %w", structField.Name, err)
					}
				}

			case "required":
				if fieldValue.IsZero() {
					return fmt.Errorf("config check: missing 'required' environment variable '%s_%s' for field '%s'", c.envPrefix, msTag, structField.Name)
				}

			case "default":

				if !fieldValue.IsZero() {
					continue // there is a value provided, continue to next item
				}

				if len(kv) != 2 {
					// missing default value, return error
					return fmt.Errorf("config check: missing 'default' value for field '%s'", structField.Name)
				}

				//fmt.Println(structField.Name, fieldValue.Kind().String(), fieldValue)

				switch fieldValue.Kind() {
				case reflect.Bool:
					b, err := strconv.ParseBool(kv[1])
					if nil != err {
						return fmt.Errorf("config check: 'default' (%s) value for field '%s' error -> %w", structField.Type.Kind().String(), structField.Name, err)
					}
					reflect.ValueOf(s).Elem().FieldByName(structField.Name).SetBool(b)

				case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
					i, err := strconv.ParseInt(kv[1], 10, 64)
					if nil != err {
						return fmt.Errorf("config check: 'default' (%s) value for field '%s' error -> %w", structField.Type.Kind().String(), structField.Name, err)
					}
					reflect.ValueOf(s).Elem().FieldByName(structField.Name).SetInt(i)

				case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
					i, err := strconv.ParseUint(kv[1], 10, 64)
					if nil != err {
						return fmt.Errorf("config check: 'default' (%s) value for field '%s' error -> %w", structField.Name, structField.Type.Kind().String(), err)
					}
					reflect.ValueOf(s).Elem().FieldByName(structField.Name).SetUint(i)

				case reflect.Float32, reflect.Float64:
					i, err := strconv.ParseFloat(kv[1], 64)
					if nil != err {
						return fmt.Errorf("config check: 'default' (%s) value for field '%s' error -> %w", structField.Name, structField.Type.Kind().String(), err)
					}
					reflect.ValueOf(s).Elem().FieldByName(structField.Name).SetFloat(i)

				case reflect.String:
					reflect.ValueOf(s).Elem().FieldByName(structField.Name).SetString(kv[1])

				}

			}
		}

	}

	return
}
