# 构建内嵌前端

在服务端仓库根目录运行：

```bash
bash scripts/build-frontend.sh
```

脚本从 `berry-shake/komari-web` 获取 `.fork/frontend-ref` 指定的完整提交，
使用 Node.js 24 和 `npm ci` 构建，将 `dist/`、主题元数据及预览图复制到
`web/public/defaultTheme/`，供 Go embed 使用。更新前端时先更新该 SHA，
不使用上游主分支或浮动的 fork 默认分支。详见 [FORK.md](../../FORK.md)。
