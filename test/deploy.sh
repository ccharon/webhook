#!/usr/bin/env bash

echo "received deployment request ${DEPLOY_ID} for ${DEPLOY_IMAGE}"

if [ "${DEPLOY_IMAGE}" == "ccharon/echoip" ] ; then

    echo "..."
    sleep 10

    echo "fertig"
fi

exit 0
