# Sub2API

用于订阅配额分发的 AI API 网关。

[English](README.md) | [日本語](README_JA.md)

## Railway 部署

本仓库已经包含 `Dockerfile` 和 `railway.toml`，可以直接接到 Railway。

1. 在 Railway 中从这个 GitHub 仓库创建项目。
2. 添加 PostgreSQL 和 Redis 服务，或者连接你已有的实例。
3. 配置 `DATABASE_URL` 和 `REDIS_URL`。
4. 部署。

Railway 会自动提供 `PORT`。如果你想手动覆盖，也可以设置 `SERVER_PORT`。

## 健康检查

部署完成后，访问 `/health` 确认服务正常。

## 说明

- 更细的部署说明保留在 [deploy/README.md](deploy/README.md)。
- 顶部 README 已经收缩成 Railway 启动页。
