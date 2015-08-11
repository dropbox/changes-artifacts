#!/bin/bash -eux

cd /vagrant/

support/bootstrap-ubuntu.sh

echo "alias work='cd \$GOPATH/src/github.com/dropbox/changes-artifacts'" | sudo tee /etc/profile.d/work-alias.sh

export PATH=/usr/local/go/bin:$PATH
export GOPATH=~/

# Install dependencies for 'go generate'
sudo chown -R `whoami` ~/src
go get -v github.com/vektra/mockery/cmd/mockery
go get -v github.com/jteeuwen/go-bindata/go-bindata
go get -v golang.org/x/tools/cmd/stringer
