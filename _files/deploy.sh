#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# SPDX-FileCopyrightText: 2023 Christian Charon
set -euo pipefail

echo "received deployment request ${WEBHOOK_ID:-?} for ${WEBHOOK_PARAM:-?}"

# fetch <url> [<token-file>]
# Writes the response body to stdout. -f makes curl fail on HTTP errors.
# With a token file the Authorization header is handed to curl through a file
# descriptor, so the token never shows up in the process list.
fetch() {
    local url="$1" token_file="${2:-}"

    if [ -z "$token_file" ]; then
        curl -fsSL "$url"
    elif [ -r "$token_file" ]; then
        curl -fsSL -H @<(printf 'Authorization: Bearer %s\n' "$(<"$token_file")") "$url"
    else
        echo "token file ${token_file} is not readable" >&2
        return 1
    fi
}

# deploy_static <tarball-url> <target-dir> [<token-file>]
# Downloads a tarball of a static site and syncs it into the target directory.
# For private repositories pass a file containing a GitHub token as third argument.
deploy_static() {
    local url="$1" target="$2" token_file="${3:-}"

    # workdir is global on purpose: the EXIT trap runs after this function has returned
    workdir=$(mktemp -d)
    trap 'rm -rf "$workdir"' EXIT

    # pipefail makes the pipeline fail when the download fails
    fetch "$url" "$token_file" | tar -xz -C "$workdir" --strip-components=1

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
    codewheels)
        # private repository: needs a GitHub token in /etc/webhook/codewheels_ghtoken
        # (owner root:webhook, mode 640)
        # before first codewheels deploy webhook has to be the owner of the target
        # one time manually run: 'chown -R webhook:webhook /var/www/codewheels.erdferkel.eu'
        deploy_static \
            https://api.github.com/repos/ccharon/website-codewheels/tarball/dist \
            /var/www/codewheels.erdferkel.eu \
            /etc/webhook/codewheels_ghtoken
        ;;
    factorio-calculator)
        # before first factorio-calculator deploy webhook has to be the owner of the target
        # one time manually run: 'chown -R webhook:webhook /var/www/factorio.erdferkel.eu'
        deploy_static \
            https://github.com/ccharon/factorio-calculator/archive/refs/heads/dist.tar.gz \
            /var/www/factorio.erdferkel.eu
        ;;
    *)
        echo "unknown deployment target: ${WEBHOOK_PARAM:-<empty>}" >&2
        exit 1
        ;;
esac

echo "Done deploying ${WEBHOOK_PARAM}"
