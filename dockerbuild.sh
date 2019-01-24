#!/usr/bin/env bash

WHO=$(who am i | awk '{print $1}')
VERSION=$(make -s version)

echo "who am i: $WHO"
echo "version: $VERSION"

docker build -t tmpreaper:$VERSION . 1>/dev/null && CID=$(docker create tmpreaper:$VERSION) && docker cp $CID:/home/reaper/reaper . && chown $WHO:users reaper && docker container rm $CID && docker image rm tmpreaper:$VERSION

