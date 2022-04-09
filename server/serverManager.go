package server

import "net"

type ServerManager struct {
	tcpListener   net.Listener
	tcpListenAddr string
}
