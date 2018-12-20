// +build ignore

package tcprpc

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/hydrogen18/memlistener"
	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
	"golang.org/x/sync/errgroup"
)

func BenchmarkFastHTTPParallel(b *testing.B) {
	const M = 10

	s := fasthttp.Server{
		Handler: func(ctx *fasthttp.RequestCtx) {
			body := ctx.Request.Body()
			_, err := ctx.Write(body)
			assert.NoError(b, err)
		},
	}

	l := memlistener.NewMemoryListener()

	c := fasthttp.Client{}

	c.Dial = func(a string) (net.Conn, error) { return l.Dial("tcp", a) }

	go func() {
		err := s.Serve(l)
		assert.NoError(b, err)
	}()

	g, _ := errgroup.WithContext(context.TODO())

	for j := 0; j < M; j++ {
		g.Go(func() error {
			for i := 0; i < b.N/M; i++ {
				args := fasthttp.AcquireArgs()
				args.Set("func", "some_long_long_string_argument_121312312312312312312312312333312_qweqewqewqweqewqweqewqweqweqewqwe")
				code, body, err := c.Post(nil, "http://host/sleep", args)
				assert.NoError(b, err)
				if i%10000 == 0 {
					assert.Equal(b, int(200), code)
					assert.Equal(b, []byte("func=some_long_long_string_argument_121312312312312312312312312333312_qweqewqewqweqewqweqewqweqweqewqwe"), body)
				}
			}
			return nil
		})
	}

	g.Wait()
}

func BenchmarkFastHTTPParallelLong(b *testing.B) {
	const M = 10

	s := fasthttp.Server{
		Handler: func(ctx *fasthttp.RequestCtx) {
			body := ctx.Request.Body()
			time.Sleep(100 * time.Millisecond)
			_, err := ctx.Write(body)
			assert.NoError(b, err)
		},
	}

	l := memlistener.NewMemoryListener()

	c := fasthttp.Client{}

	c.Dial = func(a string) (net.Conn, error) { return l.Dial("tcp", a) }

	go func() {
		err := s.Serve(l)
		assert.NoError(b, err)
	}()

	g, _ := errgroup.WithContext(context.TODO())

	for j := 0; j < M; j++ {
		g.Go(func() error {
			for i := 0; i < b.N/M; i++ {
				args := fasthttp.AcquireArgs()
				args.Set("func", "some_long_long_string_argument_121312312312312312312312312333312_qweqewqewqweqewqweqewqweqweqewqwe")
				code, body, err := c.Post(nil, "http://host/sleep", args)
				assert.NoError(b, err)
				if i%10000 == 0 {
					assert.Equal(b, int(200), code)
					assert.Equal(b, []byte("func=some_long_long_string_argument_121312312312312312312312312333312_qweqewqewqweqewqweqewqweqweqewqwe"), body)
				}
			}
			return nil
		})
	}

	g.Wait()
}
