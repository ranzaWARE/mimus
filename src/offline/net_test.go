package offline

import "net"

func netListen() (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") }

func netListenAddr(addr string) (net.Listener, error) { return net.Listen("tcp", addr) }

func acceptLoop(ln net.Listener, stop chan struct{}) {
	for {
		c, err := ln.Accept()
		if err != nil {
			select {
			case <-stop:
			default:
			}
			return
		}
		_ = c.Close()
	}
}
