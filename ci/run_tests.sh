#!/bin/bash -eux

pwd
ls -lh

cd $GOPATH/src/github.com/dropbox/changes-artifacts/
go get -v ./...
go test -v -short ./...
