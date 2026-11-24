SERVICES := storefront catalog cart checkout payments-mock
IMAGE_PREFIX ?= mercadito
MAX_IMAGE_MB ?= 30

.PHONY: tools test vet lint build images size scan docs docs-check hygiene ci up down integration

tools:
	@command -v golangci-lint >/dev/null || brew install golangci-lint

test:
	go test -race ./...

integration:
	go test -race -tags integration ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

build:
	@for s in $(SERVICES); do CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/$$s ./cmd/$$s || exit 1; done

images:
	@for s in $(SERVICES); do docker build -q --build-arg SERVICE=$$s -t $(IMAGE_PREFIX)/$$s:dev . || exit 1; done

size:
	@fail=0; for s in $(SERVICES); do \
	  b=$$(docker image inspect $(IMAGE_PREFIX)/$$s:dev --format '{{.Size}}'); mb=$$((b/1000000)); \
	  echo "$$s: $${mb} MB"; [ $$mb -lt $(MAX_IMAGE_MB) ] || fail=1; \
	  u=$$(docker image inspect $(IMAGE_PREFIX)/$$s:dev --format '{{.Config.User}}'); \
	  case "$$u" in ""|root|0) echo "$$s runs as root"; fail=1;; esac; \
	done; exit $$fail

scan:
	@for s in $(SERVICES); do trivy image --quiet --severity HIGH,CRITICAL --ignore-unfixed --exit-code 1 $(IMAGE_PREFIX)/$$s:dev || exit 1; done

docs:
	go run ./cmd/docs -readme README.md

docs-check:
	go run ./cmd/docs -readme README.md -check

hygiene:
	@! git ls-files 2>/dev/null | grep -E '^\.github/|dependabot|release-please|CHANGELOG|/adr/' ; \
	! find . -path ./.git -prune -o \( -path './.github' -o -name 'dependabot*' -o -name 'release-please*' -o -name 'CHANGELOG*' \) -print | grep . ; \
	! grep -rIniwE 'bug|intentional|intentionally|naive|fragile' --exclude-dir=.git --exclude-dir=bin --exclude=Makefile . | grep .

ci: vet lint test images size scan docs-check hygiene

up:
	docker compose up -d --build --wait

down:
	docker compose down -v
