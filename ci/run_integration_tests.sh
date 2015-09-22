#!/bin/bash -x

cd $GOPATH/src/github.com/dropbox/changes-artifacts/
export PATH=$GOPATH/bin:$PATH

go get -v github.com/jstemmer/go-junit-report
go get -v ./...

go run server.go -migrations-only

go run server.go -verbose &
sudo fakes3 -r /var/cache/fakes3 -p 4569 &

go test -race -cover -v ./... | tee test.output | go-junit-report > junit.xml

statuscode=${PIPESTATUS[0]}

pkill -9 server
sudo pkill -9 fakes3

cat test.output

exit $statuscode
