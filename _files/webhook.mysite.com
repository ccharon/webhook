limit_req_zone $binary_remote_addr zone=webhook:10m rate=5r/m;

upstream webhook {
    server localhost:6080;
}

server {
    listen 443 ssl;
    server_name webhook.mysite.com;

    ssl_certificate /etc/letsencrypt/live/mysite.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/mysite.com/privkey.pem;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;

    location / {
        limit_req zone=webhook burst=2 nodelay;
        limit_req_status 429;

        proxy_pass http://webhook;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}

server {
    listen 80;
    server_name webhook.mysite.com;
    return 308 https://$host$request_uri;
}