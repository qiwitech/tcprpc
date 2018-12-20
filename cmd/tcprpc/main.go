package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/qiwitech/tcprpc"
	"github.com/qiwitech/tcprpc/tcprpcpb"
)

var (
	addr  = flag.String("addr", "", "address to bind to")
	caddr = flag.String("call", "", "address to call remote procedure on")
)

func main() {
	flag.Parse()

	if *addr != "" {
		l, err := net.Listen("tcp", *addr)
		if err != nil {
			panic(err)
		}

		s := tcprpc.NewServer()

		s.Handle("hello", tcprpc.NewHandler(
			func() proto.Message { return new(tcprpcpb.Message) },
			func(ctx context.Context, m proto.Message) (proto.Message, error) {
				log.Printf("call: %v %v", "hello", m)
				return m, nil
			}))

		s.Handle("error", tcprpc.NewHandler(
			func() proto.Message { return nil },
			func(ctx context.Context, m proto.Message) (proto.Message, error) {
				log.Printf("call: %v %v", "error", m)
				return nil, errors.New("some_error")
			}))

		s.Handle("nil", tcprpc.NewHandler(
			func() proto.Message { return new(tcprpcpb.Message) },
			func(ctx context.Context, m proto.Message) (proto.Message, error) {
				log.Printf("call: %v %v", "nil", m)
				return nil, nil
			}))

		s.HandleHTTP(http.DefaultServeMux)

		log.Printf("Listen: %v", l.Addr())

		if flag.NArg() == 0 {
			panic(s.Serve(l))
		} else {
			go func() {
				panic(s.Serve(l))
			}()
		}
	}

	client()
}

func client() {
	var c *tcprpc.Client
	if *caddr != "" {
		c = tcprpc.NewClient(*caddr)
	}

	var err error
	var meth string
	var req, resp proto.Message
	for i := 0; i < flag.NArg(); i++ {
		arg := flag.Arg(i)
		switch arg {
		case "hello", "error", "nil":
			meth = arg
		case "call":
			err = c.Call(context.Background(), meth, req, resp)
		case "err":
			log.Printf("err: %v", err)
		case "res":
			log.Printf("res: %v", resp)
		case "req":
			req = &tcprpcpb.Message{Func: "some_arg"}
		case "resp":
			resp = &tcprpcpb.Message{}
		case "sleep":
			log.Printf("sleep")
			time.Sleep(time.Second)
		case "block":
			log.Printf("block")
			select {}
		case "wait":
			fmt.Printf("enter to continue")
			fmt.Scanln()
		case "wait1":
			fmt.Printf("enter '1' to continue\n")
			for {
				var v int
				_, err := fmt.Scanln(&v)
				if err == nil {
					break
				}
			}
		case "repeat":
			i = 0
		default:
			log.Printf("undefined command: %v", arg)
		}
	}
}
