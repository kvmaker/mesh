.PHONY: build test clean install deploy-linux deploy-macmini

INSTALL_DIR := /usr/local/bin

build:
	go build -o bin/meshd ./cmd/meshd
	go build -o bin/mesh ./cmd/mesh

test:
	go test ./...

clean:
	rm -rf bin/

build-linux:
	GOOS=linux GOARCH=amd64 go build -o bin/meshd-linux-amd64 ./cmd/meshd
	GOOS=linux GOARCH=amd64 go build -o bin/mesh-linux-amd64 ./cmd/mesh

build-darwin:
	GOOS=darwin GOARCH=arm64 go build -o bin/mesh-darwin-arm64 ./cmd/mesh

build-all: build-darwin build-linux

# macOS 本机安装：go install 直接输出到 GOPATH/bin 避免 xattr 问题，
# 再 codesign ad-hoc 签名防止 Gatekeeper kill。
install:
	go build -o $(INSTALL_DIR)/mesh ./cmd/mesh
	codesign --sign - --force $(INSTALL_DIR)/mesh
	@echo "已安装: $(INSTALL_DIR)/mesh"

# 部署到远程 Linux 主机（客户端）
# 用法: make deploy-linux HOST=hk-test
deploy-linux: build-linux
	scp bin/mesh-linux-amd64 $(HOST):/tmp/mesh-new
	ssh $(HOST) "sudo install -m 755 /tmp/mesh-new $(INSTALL_DIR)/mesh && rm /tmp/mesh-new"
	@echo "已部署到 $(HOST)"

# 部署到远程 macOS 主机（客户端）
# 用法: make deploy-macmini HOST=macmini-public
deploy-macmini: build-darwin
	scp bin/mesh-darwin-arm64 $(HOST):/tmp/mesh-new
	ssh -t $(HOST) "sudo install -m 755 /tmp/mesh-new $(INSTALL_DIR)/mesh && sudo codesign --sign - --force $(INSTALL_DIR)/mesh && rm /tmp/mesh-new"
	@echo "已部署到 $(HOST)"

# 部署服务端到远程 Linux 主机
# 用法: make deploy-server HOST=ubuntu@1.14.193.183
deploy-server: build-linux
	scp bin/meshd-linux-amd64 $(HOST):/tmp/meshd-new
	ssh $(HOST) "sudo systemctl stop meshd; sudo install -m 755 /tmp/meshd-new $(INSTALL_DIR)/meshd && rm /tmp/meshd-new && sudo systemctl start meshd"
	@echo "已部署服务端到 $(HOST)"
