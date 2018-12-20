package tcprpc

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"math/rand"
	"net"
	"sync"

	"github.com/gogo/protobuf/proto"
	"github.com/qiwitech/tcprpc/tcprpcpb"
	"github.com/pkg/errors"
	"golang.org/x/net/trace"
)

var ErrConnClosed = errors.New("connection closed")

type (
	Dialer func(net, addr string) (net.Conn, error)

	Reader interface {
		io.Reader
		io.ByteReader
	}

	BufConn struct {
		*bufio.Reader
		net.Conn
	}

	Client struct {
		addr string
		dial Dialer

		prefix string

		send  chan *callReq
		stopc chan struct{}

		readerErrCh chan error
		stopped     bool

		mu       sync.Mutex
		inflight map[int64]*callReq
		conn     io.WriteCloser
	}

	callReq struct {
		msg  *tcprpcpb.Message
		errc chan error
		conn io.Reader
	}
)

var (
	dialer = net.Dial

	msgPool = sync.Pool{New: func() interface{} { return new(tcprpcpb.Message) }}
)

func NewClient(addr string) *Client {
	cl := &Client{
		addr:     addr,
		dial:     dialer,
		inflight: make(map[int64]*callReq),
		send:     make(chan *callReq, 1),
		stopc:    make(chan struct{}),
	}
	cl.readerErrCh = make(chan error, 1)
	cl.readerErrCh <- nil //reader has not started
	go cl.netWriter()
	return cl
}

func (c *Client) Call(ctx context.Context, meth string, args, reply interface{}) error {
	tr := trace.New("rpc."+c.prefix+meth, c.addr)
	defer tr.Finish()

	req := getMsg()

	req.Type = tcprpcpb.Type_CALL
	req.Func = c.prefix + meth

	if args, ok := args.(proto.Message); ok {
		var err error
		req.Body, err = proto.Marshal(args)
		if err != nil {
			tr.SetError()
			tr.LazyPrintf("marshal: %v", err)
			return err
		}
		tr.LazyPrintf("args: %v", args)
	} else {
		req.Body = nil
	}

	call := callReq{
		msg:  req,
		errc: make(chan error, 1),
	}

	c.send <- &call
	tr.LazyPrintf("sent")

	var err error
	select {
	case err = <-call.errc:
		tr.LazyPrintf("response err: %v", err)
	case <-ctx.Done():
		err = ctx.Err()
		tr.LazyPrintf("context.Done: %v", err)
	}
	if err != nil {
		tr.SetError()
		return err
	}

	if call.msg.Error != "" {
		tr.SetError()
		tr.LazyPrintf("request error: %v", call.msg.Error)
		return errors.New(call.msg.Error)
	}

	if reply, ok := reply.(proto.Message); ok {
		err = proto.Unmarshal(call.msg.Body, reply)
		if err != nil {
			tr.SetError()
			tr.LazyPrintf("unmarshal reply: %v", err)
			return err
		}
		tr.LazyPrintf("reply: %v", reply)
	}

	return nil
}

func (c *Client) Addr() string {
	return c.addr
}

func (c *Client) Close() error {
	close(c.stopc) // stop all background workers

	c.mu.Lock()
	c.stopped = true // don't start new requests
	_ = c.conn.Close()
	c.mu.Unlock()

	err := <-c.readerErrCh // wait for reader to stop

	return err
}

func (c *Client) getConn() (io.WriteCloser, error) {
	if c.stopped {
		return nil, ErrConnClosed
	}

	if c.conn != nil {
		return c.conn, nil
	}

	conn, err := c.dial("tcp", c.addr)
	if err != nil {
		return nil, err
	}

	_ = <-c.readerErrCh // wait for previous reader to stop

	go c.netReader(BufferConnection(conn)) // start new reader

	c.conn = conn

	return conn, nil
}

func (c *Client) disconnect() error {
	c.mu.Lock()
	err := c.conn.Close()
	c.conn = nil
	c.mu.Unlock()
	return err
}

func (c *Client) netReader(conn Reader) (err error) {
	ev := trace.NewEventLog("client.reader", c.addr)
	defer ev.Finish()

	defer func() {
		c.mu.Lock()
		ev.Printf("close calls: %v", c.inflight)
		for id, call := range c.inflight {
			delete(c.inflight, id)
			call.errc <- err
		}
		c.conn.Close()
		c.conn = nil
		c.mu.Unlock()
		select {
		case <-c.stopc:
			err = nil
		default:
		}
		ev.Printf("finished with error: %v", err)
		c.readerErrCh <- err
	}()

	var buf []byte
	for {
		l, err := binary.ReadUvarint(conn)
		if err != nil {
			ev.Errorf("read length: %v", err)
			return errors.Wrap(err, "read length")
		}

		buf = grow(buf, int(l))

		_, err = io.ReadFull(conn, buf[:l])
		if err != nil {
			ev.Errorf("read message: %v", err)
			return errors.Wrap(err, "read message")
		}

		msg := getMsg()
		err = proto.Unmarshal(buf[:l], msg)
		if err != nil {
			ev.Errorf("unmarshal: %v", err)
			return errors.Wrap(err, "unmarshal")
		}

		c.mu.Lock()
		call, ok := c.inflight[msg.Id]
		delete(c.inflight, msg.Id)
		c.mu.Unlock()
		ev.Printf("response to %v, ok %v", msg.Id, ok)
		if !ok {
			continue
		}

		call.msg = msg

		call.errc <- nil
	}
}

func (c *Client) netWriter() {
	ev := trace.NewEventLog("client.writer", c.addr)
	defer ev.Finish()

	var buf proto.Buffer
	for {
		var call *callReq
		select {
		case call = <-c.send:
		case <-c.stopc:
			ev.Errorf("stopped")
			return
		}

		c.mu.Lock()
	retry:
		id := rand.Int63()
		if _, ok := c.inflight[id]; ok {
			goto retry
		}

		call.msg.Id = id
		c.inflight[id] = call

		conn, err := c.getConn()
		c.mu.Unlock()
		ev.Printf("serve call: %v, conn: %v, err %v", id, conn, err)
		if err != nil {
			ev.Errorf("getConn: %v", err)
			call.errc <- err
			continue
		}

		buf.Reset()

		buf.EncodeMessage(call.msg)

		_, err = conn.Write(buf.Bytes())
		ev.Printf("written: %v", err)
		if err != nil {
			ev.Errorf("write: %v", err)
			c.disconnect()
			call.errc <- err
			continue
		}
	}
}

func getMsg() *tcprpcpb.Message {
	m := msgPool.Get().(*tcprpcpb.Message)
	return m
}

func BufferConnection(c net.Conn) BufConn {
	return BufConn{Conn: c, Reader: bufio.NewReader(c)}
}

func (b BufConn) Read(p []byte) (int, error) {
	return b.Reader.Read(p)
}

func grow(buf []byte, l int) []byte {
	if cap(buf) >= l {
		return buf
	}

	if len(buf) < 1024 {
		if cap(buf)*4/3 > l {
			l = cap(buf) * 4 / 3
		}
	} else {
		if cap(buf)*2 > l {
			l = cap(buf) * 2
		}
	}

	return make([]byte, l)
}
