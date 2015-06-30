#!/bin/sh

cd /go/src/github.com/dropbox/changes-artifacts

# Import missing dependencies
go get -v ./...

/mnt/run_server.sh &
# No need to wait here. Test implicitly waits for the server to be running.
go test -v ./client/... && echo '\n\n\n' && go test -cover ./client/...
pkill -9 artifacts
