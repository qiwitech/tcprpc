package tcprpc

import (
	"net"

	"github.com/pkg/errors"
)

var ErrSingleListenerEOF = errors.New("single listener: EOF")

type SingleListener struct {
	c net.Conn
}

func (l *SingleListener) Accept() (net.Conn, error) {
	if l.c == nil {
		return nil, ErrSingleListenerEOF
	}
	c := l.c
	l.c = nil
	return c, nil
}

func (l *SingleListener) Close() error { return nil }

func (l *SingleListener) Addr() net.Addr { return nil }
