//  SPDX-FileCopyrightText: 2024 Paulo Almeida <almeidapaulopt@gmail.com>
//  SPDX-License-Identifier: MIT

package core

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/creasty/defaults"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/v2"

	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
)

const prefix = "TSDPROXY_"

type (
	// Config stores complete configuration.
	//
	Config struct {
		PublicURL   string `default:"http://localhost:8080"`
		DataDir     string `default:"/data/"`
		AuthKey     string
		AuthKeyFile string
		Hostname    string `default:"127.0.0.1"`
		ControlURL  string

		Log  LogConfig
		HTTP HTTPConfig

		ContainerAccessLog bool `default:"true"`
	}

	// LogConfig stores logging configuration.
	LogConfig struct {
		Level string `default:"info"`
		JSON  bool   `default:"false"`
	}

	// HTTPConfig stores HTTP configuration.
	HTTPConfig struct {
		Hostname string `default:"0.0.0.0"`
		Port     uint16 `default:"8080"`
	}
)

// GetConfig loads and returns configuration.
func GetConfig() (*Config, error) {
	c := new(Config)

	// load default values
	//
	if err := defaults.Set(c); err != nil {
		fmt.Printf("Error loading defaults: %v", err)
	}

	// load environment variables
	//
	k := koanf.New(".")
	err := k.Load(
		env.Provider(
			prefix,
			".",
			func(s string) string {
				return strings.Replace(
					strings.ToLower(strings.TrimPrefix(s, prefix)),
					"_",
					".",
					-1)
			},
		),
		nil,
	)
	if err != nil {
		fmt.Printf("Error loading env: %v", err)
	}

	// unmarshal config to struct
	//
	err = k.UnmarshalWithConf("", &c, koanf.UnmarshalConf{
		Tag: "env",
	})
	if err != nil {
		return nil, fmt.Errorf("unmarshal config failed: %w", err)
	}

	err = k.Load(file.Provider("config/tsdproxy.yml"), yaml.Parser())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("cannot load config from file config/tsdproxy.yml: %w", err)
	}

	// Read auth key from file (for example docker secret)
	//
	if c.AuthKeyFile != "" {
		key, err := os.ReadFile(c.AuthKeyFile)
		if err != nil {
			return nil, fmt.Errorf("read auth key from file: %w", err)
		}
		c.AuthKey = strings.TrimSpace(string(key))
	}

	return c, nil
}
