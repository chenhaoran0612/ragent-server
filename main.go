package main

import (
	"amenity.com/ragent-server/kit/cache"
	"amenity.com/ragent-server/kit/cache/gredis"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"amenity.com/ragent-server/config"
	"amenity.com/ragent-server/server"
	"go.uber.org/zap"

	service "amenity.com/ragent-server/service/rpc"
	_ "net/http/pprof"
)

func main() {
	var configPath, logDir string
	flag.StringVar(&configPath, "config", "./config/dev/config.dev.yaml", "Path of config file")
	flag.StringVar(&logDir, "log_dir", "./log/", "logger directory.")
	flag.Parse()
	if configPath == "" {
		configPath = os.Getenv(config.CONFIG_PATH)
	}
	// runtimeFilePath := flag.String("runtime", "", "Path of runtime file, use for zero downtime upgrade.")
	if logDir == "" {
		logDir = os.Getenv(config.LOG_DIR)
	}

	gS := server.NewServerInstance(configPath, logDir)
	defer gS.Close()

	undo := zap.ReplaceGlobals(gS.ZapLog.Logger)
	defer undo()

	zap.L().Info("init logger", zap.String("logName", gS.Config.Logger.Filename), zap.String("logDir", logDir), zap.Int("noNeedVerbose", gS.Config.Logger.Verbose))

	// 初始化redis
	err := cache.InitRedis(gS.Config.Redis.Address, gS.Config.Redis.Port, gS.Config.Redis.Password, gredis.RedisCache, cache.MaxIdle(gS.Config.Redis.OpMaxActive), cache.IdleTimeout(gS.Config.Redis.OpIdleTimeout), cache.MaxActive(gS.Config.Redis.OpMaxActive))
	if err != nil {
		zap.L().Fatal("Server Redis Not Found")
	}
	// kit.G(func() {
	go gS.Run(gS.Config.ServerTlsCrtPath, gS.Config.ServerTlsKeyPath)
	// })

	rS := service.NewRpcService(gS.Config.RpcPort)
	go rS.Run()

	// go 性能分析工具
	go func() {
		http.ListenAndServe(fmt.Sprintf(":%d", gS.Config.PerformancePort), nil)
	}()

	signalChan := make(chan os.Signal)

	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGUSR2)

	select {
	case sig := <-signalChan:

		switch sig {
		case syscall.SIGUSR2:
			zap.L().Info("now upgrading")
			err := gS.UpgradeSvc.Upgrade()
			if err != nil {
				zap.L().Warn("upgrade failed", zap.Error(err))
			}
			break
		default:
			zap.L().Info("now exiting", zap.String("sig", sig.String()))
		}
		break
	}
}
