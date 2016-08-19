#! /bin/bash
set -e
PROJ=./docker
stop docker | true
cp docker.ok /usr/bin/docker
start docker | true

cd $PROJ
make binary
cd -

stop docker | true
cp $PROJ/bundles/latest/binary-*/docker* /usr/bin/
start docker

echo "\n\n\n\n\n\n\n\n\n\n\n\n" >> /var/log/upstart/docker.logs
