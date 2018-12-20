package tcprpc

import (
	"context"
	"encoding/binary"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"reflect"
	"runtime/debug"
	"sync"

	"github.com/gogo/protobuf/proto"
	"github.com/qiwitech/tcprpc/tcprpcpb"
	"github.com/pkg/errors"
	"golang.org/x/net/trace"
)

type (
	Handler struct {
		alloc   func() proto.Message
		process func(context.Context, proto.Message) (proto.Message, error)
	}

	Server struct {
		http     http.Handler
		handlers map[string]Handler

		mu sync.Mutex
		l  net.Listener

		errc chan error
	}

	Peeker interface {
		Peek(int) ([]byte, error)
	}
)

var (
	writeMessage = pbWriteMessage
	readMessage  = pbReadMessage
)

func NewServer() *Server {
	return &Server{
		handlers: make(map[string]Handler),
	}
}

func (s *Server) Handle(meth string, h Handler) {
	s.handlers[meth] = h
}

func (s *Server) HandleHTTP(h http.Handler) { s.http = h }

func (s *Server) Serve(l net.Listener) error {
	s.mu.Lock()
	s.l = l
	s.errc = make(chan error, 1)
	s.mu.Unlock()

	for {
		c, err := l.Accept()
		if err != nil {
			s.errc <- err
			return err
		}

		go s.serve(c)
	}
}

func (s *Server) Close() error {
	s.mu.Lock()
	err := s.l.Close()
	err2 := <-s.errc
	s.mu.Unlock()
	if err == nil {
		err = err2
	}
	return err
}

func (s *Server) Shutdown() error {
	err := s.Close()
	// TODO(nik): wait for opened connections somehow
	return err
}

func (s *Server) serve(c net.Conn) (err error) {
	defer func() {
		if errors.Cause(err) == io.EOF {
			err = nil
		}
		if err != nil {
			log.Printf("serve error: %v", err)
		}
		if p := recover(); p != nil {
			log.Printf("serve panic: %v", p)
			debug.PrintStack()
		}
	}()

	bc := BufferConnection(c)

	ok, err := s.checkHTTP(bc)
	if err != nil {
		return errors.Wrap(err, "check HTTP")
	}

	if ok {
		err = s.serveHTTP(bc)
		return errors.Wrap(err, "http")
	}

	err = s.serveTCP(bc)
	return errors.Wrap(err, "tcp")
}

func (s *Server) serveTCP(c BufConn) (err error) {
	defer func() {
		e := c.Close()
		if err == nil {
			err = e
		}
	}()
	var rawbuf []byte

	for {
		req, buf, err := readMessage(c, rawbuf)
		if err != nil {
			return err
		}
		rawbuf = buf

		err = s.serveRequest(c, req)
		if err != nil {
			return errors.Wrap(err, "serve request")
		}
	}
}

func (s *Server) serveRequest(c io.Writer, req *tcprpcpb.Message) error {
	tr := trace.New(req.Func, "")

	tr.LazyPrintf("request: %+v", req)

	h, ok := s.handlers[req.Func]
	if !ok {
		tr.SetError()
		req.Error = "no such method: " + req.Func
		req.Func = ""
		return writeMessage(c, req, nil)
	}

	args := h.alloc()

	if args != nil {
		buf := proto.NewBuffer(req.Body)
		err := buf.Unmarshal(args)
		if err != nil {
			tr.SetError()
			req.Func = ""
			req.Error = errors.Wrap(err, "unmarshal args").Error()
			return writeMessage(c, req, nil)
		}

		tr.LazyPrintf("args: %v", args)
	}

	go func() {
		defer tr.Finish()
		defer freeMsg(req)

		ctx := context.TODO()

		reply, err := h.process(ctx, args)

		tr.LazyPrintf("processed: %v", err)

		if req.Type != tcprpcpb.Type_CALL {
			return
		}

		tr.LazyPrintf("reply: %v", reply)

		req.Func = ""
		if err != nil {
			tr.SetError()
			req.Error = err.Error()
		}

		err = writeMessage(c, req, reply)
		tr.LazyPrintf("written: %v %v", err, req)
		if err != nil {
			tr.SetError()
			log.Printf("write error: %v", err)
			return
		}
	}()

	return nil
}

func (s *Server) serveHTTP(c net.Conn) error {
	tr := trace.New("serveHTTP", "")
	defer tr.Finish()

	serv := http.Server{Handler: s.http}
	err := serv.Serve(&SingleListener{c: c})
	if err == ErrSingleListenerEOF {
		err = nil
	}
	return err
}

func (s *Server) checkHTTP(r Peeker) (bool, error) {
	peek, err := r.Peek(10)
	if err != nil {
		return false, errors.Wrap(err, "peek")
	}

	p := 0
	for p < len(peek) && peek[p] != ' ' {
		p++
	}
	if p == len(peek) || peek[p] != ' ' {
		return false, nil
	}

	switch string(peek[:p]) {
	case "HEAD", "GET", "POST", "PUT", "DELETE":
		return true, nil
	default:
		return false, nil
	}
}

func NewHandler(alloc func() proto.Message, process func(context.Context, proto.Message) (proto.Message, error)) Handler {
	return Handler{alloc: alloc, process: process}
}

func pbReadMessage(c BufConn, buf []byte) (*tcprpcpb.Message, []byte, error) {
	l, err := binary.ReadUvarint(c)
	if err != nil {
		return nil, nil, errors.Wrap(err, "read length")
	}

	buf = grow(buf, int(l))

	_, err = io.ReadFull(c, buf[:l])
	if err != nil {
		return nil, nil, errors.Wrap(err, "read message")
	}

	req := getMsg()
	err = proto.Unmarshal(buf[:l], req)
	if err != nil {
		return nil, nil, errors.Wrap(err, "unmarshal")
	}

	return req, buf, nil
}

func pbWriteMessage(w io.Writer, msg *tcprpcpb.Message, args proto.Message) error {
	var buf proto.Buffer

	if !isNil(args) {
		err := buf.Marshal(args)
		if err != nil {
			return errors.Wrap(err, "marshal args")
		}

		msg.Body = buf.Bytes()
	} else {
		msg.Body = nil
	}

	buf.SetBuf(nil)

	buf.EncodeMessage(msg)

	_, err := w.Write(buf.Bytes())
	if err != nil {
		return errors.Wrap(err, "write")
	}

	return nil
}

func freeMsg(m *tcprpcpb.Message) {
	msgPool.Put(m)
}

func RegisterHostnameHandler() {
	h, _ := os.Hostname()
	res := []byte(h)
	http.Handle("/hostname", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write(res)
	}))
}

func SetDialer(d Dialer) {
	dialer = d
}

func isNil(v interface{}) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	return r.IsNil()
}
