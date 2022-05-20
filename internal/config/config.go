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

func (x *Config) Init() {
	x.MQTT.Init()
	x.MySQL.Init()
	x.Settings.Init()
	x.UIClient.Init()
	x.Influx.Init()
	x.Common.Init()
}

const configFileName = "config.yaml"

var log = logger.L()

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

func processError(err error) {
	log.Fatal(err)
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func readFile(cfg *Config) {
	if !fileExists(configFileName) {
		return
	}
	f, err := os.Open(configFileName)
	if err != nil {
		processError(err)
	}
	defer f.Close()
	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(cfg)
	if err != nil {
		if err != io.EOF {
			processError(err)
		}
	}
}

func readEnv(cfg *Config) {
	err := envconfig.Process("", cfg)
	if err != nil {
		processError(err)
	}
}

func prettyPrint(cfg *Config) {
	d, _ := yaml.Marshal(cfg)
	log.Infof("--- Config ---\n%s\n\n", string(d))
}

// GetConfig : get config file
func GetConfig() *Config {
	cfg := defConfig()

	readFile(cfg)
	readEnv(cfg)
	readOpts(cfg)

	logger.SetLevel(zapcore.Level(cfg.Settings.LogLevel))
	prettyPrint(cfg)

	return cfg
}
