package server

import (
	"crypto/tls"
	"fmt"
	"time"

	"amenity.com/ragent-server/config"
	"amenity.com/ragent-server/log"
	"amenity.com/ragent-server/service/agent"
	"amenity.com/ragent-server/service/stat"
	"amenity.com/ragent-server/service/upgrade"
	"go.uber.org/zap"
)

type RAgentServer struct {
	Config     *config.Config
	StatSvc    *stat.StatService
	Am         *agent.AgentManager
	serverM    *ServerManager
	UpgradeSvc *upgrade.UpgradeService
	ZapLog     *log.ZapLog
}

// Global Server
var Ras *RAgentServer

const DEFAULT_CONNECT_TIME_OUT = 3000

func NewServerInstance(configPath, logDir string) *RAgentServer {

	if Ras != nil {
		return Ras
	}
	Ras = new(RAgentServer)
	Ras.Config = config.LoadConfig(configPath)
	//zapLog
	if len(logDir) > 0 {
		Ras.Config.Logger.Filename = logDir + "/ragent.log"
	}
	Ras.serverM = Ras.newServerManager()
	// ras.UpgradeSvc = upgrade.NewUpgradeService()
	Ras.Am = newAgentManager(Ras)

	Ras.ZapLog = log.NewZapLog(Ras.Config.Logger.Filename,
		Ras.Config.Logger.MaxSize,
		Ras.Config.Logger.MaxBackups,
		Ras.Config.Logger.MaxAge,
		Ras.Config.Logger.Level,
		Ras.Config.Logger.Verbose,
		Ras.Config.Logger.Compress,
		[]zap.Field{zap.String("serverId", Ras.Config.ServerId)})

	return Ras
}

func (r RAgentServer) Close() {

	// gs.RAgentService.Close()
}

func (r RAgentServer) newServerManager() (sm *ServerManager) {
	sm = new(ServerManager)
	zap.L().Warn("any", zap.Any("port", r.Config.ListenPort))
	sm.tcpListenAddr = fmt.Sprintf("0.0.0.0:%d", r.Config.ListenPort)
	return
}

func (r RAgentServer) Run(serverCrtPath, serverKeyPath string) (err error) {
	if r.serverM.tcpListener == nil {
		cer, cerE := tls.LoadX509KeyPair(serverCrtPath, serverKeyPath)
		if cerE != nil {
			zap.L().Fatal("Server Listen CRT ERR", zap.Error(cerE))
			return err
		}
		zap.L().Info("listen tcp", zap.String("listenAddr", r.serverM.tcpListenAddr))
		config := &tls.Config{Certificates: []tls.Certificate{cer}}
		ln, e := tls.Listen("tcp", r.serverM.tcpListenAddr, config)
		if e != nil {
			zap.L().Fatal("Server Listen Failed", zap.Error(e))
			return e
		}
		defer ln.Close()
		r.serverM.tcpListener = ln
	}

	for {
		conn, err := r.serverM.tcpListener.Accept()
		zap.L().Warn("New Client Coming", zap.String("Client IP", conn.RemoteAddr().String()))

		if err != nil {
			zap.L().Warn("New Client Coming Error", zap.Error(err))
			continue
		}
		r.Am.Run(conn)
		/* prevent from lots of coming connection on restart server*/
		time.Sleep(30 * time.Millisecond)
	}
}

func newAgentManager(r *RAgentServer) *agent.AgentManager {
	am := new(agent.AgentManager)
	am.PoolUrl = r.Config.PoolServer
	am.ConnTimeOut = DEFAULT_CONNECT_TIME_OUT

	return am
}
