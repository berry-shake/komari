# berry-shake 维护说明

这三个仓库共同组成固定基线的自用 Komari 发行版：

| 仓库 | 上游基线 | 维护/默认分支 | 自有发布标签 |
| --- | --- | --- | --- |
| berry-shake/komari | 1.3.2 | mod | 1.3.2-fork.N |
| berry-shake/komari-web | 1.3.2 | mod | 1.3.2-fork.N |
| berry-shake/komari-agent | 1.2.60 | mod | 1.2.60-fork.N |

Agent 上游没有 1.3.2 标签；1.2.60 是服务端 1.3.2 发布时配套的正式版本。
`origin` 是 berry-shake 的 fork，`upstream` 是 komari-monitor。保留原版标签和
`main` / `radix` 分支作为基线，不在维护分支中自动跟随上游。
Go module、包导入路径和原作者署名保留原值；它们不是程序更新地址。

## 构建与更新来源

- 服务端二进制、安装脚本的升级下载、后台版本提示：`berry-shake/komari`。
- 探针二进制、自动更新、Linux/macOS/Windows 安装脚本：`berry-shake/komari-agent`。
- 默认主题及主题升级：`berry-shake/komari-web`。
- 容器：`ghcr.io/berry-shake/komari` 和 `ghcr.io/berry-shake/komari-agent`。
- 在线安装脚本从 `mod` 获取；正式构建及服务端内嵌前端固定具体提交。
- 不回退到官方仓库。首次发布前，fork 没有 Release 附件，安装脚本将安全失败。
- 第三方主题市场仍是原来的可选上游目录；独立第三方主题使用各自作者的更新源。
  本次没有 fork 第四个主题市场仓库，也没有更改第三方主题的所有权。

## Actions

`CI` 在 `mod` 推送、指向 `mod` 的 PR 或手动运行时进行检查，永不发布。
`Release` **只接受从 mod 手动触发**，参数 `tag` 必须是已推送、属于 mod 历史的
上述 fork 标签。不会自动创建标签、跟随上游发布、生成快照或部署服务器。

发布过程先完成检查和构建，生成每个附件的 `.sha256` 及 `SHA256SUMS`，再上传
Release 草稿并公开为正式 Release。服务端/Agent 同时构建 Linux amd64/arm64 镜像，
发布版本镜像后公开 Release，最后推进镜像 `latest`。任何单个平台构建失败都会阻止发布。
原有 Release（包括草稿）禁止覆盖；旧版本禁止覆盖 `latest`。

版本示例：首版服务端和前端 `1.3.2-fork.1`，首版 Agent `1.2.60-fork.1`，
后续只递增 `fork.N`。**GitHub 的 pre-release 选项必须为 false**，工作流会自动设置。
不要在 fork 中给原版标签创建 Release：Agent 使用 SemVer，原版 `1.2.60` 会被认为
高于 `1.2.60-fork.N`。首次从官方版迁移时使用我们的安装脚本重新安装一次；之后
未设置 `--disable-auto-update` 的裸机 Agent 会继续从自己的 fork 更新。容器 Agent 不进行
二进制自更新，通过替换自有镜像更新。`dev` 构建不自动更新。

首次发布镜像后，确认 Packages 的可见性为 Public 并验证匿名拉取，之后再部署。
如果失败时遗留未公开草稿，检查构建日志后删除该草稿并重跑工作流；已公开版本
使用新的 fork 修订号修复。若最后一步镜像 latest 推进失败，可将已成功构建的
版本镜像重新指向 latest，无需覆盖 Release 附件。

## 发布次序

1. 前端改动完成后更新 `komari-theme.json.version`，提交并推送 `mod`；创建对应
   fork 标签并推送，然后手动运行 `Release`。
2. 服务端 `.fork/frontend-ref` 填入这次前端的完整 40 位 commit SHA，提交并推送
   `mod`，通过 CI 后再打服务端 fork 标签并运行 `Release`。服务端构建不依赖前端
   Release 是否可下载，但其主题元数据必须与所固定前端版本一致。
3. Agent 独立使用 `1.2.60-fork.N` 标签发布；服务端和 Agent 不强行使用同一个数字版本。

示例（在各自仓库中，替换 TAG，确认当前提交已通过 CI）：

```bash
git switch mod
git tag -a TAG -m TAG
git push origin TAG
gh workflow run release.yml --ref mod -f tag=TAG
```

## 安装脚本的失败处理

安装脚本先下载到临时文件，校验 SHA-256，再停止/替换原安装。网络错误、404、缺失
校验文件和校验不符不会覆盖现有程序。服务端升级保存唯一命名备份，启动失败时
恢复该备份；Agent 安装保留旧二进制备份，服务管理器配置失败需按输出手动恢复。
校验文件防止损坏/错误响应；它与二进制同仓库发布，不是独立的签名信任链。

## 本地构建

需要 Go（版本见 go.mod）、Node.js 24、npm、Python 3.11+、C 编译器。

```bash
bash scripts/build-frontend.sh
python3 -m unittest discover -s scripts -p 'test_*.py'
go test -short ./...
VERSION=1.3.2-fork.1
VERSION_HASH=$(git rev-parse HEAD)
go build -trimpath -ldflags="-s -w -X github.com/komari-monitor/komari/utils.CurrentVersion=$VERSION -X github.com/komari-monitor/komari/utils.VersionHash=$VERSION_HASH" -o build/komari .
```

未注入版本的本地构建显示 dev。`scripts/build-frontend.sh` 只接受
`.fork/frontend-ref` 中的完整 SHA，使用 `npm ci`、运行前端测试和构建，再嵌入主题。
CI 除本机测试外，还交叉编译发布所需的 Linux/Windows 架构。

安装入口（首次正式发布完成后）：

```bash
curl -fsSL https://raw.githubusercontent.com/berry-shake/komari/mod/install-komari.sh -o install-komari.sh
sudo bash install-komari.sh
```
