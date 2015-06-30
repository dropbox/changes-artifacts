#!/bin/sh

export GOMAXPROCS=4
cd /go/src/github.com/dropbox/changes-artifacts
go run server.go -verbose
