#!/bin/bash -eux

GO_VERSION=1.4

cd /tmp
wget "http://golang.org/dl/go${GO_VERSION}.linux-amd64.tar.gz"
sudo tar -C /usr/local -xzf "go${GO_VERSION}.linux-amd64.tar.gz"
