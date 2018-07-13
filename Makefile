.PHONY: empty build strip

empty:
	@echo Use build or strip

build:
	go build $(FLAGS)

# just use build target with flags
strip: FLAGS := -ldflags=...="-s -w"
strip: build