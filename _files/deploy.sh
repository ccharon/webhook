#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# SPDX-FileCopyrightText: 2023 Christian Charon

echo "received deployment request ${WEBHOOK_ID} for ${WEBHOOK_PARAM}"

if [ "${WEBHOOK_PARAM}" == "ccharon/echoip" ] ; then
        cd /path/to/compose/echoip || exit 1
        docker compose stop
        docker compose rm -f
        docker pull ccharon/echoip:latest
        docker compose up -d

        echo "Done deploying ${WEBHOOK_PARAM}"
fi

exit 0
