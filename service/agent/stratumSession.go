package agent

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"go.uber.org/zap"
)

func NewStratumSession(as *AgentService) *StratumSession {
	ss := new(StratumSession)
	ss.agentService = as
	ss.rpcCacheDataManager.rpcCacheDataMap = make(map[int32]*RpcCacheData, 100)
	return ss
}

// IsRunning 检查会话是否在运行（线程安全）
func (ss *StratumSession) IsRunning() bool {
	ss.lock.Lock()
	defer ss.lock.Unlock()

	return ss.isRunning
}

func (ss *StratumSession) Start() (err error) {

	if ss.IsRunning() {
		return
	}
	err = ss.connectPool()
	if err != nil {
		zap.L().Error("Pool Conn Error", zap.String("err", err.Error()))
		return err
	}
	// kit.G(func() {
	go ss.runListeningPool()
	// })
	return
}

func (ss *StratumSession) connectPool() (err error) {

	ss.startTime = time.Now().Unix()
	poolAddr := ss.agentService.poolAddr
	zap.L().Info("ConnectPool ", zap.Uint32("ReqNo", ss.reqNo), zap.String("Pool Addr", poolAddr))
	if len(poolAddr) == 0 {
		ss.sendMachineOffline()
		return errors.New("Agent Pool Address Error")
	}

	ss.poolConn, err = net.DialTimeout("tcp", poolAddr, time.Millisecond*time.Duration(ss.agentService.poolConnTimeOut))
	if err != nil {
		ss.sendMachineOffline()
		return errors.New("Pool connect timeout")
	}
	ss.isRunning = true
	ss.poolReader = bufio.NewReaderSize(ss.poolConn, BufioReaderBufSize)
	ss.poolRPC = MakeStratumInterface(ss.poolConn, ss.poolReader)

	return
}

func (ss *StratumSession) runListeningPool() {
	defer ss.Stop("Pool IO Stop", true, true)
	for ss.IsRunning() {
		req, buf, err := ss.readPoolMsg()
		// 读取信息出错了 直接停止
		if err != nil {

			if ss.IsRunning() {
				zap.L().Error("Pool Msg Error", zap.Uint32("ReqNo", ss.reqNo), zap.String("err", err.Error()))
			}

			ss.lock.Lock()
			ss.isRunning = false
			ss.lock.Unlock()
		} else {
			// 判断对应的类型，发送给 RAGENT-CLIENT
			ss.sendMessage(req, buf)
		}
	}
}

func (ss *StratumSession) readPoolMsg() (req JSONRPC, buf []byte, err error) {

	buf, err = ss.poolReader.ReadBytes('\n')

	zap.L().Info("PoolMessageRead", zap.Uint32("ReqNo", ss.reqNo), zap.ByteString("Data", buf))

	if err != nil {
		return
	}

	req = JSONRPC{}
	err = json.Unmarshal(buf, &req)
	if err != nil {
		zap.L().Error("runListeningPool Err", zap.Error(err))
	}

	return
}

func (ss *StratumSession) authenticationMiner(requestId int32, workerNameStr, passwordStr string) (b bool) {
	if ss.poolRPC == nil {
		b = false
		return
	}
	b = ss.poolRPC.authorize(requestId, workerNameStr, passwordStr)
	return
}

func (ss *StratumSession) submitShare(requestId int32, extraNonce2, nonce, jobId, sTime string, config ...string) (b bool) {
	if ss.poolRPC == nil {
		zap.L().Error("PoolRPC is Nil")
		return false
	}
	b = ss.poolRPC.submitShare(requestId, string(ss.workerName), extraNonce2, nonce, jobId, sTime, config...)
	return true
}

// 需要给SS extraNonce1 和 extraNonce2size 对应的是矿池的返回
// 并且开启 SessionId extraNonce1后续是对应的SessionID的原始 对应的goroutine
func (ss *StratumSession) poolSubscribe(reqId int32, agentName, exn1 string) (err error) {

	// exn1 := ""
	if ss.poolRPC == nil {
		zap.L().Error("PoolSubscribe is Nil")
		return errors.New("PoolSubscribe is Nil")
	}

	ss.poolRPC.subscribe(reqId, agentName, exn1)
	if err != nil {
		zap.L().Error("read subscribe req failed", zap.Error(err))
		return err
	}

	ss.requestID = reqId

	return
}

func (ss *StratumSession) sendMessage(jrpc JSONRPC, buf []byte) (err error) {

	ss.lock.Lock()
	defer ss.lock.Unlock()

	// 判断 是否有Method 来确定是矿池响应矿机, 还是矿池请求(下发配置)
	if jrpc.Method != "" {
		zap.L().Info("SendMessageWithMethod", zap.Uint32("ReqNo", ss.reqNo), zap.Any("Jrpc", jrpc), zap.ByteString("Byte", buf))
		switch jrpc.Method {
		case MINING_SET_DIFF:
			err = ss.sendSetDiff2Agent(jrpc)
			return
		case MINING_NOTIFY:
			err = ss.sendNotify2Agent(jrpc, buf)
			return
		default:
			ss.sendPene(buf)
			return
		}
	} else {
		if jrpc.ID != nil {
			reqId := jrpc.ID.(float64)
			reqI := int32(reqId)

			ss.rpcCacheDataManager.rpcCacheDataMapLock.RLock()
			cacheData := ss.rpcCacheDataManager.rpcCacheDataMap[reqI]
			ss.rpcCacheDataManager.rpcCacheDataMapLock.RUnlock()

			if cacheData == nil {
				zap.L().Debug("SendMessageRpcCacheDataMapNil", zap.Uint32("ReqNo", ss.reqNo), zap.Any("SS RpcCacheDataMap", "nil"))
				ss.sendPenetrate2Agent(buf)
				return
			}

			zap.L().Debug("SendMessageWithCache", zap.Uint32("ReqNo", ss.reqNo), zap.Any("Jrpc", jrpc), zap.ByteString("Byte", buf), zap.Any("Cache", cacheData))
			switch cacheData.method {
			case SUBMIT_SHARE:
				errStr := ""
				f := true
				if jrpc.Result == nil || !jrpc.Result.(bool) {
					errStrA := jrpc.Error
					errStr = errStrA[1].(string)
					f = false
				}
				ss.sendShareSubmitRes2Agent(reqI, f, errStr)
				return
			case AUTHENTICATION_MINER:
				f := true
				if jrpc.Result == nil || !jrpc.Result.(bool) {
					f = false
				}
				ss.sendAuthentication2Agent(reqI, f)
			case REGISTER_MINER:
				resultList, ok := jrpc.Result.([]interface{})
				if !ok {
					zap.L().Error("REGISTER_MINER_ERR", zap.Uint32("ReqNo", ss.reqNo), zap.Any("REGISTER_MINER Change Error", resultList))
					return
				}
				ss.extraNonce1 = resultList[1].(string)
				ss.extraNonce2size = int64(resultList[2].(float64))
				zap.L().Warn("REGISTER_MINER", zap.Uint32("ReqNo", ss.reqNo), zap.Any("REGISTER_MINER", resultList))
				ss.sendRegisterMsg2Agent(int32(ss.reqNo), reqI)
			default:
				ss.sendPenetrate2Agent(buf)
			}
			// 删除对应的rcpCacheDataMap
			delete(ss.rpcCacheDataManager.rpcCacheDataMap, reqI)
		} else {
			ss.sendPenetrate2Agent(buf)
		}
	}
	//ss.sendPenetrate2Agent(buf)
	return
}

func (ss *StratumSession) sendPene(buf []byte) {

	bytesBuff := ss.getSendCommonByte(0, S2C_PENETRATE, len(buf))
	bytesBuff.Write(buf)
	ss.sendMessage2Chan(bytesBuff, S2C_PENETRATE, "")
}

func (ss *StratumSession) sendMachineOffline() {
	if ss.isSendMachineOffline {
		return
	}

	bytesBuff := ss.getSendCommonByte(0, S2C_MACHINE_OFFLINE, 0)
	ss.sendMessage2Chan(bytesBuff, S2C_MACHINE_OFFLINE, "")

	ss.lock.Lock()
	ss.isSendMachineOffline = true
	ss.lock.Unlock()
}

// 发送矿机上线消息给Agent
func (ss *StratumSession) sendRegisterMsg2Agent(reqN, reqId int32) {

	extraNonce2size := make([]byte, 2)
	binary.LittleEndian.PutUint16(extraNonce2size, uint16(ss.extraNonce2size))

	sessionIdBytes := []byte(ss.extraNonce1)

	bytesBuff := ss.getSendCommonByte(uint32(reqId), S2C_REGISTER_MINER, 2+1+len(sessionIdBytes))

	bytesBuff.Write(extraNonce2size)
	bytesBuff.Write(sessionIdBytes)
	bytesBuff.WriteByte(0x0)

	ss.sendMessage2Chan(bytesBuff, S2C_REGISTER_MINER, "")
}

// 发送返回到矿池
func (ss *StratumSession) sendShareSubmitRes2Agent(reqI int32, s bool, reason string) {

	var sb uint16
	sb = uint16(0)
	if s {
		sb = uint16(1)
	}

	sbByte := make([]byte, 2)
	binary.LittleEndian.PutUint16(sbByte, sb)

	reasonByte := []byte(reason)

	bytesBuff := ss.getSendCommonByte(uint32(reqI), S2C_SUBMIT_SHARE, 2+1+len(reasonByte))

	bytesBuff.Write(sbByte)
	bytesBuff.Write(reasonByte)
	bytesBuff.WriteByte(0x0)

	ss.sendMessage2Chan(bytesBuff, S2C_SUBMIT_SHARE, "")
}

func (ss *StratumSession) sendAuthentication2Agent(reqI int32, s bool) {

	var sb uint16
	sb = uint16(0)
	if s {
		sb = uint16(1)
	}

	sbByte := make([]byte, 2)
	binary.LittleEndian.PutUint16(sbByte, sb)

	bytesBuff := ss.getSendCommonByte(uint32(reqI), S2C_AUTHENTICATION_MINER, 2)

	bytesBuff.Write(sbByte)

	ss.sendMessage2Chan(bytesBuff, S2C_AUTHENTICATION_MINER, "")
}

// 发送透传信息
func (ss *StratumSession) sendPenetrate2Agent(buf []byte) {

	bytesBuff := ss.getSendCommonByte(0, S2C_PENETRATE, len(buf))

	bytesBuff.Write(buf)
	ss.sendMessage2Chan(bytesBuff, S2C_PENETRATE, "")
}

// 发送信息到Agent 统一出口
func (ss *StratumSession) sendMessage2Chan(bytesBuff bytes.Buffer, t byte, jobId string) {

	sendByte := bytesBuff.Bytes()
	zap.L().Debug("sendMessage2Chan", zap.Uint32("ReqNo", ss.reqNo), zap.ByteString("Byte", sendByte))
	//chan传递
	ss.agentService.Send2AgentChan <- Msg2AgentStruct{
		data:    sendByte,
		S2CType: t,
		jobID:   jobId,
		reqNo:   ss.reqNo,
	}
}

func (ss *StratumSession) sendSetDiff2Agent(req JSONRPC) (err error) {

	var diff uint32
	if req.Params[0] != nil {
		diff = uint32(req.Params[0].(float64))
	}

	diffByte := make([]byte, 4)
	binary.LittleEndian.PutUint32(diffByte, diff)

	bytesBuff := ss.getSendCommonByte(0, S2C_MINING_SET_DIFF, 4)
	bytesBuff.Write(diffByte)

	ss.sendMessage2Chan(bytesBuff, S2C_MINING_SET_DIFF, "")

	return
}

func (ss *StratumSession) sendNotify2Agent(req JSONRPC, buf []byte) (err error) {

	data := JSONRPCRequest{}
	sendData := JSONRPCRequest{}
	err = json.Unmarshal(buf, &data)
	err = json.Unmarshal(buf, &sendData)
	dataJson, err := json.Marshal(data)

	if err != nil {
		zap.L().Error("SendCommonNotifyError", zap.Uint32("ReqNo", ss.reqNo), zap.ByteString("DATA JSON", dataJson))
		return
	}
	jobId := req.Params[0].(string)
	md5OriStr := data.Params[1].(string) + data.Params[3].(string) + fmt.Sprintf("%s", data.Params[4]) + data.Params[7].(string)
	keyStr := fmt.Sprintf("%d %x", ss.serverId, md5.Sum([]byte(md5OriStr)))

	_, err = ss.agentService.RedisClient.GetStr("key:" + keyStr)

	// 加入日志观察一下是不是判断类型少了
	if err != nil {
		zap.L().Error("Server Redis Find Error", zap.Error(err), zap.Uint32("ReqNo", ss.reqNo))
	}

	// 如果没有发现就设置一下
	if err != nil && err.Error() == "redigo: nil returned" {
		zap.L().Debug("SendCommonNotifyData", zap.Uint32("ReqNo", ss.reqNo), zap.String("key", keyStr), zap.ByteString("Data", dataJson))
		sendCommonData := &data
		sendCommonData.Params[0] = ""
		sendCommonData.Params[2] = ""
		sendCommonData.Params[5] = ""
		sendCommonData.Params[6] = ""
		commonJson, err := json.Marshal(sendCommonData)
		if err != nil {
			zap.L().Error("SendCommonNotifyError", zap.Uint32("ReqNo", ss.reqNo), zap.ByteString("DATA JSON", dataJson))
			return nil
		}
		commonBytesBuff := ss.getSendCommonByte(0, S2C_DISPATCH_WORKER_COMMON_JOB, len(commonJson)+1)
		commonBytesBuff.Write(commonJson)
		commonBytesBuff.WriteByte('\n')
		ss.sendMessage2Chan(commonBytesBuff, S2C_DISPATCH_WORKER_COMMON_JOB, jobId)
		ss.agentService.RedisClient.SetStr("key:"+keyStr, string(dataJson[:]), 3*60*1000)
	}

	// 如果是redis挂了这种情况就走透传
	var dataJsonSend []byte
	if err != nil && err.Error() != "redigo: nil returned" {
		dataJsonSend, err = json.Marshal(sendData)
		if err != nil {
			zap.L().Error("SendNotifyError", zap.Uint32("ReqNo", ss.reqNo), zap.ByteString("DATA JSON", dataJson))
			return nil
		}
	} else {
		sendData.Params[1] = keyStr
		sendData.Params[3] = ""
		sendData.Params[4] = ""
		sendData.Params[7] = ""
		dataJsonSend, err = json.Marshal(sendData)
		if err != nil {
			zap.L().Error("SendNotifyError", zap.Uint32("ReqNo", ss.reqNo), zap.ByteString("DATA JSON", dataJson))
			return nil
		}
	}

	var str = string(dataJsonSend[:])
	zap.L().Debug("SendNotifyData : ", zap.String("Data : ", str))
	bytesBuff := ss.getSendCommonByte(0, S2C_DISPATCH_WORKER_JOB, len(str)+1)
	bytesBuff.Write([]byte(str))
	bytesBuff.WriteByte('\n')
	ss.sendMessage2Chan(bytesBuff, S2C_DISPATCH_WORKER_JOB, jobId)
	return nil

	return
}

// 获取通用发送的数组
func (ss *StratumSession) getSendCommonByte(reqId uint32, job byte, extraLen int) (bytesBuff bytes.Buffer) {

	messageLen := COMMON_SEND_BYTE_LENTH + extraLen
	messageLenBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(messageLenBytes, uint16(messageLen))

	serverId := make([]byte, 4)
	binary.LittleEndian.PutUint32(serverId, uint32(ss.serverId))

	reqNo := make([]byte, 4)
	binary.LittleEndian.PutUint32(reqNo, uint32(ss.reqNo))
	reqIdByte := make([]byte, 4)
	binary.LittleEndian.PutUint32(reqIdByte, reqId)

	bytesBuff.WriteByte(RA_MAGIC)
	bytesBuff.WriteByte(job)
	bytesBuff.Write(messageLenBytes)
	bytesBuff.Write(serverId)
	bytesBuff.Write(reqNo)
	bytesBuff.Write(reqIdByte)

	return
}
