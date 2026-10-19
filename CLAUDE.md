# mesh 项目规范

## 版本发布流程（所有代码变更统一走此流程）

```
分支开发 → coderabbit review → 合并 master → push → 打 tag → CI 自动发 Release → 各机器 curl 升级
```

1. **分支开发**：`git checkout -b <type>/<topic>`；合并回 master 前必须跑 coderabbit review
2. **推送**：`git push origin master`
3. **打 tag 发版**（触发 `.github/workflows/release.yml`：全量测试 → 构建 4 平台二进制 → 自动发 Release）：
   ```bash
   git tag -a vX.Y.Z -m "vX.Y.Z: <一句话摘要>"
   git push origin vX.Y.Z
   ```
4. **确认产物**：`gh run watch --repo kvmaker/mesh`（等 CI），`gh release view vX.Y.Z --repo kvmaker/mesh`
5. **各机器升级**（重复执行安装命令即升级，服务自动重启）：
   ```bash
   # macOS / Linux 客户端
   curl -fsSL https://raw.githubusercontent.com/kvmaker/mesh/master/install.sh | sudo bash -s -- client
   # 服务器
   curl -fsSL https://raw.githubusercontent.com/kvmaker/mesh/master/install.sh | sudo bash -s -- server --mode relay --domain gz.kvmaker.cn
   ```

版本号语义化（vX.Y.Z）；纯文档变更可不发版。

## 关键约定

- **install.sh 从 master 拉取**：修改 install.sh 后必须 push 到 master，线上 `curl .../master/install.sh` 才生效；老机器可加 `--version X.Y.Z` 钉住旧版
- **境内云服务器直连 GitHub 慢**（18MB meshd 可挂数分钟）：gz-ubuntu 升级走本地构建 + scp（注入版本号便于 `meshd version` 追溯）：
  ```bash
  GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X github.com/maxyu/mesh/internal/common/version.Version=vX.Y.Z-local" -o /tmp/meshd ./cmd/meshd
  scp /tmp/meshd gz-ubuntu:/tmp/meshd.new
  ssh gz-ubuntu 'sudo install -m 755 /tmp/meshd.new /usr/local/bin/meshd && sudo systemctl restart meshd'
  ```
- 升级不影响设备注册（`~/.mesh`）与服务器配置（`/etc/mesh/meshd.yaml`）
- 服务器 meshd 重启会瞬断所有隧道几秒，客户端 3 秒内自动重连，无需干预
