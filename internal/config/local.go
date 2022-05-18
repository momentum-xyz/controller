package config

type Local struct {
	Address          string `yaml:"bind_address" envconfig:"CONTROLLER_BIND_ADDRESS"`
	Port             uint   `yaml:"bind_port" envconfig:"CONTROLLER_BIND_PORT"`
	LogLevel         int    `yaml:"loglevel"  envconfig:"CONTROLLER_LOGLEVEL"`
	ExtensionStorage string `yaml:"storage"  envconfig:"CONTROLLER_STORAGE"`
}

func (x *Local) Init() {
	x.LogLevel = 1
	x.Address = "0.0.0.0"
	x.Port = 4000
	x.ExtensionStorage = "/opt/controller"
}
