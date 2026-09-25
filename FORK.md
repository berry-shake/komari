# berry-shake 单库维护版

维护/默认分支为 `mod`。仓库只保留 `mod` 与上游默认分支（服务端和 Agent 为 `main`，前端为 `radix`）。

## 独立发行版本

从 **1.2.4** 开始，三个仓库统一使用纯数字 `主版本.次版本.修订号`，不加 `v` 或 `fork` 后缀。此版本号属于 berry-shake 发行版，与上游版本独立；例如 1.2.4 不表示将上游代码升级到 1.2.4。

| 仓库 | 上游代码基线 | 当前发行版本 |
| --- | --- | --- |
| komari | 1.2.3 (`618ced3b8f0abd53d0e9ec1db0d90a948d2d221a`) | 1.2.5 |
| komari-web | 1.2.3 (`296fe766fab39c2ad7a4a3dd8caf3dcfebae3222`) | 1.2.5 |
| komari-agent | 1.2.13 | 1.2.4 |

服务端继续使用 1.2.3 的单 SQLite 设计，不引入新指标存储模块，不创建 metrics.db，不连接 PostgreSQL。节点 UUID/Token、延迟任务、GitHub OAuth、账号绑定和主题配置保持兼容。WAL/SHM 是同一 SQLite 数据库的活动文件。

版本比较按三个数字逐项进行：1.2.9 < 1.2.10 < 1.3.0。候选更新必须来自 berry-shake 对应仓库、为正式 Release、标签为纯数字且不低于 1.2.4。旧 fork 标签、草稿、预发布标签不参与新版本选择。更新不会查询上游仓库。

旧 Agent 1.2.13-fork.N 使用旧标签筛选规则，不能自动发现本次 1.2.4；首次更名切换须显式部署 1.2.4，之后继续正常自动更新。此次更名属于发行编号迁移，不能用上游基线数字判断升级/降级。

拉取上游代码时使用 `git fetch upstream --no-tags`，避免将上游同名标签导入本地发行标签空间；不要将上游标签推送到 origin。

## 更新和构建来源

服务端与后台更新：berry-shake/komari。Agent 安装及自更新：berry-shake/komari-agent。默认主题：berry-shake/komari-web。镜像：ghcr.io/berry-shake/komari 和 ghcr.io/berry-shake/komari-agent。安装脚本来自 `mod`。

服务端内嵌前端由 `.fork/frontend-ref` 锁定完整 commit SHA。Go module/import 路径、第三方主题和原作者信息保持原值。`--disable-auto-update` 和 `--disable-web-ssh` 行为不变；容器 Agent 通过更换镜像更新。

## CI 和发布

CI 在 mod push、PR 或手动触发时执行，不发布。Release 仅从 mod 手动触发，要求已经存在且属于该分支历史的纯数字标签。拒绝覆盖任何已有 Release/草稿，拒绝倒退到已发布数字版本；旧 fork 标签只在首次迁移预检中跳过，不重新发布。

前端先验证、发布并固定 SHA，再发布服务端；Agent 可独立发布。发布生成每个附件的 .sha256 和 SHA256SUMS，正式 Release 与版本镜像完成后，将 GitHub latest 和镜像 latest 指向本次版本。镜像 latest 的用户可能由其自行配置的更新器自动升级。

```sh
git switch mod
git tag -a 1.2.4 -m 1.2.4
git push origin mod refs/tags/1.2.4
gh -R berry-shake/REPOSITORY workflow run release.yml --ref mod -f tag=1.2.4
```

默认不覆盖已发布资产，后续修复递增版本。本次按维护者明确要求重发 1.2.5：先验证新提交，再删除原 1.2.5 Release、重建标签并重新构建；旧产物在本地留档。安装器先下载临时文件并验证 SHA256，再替换服务；原安装参数继续保留。

## 原生 DDNS

原生 DDNS 使用 Go 模块与后台 `/admin/ddns`，Cloudflare 设置、记录、状态和最近 500 条日志均存入 komari.db，无需插件运行时。1.2.4 增加服务端日志分页，后台默认每页 20 条，可切换 50/100 条，支持筛选、首末页及前后翻页；清空和刷新回到第一页。

来源、许可和使用说明见 [DDNS 文档](https://github.com/berry-shake/komari/blob/mod/docs/DDNS.md)。

## 后台登录入口

1.2.5 在后台路由入口验证登录状态，确认登录后才加载管理导航、子页面和设置。未登录显示独立登录页；身份请求失败显示可重试状态，不加载管理页面。密码和双因素登录保留原访问路径，GitHub OAuth 继续使用原入口。页面重新获得焦点或每分钟检查会话，失效后卸载管理页面；合法会话的后台检查不打断当前表单。法律声明只在登录后且设置明确要求接受时显示。

## 已移除的集成

1.2.5 重发移除哪吒 Agent gRPC 兼容和内置 Cloudflare Tunnel：包含启动逻辑、管理 API、后台入口、配置项及镜像中的 cloudflared。启动迁移仅清理这两项功能的三个旧配置键，管理 API 拒绝重新写入。Komari 原生 WebSocket/HTTP 上报、单 SQLite、延迟监控、GitHub 登录及 Cloudflare DNS DDNS 保留。DDNS 的 Cloudflare 设置卡片默认折叠，展开后可编辑；临时收起不会丢失尚未保存的输入。

## 本地验证

使用 go.mod 声明的 Go 工具链、Node.js 24、Python 3.11+、C 编译器。前端 npm ci/test/build；服务端 scripts/build-frontend.sh、Python 脚本测试、go test -short ./... 和 DDNS/API race 测试；Agent 执行 Go/Python 测试、更新逻辑 race 测试及发布矩阵构建。正式 Linux amd64/arm64 镜像需验证 Agent 上报、延迟检测、单库持久化和日志分页后交付。未注入版本的开发构建显示 dev。
