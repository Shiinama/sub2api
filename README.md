# Sub2API

AI API gateway for subscription quota distribution.

[中文](README_CN.md) | [日本語](README_JA.md)

## Railway deploy

This repo is ready for Railway with `Dockerfile` and `railway.toml`.

1. Create a new Railway project from this GitHub repo.
2. Add PostgreSQL and Redis services, or connect your existing ones.
3. Set `DATABASE_URL` and `REDIS_URL`.
4. Deploy.

Railway will provide `PORT` automatically. The app also accepts `SERVER_PORT` if you want to override it.

## Health check

After deploy, open `/health` to verify the service is up.

## Notes

- The main Docker entrypoint and Railway settings already live in this repo.
- More deployment details are in [deploy/README.md](deploy/README.md).
