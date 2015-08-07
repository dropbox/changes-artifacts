# Shamelessly copied from https://github.com/dropbox/changes-client/blob/master/Makefile

BIN=${GOPATH}/bin/changes-artifacts

# Revision shows date of latest commit and abbreviated commit SHA
# E.g., 1438708515-753e183
REV=`git show -s --format=%ct-%h HEAD`

# TODO(anupc): Once we have a clear database migration system in place, make sure the migration
# tool and sql files are included in the deb.

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
	# -f is needed to prevent `go get -u` from cribbing if we use git@github.com:... urls
	# instead of https://github.com/... for some repos.
	#
	# -u is used to make sure we get the latest version of all dependencies - this is
	# potentially dangerous, but is useful to make sure there are no differences between
	# packages produced by multiple users.
	go get -f -u -v ./...
