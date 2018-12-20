# protoc-gen-httpgw

  protoc code generator

## Назначение:

  Генерация кода http сервера и клиента по .proto файлу.

## Установка

```
go install github.com/qiwitech/tcprpc/protoc-gen-tcpgen
```


## Зависимости

1. Сгенерированный код зависит от кода, полученного с помощью github.com/gogo/protobuf из того же .proto файла. Поэтому имеет смысл установить:

  ```
  # https://github.com/gogo/protobuf
  go get github.com/gogo/protobuf/protoc-gen-gogo

  ```

## Пример совместного использования генераторов (см. example/):

```
protoc -I${GOPATH}/src -I. --gogo_out=:./ srv.proto
protoc -I${GOPATH}/src -I. --httpgw_out=:./ srv.proto
```
