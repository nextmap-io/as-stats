#!/bin/sh
# Removes everything up.sh created (images are kept).
STUB=$(docker inspect -f '{{ index .Config.Labels "asdemo.stub" }}' asdemo-ripe 2>/dev/null)
docker rm -f -v asdemo-web asdemo-api asdemo-ch asdemo-ripe >/dev/null 2>&1
docker network rm asdemo >/dev/null 2>&1
case "$STUB" in
  /tmp/tmp.*) rm -rf "$STUB" ;;
esac
true
