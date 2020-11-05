package filesvc

import (
	"fmt"
	sharedConfig "github.com/getcouragenow/sys-share/sys-core/service/config"
	commonCfg "github.com/getcouragenow/sys-share/sys-core/service/config/common"
	"gopkg.in/yaml.v2"
)

const (
	errParsingConfig = "error parsing %s config: %v\n"
)

type FileServiceConfig struct {
	DBConfig commonCfg.Config `json:"dbConfig" yaml:"dbConfig"`
}

func (f *FileServiceConfig) Validate() error {
	return f.DBConfig.Validate()
}

func NewConfig(filepath string) (*FileServiceConfig, error) {
	fileSvcConfig := &FileServiceConfig{}
	f, err := sharedConfig.LoadFile(filepath)
	if err != nil {
		return nil, err
	}
	if err := yaml.UnmarshalStrict(f, &fileSvcConfig); err != nil {
		return nil, fmt.Errorf(errParsingConfig, filepath, err)
	}
	if err := fileSvcConfig.Validate(); err != nil {
		return nil, err
	}

	return fileSvcConfig, nil
}
