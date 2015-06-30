FROM ruby:latest
MAINTAINER Anup Chenthamarakshan <anupc@dropbox.com>

# Almost entirely lifted from https://github.com/spurious-io/s3/blob/master/Dockerfile
# and https://registry.hub.docker.com/u/lphoward/fake-s3/dockerfile/

RUN mkdir -p /var/data/fakes3
WORKDIR /var/data/fakes3

RUN gem install fakes3

# Not having below line makes PutObject requests terribly slow  in some cases (not sure what)
RUN cd /usr/local/lib/ruby && grep -l -ri ':DoNotReverseLookup *=> nil' * | xargs sed -i "s/:DoNotReverseLookup *=> nil/:DoNotReverseLookup => true/"

EXPOSE 4569

ENTRYPOINT ["fakes3", "-r" ,"/var/data/fakes3", "-p", "4569"]
