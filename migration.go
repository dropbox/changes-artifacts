package main

// go get github.com/jteeuwen/go-bindata/... to install go-bindata

//go:generate go-bindata -pkg database -o database/bindata.go migrations/
