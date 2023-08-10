#!/usr/bin/env bash

echo "received deployment request ${DEPLOY_ID} for ${DEPLOY_IMAGE}"

if [ "${DEPLOY_IMAGE}" == "ccharon/echoip" ] ; then
    echo "deployment started"
    sleep 10
    echo "deployment finished"
fi

exit 0
