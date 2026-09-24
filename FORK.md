# berry-shake 单库维护版

当前维护/默认分支为 `mod`。仓库只保留 `mod` 与上游默认分支（服务端和 Agent 为 `main`，前端为 `radix`）。旧维护提交保留在 Git 历史中；部署备份用于回退，过时 Release、标签和构建产物在迁移验证通过后清理。

| 仓库 | 上游代码基线 | 本次 fork 版本 |
| --- | --- | --- |
| komari | 1.2.3 (`618ced3b8f0abd53d0e9ec1db0d90a948d2d221a`) | 1.2.3-fork.3 |
| komari-web | 1.2.3 (`296fe766fab39c2ad7a4a3dd8caf3dcfebae3222`) | 1.2.3-fork.3 |
| komari-agent | 1.2.13 | 1.2.13-fork.2 |

服务端 1.2.3 是引入新指标存储模块之前的最后一个正式版本。1.2.5 虽然尚未启用独立监控库，已新增 pkg/metric 模块；本维护线不包含这套新机制。前端使用配套 1.2.3，Agent 使用同期正式版 1.2.13。

## 运行和数据

配置和监控记录使用同一个 `data/komari.db`。SQLite 的 `-wal`/`-shm` 是同库日志文件，不是第二个监控数据库。不创建 `metrics.db`，不连接 PostgreSQL。迁移保留节点 UUID/Token、全部节点配置、延迟检测目标和分配、GitHub OAuth 和用户绑定及主题设置；历史监控数据可重新开始。

服务端内嵌前端由 `.fork/frontend-ref` 锁定本 fork 的完整 commit SHA。现有第三方主题、作者信息、Go module/import 路径仍保留原值。

## 更新来源

服务端和后台版本检测：berry-shake/komari。探针自更新与安装脚本：berry-shake/komari-agent。默认主题：berry-shake/komari-web。容器：ghcr.io/berry-shake/komari 与 ghcr.io/berry-shake/komari-agent。安装脚本来自 `mod`。

后台版本提示仅接受 `1.2.3-fork.N` 单库维护线，避免其他基线被当作升级。Agent 使用 `1.2.13-fork.N`，未设置 `--disable-auto-update` 时从自己的 fork 检查更新；容器探针通过更换镜像更新。`--disable-web-ssh` 继续禁用 Web SSH/远程执行。

## CI 和发布

CI 在维护分支 push、PR 或手动触发时执行，不发布。Release 仅可从 `mod` 手动触发；必须提供已存在且属于该分支历史的 fork 标签。拒绝覆盖已有 Release/草稿，拒绝在同一维护线上倒退修订号；其他基线不参与该线版本排序。

发布先测试与构建，生成每件附件的 `.sha256` 和 SHA256SUMS，再发布稳定 Release（prerelease=false）及版本镜像，最后将 GitHub latest 和镜像 latest 指向当前单库维护线。首次降基线切换必须由运维按备份方案显式部署，不能依赖版本数字排序自动降级。

前端先发布并固定 SHA，再发布服务端；Agent 可独立发布。服务端 Linux amd64/arm64 运行验证均需确认只有 komari.db，Agent 上报和延迟检测正常，之后才部署。

```sh
git switch mod
git tag -a TAG -m TAG
git push origin TAG
gh -R berry-shake/REPOSITORY workflow run release.yml --ref mod -f tag=TAG
```

安装器先下载到临时文件并验证 SHA256，成功后才停止/替换服务。服务端启动失败自动回退旧二进制；Agent 保存旧二进制，服务配置保持使用者指定值。版本发布资产不可覆盖，修复需递增 fork.N。

## 本地验证

使用 go.mod 声明的 Go 版本、Node.js 24、Python 3.11+、C 编译器。前端 npm ci/test/build；服务端 scripts/build-frontend.sh、Python 脚本测试和 go test -short ./...；Agent 同样执行 Go/Python 测试及发布矩阵交叉编译。未注入版本的开发构建显示 dev。

## 原生 DDNS

1.2.3-fork.3 将 Komari DDNS v0.1.3 的 Cloudflare 功能移植为服务端 Go 模块及后台 `/admin/ddns` 页面。设置、记录、状态和最近 500 条日志均存入同一个 `komari.db`，无需插件运行时。来源和使用说明见 [DDNS 文档](https://github.com/berry-shake/komari/blob/mod/docs/DDNS.md)。Agent 继续使用 1.2.13-fork.2。
