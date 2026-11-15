# webhook

Minimal HTTP server that receives a signed POST request and runs a configured shell script.
Intended to run as a systemd service on Linux, fronted by nginx for TLS termination. Standard library only.

Authentication uses HMAC-SHA256 over the raw request body, the same mechanism as GitHub webhooks.
The caller signs the body with a shared secret and sends the result in the `X-Hub-Signature-256` header.
The body contains a Unix timestamp; requests outside a ±30 second window are rejected, and every accepted
signature is remembered until it expires, so a captured request cannot be replayed (HTTP 403
`request was already processed`). A retry must carry a fresh `unix_seconds`.

## Install

Requires Go (to build) and root (to install). Build as a normal user, install as root:

```bash
git clone https://github.com/ccharon/webhook
cd webhook
make build && sudo make install
```

`make install` creates the `webhook` system user (added to the `docker` group if that group exists),
installs the binary to `/usr/local/sbin`, config and script to `/etc/webhook/`, the systemd unit, and
the nginx site file to `sites-available/` (if nginx is present). It never overwrites an existing
`config.json` or `deploy.sh`. On a fresh install the service is enabled but not started; on an upgrade
(config already present) it is restarted. `make uninstall` removes binary, unit and nginx file but keeps
`/etc/webhook` and the user. Other targets: `vet`, `test` (with `-race`), `clean`. `DESTDIR` is supported
for staging (no user management or systemctl calls then).

### Required steps after a fresh install

1. **Set a strong token.** The service refuses to start with the placeholder token.
   ```bash
   openssl rand -base64 32
   sudoedit /etc/webhook/config.json
   sudo systemctl start webhook
   ```
2. **Adapt the deploy script.** `/etc/webhook/deploy.sh` receives `WEBHOOK_ID` and `WEBHOOK_PARAM` as
   environment variables and dispatches on `param` with a `case` statement (`echoip`, `soundboard`,
   `qsensors` are examples; unknown values exit non-zero). Adapt it to your own targets.
3. **Finish nginx setup.** Edit domain and certificate paths, then:
   ```bash
   sudo ln -s /etc/nginx/sites-available/webhook.mysite.com /etc/nginx/sites-enabled/
   sudo nginx -t && sudo systemctl reload nginx
   ```

## Configuration

`/etc/webhook/config.json`:
```json
{
  "server": { "host": "localhost", "port": 6080 },
  "token": "your-secret-min-32-chars",
  "script": "/etc/webhook/deploy.sh",
  "timeout": 300,
  "param_max_length": 64
}
```

| Field | Description |
|---|---|
| `server.host` | Listen address. Must not be empty. |
| `server.port` | Listen port, 1-65535. |
| `token` | HMAC-SHA256 secret. At least 32 characters, must not be the placeholder `REPLACE_WITH_SECRET_MIN_32_CHARS`. |
| `script` | Script to run. Must exist, be a regular file and be executable. Relative paths are resolved against the working directory. |
| `timeout` | Seconds before the script is killed, 0-86400. 0 or omitted = 300. |
| `param_max_length` | Maximum length of `param` in bytes, 0-65536. 0 or omitted = 64. |

The configuration is validated at startup; on any violation the service refuses to start.
Logs are structured (slog text format) and go to the journal: `journalctl -u webhook`.

## Request format

`id` accepts `[a-zA-Z0-9_-]`, max 36 characters.
`param` accepts `[a-zA-Z0-9_.-]`, max `param_max_length` characters. A slash is deliberately not allowed.

```bash
BODY="{\"id\": \"deploy-1\", \"param\": \"soundboard\", \"unix_seconds\": $(date +%s)}"
SIG="sha256=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "your-secret" | awk '{print $NF}')"
curl --fail-with-body -X POST https://webhook.mysite.com \
  -H "Content-Type: application/json" \
  -H "X-Hub-Signature-256: $SIG" \
  -d "$BODY"
```

Only one deployment runs at a time. If the script exceeds `timeout`, its whole process group gets
SIGTERM, then SIGKILL. On service stop the server stops accepting requests and lets a running
deployment finish for up to 60 seconds.

### Response codes

| Code | Meaning |
|---|---|
| 200 | Signature valid, deployment started |
| 400 | Malformed JSON, unknown field, or invalid `id` / `param` |
| 403 | Missing/invalid signature, timestamp outside the window, or request already processed (replay) |
| 404 | Path other than `/` |
| 405 | Method other than POST |
| 413 | Body larger than 1 MiB |
| 415 | Content-Type is not `application/json` |
| 429 | A deployment is already running (or nginx rate limit hit) |

## GitHub Actions example

Values are passed through `env:` instead of being interpolated into the script, which avoids
script injection via e.g. branch names.

```yaml
- name: Trigger deployment
  env:
    RUN_ID: ${{ github.run_id }}
    REF_NAME: ${{ github.ref_name }}
    WEBHOOK_TOKEN: ${{ secrets.WEBHOOK_TOKEN }}
    WEBHOOK_URL: ${{ secrets.WEBHOOK_URL }}
  run: |
    BODY="{\"id\": \"${RUN_ID}\", \"param\": \"${REF_NAME}\", \"unix_seconds\": $(date +%s)}"
    SIG="sha256=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$WEBHOOK_TOKEN" | awk '{print $NF}')"
    curl --fail-with-body -X POST "$WEBHOOK_URL" \
      -H "Content-Type: application/json" \
      -H "X-Hub-Signature-256: $SIG" \
      -d "$BODY"
```

## Security notes

- Membership in the `docker` group is root-equivalent. Anyone who can change `deploy.sh` or the
  token effectively controls the host; keep `/etc/webhook` at `root:webhook 750` (as installed).
- Treat the token like a password: generate it randomly, keep it only in `config.json` (mode 640)
  and in your CI secret store, rotate it if it leaks. Always run behind TLS.
- `deploy.sh` runs with `WEBHOOK_PARAM` from the caller. Never pass it unquoted to a shell or use it
  to build paths without a `case` whitelist, as the example does.
- The systemd unit is hardened conservatively (`NoNewPrivileges`, `PrivateTmp`, `ProtectSystem=full`,
  `ProtectHome`, kernel/cgroup protections, ...), but deliberately keeps access to the docker socket
  and `/var/www`. Tighten it (e.g. `ReadWritePaths=`) if your scripts need less.

## Local development

```bash
make test                     # unit tests with -race
./test/start_server.sh        # builds ./webhook and starts it on localhost:6080 with test/config.json
./test/send_request.sh        # sends a signed test request (dev token from test/config.json)
./test/send_request.sh my-id v1.0.0   # with explicit id and param
```

`requests.http` contains further example requests. CI (`.github/workflows/ci.yml`) runs gofmt, vet,
tests, staticcheck, govulncheck and shellcheck.
