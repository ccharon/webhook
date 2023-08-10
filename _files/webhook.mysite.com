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
