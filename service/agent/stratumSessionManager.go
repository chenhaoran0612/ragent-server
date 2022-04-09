package agent

import (
	"bufio"
	"fmt"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	// Ragent C2S协议
	REGISTER_MINER       = 0x01
	AUTHENTICATION_MINER = 0x02
	SUBMIT_SHARE         = 0x03
	MACHINE_OFFLINE      = 0x06
	PENETRATE            = 0x07

	// Ragent S2C协议
	S2C_DISPATCH_JOB               = 0x04
	S2C_MINING_SET_DIFF            = 0x05
	S2C_DISPATCH_WORKER_JOB        = 0x08
	S2C_DISPATCH_WORKER_COMMON_JOB = 0x09
	S2C_REGISTER_MINER             = 0x81
	S2C_AUTHENTICATION_MINER       = 0x82
	S2C_SUBMIT_SHARE               = 0x83
	S2C_MACHINE_OFFLINE            = 0x86
	S2C_PENETRATE                  = 0x87
)

/**
 * 协议规定的MAGIC NUMBER
 */
const RA_MAGIC = 0x2B

/**
 * 协议规定的发送客户端长度
 */
const COMMON_SEND_BYTE_LENTH = 12 + 4

type StratumSession struct {
	isRunning            bool
	isSendMachineOffline bool

	poolAddr   string
	poolConn   net.Conn
	poolReader *bufio.Reader
	poolRPC    IStratumServer

	startTime       int64
	extraNonce1     string
	extraNonce2size int64

	agent    string
	password string

	workerName string

	lock sync.Mutex
	wg   sync.WaitGroup

	requestID    interface{}
	reqNo        uint32
	serverId     uint32
	agentService *AgentService

	// requestId & sessionID map
	rpcCacheDataManager RpcCacheDataManager
}

type RpcCacheDataManager struct {
	rpcCacheDataMap     map[int32]*RpcCacheData
	rpcCacheDataMapLock sync.RWMutex
}

type RpcCacheData struct {
	method int
	reqNo  uint32
}

type StratumSessionManager struct {
	// WorkerName StratumSession Map
	StratumSessionMap     map[string]*StratumSession
	StratumSessionMapLock sync.RWMutex
}

func NewStratumSessionManager() *StratumSessionManager {
	ssm := new(StratumSessionManager)

	ssm.StratumSessionMap = make(map[string]*StratumSession, 0)
	return ssm
}

// Stop 单个Miner 链接矿池关闭
func (ss *StratumSession) Stop(reason string, isRemoveStratumSessionMap bool, isSendMachineOffline bool) {

	// recover 崩溃让代码继续跑
	defer func() {
		if err := recover(); err != nil {
			zap.L().Error("SS Stop Panic", zap.String("sid", ss.extraNonce1),
				zap.String("reason", reason),
				zap.String("minerName", ss.workerName),
				zap.Uint32("reqNo", ss.reqNo),
				zap.String("agent", ss.agent),
				zap.Any("Err", err))
		}
	}()

	ss.lock.Lock()
	ss.isRunning = false
	ss.lock.Unlock()

	if ss.poolConn != nil {
		_ = ss.poolConn.Close()
		if isRemoveStratumSessionMap {
			// 清理Map并关闭
			ss.agentService.StratumSessionManager.StratumSessionMapLock.Lock()
			delete(ss.agentService.StratumSessionManager.StratumSessionMap, fmt.Sprintf("%d-%d", ss.serverId, ss.reqNo))
			ss.agentService.StratumSessionManager.StratumSessionMapLock.Unlock()
		}
	}

	// 停止的时候 发送下线通知
	if isSendMachineOffline {
		ss.sendMachineOffline()
	}

	addrStr := "default"
	if ss.poolConn != nil && ss.poolConn.LocalAddr() != nil {
		addrStr = ss.poolConn.LocalAddr().String()
	}

	durationSeconds := time.Now().Unix() - ss.startTime
	if durationSeconds > 10 {
		zap.L().Error("stopped",
			zap.String("sid", ss.extraNonce1),
			zap.String("reason", reason),
			zap.String("poolConnAddr", addrStr),
			zap.String("minerName", ss.workerName),
			zap.Uint32("reqNo", ss.reqNo),
			zap.String("agent", ss.agent),
			zap.Int64("durationSeconds", durationSeconds),
			zap.Int64("startTime", ss.startTime))
	} else {
		zap.L().Error("stopped too quickly", zap.String("sid", ss.extraNonce1),
			zap.String("reason", reason),
			zap.String("poolConnAddr", addrStr),
			zap.String("minerName", ss.workerName),
			zap.Uint32("reqNo", ss.reqNo),
			zap.String("agent", ss.agent),
			zap.Int64("durationSeconds", durationSeconds),
			zap.Int64("startTime", ss.startTime))
	}
}
