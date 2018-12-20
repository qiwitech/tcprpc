package tcprpc

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/hydrogen18/memlistener"
	"github.com/qiwitech/tcprpc/tcprpcpb"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/errgroup"
)

type FakeConn struct {
	bytes.Buffer
	net.Conn
}

type ProtoString string

func TestWriteReadMessageNilArgs(t *testing.T) {
	var buf FakeConn

	req := &tcprpcpb.Message{Id: 2, Func: "func_name", Error: "error_string"}

	err := writeMessage(&buf, req, nil)
	assert.NoError(t, err)

	bc := BufferConnection(&buf)

	resp, _, err := readMessage(bc, nil)
	assert.NoError(t, err)

	assert.Equal(t, req, resp)

	t.Logf("resp: %v", resp)
}

func TestWriteReadMessageArgs(t *testing.T) {
	var buf FakeConn

	req := &tcprpcpb.Message{Id: 2, Func: "func_name", Error: "error_string"}

	args := &tcprpcpb.Message{Func: "some_args_string", Id: 34}

	err := writeMessage(&buf, req, args)
	assert.NoError(t, err)

	bc := BufferConnection(&buf)

	resp, _, err := readMessage(bc, nil)
	assert.NoError(t, err)

	assert.Equal(t, req, resp)
}

func TestServer(t *testing.T) {
	s := NewServer()

	l := memlistener.NewMemoryListener()

	finished := make(chan error)
	go func() {
		err := s.Serve(l)
		finished <- err
	}()

	defer func() {
		err := s.Close()
		assert.EqualError(t, err, "Listener closed")
		err = <-finished
		assert.EqualError(t, err, "Listener closed")
	}()

	c, _ := l.Dial("", "")

	// write
	var buf proto.Buffer

	// 1
	req1 := &tcprpcpb.Message{Id: 1, Func: "func", Body: nil}

	_ = buf.EncodeMessage(req1)

	_, _ = c.Write(buf.Bytes())

	// 2
	buf.Reset()

	req2 := &tcprpcpb.Message{Id: 2, Func: "func2", Body: nil}

	_ = buf.EncodeMessage(req2)

	_, _ = c.Write(buf.Bytes())

	// read
	rbuf := make([]byte, 1000)

	// 1
	n, err := c.Read(rbuf)
	assert.NoError(t, err)

	buf.SetBuf(rbuf[:n])

	var resp tcprpcpb.Message
	err = buf.DecodeMessage(&resp)
	assert.NoError(t, err)

	assert.Equal(t, &tcprpcpb.Message{Id: 1, Error: "no such method: func"}, &resp)

	// 2
	n, err = c.Read(rbuf)
	assert.NoError(t, err)

	buf.SetBuf(rbuf[:n])

	err = buf.DecodeMessage(&resp)
	assert.NoError(t, err)

	assert.Equal(t, &tcprpcpb.Message{Id: 2, Error: "no such method: func2"}, &resp)
}

func TestSmoke(t *testing.T) {
	s := NewServer()

	s.Handle("tolower", NewHandler(
		func() proto.Message { return new(tcprpcpb.Message) },
		func(ctx context.Context, args proto.Message) (proto.Message, error) {
			msg := args.(*tcprpcpb.Message)

			msg.Func = strings.ToLower(msg.Func)

			return msg, nil
		}))

	s.Handle("toupper", NewHandler(
		func() proto.Message { return new(tcprpcpb.Message) },
		func(ctx context.Context, args proto.Message) (proto.Message, error) {
			msg := args.(*tcprpcpb.Message)

			msg.Func = strings.ToLower(msg.Func)

			return msg, nil
		}))

	l := memlistener.NewMemoryListener()

	c := NewClient("")

	c.dial = l.Dial

	finished := make(chan error)
	go func() {
		err := s.Serve(l)
		finished <- err
	}()

	defer func() {
		err := s.Close()
		assert.EqualError(t, err, "Listener closed")
		err = <-finished
		assert.EqualError(t, err, "Listener closed")
	}()

	var err error

	var reply1 tcprpcpb.Message
	err = c.Call(context.TODO(), "lower", &tcprpcpb.Message{Func: "AbCdE"}, &reply1)
	assert.EqualError(t, err, "no such method: lower")

	var reply2 tcprpcpb.Message
	err = c.Call(context.TODO(), "tolower", &tcprpcpb.Message{Func: "AbCdE"}, &reply2)
	assert.NoError(t, err)
	assert.Equal(t, "abcde", reply2.Func)

	err = c.Call(context.TODO(), "toupper", &tcprpcpb.Message{Func: "AbCdE"}, &reply1)
	assert.NoError(t, err)
	assert.Equal(t, "abcde", reply1.Func)
}

func TestHTTP(t *testing.T) {
	s := NewServer()

	s.HandleHTTP(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("hello"))
	}))

	l := memlistener.NewMemoryListener()

	go func() {
		err := s.Serve(l)
		assert.NoError(t, err)
	}()

	// 1
	cl := http.Client{
		Transport: &http.Transport{
			Dial: l.Dial,
		},
	}

	resp, err := cl.Get("http://host")
	if !assert.NoError(t, err) {
		return
	}
	data, err := ioutil.ReadAll(resp.Body)
	assert.NoError(t, err)
	err = resp.Body.Close()
	assert.NoError(t, err)

	assert.Equal(t, []byte("hello"), data)

	// 2
	cl = http.Client{
		Transport: &http.Transport{
			Dial: l.Dial,
		},
	}

	resp, err = cl.Get("http://host")
	if !assert.NoError(t, err) {
		return
	}
	data, err = ioutil.ReadAll(resp.Body)
	assert.NoError(t, err)
	err = resp.Body.Close()
	assert.NoError(t, err)

	assert.Equal(t, []byte("hello"), data)
}

func TestParallel(t *testing.T) {
	const M, N = 10, 1000

	s := NewServer()

	s.Handle("tolower", NewHandler(
		func() proto.Message { return new(tcprpcpb.Message) },
		func(ctx context.Context, args proto.Message) (proto.Message, error) {
			msg := args.(*tcprpcpb.Message)

			msg.Func = strings.ToLower(msg.Func)

			return msg, nil
		}))

	s.Handle("toupper", NewHandler(
		func() proto.Message { return new(tcprpcpb.Message) },
		func(ctx context.Context, args proto.Message) (proto.Message, error) {
			msg := args.(*tcprpcpb.Message)

			msg.Func = strings.ToUpper(msg.Func)

			return msg, nil
		}))

	l := memlistener.NewMemoryListener()

	finished := make(chan error)
	go func() {
		err := s.Serve(l)
		finished <- err
	}()

	var wg sync.WaitGroup
	wg.Add(M)

	// clients
	for j := 0; j < M; j++ {
		j := j
		go func() {
			defer wg.Done()

			c := NewClient("")
			c.dial = l.Dial
			defer func() {
				err := c.Close()
				assert.NoError(t, err)
			}()

			var err error
			var reply tcprpcpb.Message
			exp := tcprpcpb.Message{Func: "abcde"}
			meth := "tolower"
			if j%2 == 0 {
				meth = "toupper"
				exp.Func = "ABCDE"
			}

			for i := 0; i < N/M; i++ {
				err = c.Call(context.TODO(), meth, &tcprpcpb.Message{Func: "AbCdE"}, &reply)
				assert.NoError(t, err)
				assert.Equal(t, exp, reply)
			}
		}()
	}

	wg.Wait()

	err := s.Close()
	assert.EqualError(t, err, "Listener closed")

	select {
	case err = <-finished:
	case <-time.After(time.Second / 10):
		t.Errorf("served doesn't finished")
		return
	}

	assert.EqualError(t, err, "Listener closed")
}

func BenchmarkServer(b *testing.B) {
	s := NewServer()

	s.Handle("donothing", NewHandler(
		func() proto.Message { return new(tcprpcpb.Message) },
		func(ctx context.Context, args proto.Message) (proto.Message, error) {
			return args, nil
		}))

	l := memlistener.NewMemoryListener()

	c := NewClient("")

	c.dial = l.Dial

	go func() {
		err := s.Serve(l)
		assert.NoError(b, err)
	}()

	for i := 0; i < b.N; i++ {
		req := tcprpcpb.Message{Func: "some_long_long_string_argument_121312312312312312312312312333312_qweqewqewqweqewqweqewqweqweqewqwe"}
		var reply tcprpcpb.Message
		err := c.Call(context.TODO(), "donothing", &req, &reply)
		assert.NoError(b, err)
		if i%10000 == 0 {
			assert.Equal(b, req, reply)
		}
	}
}

func BenchmarkServerParallel(b *testing.B) {
	const M = 100000

	s := NewServer()

	s.Handle("donothing", NewHandler(
		func() proto.Message { return new(tcprpcpb.Message) },
		func(ctx context.Context, args proto.Message) (proto.Message, error) {
			return args, nil
		}))

	l := memlistener.NewMemoryListener()

	c := NewClient("")

	c.dial = l.Dial

	finished := make(chan error, 1)

	go func() {
		err := s.Serve(l)
		finished <- err
	}()

	g, ctx := errgroup.WithContext(context.TODO())

	for j := 0; j < M; j++ {
		g.Go(func() error {
			for i := 0; i < b.N/M; i++ {
				err := c.Call(ctx, "donothing", nil, nil)
				assert.NoError(b, err)
			}
			return nil
		})
	}

	g.Wait()

	err := s.Close()
	assert.EqualError(b, err, "Listener closed")

	select {
	case err = <-finished:
	case <-time.After(time.Second / 10):
		err = errors.New("service finish timeout")
	}
	assert.EqualError(b, err, "Listener closed")

	err = c.Close()
	assert.NoError(b, err)
}

func BenchmarkServerParallelLong(b *testing.B) {
	const M = 100000

	s := NewServer()

	s.Handle("sleep", NewHandler(
		func() proto.Message { return new(tcprpcpb.Message) },
		func(ctx context.Context, args proto.Message) (proto.Message, error) {
			time.Sleep(100 * time.Millisecond)
			return args, nil
		}))

	l := memlistener.NewMemoryListener()

	c := NewClient("")

	c.dial = l.Dial

	finished := make(chan error, 1)

	go func() {
		err := s.Serve(l)
		finished <- err
	}()

	g, ctx := errgroup.WithContext(context.TODO())

	for j := 0; j < M; j++ {
		g.Go(func() error {
			for i := 0; i < b.N/M; i++ {
				req := tcprpcpb.Message{Func: "some_long_long_string_argument_121312312312312312312312312333312_qweqewqewqweqewqweqewqweqweqewqwe"}
				var reply tcprpcpb.Message
				err := c.Call(ctx, "sleep", &req, &reply)
				assert.NoError(b, err)
				if i%10000 == 0 {
					assert.Equal(b, req, reply)
				}
			}
			return nil
		})
	}

	g.Wait()

	err := s.Close()
	assert.EqualError(b, err, "Listener closed")

	select {
	case err = <-finished:
	case <-time.After(time.Second / 10):
		err = errors.New("service finish timeout")
	}

	assert.EqualError(b, err, "Listener closed")

	err = c.Close()
	assert.NoError(b, err)
}

func BenchmarkClient(b *testing.B) {

	id := make(chan int64, 1)

	oldWr := writeMessage
	oldRd := readMessage
	defer func() {
		writeMessage = oldWr
		readMessage = oldRd
	}()

	closed := make(chan struct{})

	writeMessage = func(w io.Writer, msg *tcprpcpb.Message, args proto.Message) error {
		id <- msg.Id
		return nil
	}
	readMessage = func(c BufConn, buf []byte) (*tcprpcpb.Message, []byte, error) {
		m := getMsg()
		select {
		case m.Id = <-id:
		case <-closed:
			return nil, nil, errors.New("closed")
		}

		m.Error = ""
		return m, nil, nil
	}

	c := NewClient("")
	c.dial = func(n, a string) (net.Conn, error) { return nil, nil }
	defer func() {
		close(closed)
		err := c.Close()
		assert.EqualError(b, err, "closed")
	}()

	for i := 0; i < b.N; i++ {
		err := c.Call(context.TODO(), "meth", nil, nil)
		assert.NoError(b, err)
	}
}

func (c *FakeConn) Read(p []byte) (int, error)  { return c.Buffer.Read(p) }
func (c *FakeConn) Write(p []byte) (int, error) { return c.Buffer.Write(p) }

func (s *ProtoString) Reset()         { *s = "" }
func (s *ProtoString) String() string { return string(*s) }
func (s *ProtoString) ProtoMessage()  {}
