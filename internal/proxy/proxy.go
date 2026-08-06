package proxy

import (
	"io"
	"net"
	"sync"
	"time"

	"github.com/Sachinxmpl/gobalancer/cmd/gobalancer/logger"
)

type closeWriter interface {
	CloseWrite() error
}

// tells c's peer -> no more bytes from this side without disturbing  peer's reply still n flight
// if c cannot half-close, it is fully closed
func halfCloseWrite(c net.Conn) {
	if cw, ok := c.(closeWriter); ok {
		logger.Debug("half closing write side of connection", "addr", c.RemoteAddr().String())
		cw.CloseWrite()
		return
	}
	c.Close()
}

// Relays both ways between client and backend until both directions end, then close both socket at once
// When one-direction ends, its EOF is forwarded to the other side as a half-close, other open direction is given drainTimeout to finish
func L4(client, backend net.Conn, drainTimeout time.Duration) {
	logger.Info("l4 relay started",
		"client", client.RemoteAddr().String(),
		"backend", backend.RemoteAddr().String(),
		"drain_timeout", drainTimeout,
	)

	var wg sync.WaitGroup
	wg.Add(2)

	var once sync.Once

	copyOneWay := func(dst, src net.Conn) {
		defer wg.Done()

		if _, err := io.Copy(dst, src); err != nil {
			logger.Debug("l4 copy finished", "dst", dst.RemoteAddr().String(), "err", err)
		} else {
			logger.Debug("l4 copy finished", "dst", dst.RemoteAddr().String())
		}

		halfCloseWrite(dst)

		// first direction to finish sets readline for other
		once.Do(func() {
			// bound the other directon (its source is this goroutine's dst)
			dst.SetReadDeadline(time.Now().Add(drainTimeout))
		})
	}

	// client -> backed
	// exists when: client sends EOF/errors, or (as the losign half) its read deadline set by the other gorouting fires
	go copyOneWay(backend, client)

	//backend -> client
	//exists when: backend sends EOF/erros, or its read deadline fires
	go copyOneWay(client, backend)

	wg.Wait()

	client.Close()
	backend.Close()
	logger.Info("l4 relay finished",
		"client", client.RemoteAddr().String(),
		"backend", backend.RemoteAddr().String(),
	)

}
