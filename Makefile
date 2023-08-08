all: build install

test:
	go test ./...

build:
	go build .

install:
	$DESTDIR/usr/local/bin
	cp webhook $DESTDIR/usr/local/bin/webhook
	mkdir -p $DESTDIR/etc/webhook
