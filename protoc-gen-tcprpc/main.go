package main

import (
	"io"
	"io/ioutil"
	"os"

	"github.com/golang/glog"

	"github.com/golang/protobuf/proto"
	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"
	"github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway/descriptor"
)

func main() {
	// flag.Set("logtostderr", "true") // nolint: errcheck
	// flag.Set("v", "1")              // nolint: errcheck
	// flag.Parse()

	reg := descriptor.NewRegistry()

	glog.V(1).Info("Processing code generator request")
	req, err := parseReq(os.Stdin)
	if err != nil {
		glog.Fatal(err)
	}

	if err = reg.Load(req); err != nil {
		emitError(err)
		return
	}

	g := NewGenerator(reg)

	var targets []*descriptor.File
	for _, target := range req.FileToGenerate {
		var f *descriptor.File
		f, err = reg.LookupFile(target)
		if err != nil {
			glog.Fatal(err)
		}
		targets = append(targets, f)
	}

	out, err := g.Generate(targets)
	glog.V(1).Info("Processed code generator request")
	if err != nil {
		emitError(err)
		return
	}

	emitFiles(out)
}

func parseReq(r io.Reader) (*plugin.CodeGeneratorRequest, error) {
	glog.V(1).Info("Parsing code generator request")
	input, err := ioutil.ReadAll(r)
	if err != nil {
		glog.Errorf("Failed to read code generator request: %v", err)
		return nil, err
	}
	req := new(plugin.CodeGeneratorRequest)
	if err = proto.Unmarshal(input, req); err != nil {
		glog.Errorf("Failed to unmarshal code generator request: %v", err)
		return nil, err
	}
	glog.V(1).Info("Parsed code generator request")
	return req, nil
}

func emitFiles(out []*plugin.CodeGeneratorResponse_File) {
	emitResp(&plugin.CodeGeneratorResponse{File: out})
}

func emitError(err error) {
	emitResp(&plugin.CodeGeneratorResponse{Error: proto.String(err.Error())})
}

func emitResp(resp *plugin.CodeGeneratorResponse) {
	buf, err := proto.Marshal(resp)
	if err != nil {
		glog.Fatal(err)
	}
	if _, err := os.Stdout.Write(buf); err != nil {
		glog.Fatal(err)
	}
}
