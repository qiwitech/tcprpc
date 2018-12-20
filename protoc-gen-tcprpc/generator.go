package main

import (
	"errors"
	"fmt"
	"go/format"
	"path"
	"path/filepath"
	"strings"

	"github.com/golang/glog"

	"github.com/golang/protobuf/proto"
	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"
	"github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway/descriptor"
	gen "github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway/generator"
	options "google.golang.org/genproto/googleapis/api/annotations"
)

var (
	errNoTargetService = errors.New("no target service defined in the file")
)

type generator struct {
	reg         *descriptor.Registry
	baseImports []descriptor.GoPackage
}

// New returns a new generator which generates grpc gateway files.
func NewGenerator(reg *descriptor.Registry) gen.Generator {
	var imports []descriptor.GoPackage
	for _, pkgpath := range []string{
		"context",
		"github.com/qiwitech/tcprpc",
		"github.com/gogo/protobuf/proto",
	} {
		pkg := descriptor.GoPackage{
			Path: pkgpath,
			Name: path.Base(pkgpath),
		}
		if err := reg.ReserveGoPackageAlias(pkg.Name, pkg.Path); err != nil {
			for i := 0; ; i++ {
				alias := fmt.Sprintf("%s_%d", pkg.Name, i)
				if err := reg.ReserveGoPackageAlias(alias, pkg.Path); err != nil {
					continue
				}
				pkg.Alias = alias
				break
			}
		}
		imports = append(imports, pkg)
	}
	return &generator{reg: reg, baseImports: imports}
}

func (g *generator) Generate(targets []*descriptor.File) ([]*plugin.CodeGeneratorResponse_File, error) {
	var files []*plugin.CodeGeneratorResponse_File
	for _, file := range targets {
		glog.V(1).Infof("Processing %s", file.GetName())
		code, err := g.generate(file)
		if err == errNoTargetService {
			glog.Errorf("%s: %v", file.GetName(), err)
			continue
		}
		if err != nil {
			return nil, err
		}

		formatted, err := format.Source([]byte(code))
		if err != nil {
			return nil, err
		}
		name := file.GetName()
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		output := fmt.Sprintf("%s.tcpgen.go", base)
		files = append(files, &plugin.CodeGeneratorResponse_File{
			Name:    proto.String(output),
			Content: proto.String(string(formatted)),
		})
		glog.V(1).Infof("Will emit %s", output)
	}
	return files, nil
}

func (g *generator) generate(file *descriptor.File) (string, error) {
	pkgSeen := make(map[string]struct{})
	var imports []descriptor.GoPackage
	for _, pkg := range g.baseImports {
		pkgSeen[pkg.Path] = struct{}{}
		imports = append(imports, pkg)
	}
	for _, svc := range file.Services {
		for _, m := range svc.Methods {
			pkg := m.RequestType.File.GoPkg
			if _, exists := pkgSeen[pkg.Path]; exists {
				continue
			}
			if m.Options == nil || !proto.HasExtension(m.Options, options.E_Http) || pkg == file.GoPkg {
				continue
			}
			pkgSeen[pkg.Path] = struct{}{}
			imports = append(imports, pkg)
		}
	}
	return applyTemplate(headerParams{File: file, Imports: imports})
}
