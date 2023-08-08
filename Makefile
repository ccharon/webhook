all: build install

test:
	go test ./...

build:
	go build .

install:
	$DESTDIR/usr/local/bin
	cp webhook $DESTDIR/usr/local/sbin/webhook
	mkdir -p $DESTDIR/etc/webhook
	# chown -R  $DESTDIR/etc/webhook
	chmod 700 $DESTDIR/etc/webhook

	usermod -a -G docker webhook
