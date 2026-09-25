# 安全加固与升级说明

这次修改保留现有 SQLite、节点 UUID/Token、延迟任务、DDNS 和 GitHub 登录配置，不需要重新注册节点。

## 行为变化

- 私有站点的最近监控接口与其他监控接口采用相同的登录限制；公开站点仍可显示非隐藏节点。
- 一般 API 请求及 WebSocket 解码后的单条消息上限为 1 MiB；登录请求为 16 KiB。管理员主题上传允许 64 MiB，备份上传允许 256 MiB，图标上传允许 5 MiB；主题解压后的总大小不超过 256 MiB。
- 无身份的管理请求不会再为了寻找节点 Token 而读取整个请求体。旧节点的 query / JSON Token 仍可使用，新 Agent 使用 `X-Client-Token`。
- 主题删除、设置、更新和导入校验主题名称与目录，拒绝目录穿越和符号链接目录；主题临时文件使用随机名称。
- 新密码保存为独立随机盐、成本 12 的 bcrypt 哈希。输入先经过 SHA-256/Base64 编码，保留原有超过 72 字节密码的完整含义。旧固定盐 SHA-256 哈希仅用于兼容验证，正确登录后自动升级。
- 密码登录按账号限制为每分钟 10 次，并按实际连接来源限制为每分钟 30 次；OAuth 发起按连接来源限制为每分钟 20 次。来源限制不信任任意转发头，位于反向代理后时，该部分限额由代理出口共享。超限返回 429。
- GitHub OAuth state 有效期为 5 分钟，只能消费一次，最多保留 1024 个未过期 state。
- 请求日志脱敏 query 中的凭据；SQL 调试日志保留操作类型、行数、耗时和代码位置，不打印完整 SQL 值。显式提供的 `ADMIN_PASSWORD` 不写入启动日志。
- Go 构建基线为 1.26.8。CI 和发布工作流运行固定版本 govulncheck，存在代码可达的已知漏洞时阻止流程成功。

## 部署和回退

应先部署本次服务端，再部署对应的新 Agent。服务端同时接受旧节点协议；新 Agent 仅在 Header 中发送节点 Token，需要本次服务端支持。

部署前按常规方式备份 SQLite。密码升级不改变 OAuth 绑定和其他配置，但旧二进制无法识别新密码哈希；若需要回退到未包含本次修改的服务端，应连同升级前的数据库备份一起回退，或在旧版本重设本地密码。

## 本地验证

```sh
go test -short ./...
go test -race -short ./database/accounts ./pkg/ddns ./web/api/... ./web/connection ./web/oauth/github ./web/security ./utils/log
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
```

扫描按实际导入和调用判断。`golang.org/x/crypto` 的 OpenPGP 模块级公告涉及未被此项目导入的包，没有以忽略规则隐藏它。
