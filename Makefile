DESTDIR ?=
SBINDIR  = $(DESTDIR)/usr/local/sbin
CONFDIR  = $(DESTDIR)/etc/webhook
UNITDIR  = $(DESTDIR)/etc/systemd/system
NGINXDIR = $(DESTDIR)/etc/nginx/sites-available

.PHONY: all build vet test clean install uninstall

all: build

build:
	go build -trimpath -ldflags="-s -w" -o webhook .

vet:
	go vet ./...

test:
	go test -race ./...

clean:
	rm -f webhook

# Run `make build` as a normal user first, then `sudo make install`.
install:
	@test -x ./webhook || { echo "./webhook not found: run 'make build' first (as a normal user), then 'sudo make install'" >&2; exit 1; }
	@set -e; \
	if [ -z "$(DESTDIR)" ]; then \
		if ! id webhook >/dev/null 2>&1; then \
			echo "creating system user webhook"; \
			useradd --system --user-group --no-create-home --shell /usr/sbin/nologin webhook \
				|| adduser --system --group --no-create-home --shell /usr/sbin/nologin webhook; \
		fi; \
		if getent group docker >/dev/null 2>&1; then \
			usermod -a -G docker webhook; \
		fi; \
	fi; \
	install -d -o root -g root -m 755 "$(SBINDIR)"; \
	install -o root -g root -m 755 ./webhook "$(SBINDIR)/webhook"; \
	install -d -o root -g webhook -m 750 "$(CONFDIR)"; \
	fresh=1; \
	if [ -e "$(CONFDIR)/config.json" ]; then \
		fresh=0; \
		echo "$(CONFDIR)/config.json exists, not overwriting"; \
	else \
		install -o root -g webhook -m 640 ./_files/config.json "$(CONFDIR)/config.json"; \
	fi; \
	if [ -e "$(CONFDIR)/deploy.sh" ]; then \
		echo "$(CONFDIR)/deploy.sh exists, not overwriting"; \
	else \
		install -o root -g webhook -m 750 ./_files/deploy.sh "$(CONFDIR)/deploy.sh"; \
	fi; \
	if [ -d "$(NGINXDIR)" ]; then \
		install -o root -g root -m 644 ./_files/webhook.mysite.com "$(NGINXDIR)/webhook.mysite.com"; \
	fi; \
	if [ -d "$(UNITDIR)" ]; then \
		install -o root -g root -m 644 ./_files/webhook.service "$(UNITDIR)/webhook.service"; \
		if [ -z "$(DESTDIR)" ]; then \
			systemctl daemon-reload; \
			systemctl enable webhook; \
			if [ "$$fresh" = 0 ]; then \
				systemctl restart webhook; \
			else \
				echo; \
				echo "Fresh install: set a token in $(CONFDIR)/config.json (openssl rand -base64 32),"; \
				echo "adapt $(CONFDIR)/deploy.sh, then run: systemctl start webhook"; \
			fi; \
		fi; \
	fi

# Leaves /etc/webhook (config.json, deploy.sh) and the webhook user in place.
uninstall:
	@set -e; \
	if [ -z "$(DESTDIR)" ] && command -v systemctl >/dev/null 2>&1; then \
		systemctl stop webhook 2>/dev/null || true; \
		systemctl disable webhook 2>/dev/null || true; \
	fi; \
	rm -f "$(UNITDIR)/webhook.service" "$(SBINDIR)/webhook" "$(NGINXDIR)/webhook.mysite.com"; \
	if [ -z "$(DESTDIR)" ] && command -v systemctl >/dev/null 2>&1; then \
		systemctl daemon-reload || true; \
	fi; \
	echo "Removed binary, systemd unit and nginx site file."; \
	echo "Kept $(CONFDIR) (config.json, deploy.sh) and the webhook user; remove them manually if wanted:"; \
	echo "  rm -r /etc/webhook; userdel webhook"
