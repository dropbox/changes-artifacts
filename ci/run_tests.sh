#!/bin/bash -eux

pwd
ls -lh

cd $GOPATH/src/github.com/dropbox/changes-artifacts/
go get -v ./...

go get -v github.com/jstemmer/go-junit-report

go test -short -v ./... | tee test.output | go-junit-report > junit.xml
cat test.output
