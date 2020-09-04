.PHONY: empty build strip

empty:
	@echo Use build or strip

build:
	$(ENV) go build -mod=vendor $(FLAGS) ./cmd/rupicola

# just use build target with flags
strip: FLAGS += -ldflags=...="-s -w"
strip: build

android: ENV := GOOS=linux
android: ENV += GOARCH=arm
android: ENV += GOARM=7 
android: FLAGS := -mod=vendor -ldflags=.='-s -w -buildid=' -trimpath
android: build

#GOOS=linux GOARCH=arm GOARM=7 go build -mod=vendor -ldflags=.='-s -w -buildid=' -trimpath ./cmd/rupicola