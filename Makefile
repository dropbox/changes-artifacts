# Shamelessly copied from https://github.com/dropbox/changes-client/blob/master/Makefile

BIN=${GOPATH}/bin/changes-artifacts

# Revision shows date of latest commit and abbreviated commit SHA
# E.g., 1438708515-753e183
REV=`git show -s --format=%ct-%h HEAD`

deb:
	@echo "Compiling changes-artifacts"
	@make install

	@echo "Setting up temp build folder"
	rm -rf /tmp/changes-artifacts-build
	mkdir -p /tmp/changes-artifacts-build/usr/bin
	cp $(BIN) /tmp/changes-artifacts-build/usr/bin/changes-artifacts

	@echo "Creating .deb file"
	fpm -s dir -t deb -n "changes-artifacts" -v "`$(BIN) --version`" -C /tmp/changes-artifacts-build .

install:
	@make deps
	go clean -i ./...
	go install -ldflags "-X github.com/dropbox/changes-artifacts/common.gitVersion $(REV)" -v ./...

deps:
	go get -v ./...
