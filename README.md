***NOTICE: THIS REPO IS NO LONGER UPDATED***


Changes Artifacts
=================
An artifact server and client for use with changes. Used for storing
the results of builds in Amazon S3.

This project is in its infancy - even more so than changes itself -
and so is unstable.

There is a test suite and test environment for this project - run
"fig up" in the client/ directory to run it. This runs against
fake-s3 so there is no need for S3 credentials.

Building deb package
--------------------

```
host$ vagrant up
host$ vagrant ssh
VM$ sudo chown -R vagrant:vagrant src/
VM$ work
VM$ make
```
