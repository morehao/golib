package conf

import (
	"os"

	"gopkg.in/yaml.v3"
)

func LoadConfig(configPath string, dest any) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, dest)
}