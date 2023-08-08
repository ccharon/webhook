# Simple restapi to receive a postrequest and start a shell script 

# WORK IN PROGRESS

The idea is to start this restapi with a systemd service. 
The api should bind to some port on localhost then a nginx config should be used to forward requests to the api.

If publishing the api you should enforce https so that the token will be transfered encrypted


```bash
# create the user that the service will run with
adduser webhook --group --system
```

./webhook -c /etc/webhook/config.json

#### /etc/webhook/config.json
```json
{
  "server": {
    "host": "localhost",
    "port": 6080
  },
  "user": "webhook",
  "token": "abcdefgh",
  "script": "/etc/webhook/deploy.sh"
}
```

#### deploy.sh
```bash
#!/usr/bin/env bash

echo "${DEPLOY_ID}"
echo "${DEPLOY_IMAGE}"

# do whatever needs to be done

exit 0
```
