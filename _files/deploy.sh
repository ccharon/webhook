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

if [ "${WEBHOOK_PARAM}" == "soundboard" ] ; then
        # before first soundboard deploy webhook has to be allowed to write the target
        # one time manually run: 'chown -R webhook:webhook /var/www/sound.erdferkel.eu'
        
        TMPDIR=$(mktemp -d)
        trap 'rm -rf "$TMPDIR"' EXIT
        
        curl -sL https://github.com/ccharon/soundboard/archive/refs/heads/dist.tar.gz \
                | tar -xz -C "$TMPDIR" --strip-components=1
        
        rsync -a --delete --chmod=D755,F644 "$TMPDIR/" /var/www/sound.erdferkel.eu/
        
        echo "Done deploying soundboard"
fi

exit 0
