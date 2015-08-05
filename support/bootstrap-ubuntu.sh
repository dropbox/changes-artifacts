#!/bin/bash -eux

export DEBIAN_FRONTEND=noninteractive

GO_VERSION=1.4.2

sudo apt-get update -y

# Install go
set -ex
cd /tmp
wget "http://golang.org/dl/go${GO_VERSION}.linux-amd64.tar.gz"
tar -C /usr/local -xzf "go${GO_VERSION}.linux-amd64.tar.gz"
echo 'export PATH=/usr/local/go/bin:$PATH' > /etc/profile.d/golang.sh
echo 'export GOPATH=~/' > /etc/profile.d/gopath.sh

# Install fpm and git (required for go get)
sudo apt-get install -y ruby-dev gcc git
sudo gem install fpm --no-ri --no-rdoc
