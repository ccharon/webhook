all: build install

test:
	go test ./...

build:
	go build .

install: build
	id webhook > /dev/null 2>&1 || ( adduser webhook --group --system && usermod -a -G docker webhook )
	cp ./webhook $DESTDIR/usr/local/sbin/webhook

	mkdir -p $DESTDIR/etc/webhook
	chown root:webhook $DESTDIR/etc/webhook
	chmod 750 $DESTDIR/etc/webhook

	cp ./_files/config.json $DESTDIR/etc/webhook/config.json
	chown root:webhook $DESTDIR/etc/webhook/config.json
	chmod 640 $DESTDIR/etc/webhook/config.json

	cp ./_files/deploy.sh $DESTDIR/etc/webhook/deploy.sh
	chown root:webhook $DESTDIR/etc/webhook/deploy.sh
	chmod 750 $DESTDIR/etc/webhook/deploy.sh

	[ -d $DESTDIR/etc/systemd/system ] \
		&& cp ./_files/webhook.service $DESTDIR/etc/systemd/system/webhook.service \
		&& chown root:root $DESTDIR/etc/systemd/system/webhook.service \
		&& chmod 755 $DESTDIR/etc/systemd/system/webhook.service \
		&& systemctl daemon-reload

	[ -d $DESTDIR/etc/nginx/sites-available ] \
		&& cp ./_files/webhook.mysite.com $DESTDIR/etc/nginx/sites-available/webhook.mysite.com \
		&& chown root:root $DESTDIR/etc/nginx/sites-available/webhook.mysite.com \
		&& chmod 644 $DESTDIR/etc/nginx/sites-available/webhook.mysite.com

uninstall:
	id webhook > /dev/null 2>&1 && deluser webhook

	[ -f $DESTDIR/usr/local/sbin/webhook ] && rm $DESTDIR/usr/local/sbin/webhook

	[ -f $DESTDIR/etc/systemd/system/webhook.service ] systemctl stop webhook \
		&& systemctl disable webhook \
		&& rm $DESTDIR/etc/systemd/system/webhook.service \
		&& systemctl daemon-reload

	[ -f $DESTDIR/etc/webhook/config.json ] && rm $DESTDIR/etc/webhook/config.json
	[ -f $DESTDIR/etc/webhook/deploy.sh ] && rm $DESTDIR/etc/webhook/deploy.sh
	[ -d $DESTDIR/etc/webhook ] rmdir $DESTDIR/etc/webhook
