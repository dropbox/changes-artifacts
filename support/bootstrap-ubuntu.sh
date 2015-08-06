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

# Install postgresql for integration tests
sudo apt-get install -y postgresql-9.3
echo "127.0.0.1 artifactsdb" >> /etc/hosts
sudo -u postgres psql -U postgres << EOF
CREATE ROLE artifacts LOGIN password 'artifacts';
CREATE DATABASE artifacts ENCODING 'UTF8' OWNER artifacts;
EOF

sudo sed -i "s/peer$/md5/g" /etc/postgresql/9.3/main/pg_hba.conf
sudo service postgresql restart

# Install fakes3
gem install fakes3
sudo mkdir -p /var/cache/fakes3
echo "127.0.0.1 fakes3" >> /etc/hosts
