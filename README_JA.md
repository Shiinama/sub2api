# Sub2API

サブスクリプションクォータ配分向けの AI API ゲートウェイです。

[English](README.md) | [中文](README_CN.md)

## Railway デプロイ

このリポジトリには `Dockerfile` と `railway.toml` が含まれており、そのまま Railway で使えます。

1. Railway でこの GitHub リポジトリから新しいプロジェクトを作成します。
2. PostgreSQL と Redis を追加するか、既存のサービスに接続します。
3. `DATABASE_URL` と `REDIS_URL` を設定します。
4. デプロイします。

Railway は `PORT` を自動で提供します。必要なら `SERVER_PORT` で上書きできます。

## ヘルスチェック

デプロイ後に `/health` を開いて稼働確認します。

## 補足

- 詳細なデプロイ手順は [deploy/README.md](deploy/README.md) に残しています。
- このトップレベル README は Railway 向けの最小構成にしました。
