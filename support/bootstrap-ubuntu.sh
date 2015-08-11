#!/bin/bash -eux

export DEBIAN_FRONTEND=noninteractive

install_go() {
  GO_VERSION=1.4.2

  if [ ! -x /usr/local/go/bin/go ]
  then
    echo "Installing Go binary...."
    cd /tmp
    wget "http://golang.org/dl/go${GO_VERSION}.linux-amd64.tar.gz"
    sudo tar -C /usr/local -xzf "go${GO_VERSION}.linux-amd64.tar.gz"
    echo "Installed Go binary...."

    echo 'export PATH=/usr/local/go/bin:$PATH' | sudo tee /etc/profile.d/golang.sh
    echo 'export GOPATH=~/' | sudo tee /etc/profile.d/gopath.sh
  else
    echo "Go binary already installed"
  fi

  /usr/local/go/bin/go version

  # Install git (required for go get)
  sudo apt-get install -y git
}

install_fpm() {
  # Install gem first
  sudo apt-get install -y ruby-dev gcc
  sudo gem install fpm --no-ri --no-rdoc
}

install_fakes3() {
  # Install gem first
  sudo apt-get install -y ruby-dev gcc
  sudo gem install fakes3
  sudo mkdir -p /var/cache/fakes3

  # Required for integration tests, OK to be run many times
  echo "127.0.0.1 fakes3" | sudo tee -a /etc/hosts
}

install_postgres() {
  PG_INSTALLED=1
  dpkg -s postgresql-9.3 >/dev/null 2>&1 || PG_INSTALLED=0
  if [ $PG_INSTALLED -ne 1 ]
  then
    sudo apt-get install -y postgresql-9.3
    echo "127.0.0.1 artifactsdb" | sudo tee -a /etc/hosts
    sudo -u postgres psql -U postgres << EOF
    CREATE ROLE artifacts LOGIN password 'artifacts';
    CREATE DATABASE artifacts ENCODING 'UTF8' OWNER artifacts;
EOF

    sudo sed -i "s/peer$/md5/g" /etc/postgresql/9.3/main/pg_hba.conf
    sudo service postgresql restart
  fi
}

# Required to make sure postgres install doesn't fail :(
sudo apt-get -y update

install_go
install_fpm
install_fakes3
install_postgres
