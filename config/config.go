package config

import (
	"io/ioutil"

	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)

type Config struct {
	ListenPort       int    `yaml:"listenPort"`
	RpcPort          int    `yaml:"rpcPort"`
	PerformancePort  int    `yaml:"performancePort"`
	PoolServer       string `yaml:"poolServer"`
	MaxSessionCount  int    `yaml:"maxSessionCount"`
	ServerId         string `yaml:"serverId"`
	Debug            bool   `yaml:"debug"`
	ServerTlsKeyPath string `yaml:"ServerTlsKeyPath"`
	ServerTlsCrtPath string `yaml:"ServerTlsCrtPath"`
	Logger           struct {
		Filename   string `yaml:"filename"`
		MaxSize    int    `yaml:"maxSize"`
		MaxBackups int    `yaml:"maxBackups"`
		MaxAge     int    `yaml:"maxAge"`
		Level      string `yaml:"level"`
		Verbose    int    `yaml:"verbose"`
		Compress   bool   `yaml:"compress"`
	} `yaml:"logger"`
	Redis Redis `yaml:"redis"`
}
type Redis struct {
	Address       string `yaml:"address"`
	Password      string `yaml:"password"`
	Port          string `yaml:"port"`
	OpMaxIdle     int    `yaml:"op_maxidle"`
	OpMaxActive   int    `yaml:"op_maxactive"`
	OpIdleTimeout int    `yaml:"op_idletimeout"`
}

func LoadConfig(path string) (config *Config) {

	if path == "" {
		zap.L().Fatal("Please set startup parameters-configs or RAGETNT_CONFIG_PATH environment variable", zap.String("path", path))
	}
	configContent, err := ioutil.ReadFile(path)

	if err != nil {
		zap.L().Fatal("read config file error: ", zap.String("path", path))
	}

	err = yaml.Unmarshal(configContent, &config)
	if err != nil {
		zap.L().Fatal("config file can't be decode as json: ", zap.Error(err))
	}
	if config.ListenPort <= 0 {
		zap.L().Fatal("config file error: ListenPort", zap.Int("ListenPort", config.ListenPort))
	}

	if config.RpcPort <= 0 {
		zap.L().Fatal("config file error: RpcPort", zap.Int("RpcPort", config.RpcPort))
	}

	if len(config.ServerId) <= 0 {
		zap.L().Fatal("config file error: serverId", zap.String("serverId", config.ServerId))
	}

	if len(config.PoolServer) <= 0 {
		zap.L().Fatal("config file error: PoolServer", zap.String("PoolServer", config.PoolServer))
	}
	if config.MaxSessionCount <= 0 {
		zap.L().Fatal("config file error: MaxSessionCount", zap.Int("MaxSessionCount", config.MaxSessionCount))
	}
	if len(config.Logger.Filename) <= 0 {
		zap.L().Fatal("config file error: Logger Filename is null")
	}

	if config.Logger.MaxSize == 0 {
		config.Logger.MaxSize = 1000
	}

	if config.Logger.MaxBackups == 0 {
		config.Logger.MaxBackups = 100
	}

	if config.Logger.MaxAge == 0 {
		config.Logger.MaxAge = 24 * 30
	}

	if len(config.Logger.Level) <= 0 {
		config.Logger.Level = "info"
	}

	if config.Logger.Verbose == 0 {
		config.Logger.Verbose = 3
	}
	return
}

const (
	CONFIG_PATH = "RAGETNT_CONFIG_PATH"
	LOG_DIR     = "RAGETNT_LOG_DIR"
)
