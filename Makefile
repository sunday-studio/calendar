.PHONY: test run build image deploy-check backup-drill smoke vuln e2e-server e2e-up e2e-down
SERVER := server

test:
	cd $(SERVER) && go vet ./... && go test -race ./...

# Dev run: throwaway key (DEV ONLY), local db.
run:
	cd $(SERVER) && CALENDAR_MASTER_KEY=$${CALENDAR_MASTER_KEY:-0123456789abcdef0123456789abcdef} \
		CALENDAR_DB=$${CALENDAR_DB:-./calendar.db} go run ./cmd/calendard serve

build:
	cd $(SERVER) && CGO_ENABLED=0 go build -trimpath -o bin/calendard ./cmd/calendard

# ---- server operations (see deploy/) ----
image:
	docker build -t calendard:dev $(SERVER)

deploy-check:
	@[ -f deploy/calendard.env ] && made=0 || { cp deploy/calendard.env.example deploy/calendard.env; made=1; }; \
	docker compose -f deploy/compose.yaml config -q; rc=$$?; \
	[ $$made = 1 ] && rm deploy/calendard.env; \
	[ $$rc = 0 ] && echo "compose.yaml is valid"; exit $$rc

# back up, lose the database, restore it, refuse the wrong key (throwaway data, no Docker needed)
backup-drill:
	sh deploy/drill.sh

# the manual live checklist for a release
smoke:
	@cat docs/ops/SMOKE.md

vuln:
	cd $(SERVER) && (command -v govulncheck >/dev/null && govulncheck ./... || echo "install govulncheck: go install golang.org/x/vuln/cmd/govulncheck@latest")

# ---- iOS (see DECISIONS D1, D4) ----
IOS_DEST ?= platform=iOS Simulator,name=iPhone 17 Pro
IOS_FLAGS := -skipPackagePluginValidation -skipMacroValidation

.PHONY: ios-gen ios-build ios-test
ios-gen:
	cd ios && xcodegen generate

ios-build: ios-gen
	cd ios && xcodebuild build -project Calendar.xcodeproj -scheme Calendar -destination '$(IOS_DEST)' $(IOS_FLAGS)

ios-test: ios-gen
	cd ios/Packages/CalendarCore && xcodebuild test -scheme CalendarCore -destination '$(IOS_DEST)' $(IOS_FLAGS)
	cd ios && xcodebuild test -project Calendar.xcodeproj -scheme Calendar -destination '$(IOS_DEST)' $(IOS_FLAGS) CODE_SIGNING_ALLOWED=YES CODE_SIGN_IDENTITY=-

# CalendarCore sync against the fixture hub (see scripts/e2e-fixtures.sh)
.PHONY: ios-e2e ios-e2e-real
ios-e2e:
	./scripts/e2e-fixtures.sh

# XCUITest against the real hub + fake Google (see docs/engineering/e2e.md)
ios-e2e-real:
	./scripts/ios-e2e.sh

# ---- end-to-end against a fake Google (see docs/engineering/e2e.md) ----
e2e-server:
	./scripts/e2e.sh test

# leaves hub :8787 + fake Google :8788 running and prints a dev token (for the iOS UI test)
e2e-up:
	./scripts/e2e.sh up

e2e-down:
	./scripts/e2e.sh down

# Free "Personal Team" signing cannot use Push or Time Sensitive Notifications. This generates the project
# without those two entitlements (same Calendar.xcodeproj; run `make ios-gen` to go back to the full set).
.PHONY: ios-gen-personal
ios-gen-personal:
	cd ios && sed -e '/aps-environment: development/d' -e '/usernotifications.time-sensitive: true/d' project.yml > project.personal.yml && xcodegen generate --spec project.personal.yml && rm -f project.personal.yml

# ---- release builds with the paid team (docs/ops/testflight.md) ----
# CALENDAR_TEAM_ID feeds DEVELOPMENT_TEAM in ios/project.yml; default is the free Personal Team.
# Release: CALENDAR_TEAM_ID=<paid id> make ios-gen
export CALENDAR_TEAM_ID ?= 2QG5WSN24S

# TestFlight: TEAM_ID=<paid id> [BUILD_NUMBER=n] [NO_UPLOAD=1] make ios-archive
.PHONY: ios-archive ios-archive-adhoc
ios-archive:
	./scripts/ios-archive.sh

# Ad hoc .ipa for registered devices: TEAM_ID=<paid id> [BUILD_NUMBER=n] make ios-archive-adhoc
ios-archive-adhoc:
	./scripts/ios-archive.sh --adhoc

# ---- releases (docs/ops/RELEASING.md) ----
# make release-dry VERSION=patch|minor|major|X.Y.Z     checks + plan, writes nothing
# make release     VERSION=...                         bumps VERSION/CHANGELOG/ios MARKETING_VERSION, commits, tags (never pushes)
.PHONY: release-dry release
release-dry:
	@test -n "$(VERSION)" -a "$(VERSION)" != "dev" || { echo "usage: make release-dry VERSION=patch|minor|major|X.Y.Z"; exit 2; }
	./scripts/release.sh --dry-run $(VERSION)

release:
	@test -n "$(VERSION)" -a "$(VERSION)" != "dev" || { echo "usage: make release VERSION=patch|minor|major|X.Y.Z"; exit 2; }
	./scripts/release.sh --execute $(VERSION)

# Static Linux binaries for the Docker image's platforms, built without cgo exactly as the Dockerfile does.
# Offline part of "does the image build": needs no Docker daemon and no network. make build-linux [VERSION=x]
.PHONY: build-linux
build-linux:
	@set -e; cd $(SERVER); mkdir -p bin; \
	for arch in amd64 arm64; do \
		out=bin/calendard-linux-$$arch; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -trimpath -ldflags "-s -w -X main.version=$(or $(VERSION),dev)" -o $$out ./cmd/calendard; \
		go version -m $$out | grep -q 'CGO_ENABLED=0' || { echo "$$out was built with cgo"; exit 1; }; \
		head -c4 $$out | od -An -c | grep -q 'E *L *F' || { echo "$$out is not an ELF binary"; exit 1; }; \
		printf '%s  %s bytes\n' $$out $$(wc -c < $$out | tr -d ' '); \
	done
