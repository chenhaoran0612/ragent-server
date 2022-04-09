package agent

import (
	"amenity.com/ragent-server/kit/cache/gredis"
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"amenity.com/ragent-server/kit"
	"go.uber.org/zap"
)

var lastMagic byte
var lastReqNo uint32
var lastRequestId uint32

func NewAgentService(am *AgentManager, conn net.Conn, agentAddr string) *AgentService {
	as := new(AgentService)
	as.isRunning = false
	as.ctx = context.Context(context.Background())
	as.StratumSessionManager = NewStratumSessionManager()
	as.startTime = time.Now().Unix()
	as.agentReader = bufio.NewReaderSize(conn, BufioReaderBufSize)
	as.agentAddr = agentAddr
	as.poolAddr = am.PoolUrl
	as.poolConnTimeOut = am.ConnTimeOut
	as.Am = am
	as.Conn = conn
	as.RedisClient = gredis.NewCache()
	return as

}

func (as *AgentService) RunListeningAgent() {
	as.lock.Lock()
	as.isRunning = true
	as.startTime = time.Now().Unix()
	as.lock.Unlock()

	as.Send2AgentChan = make(chan Msg2AgentStruct, Msg2AgentChanLen)
	// kit.G(func() {
	go as.parse2AgentMsg()
	// })

	// 然后一直不停的读取conn的信息
	for as.IsRunning() {
		msg, err := as.ReadMessage(as.agentReader)
		if err != nil {
			zap.L().Error("ReceivedClientMsgErr", zap.Error(err))
			// 处理 协议错误不断开, 其他错误断开
			if err.Error() == "incorrect magic" {
				time.Sleep(time.Millisecond * 10)
				continue
			} else {
				as.Stop("Agent Server Read Err")
				break
			}
		} else {
			zap.L().Info("ReceivedClientMsg", zap.Any("msg", msg))
			as.DoByMsg(msg)
		}
	}
}

// 处理矿池发来的信息 发送到AGENT
func (as *AgentService) parse2AgentMsg() {

	for as.IsRunning() {
		s := <-as.Send2AgentChan
		sT := time.Now()
		zap.L().Info("Send Agent Chan Start Status", zap.Int("Length", len(as.Send2AgentChan)), zap.String("Time : ", sT.String()))
		switch s.S2CType {
		//这里仅仅处理需要特殊处理的 下发JOB 这个信息
		case S2C_DISPATCH_JOB:
			// 如果JOBID为空 或者JOBID 与之前相同就不下发了 控制流量
			if as.JobId == s.jobID || len(s.jobID) <= 0 {
				continue
			}
			as.JobId = s.jobID
			//go func() {
			//	if err := recover(); err != nil {
			//		zap.L().Error("GOROUTINE RECOVERED: sendMsg2Server panic", zap.Any("Error Reason : ", err))
			//	}
			as.sendMessage2Agent(s.data, s.reqNo)
			//}()
		default:
			//go func() {
			//	if err := recover(); err != nil {
			//		zap.L().Error("GOROUTINE RECOVERED: sendMsg2Server panic", zap.Any("Error Reason : ", err))
			//	}
			as.sendMessage2Agent(s.data, s.reqNo)
			//}()
		}
		zap.L().Info("Send Agent Chan End Status", zap.Int("Length", len(as.Send2AgentChan)),
			zap.String("Time : ", time.Now().String()), zap.Int64("Spend Nanoseconds", time.Now().Sub(sT).Nanoseconds()))
	}
}

func (as *AgentService) Stop(reason string) {
	as.lock.Lock()
	as.isRunning = false
	as.lock.Unlock()
	// 结束时间
	d := time.Now().Unix() - as.startTime
	// 关闭所有的 SS  在这里加锁，看看是不是还会出现挂对情况
	as.StratumSessionManager.StratumSessionMapLock.RLock()
	for reqNo, ss := range as.StratumSessionManager.StratumSessionMap {
		ss.Stop("AgentService Stop", false, false)
		zap.L().Error("AgentService Stop Pool Session Stop", zap.String("ReqNo", reqNo))
	}
	as.StratumSessionManager.StratumSessionMapLock.RUnlock()
	// 清空对应map
	as.StratumSessionManager.StratumSessionMap = nil
	zap.L().Error(
		"AgentService Stop Finish",
		zap.String("Pool Addr ", as.poolAddr),
		zap.String("Stop Reason ", reason),
		zap.String("Agent Addr  ", as.agentAddr),
		zap.Int64("Duration", d),
	)
}

func (as *AgentService) DoByMsg(msg *AgentServiceClientRequest) {

	action := msg.MessageType
	lef := msg.Length - ProtoHeadLen
	var ss *StratumSession
	as.StratumSessionManager.StratumSessionMapLock.RLock()
	ss = as.StratumSessionManager.StratumSessionMap[fmt.Sprintf("%d-%d", msg.ServerId, msg.ReqNo)]
	as.StratumSessionManager.StratumSessionMapLock.RUnlock()
	if ss == nil {
		ss = NewStratumSession(as)
		ss.reqNo = msg.ReqNo
		ss.serverId = msg.ServerId
		err := ss.Start()
		if err != nil {
			zap.L().Error("StratumSession Start Failed", zap.Uint32("ReqNo", msg.ReqNo), zap.Uint32("ServerId", msg.ServerId))
			byt := make([]byte, lef)
			_, _ = io.ReadFull(as.agentReader, byt)
			return
		}
		as.StratumSessionManager.StratumSessionMapLock.Lock()
		as.StratumSessionManager.StratumSessionMap[fmt.Sprintf("%d-%d", msg.ServerId, msg.ReqNo)] = ss
		as.StratumSessionManager.StratumSessionMapLock.Unlock()
		zap.L().Info("New StratumSession", zap.Uint32("ReqNo", msg.ReqNo), zap.Uint32("ServerId", msg.ServerId))
	}

	lastMagic = msg.MessageType
	lastReqNo = msg.ReqNo
	lastRequestId = msg.RequestId

	switch action {
	case REGISTER_MINER:
		as.registerMiner(ss, msg, lef)
	case AUTHENTICATION_MINER:
		as.authenticationMiner(ss, msg, lef)
	case SUBMIT_SHARE:
		as.submitShareToPool(ss, msg, lef)
	case MACHINE_OFFLINE:
		as.machineOffline(ss, msg, lef)
	case PENETRATE:
		as.penetrate(ss, msg, lef)
	}

}

func (as *AgentService) penetrate(ss *StratumSession, msg *AgentServiceClientRequest, left uint16) (err error) {

	byt := make([]byte, left)

	io.ReadFull(as.agentReader, byt)
	//as.agentReader.Read(byt)
	// 直接透传就好
	ss.rpcCacheDataManager.rpcCacheDataMapLock.Lock()
	ss.rpcCacheDataManager.rpcCacheDataMap[int32(msg.RequestId)] = &RpcCacheData{
		method: PENETRATE,
		reqNo:  msg.ReqNo,
	}
	ss.rpcCacheDataManager.rpcCacheDataMapLock.Unlock()
	zap.L().Info(
		"PENETRATE",
		zap.Uint32("ReqNo", msg.ReqNo),
		zap.Any("MSG", msg),
		zap.Int32("RequestId", int32(msg.RequestId)),
		zap.ByteString("penetrateByt", byt),
	)

	if ss.poolConn != nil {
		ss.poolConn.Write(byt)
	}
	return
}

// 矿机下线
func (as *AgentService) machineOffline(ss *StratumSession, msg *AgentServiceClientRequest, left uint16) (err error) {

	// 即使没什么用 也要读出来
	//byt := make([]byte, left)
	//io.ReadFull(as.agentReader, byt)
	//as.agentReader.Read(byt)
	zap.L().Info(
		"MachineOffline",
		zap.Uint32("ReqNo", msg.ReqNo),
		zap.Any("MSG", msg),
	)

	ss.lock.Lock()
	ss.isSendMachineOffline = true
	ss.lock.Unlock()

	ss.Stop("Client Stop", true, false)
	return
}

// 算力提交
func (as *AgentService) submitShareToPool(ss *StratumSession, msg *AgentServiceClientRequest, left uint16) (err error) {

	var nonce uint32
	binary.Read(as.agentReader, binary.LittleEndian, &nonce)

	extranonce2 := make([]byte, 20)

	io.ReadFull(as.agentReader, extranonce2)
	//as.agentReader.Read(extranonce2)

	extraNonce2L := kit.FindFirst0Index(extranonce2)
	extraNonce2Str := string(extranonce2[0:extraNonce2L])

	jobIdO, err := as.agentReader.ReadBytes(0x0)
	if err != nil {
		zap.L().Error("submitShareToPoolError", zap.Error(err), zap.Uint32("ReqNo", msg.ReqNo))
		return
	}
	jobId := kit.DeleteByteTail0(jobIdO)

	var sTime uint32
	binary.Read(as.agentReader, binary.LittleEndian, &sTime)

	st := fmt.Sprintf("%.8x", sTime)
	sn := fmt.Sprintf("%.8x", nonce)

	isSuccess := false
	// 对NVersion进行特殊处理
	if int(left)-len(jobIdO)-4-4-20 != 0 {
		var nVersion uint32
		binary.Read(as.agentReader, binary.LittleEndian, &nVersion)
		sv := fmt.Sprintf("%.8x", nVersion)

		isSuccess = ss.submitShare(
			int32(msg.RequestId),
			extraNonce2Str,
			sn,
			string(jobId),
			st,
			sv,
		)
		zap.L().Info(
			"SUBMIT_SHARE",
			zap.Uint32("ReqNo", msg.ReqNo),
			zap.String("WorkerName", ss.workerName),
			zap.String("SessionId", ss.extraNonce1),
			zap.Any("MSG", msg),
			zap.Int32("RequestId", int32(msg.RequestId)),
			zap.String("extraNonce2", extraNonce2Str),
			zap.String("nonce", sn),
			zap.String("jobId", string(jobId)),
			zap.String("sTime", st),
			zap.String("nVersion", sv),
		)
	} else {

		isSuccess = ss.submitShare(
			int32(msg.RequestId),
			extraNonce2Str,
			fmt.Sprint(sn),
			string(jobId),
			fmt.Sprint(st),
		)
		zap.L().Info(
			"SUBMIT_SHARE_NO_NVERSION",
			zap.Uint32("ReqNo", msg.ReqNo),
			zap.String("WorkerName", ss.workerName),
			zap.String("SessionId", ss.extraNonce1),
			zap.Any("MSG", msg),
			zap.Int32("RequestId", int32(msg.RequestId)),
			zap.String("extraNonce2", extraNonce2Str),
			zap.String("nonce", sn),
			zap.String("jobId", string(jobId)),
			zap.String("sTime", st),
		)
	}
	if isSuccess {
		ss.rpcCacheDataManager.rpcCacheDataMapLock.Lock()
		ss.rpcCacheDataManager.rpcCacheDataMap[int32(msg.RequestId)] = &RpcCacheData{
			method: SUBMIT_SHARE,
			reqNo:  msg.ReqNo,
		}
		ss.rpcCacheDataManager.rpcCacheDataMapLock.Unlock()
	} else {
		// 如果提交失败 说明poolRPC为空 将矿机踢下线
		ss.Stop("Submit Share Error", true, true)
	}

	return
}

// 矿机认证
func (as *AgentService) authenticationMiner(ss *StratumSession, msg *AgentServiceClientRequest, left uint16) (err error) {

	workerName, err := as.agentReader.ReadBytes(0x0)
	if err != nil {
		return
	}

	password, err := as.agentReader.ReadBytes(0x0)
	if err != nil {
		return
	}
	workerName = kit.DeleteByteTail0(workerName)
	password = kit.DeleteByteTail0(password)

	workerNameStr := string(workerName)
	passwordStr := string(password)

	b := ss.authenticationMiner(int32(msg.RequestId), workerNameStr, passwordStr)

	if !b {
		zap.L().Error(
			"AUTHENTICATION_MINER ERROR",
			zap.Uint32("ReqNo", msg.ReqNo),
			zap.Any("MSG", msg),
			zap.Int32("RequestId", int32(msg.RequestId)),
			zap.String("WorkerName", workerNameStr),
			zap.String("Password", passwordStr),
		)
		ss.sendMachineOffline()
		return
	}
	zap.L().Info(
		"AUTHENTICATION_MINER",
		zap.Uint32("ReqNo", msg.ReqNo),
		zap.Any("MSG", msg),
		zap.Int32("RequestId", int32(msg.RequestId)),
		zap.String("WorkerName", workerNameStr),
		zap.String("Password", passwordStr),
	)

	// 认证时设置矿工名
	ss.workerName = workerNameStr

	ss.rpcCacheDataManager.rpcCacheDataMapLock.Lock()
	ss.rpcCacheDataManager.rpcCacheDataMap[int32(msg.RequestId)] = &RpcCacheData{
		method: AUTHENTICATION_MINER,
		reqNo:  msg.ReqNo,
	}
	ss.rpcCacheDataManager.rpcCacheDataMapLock.Unlock()

	return
}

// 矿机上线
func (as *AgentService) registerMiner(ss *StratumSession, msg *AgentServiceClientRequest, left uint16) (err error) {

	byt := make([]byte, left)
	//as.agentReader.Read(byt)
	io.ReadFull(as.agentReader, byt)
	minerName := string(byt)
	ss.rpcCacheDataManager.rpcCacheDataMapLock.Lock()
	ss.rpcCacheDataManager.rpcCacheDataMap[int32(msg.RequestId)] = &RpcCacheData{
		method: REGISTER_MINER,
		reqNo:  msg.ReqNo,
	}
	ss.rpcCacheDataManager.rpcCacheDataMapLock.Unlock()
	err = ss.poolSubscribe(int32(msg.RequestId), minerName, "00000000")
	if err != nil {
		zap.L().Error("PoolSubscribe Err", zap.String("err", err.Error()), zap.Uint32("ReqNo", msg.ReqNo))
		// 矿机异常Subscribe暂时不发
		return
	}
	zap.L().Info("REGISTER_MINER", zap.Uint32("ReqNo", msg.ReqNo), zap.Any("MSG", msg), zap.String("MinerName", minerName))

	return
}

// 发送信息到Agent 统一出口 原来的方法是直接调用，现在改为chan传递
func (as *AgentService) sendMessage2Agent(sendByte []byte, reqNo uint32) {
	sT := time.Now()
	zap.L().Info("SendMessage2AgentStart", zap.Uint32("ReqNo:", reqNo), zap.ByteString("SendByte", sendByte), zap.String("Time", sT.String()))
	as.Conn.Write(sendByte)
	zap.L().Info("SendMessage2AgentEnd", zap.Uint32("ReqNo:", reqNo), zap.ByteString("SendByte", sendByte),
		zap.String("Time", time.Now().String()), zap.Int64("Spend Nanoseconds", time.Now().Sub(sT).Nanoseconds()))
}

// ReadMessage 读取Agent 发来的数据  目前协议设计 表头字段长度为  1 + 1 + 2 + 4 + 4  = 12
func (as *AgentService) ReadMessage(src *bufio.Reader) (asc *AgentServiceClientRequest, err error) {

	magic, err := src.ReadByte()
	if err != nil {
		zap.L().Error("Read Magic Error", zap.Any("MessageType", lastMagic), zap.Uint32("LastReqNo", lastReqNo),
			zap.Uint32("LastRequestId", lastRequestId), zap.String("Pool Addr ", as.poolAddr))
		return
	}

	if magic != RA_MAGIC {
		zap.L().Error("LastMagicMessageType", zap.Any("MessageType", lastMagic), zap.Uint32("LastReqNo", lastReqNo),
			zap.Uint32("LastRequestId", lastRequestId), zap.String("Pool Addr ", as.poolAddr))

		err = errors.New("incorrect magic")
		return
	}

	messageType, err := src.ReadByte()
	if err != nil {
		zap.L().Error("Read MessageType Error", zap.Any("MessageType", lastMagic), zap.Uint32("LastReqNo", lastReqNo),
			zap.Uint32("LastRequestId", lastRequestId), zap.String("Pool Addr ", as.poolAddr))
		return
	}

	var messageLen uint16
	binary.Read(src, binary.LittleEndian, &messageLen)

	var serverId uint32
	binary.Read(src, binary.LittleEndian, &serverId)

	var reqNo uint32
	binary.Read(src, binary.LittleEndian, &reqNo)

	var requestId uint32
	binary.Read(src, binary.LittleEndian, &requestId)

	asc = &AgentServiceClientRequest{
		Magic:       magic,
		MessageType: messageType,
		Length:      messageLen,
		ServerId:    serverId,
		ReqNo:       reqNo,
		RequestId:   requestId,
	}

	return

}

// IsRunning 检查会话是否在运行（线程安全）
func (as *AgentService) IsRunning() bool {
	as.lock.Lock()
	defer as.lock.Unlock()

	return as.isRunning
}
