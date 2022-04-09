package agent

import (
	"bufio"
	"encoding/json"
	"net"

	"go.uber.org/zap"
)

/**
 * JSon RPC Go
 */

// JSONRPCRequest JSON RPC 请求的数据结构
type JSONRPCRequest struct {
	ID     interface{}   `json:"id"`
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

// JSONRPC 对应的 response 或者request 通用struct
type JSONRPC struct {
	ID     interface{}   `json:"id"`
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
	Result interface{}   `json:"result"`
	Error  []interface{} `json:"error"`
}

// JSONRPCResponse JSON RPC 响应的数据结构
type JSONRPCResponse struct {
	ID     interface{} `json:"id"`
	Result interface{} `json:"result"`
	Error  interface{} `json:"error"`
}

const (
	MINING_SUBSCRIBE = "mining.subscribe"
	MINING_AUTHORIZE = "mining.authorize"
	MINING_SET_DIFF  = "mining.set_difficulty"
	MINING_NOTIFY    = "mining.notify"
)

// JSONRPCArray JSON RPC 数组
type JSONRPCArray []interface{}

// NewJSONRPCRequest 解析 JSON RPC 请求字符串并创建 JSONRPCRequest 对象
func NewJSONRPCRequest(rpcJSON []byte) (*JSONRPCRequest, error) {
	rpcData := new(JSONRPCRequest)

	err := json.Unmarshal(rpcJSON, &rpcData)

	return rpcData, err
}

// AddParam 向 JSONRPCRequest 对象添加一个或多个参数
func (rpcData *JSONRPCRequest) AddParam(param ...interface{}) {
	rpcData.Params = append(rpcData.Params, param...)
}

// SetParam 设置 JSONRPCRequest 对象的参数
// 传递给 SetParam 的参数列表将按顺序复制到 JSONRPCRequest.Params 中
func (rpcData *JSONRPCRequest) SetParam(param ...interface{}) {
	rpcData.Params = param
}

// ToJSONBytes 将 JSONRPCRequest 对象转换为 JSON 字节序列
func (rpcData *JSONRPCRequest) ToJSONBytes() ([]byte, error) {
	return json.Marshal(rpcData)
}

// NewJSONRPCResponse 解析 JSON RPC 响应字符串并创建 JSONRPCResponse 对象
func NewJSONRPCResponse(rpcJSON []byte) (*JSONRPCResponse, error) {
	rpcData := new(JSONRPCResponse)

	err := json.Unmarshal(rpcJSON, &rpcData)

	return rpcData, err
}

// SetResult 设置 JSONRPCResponse 对象的返回结果
func (rpcData *JSONRPCResponse) SetResult(result interface{}) {
	rpcData.Result = result
}

// ToJSONBytes 将 JSONRPCResponse 对象转换为 JSON 字节序列
func (rpcData *JSONRPCResponse) ToJSONBytes() ([]byte, error) {
	return json.Marshal(rpcData)
}

type stratumProt struct {
	conn   net.Conn
	reader *bufio.Reader
}

func (sc *stratumProt) subscribe(requestID int32, agent string, extranonce1 string) {

	req := JSONRPCRequest{requestID, "mining.subscribe", nil}

	req.SetParam(agent)
	if extranonce1 != "" {
		req.AddParam(extranonce1)
	}

	buf, err := req.ToJSONBytes()
	if err != nil {
		return
	}
	zap.L().Info("JRPC Subscribe", zap.String("BUF", string(buf)))

	err = sc.writeline(buf)

	return

}

func (sc *stratumProt) writeline(line []byte) (err error) {
	buf := line[:]
	buf = append(buf, '\n')

	for cnts, err := sc.conn.Write(buf); err == nil && cnts != len(buf); buf = buf[cnts:] {
		cnts, err = sc.conn.Write(buf)
	}

	return
}

func (sc *stratumProt) authorize(requestID int32, u, p string) bool {

	req := JSONRPCRequest{requestID, "mining.authorize", nil}
	req.SetParam(u, p)

	buf, err := req.ToJSONBytes()
	if err != nil {
		return false
	}
	zap.L().Info("JRPC Authorize", zap.String("BUF", string(buf)))

	err = sc.writeline(buf)

	return err == nil

}

func (sc *stratumProt) submitShare(requestID int32, workerName, extraNonce2, nonce, jobId, sTime string, config ...string) bool {

	req := JSONRPCRequest{requestID, "mining.submit", nil}
	if len(config) > 0 {
		req.SetParam(workerName, jobId, extraNonce2, sTime, nonce, config[0])
	} else {
		req.SetParam(workerName, jobId, extraNonce2, sTime, nonce)
	}
	buf, err := req.ToJSONBytes()
	if err != nil {
		return false
	}

	err = sc.writeline(buf)

	return err == nil
}

func MakeStratumInterface(serverConn net.Conn, reader *bufio.Reader) IStratumServer {
	return &stratumProt{serverConn, reader}
}

type IStratumServer interface {
	subscribe(requestID int32, agent string, extranonce1 string)
	authorize(requestID int32, u, p string) bool
	submitShare(requestID int32, workerName, extraNonce2, nonce, jobId, sTime string, config ...string) bool
}
