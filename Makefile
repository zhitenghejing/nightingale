.PHONY: prebuild build verify verify-all frontend-install frontend-build frontend-verify frontend-e2e preview preview-refresh preview-status preview-url preview-stop preview-destroy
.PHONY: stable stable-refresh stable-status stable-url stable-stop

ROOT:=$(shell pwd -P)
GIT_COMMIT:=$(shell git --work-tree ${ROOT}  rev-parse 'HEAD^{commit}')
_GIT_VERSION:=$(shell git --work-tree ${ROOT} describe --tags --abbrev=14 "${GIT_COMMIT}^{commit}" 2>/dev/null)
TAG=$(shell echo "${_GIT_VERSION}" |  awk -F"-" '{print $$1}')
RELEASE_VERSION:="$(TAG)-$(GIT_COMMIT)"

all: prebuild build

prebuild:
	sh fe.sh

frontend-install:
	python3 -B scripts/frontend.py install

frontend-build:
	python3 -B scripts/frontend.py build

frontend-verify:
	python3 -B scripts/frontend.py verify

frontend-e2e:
	python3 -B scripts/frontend.py e2e

build:
	go build -ldflags "-w -s -X github.com/ccfos/nightingale/v6/pkg/version.Version=$(RELEASE_VERSION)" -o n9e ./cmd/center/main.go

build-edge:
	go build -ldflags "-w -s -X github.com/ccfos/nightingale/v6/pkg/version.Version=$(RELEASE_VERSION)" -o n9e-edge ./cmd/edge/

build-alert:
	go build -ldflags "-w -s -X github.com/ccfos/nightingale/v6/pkg/version.Version=$(RELEASE_VERSION)" -o n9e-alert ./cmd/alert/main.go

build-pushgw:
	go build -ldflags "-w -s -X github.com/ccfos/nightingale/v6/pkg/version.Version=$(RELEASE_VERSION)" -o n9e-pushgw ./cmd/pushgw/main.go

build-cli: 
	go build -ldflags "-w -s -X github.com/ccfos/nightingale/v6/pkg/version.Version=$(RELEASE_VERSION)" -o n9e-cli ./cmd/cli/main.go

run:
	nohup ./n9e > n9e.log 2>&1 &

run-alert:
	nohup ./n9e-alert > n9e-alert.log 2>&1 &

run-pushgw:
	nohup ./n9e-pushgw > n9e-pushgw.log 2>&1 &

release:
	goreleaser --skip-validate --skip-publish --snapshot

verify:
	python3 -B scripts/verify.py

verify-all:
	python3 -B scripts/verify.py --integrations --vet --frontend

preview:
	python3 -B scripts/preview.py up

preview-refresh:
	python3 -B scripts/preview.py refresh

preview-status:
	python3 -B scripts/preview.py status

preview-url:
	python3 -B scripts/preview.py url

preview-stop:
	python3 -B scripts/preview.py stop --if-exists

preview-destroy:
	python3 -B scripts/preview.py destroy --if-exists

stable:
	python3 -B scripts/preview.py up --stable

stable-refresh:
	python3 -B scripts/preview.py refresh --stable

stable-status:
	python3 -B scripts/preview.py status --stable

stable-url:
	python3 -B scripts/preview.py url --stable

stable-stop:
	python3 -B scripts/preview.py stop --stable --if-exists
