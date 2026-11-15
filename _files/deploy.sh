#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# SPDX-FileCopyrightText: 2023 Christian Charon
set -euo pipefail

echo "received deployment request ${WEBHOOK_ID:-?} for ${WEBHOOK_PARAM:-?}"

# deploy_static <repo-url> <target-dir>
# Downloads a tarball of a static site and syncs it into the target directory.
deploy_static() {
    local url="$1" target="$2"

    # workdir is global on purpose: the EXIT trap runs after this function has returned
    workdir=$(mktemp -d)
    trap 'rm -rf "$workdir"' EXIT

    # -f makes curl fail on HTTP errors, pipefail makes the pipeline fail with it
    curl -fsSL "$url" | tar -xz -C "$workdir" --strip-components=1

    # never sync an empty directory, rsync --delete would wipe the target
    if [ -z "$(ls -A "$workdir")" ]; then
        echo "download of ${url} is empty, refusing to deploy" >&2
        exit 1
    fi

    rsync -a --delete --chmod=D755,F644 "$workdir/" "$target/"
}

case "${WEBHOOK_PARAM:-}" in
    echoip)
        cd /path/to/compose/echoip || exit 1
        docker compose stop
        docker compose rm -f
        docker pull ccharon/echoip:latest
        docker compose up -d
        ;;
    soundboard)
        # before first soundboard deploy webhook has to be allowed to write the target
        # one time manually run: 'chown -R webhook:webhook /var/www/sound.erdferkel.eu'
        deploy_static \
            https://github.com/ccharon/website-soundboard/archive/refs/heads/dist.tar.gz \
            /var/www/sound.erdferkel.eu
        ;;
    qsensors)
        # before first qsensors deploy webhook has to be allowed to write the target
        # one time manually run: 'chown -R webhook:webhook /var/www/qsensors.erdferkel.eu'
        deploy_static \
            https://github.com/ccharon/website-qsensors/archive/refs/heads/dist.tar.gz \
            /var/www/qsensors.erdferkel.eu
        ;;
    *)
        echo "unknown deployment target: ${WEBHOOK_PARAM:-<empty>}" >&2
        exit 1
        ;;
esac

echo "Done deploying ${WEBHOOK_PARAM}"
