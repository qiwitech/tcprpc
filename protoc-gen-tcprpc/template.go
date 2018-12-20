package main

import (
	"bytes"
	"strings"
	"text/template"

	"github.com/golang/glog"
	"github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway/descriptor"
)

var (
	funcLower = template.FuncMap{
		"ToLower": strings.ToLower,
	}
)

func applyTemplate(p headerParams) (string, error) {
	w := bytes.NewBuffer(nil)
	if err := headerTemplate.Execute(w, p); err != nil {
		return "", err
	}
	for _, svc := range p.Services {
		glog.V(1).Infof("Processing service %s", svc.GetName())

		for _, m := range svc.Methods {
			glog.V(1).Infof("\tmethod %s.%s", svc.GetName(), m.GetName())
			// Приведение имен к виду
			methName := strings.Title(*m.Name)
			m.Name = &methName
			// trim package name
			inputType := (*m.InputType)[len(p.GetPackage())+2:]
			m.InputType = &inputType
			outputType := (*m.OutputType)[len(p.GetPackage())+2:]
			m.OutputType = &outputType
		}

		if err := serviceTemplate.Execute(w, map[string]interface{}{"Service": svc}); err != nil {
			return "", err
		}

		if err := interfaceTemplate.Execute(w, bindingParams{Service: svc, Methods: svc.Methods}); err != nil {
			return "", err
		}
	}

	return w.String(), nil
}

type headerParams struct {
	*descriptor.File
	Imports []descriptor.GoPackage
}

var (
	headerTemplate = template.Must(template.New("header").Parse(`// Code generated by protoc-gen-tcpgen
// source: {{.GetName}}
// DO NOT EDIT!

/*
Package {{.GoPkg.Name}} is a http proxy.
*/

package {{.GoPkg.Name}}

import (
{{range $i := .Imports}}{{if $i.Standard}}{{$i | printf "\t%s\n"}}{{end}}{{end}}

{{range $i := .Imports}}{{if not $i.Standard}}{{$i | printf "\t%s\n"}}{{end}}{{end}}
)
`))
)

// type descriptor.Service

var (
	serviceTemplate = template.Must(template.New("service").Parse(`
func Register{{.Service.Name}}Handlers(s *tcprpc.Server, prefix string, srv {{.Service.Name}}Interface) {
{{range $m := .Service.Methods}}
	s.Handle(prefix + "{{$m.Name}}", tcprpc.NewHandler(
		func() proto.Message { return new({{$m.InputType}}) },
		func(ctx context.Context, inp proto.Message) (proto.Message, error) {
			args := inp.(*{{$m.InputType}})
			return srv.{{$m.Name}}(ctx, args)
		}))
{{end}}
}

type TCPRPC{{.Service.Name}}Client struct {
	cl *tcprpc.Client
	pref string
}
func NewTCPRPC{{.Service.Name}}Client(cl *tcprpc.Client, pref string) TCPRPC{{.Service.Name}}Client {
	return TCPRPC{{.Service.Name}}Client{
		cl: cl,
		pref: pref,
	}
}

{{range $m := .Service.Methods}}
func (cl TCPRPC{{$.Service.Name}}Client) {{$m.Name}}(ctx context.Context, args *{{$m.InputType}}) (*{{$m.OutputType}}, error) {
	var resp {{$m.OutputType}}
	err := cl.cl.Call(ctx, cl.pref + "{{$m.Name}}", args, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}
{{end}}
`))
)

type bindingParams struct {
	Service *descriptor.Service
	Methods []*descriptor.Method
}

var (
	interfaceTemplate = template.Must(template.New("interface").Parse(``))
)
