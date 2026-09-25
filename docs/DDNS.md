# 原生 Cloudflare DDNS

当前 1.2.4 中，后台侧栏 **DDNS**（`/admin/ddns`）可管理动态解析。功能移植自 [Komari DDNS v0.1.3](https://github.com/yunjianj/Komari-DDNS/tree/v0.1.3)，来源为 [Komari 插件市场](https://github.com/komari-monitor/plugin-market) 的 `cf-ddns`。原作者：穿云箭（yunjianj），MIT 许可；许可原文保留在 [licenses/Komari-DDNS-MIT.txt](licenses/Komari-DDNS-MIT.txt)。本实现使用 Go 和现有 React 管理后台，无需插件引擎或新增数据库。

## 使用

1. 设置 Cloudflare API Token。Token 需对目标域名具有 **Zone / DNS / Edit** 权限；留空 Zone ID 自动发现域名时，还需 **Zone / Zone / Read** 权限。可将 Token 权限限制为实际使用的域名。
2. 添加 A（IPv4）或 AAAA（IPv6）记录，填写完整域名并选择来源节点。支持每条记录覆盖全局 Token、手动 Zone ID、TTL、Cloudflare 代理和本地备注。
3. 点击“立即同步”验证结果，再启用自动同步并保存。新安装默认关闭自动同步；默认间隔 5 分钟，可设为 1–1440 分钟。修改间隔立即重置计时，无需重启。
4. 日志显示每条记录及整轮同步的结果。最多保留 500 条，支持按域名筛选和清空。

多来源节点按选择顺序使用第一个在线且 IP 有效的节点。离线、私网、回环、链路本地或共享地址不写入 DNS。没有可用节点时保留原解析，不删除记录。IPv6 地址按规范化结果比较，避免表示形式不同导致反复更新。

Cloudflare 中没有对应记录时创建；IP、代理开关或 TTL 变化时更新。代理记录 TTL 固定为自动。更新使用 PATCH，保留 Cloudflare 上已有的备注和标签；同名同类型存在多条记录时报告错误，须先人工明确保留哪一条。后台“移除记录”仅移除 Komari 的 DDNS 配置，Cloudflare DNS 记录会保留。修改记录名称或类型会管理新目标，也不会删除原 Cloudflare 记录。

“DNS 变更时发送通知”复用现有通知渠道及全局通知开关，只在实际创建或修改解析时发送，默认关闭。手动同步可在自动同步关闭时运行；同步期间其他同步或配置修改会返回 409，避免并发覆盖。

## 数据和接口

`ddns_settings`、`ddns_records`、`ddns_logs` 三张表均位于现有 `komari.db`，随原有整库备份一起保存。不创建 `metrics.db`。API Token 存在服务器数据库中；只通过已有管理员鉴权接口设置，API 响应只返回 `api_token_set`，不返回 Token。输入留空保留旧 Token，清除需显式勾选；日志会遮蔽当前 Token。请按其他管理配置一样保护数据库和备份。

全部接口位于 `/api/admin/ddns`，使用既有管理员 Session 或 API Key 鉴权；Agent Token 和访客无权调用。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET / PUT | `/settings` | 获取脱敏设置 / 保存设置 |
| GET / POST | `/records` | 列表 / 添加解析 |
| PUT / DELETE | `/records/:id` | 编辑 / 移除本地配置 |
| GET | `/nodes` | 节点名称、在线状态和公网地址 |
| POST | `/sync` | 手动同步及每条记录结果 |
| GET / DELETE | `/logs` | 查询 / 清空日志 |

设置示例：`{"enabled":false,"interval":5,"notify":false,"api_token":"YOUR_TOKEN"}`。之后保存时可省略 `api_token`；清除使用 `clear_api_token:true`。

记录示例：`{"record_name":"home.example.com","record_type":"A","zone_id":"","source_node":["NODE_UUID"],"ttl":60,"proxied":false,"comment":""}`。可选 `api_token`、`clear_api_token` 用于单条记录覆盖或恢复使用全局 Token。自动 Zone 发现按最长后缀匹配，分页查询并按 Token 分别缓存一轮结果。单次 HTTP 请求超时 20 秒，整轮同步最长 2 分钟。

旧版插件配置不会在新程序启动时自动启用；迁移应先备份，导入 Token 和记录，在自动同步关闭时核对并手动验证，再恢复原有自动同步设置。旧插件文件可保留用于回退，1.2.3 分支不加载这些文件。

## 同步日志分页

后台默认显示每页 20 条，可选择 50 或 100 条，显示筛选后的总条数与页数，并支持首页、上一页、下一页、末页。更换域名筛选、每页数量、刷新或清空后回到第一页。服务端继续只保留最近 500 条日志。

管理员接口 `GET /api/admin/ddns/logs?page=2&page_size=20` 返回 `data.logs`、`data.total`、`data.page`、`data.page_size`、`data.total_pages`。可同时使用 `record`（完整域名）和 `action` 筛选；旧参数 `limit` 作为 `page_size` 的兼容别名。页码和每页数量必须为正整数，每页最多 500 条；超过末页会返回最后一页，空结果返回第 1 页和空数组。计数与结果在同一数据库快照中读取。
