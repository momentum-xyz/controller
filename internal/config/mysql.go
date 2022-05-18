package config

import (
	"github.com/go-sql-driver/mysql"
	"strconv"
)

type MySQL struct {
	DATABASE string `yaml:"database" envconfig:"DB_DATABASE"`
	HOST     string `yaml:"host" envconfig:"DB_HOST"`
	PORT     uint   `yaml:"port" envconfig:"DB_PORT"`
	USERNAME string `yaml:"username" envconfig:"DB_USERNAME"`
	PASSWORD string `yaml:"password" envconfig:"DB_PASSWORD"`
}

func (x *MySQL) Init() {
	x.DATABASE = "momentum3a"
	x.HOST = "localhost"
	x.PASSWORD = ""
	x.USERNAME = "root"
	x.PORT = 3306
}

func (x *MySQL) GenConfig() *mysql.Config {
	sqlconfig := mysql.NewConfig()
	sqlconfig.User = x.USERNAME
	sqlconfig.Passwd = x.PASSWORD
	sqlconfig.DBName = x.DATABASE
	sqlconfig.Addr = x.HOST + ":" + strconv.FormatUint(uint64(x.PORT), 10)
	sqlconfig.Net = "tcp"
	sqlconfig.ParseTime = true
	return sqlconfig
}
