#!/usr/bin/env bash

echo "received deployment request ${DEPLOY_ID} for ${DEPLOY_IMAGE}"

if [ "${DEPLOY_IMAGE}" == "ccharon/echoip" ] ; then
        cd /path/to/docker/compose/echoip
        docker compose stop
        docker compose rm -f
        docker pull ccharon/echoip:latest
        docker compose up -d

        echo "Done deploying ${DEPLOY_IMAGE}"
fi

exit 0
