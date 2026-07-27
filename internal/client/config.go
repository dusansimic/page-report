package client

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config is the on-disk shape of ~/.config/page-report/config.yml.
type Config struct {
	ServerURL string `mapstructure:"server_url"`
}

// keys lists every config key so each can be explicitly bound to its PR_* env
// var; viper's AutomaticEnv alone does not surface env-only keys during
// Unmarshal.
var keys = []string{"server_url"}

// configDir returns $XDG_CONFIG_HOME/page-report, falling back to
// ~/.config/page-report. The directory is not created.
func configDir() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "page-report"), nil
}

// LoadConfig reads the CLI configuration and applies PR_* environment
// overrides. A non-empty path is an explicit file that must be readable;
// otherwise config.yml is looked up in the XDG config dir and a missing file
// yields the zero Config.
func LoadConfig(path string) (*Config, error) {
	v := viper.New()

	v.SetEnvPrefix("PR")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	for _, k := range keys {
		if err := v.BindEnv(k); err != nil {
			return nil, fmt.Errorf("bind env for %s: %w", k, err)
		}
	}

	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
	} else {
		dir, err := configDir()
		if err != nil {
			return nil, err
		}
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(dir)
		if err := v.ReadInConfig(); err != nil {
			var notFound viper.ConfigFileNotFoundError
			if !errors.As(err, &notFound) {
				return nil, fmt.Errorf("read config: %w", err)
			}
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	cfg.ServerURL = strings.TrimRight(cfg.ServerURL, "/")
	return &cfg, nil
}
