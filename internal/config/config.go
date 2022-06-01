package config

import (
	// Std
	"io"
	"os"

	// Momentum
	"github.com/momentum-xyz/controller/internal/logger"
	"github.com/pborman/getopt/v2"

	// Third-Party
	"github.com/kelseyhightower/envconfig"
	"github.com/pkg/errors"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v2"
)

// Config : structure to hold configuration
type Config struct {
	MQTT     MQTT     `yaml:"mqtt"`
	MySQL    MySQL    `yaml:"mysql"`
	Influx   Influx   `yaml:"influx"`
	UIClient UIClient `yaml:"ui_client"`
	Settings Local    `yaml:"settings"`
	Common   Common   `yaml:"common"`
}

const configFileName = "config.yaml"

var log = logger.L()

func (x *Config) Init() {
	x.MQTT.Init()
	x.MySQL.Init()
	x.Settings.Init()
	x.UIClient.Init()
	x.Influx.Init()
	x.Common.Init()
}

func defConfig() *Config {
	var cfg Config
	cfg.Init()
	return &cfg
}

func readOpts(cfg *Config) {
	helpFlag := false
	getopt.Flag(&helpFlag, 'h', "display help")
	getopt.Flag(&cfg.Settings.LogLevel, 'l', "be verbose")

	getopt.Parse()
	if helpFlag {
		getopt.Usage()
		os.Exit(0)
	}
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func readFile(cfg *Config) error {
	if !fileExists(configFileName) {
		return nil
	}

	f, err := os.Open(configFileName)
	if err != nil {
		return errors.WithMessage(err, "failed to open file")
	}
	defer f.Close()

	if err := yaml.NewDecoder(f).Decode(cfg); err != nil {
		if err != io.EOF {
			return errors.WithMessage(err, "failed to decode file")
		}
	}

	return nil
}

func readEnv(cfg *Config) error {
	return envconfig.Process("", cfg)
}

func prettyPrint(cfg *Config) {
	d, _ := yaml.Marshal(cfg)
	log.Infof("--- Config ---\n%s\n\n", string(d))
}

// GetConfig : get config file
func GetConfig() *Config {
	cfg := defConfig()

	if err := readFile(cfg); err != nil {
		log.Fatalf("GetConfig: failed to read file: %s", err)
	}
	if err := readEnv(cfg); err != nil {
		log.Fatalf("GetConfig: failed to read env: %s", err)
	}
	readOpts(cfg)

	logger.SetLevel(zapcore.Level(cfg.Settings.LogLevel))
	prettyPrint(cfg)

	return cfg
}
