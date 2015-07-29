#!/bin/bash -eux

GO_VERSION=1.4

if [ ! -x /usr/local/go/bin/go ]
then
  echo "Installing Go binary...."
  cd /tmp
  wget "http://golang.org/dl/go${GO_VERSION}.linux-amd64.tar.gz"
  sudo tar -C /usr/local -xzf "go${GO_VERSION}.linux-amd64.tar.gz"
  echo "Installed Go binary...."
else
  echo "Go binary already installed"
fi

/usr/local/go/bin/go version
