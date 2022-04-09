package agent

import (
	"amenity.com/ragent-server/kit/cache/gredis"
	"bufio"
	"context"
	"net"
	"sync"
)

type AgentManager struct {
	Conns        []net.Conn
	AgentService []AgentService
	// Conn IP & AgentService Map
	AgentConnMap map[uint32]*AgentService
	PoolUrl      string
	ConnTimeOut  uint64
}

type AgentService struct {
	isRunning bool
	lock      sync.Mutex
	ctx       context.Context
	// 会话管理器
	Am              *AgentManager
	Sessions        []StratumSession
	poolAddr        string
	poolConnTimeOut uint64
	agentAddr       string
	agentReader     *bufio.Reader
	Conn            net.Conn
	//开始时间
	startTime int64

	JobId string
	Time  string

	StratumSessionManager *StratumSessionManager

	Send2AgentChan chan Msg2AgentStruct

	RedisClient gredis.Cache
}

type Msg2AgentStruct struct {
	data    []byte
	S2CType byte
	jobID   string
	reqNo   uint32
}
type AgentServiceClientRequest struct {
	Magic       byte
	MessageType byte
	Length      uint16
	ServerId    uint32
	ReqNo       uint32
	RequestId   uint32
}

// 缓存大小
const BufioReaderBufSize = 1024

const ProtoHeadLen = 12 + 4

const Msg2AgentChanLen = 4028

func (am *AgentManager) Run(conn net.Conn) {

	remoteAddr := conn.RemoteAddr().String()
	as := NewAgentService(am, conn, remoteAddr)

	// kit.G(func() {
	go as.RunListeningAgent()
	// })

}
