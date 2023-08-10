all: build install

test:
	go test ./...

build:
	go build .

install: build
	id webhook > /dev/null 2>&1 || ( adduser webhook --group --system && usermod -a -G docker webhook)
	cp webhook $DESTDIR/usr/local/sbin/webhook

	mkdir -p $DESTDIR/etc/webhook
	cp ./_files/config.json $DESTDIR/etc/webhook/config.json
	cp ./_files/deploy.sh $DESTDIR/etc/webhook/deploy.sh
	chown -R webhook:webhook $DESTDIR/etc/webhook
	chmod 600 $DESTDIR/etc/webhook/config.json
	chmod 700 $DESTDIR/etc/webhook/deploy.sh

uninstall:
	deluser webhook
	rm $DESTDIR/usr/local/sbin/webhook
