package proxymanager

import (
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"io"
	"net"
	"syscall"
)

type TCPConn interface {
	io.Writer
	CloseWrite() error
	io.Reader
	CloseRead() error
}

// TCPProxy is a proxy for TCP connections. It implements the Proxy interface to
// handle TCP traffic forwarding between the frontend and backend addresses.
type TCPProxy struct {
	//Logger       logger
	listener     net.Listener
	frontendAddr net.Addr
	backendAddr  *net.TCPAddr
}

// NewTCPProxy creates a new TCPProxy.
func NewTCPProxy(backendAddr *net.TCPAddr) *TCPProxy {
	proxy := &TCPProxy{
		backendAddr: backendAddr,
	}
	return proxy
}

func (proxy *TCPProxy) clientLoop(client *gonet.TCPConn, quit chan bool) {
	backend, err := net.DialTCP("tcp", nil, proxy.backendAddr)
	if err != nil {
		//proxy.Logger.Printf("Can't forward traffic to backend tcp/%v: %s\n", proxy.backendAddr, err)
		_ = client.Close()
		return
	}

	event := make(chan int64)
	broker := func(to, from TCPConn) {
		written, err := io.Copy(to, from)
		if err != nil {
			// If the socket we are writing to is shutdown with
			// SHUT_WR, forward it to the other end of the pipe:
			if err, ok := err.(*net.OpError); ok && err.Err == syscall.EPIPE {
				_ = from.CloseRead()
			}
		}
		_ = to.CloseWrite()
		event <- written
	}

	go broker(client, backend)
	go broker(backend, client)

	var transferred int64
	for i := 0; i < 2; i++ {
		select {
		case written := <-event:
			transferred += written
		case <-quit:
			// Interrupt the two brokers and "join" them.
			_ = client.Close()
			_ = backend.Close()
			for ; i < 2; i++ {
				transferred += <-event
			}
			return
		}
	}
	_ = client.Close()
	_ = backend.Close()
}

// Run starts forwarding the traffic using TCP.
func (proxy *TCPProxy) Run(listener net.Listener) {
	quit := make(chan bool)
	defer close(quit)
	proxy.listener = listener
	proxy.frontendAddr = proxy.listener.Addr()
	for {
		client, err := proxy.listener.Accept()
		if err != nil {
			//proxy.Logger.Printf("Stopping proxy on tcp/%v for tcp/%v (%s)", proxy.frontendAddr, proxy.backendAddr, err)
			return
		}
		go proxy.clientLoop(client.(*gonet.TCPConn), quit)
	}
}

// Close stops forwarding the traffic.
func (proxy *TCPProxy) Close() { _ = proxy.listener.Close() }

// FrontendAddr returns the TCP address on which the proxy is listening.
func (proxy *TCPProxy) FrontendAddr() net.Addr { return proxy.frontendAddr }

// BackendAddr returns the TCP proxied address.
func (proxy *TCPProxy) BackendAddr() net.Addr { return proxy.backendAddr }
