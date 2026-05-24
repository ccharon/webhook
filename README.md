# webhook

Minimal HTTP server that receives a signed POST request and runs a configured shell script.
Intended to run as a systemd service on Linux, fronted by nginx for TLS termination.

Authentication uses HMAC-SHA256 over the raw request body — the same mechanism as GitHub webhooks.
The caller signs the body with a shared secret and sends the result in the `X-Hub-Signature-256` header.
The body includes a Unix timestamp, which closes replay attacks within a ±30 second window.

## Install

Requires Go and root.
```bash
git clone https://github.com/ccharon/webhook
cd webhook
sudo make install
```

This builds the binary, creates the `webhook` system user (added to the `docker` group),
installs config and scripts under `/etc/webhook/`, installs and starts the systemd service,
and copies the nginx config to `sites-available/`.

### Required steps after install

**1. Set a strong token**

The installed `/etc/webhook/config.json` contains a placeholder token that must be replaced
before the service will start. Generate a secret and edit the file:

```bash
openssl rand -base64 32
nano /etc/webhook/config.json
systemctl restart webhook
```

**2. Adapt the deploy script**

`/etc/webhook/deploy.sh` is a template. Add your actual deployment logic there.
The script receives `WEBHOOK_ID` and `WEBHOOK_PARAM` as environment variables.

**3. Finish nginx setup**

Edit the nginx config to match your domain and certificate paths, then activate it:

```bash
nano /etc/nginx/sites-available/webhook.mysite.com
ln -s /etc/nginx/sites-available/webhook.mysite.com /etc/nginx/sites-enabled/
nginx -t && systemctl restart nginx
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
| `token` | HMAC-SHA256 secret shared with callers. Minimum 32 characters. |
| `timeout` | Seconds before the script is killed. Default: 300. |
| `param_max_length` | Maximum length of the `param` field in bytes. Default: 64, range 1–65536. |

## Request format

`id` accepts `[a-zA-Z0-9_-]`, max 36 characters.
`param` accepts `[a-zA-Z0-9_.-]`, max `param_max_length` characters.

```bash
BODY="{\"id\": \"deploy-1\", \"param\": \"v1.2.3\", \"unix_seconds\": $(date +%s)}"
SIG="sha256=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "your-secret" | awk '{print $NF}')"
curl -X POST https://webhook.mysite.com \
  -H "Content-Type: application/json" \
  -H "X-Hub-Signature-256: $SIG" \
  -d "$BODY"
```

## GitHub Actions example

```yaml
- name: Trigger deployment
  run: |
    BODY="{\"id\": \"${{ github.run_id }}\", \"param\": \"${{ github.ref_name }}\", \"unix_seconds\": $(date +%s)}"
    SIG="sha256=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "${{ secrets.WEBHOOK_TOKEN }}" | awk '{print $NF}')"
    curl -X POST ${{ secrets.WEBHOOK_URL }} \
      -H "Content-Type: application/json" \
      -H "X-Hub-Signature-256: $SIG" \
      -d "$BODY"
```

## Local development

```bash
./test/start_server.sh        # builds and starts server on localhost:6080
./test/send_request.sh        # sends a signed test request
./test/send_request.sh my-id v1.0.0   # with explicit id and param
```