[![godoc](https://godoc.org/github.com/qiwitech/tcprpc?status.svg)](https://godoc.org/github.com/qiwitech/tcprpc)
[![Build Status](https://travis-ci.org/qiwitech/tcprpc.svg?branch=master)](https://travis-ci.org/qiwitech/tcprpc/builds)

# tcprpc

tcprpc is an RPC framework and protobuf service generator.
It's a part of [qdp](https://github.com/qiwitech/qdp) project.
You only need this to have plutos system compiled successfully.

It's designed to be simple, lightwait and optimized for lots of parallel requests.
It uses protobuf encoded requests and responses over raw tcp connection.

If you are looking for some general purpose RPC framework you'd better to use [gRPC](https://github.com/grpc/grpc)
