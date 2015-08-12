package main

// go get github.com/jteeuwen/go-bindata/... to install go-bindata

// We don't really care about modtime, because all migrations will be applied regardless of their
// timestamp.
//go:generate go-bindata -pkg database -modtime=1 -o database/bindata.go migrations/
