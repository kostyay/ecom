// Package config loads ecom configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

const configDirName = "ecom"

// Settings contains all application settings.
type Settings struct {
	Log LogSettings `mapstructure:"log"`
}

// LogSettings contains structured logging settings.
type LogSettings struct {
	Level string `mapstructure:"level"`
	File  string `mapstructure:"file"`
}

// Load reads configuration with flag, environment, and file precedence.
func Load(v *viper.Viper, configFile string) (Settings, error) {
	v.SetEnvPrefix("ECOM")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	v.SetDefault("log.level", "info")
	v.SetDefault("log.file", "")

	if configFile != "" {
		v.SetConfigFile(configFile)
	} else {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return Settings{}, fmt.Errorf("find user config directory: %w", err)
		}
		v.AddConfigPath(filepath.Join(configDir, configDirName))
		v.SetConfigName("config")
		v.SetConfigType("yaml")
	}

	if err := v.ReadInConfig(); err != nil {
		_, missing := errors.AsType[viper.ConfigFileNotFoundError](err)
		if configFile != "" || !missing {
			return Settings{}, fmt.Errorf("read configuration: %w", err)
		}
	}

	var settings Settings
	if err := v.UnmarshalExact(&settings); err != nil {
		return Settings{}, fmt.Errorf("decode configuration: %w", err)
	}

	return settings, nil
}
