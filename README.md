# Simple restapi to receive a post request and start a shell script 

The idea is to start this restapi with a systemd service. 
The api should bind to some port on localhost then a nginx config should be used to forward requests to the api.
If publishing the api you should enforce https so that the token will be transferred encrypted.
All files listed here are also available in the _files folder in this repo.

Also this is my first go project, if you happen to have suggestions do so! Maybe by using a pull request :P

#### install go for your distribution
```bash
#debian
apt install golang
```

#### clone this repo, build and copy executable
```bash
git clone https://github.com/ccharon/webhook
cd webhook
go build .
sudo cp webhook /usr/local/sbin/webhook
```

as root execute the following statements and put files in place

#### create user and config directory
```bash
# create the user that the service will run with
adduser webhook --group --system

# i want to control docker deployments, so the user has to be in the docker group
usermod -a -G docker webhook

# prepare config dir
mkdir -p /etc/webhook
```

#### create /etc/webhook/config.json
```json
{
  "server": {
    "host": "localhost",
    "port": 6080
  },
  "token": "abcdefgh",
  "script": "/etc/webhook/deploy.sh"
}
```

#### set owner and strict access rights for config.json
```bash
chown webhook:webhook /etc/webhook/config.json
chmod 600 /etc/webhook/config.json
```

#### create /etc/webhook/deploy.sh
```bash
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
```

#### make executable, set owner and strict access rights for deploy.sh
```bash
chown webhook:webhook /etc/webhook/deploy.sh
chmod 700 /etc/webhook/deploy.sh

```

#### create /etc/systemd/system/webhook.service
```ini
[Unit]
Description=webhook service
After=network.target
StartLimitIntervalSec=0

[Service]
Type=simple
Restart=always
RestartSec=1
User=webhook
ExecStart=/usr/local/sbin/webhook -c /etc/webhook/config.json

[Install]
WantedBy=multi-user.target
```

#### refresh systemd and start service
```bash
systemctl daemon-reload
systemctl start webhook

# later enable at system startup
#systemctl enable webhook
```

#### create nginx /etc/nginx/sites-available/webhook.mysite.com
```
map $http_upgrade $connection_upgrade {
  default upgrade;
  '' close;
}

upstream webhook {
    server localhost:6080;
}

server {
    listen 443 ssl;
    server_name webhook.mysite.com;

    location / {
        proxy_pass  http://webhook;
    }

    ssl_certificate /etc/letsencrypt/live/mysite.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/mysite.com/privkey.pem;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;
}

server {
    listen 80;
    server_name webhook.mysite.com;

    if ($host = webhook.mysite.com) {
        return 308 https://$host$request_uri;
    }

    return 404;
}
```

#### activate and test config, restart nginx
```bash
ln -s /etc/nginx/sites-available/webhook.mysite.com /etc/nginx/sites-enabled/
nginx -t 
systemctl restart nginx
```

#### curl test call
```bash
curl -X POST https://webhook.mysite.com -H "Content-Type: application/json" -d '{"id": "44444", "image": "ccharon/echoip", "token": "abcdefgh"}'
```

### Example Usage in github actions pipeline
see whole [pipeline](https://github.com/ccharon/echoip/blob/master/.github/workflows/ci.yml)
```yaml
      - name: Trigger deployment
        run: |
          curl -X POST ${{ secrets.WEBHOOK_URL }} -H "Content-Type: application/json" -d '{"id": "${{ github.run_id }}", "image": "${{ env.DOCKER_IMAGE }}", "token": "${{ secrets.WEBHOOK_TOKEN }}"}'
```
