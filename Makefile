PKG_PREFIX := github.com/VictoriaMetrics/VictoriaMetrics

BUILDINFO_TAG ?= $(shell echo $$(git describe --long --all | tr '/' '-')$$( \
	      git diff-index --quiet HEAD -- || echo '-dirty-'$$(git diff-index -u HEAD | openssl sha1 | cut -c 10-17)))

PKG_TAG ?= $(shell git tag -l --points-at HEAD)
ifeq ($(PKG_TAG),)
PKG_TAG := $(BUILDINFO_TAG)
endif

GO_BUILDINFO = -X '$(PKG_PREFIX)/lib/buildinfo.Version=vm-cybera-exporter-$(shell date -u +'%Y%m%d-%H%M%S')-$(BUILDINFO_TAG)'

.PHONY: $(MAKECMDGOALS)



build:
	GO111MODULE=on go build  -ldflags "-X 'github.com/VictoriaMetrics/VictoriaMetrics/lib/buildinfo.Version=cybera-exporter'" -o bin/exporter github.com/VictoriaMetrics/cybera-exporter/cmd/exporter