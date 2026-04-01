# 通过 Docker 部署 Telegram DN42 机器人

## Server

Docker Compose 示例：

```yaml
version: '3.8'

services:
  server:
    image: ghcr.io/bingxin666/dn42-bot/server:latest
    container_name: dn42-bot-server
    volumes:
      - ./config.py:/app/config.py:ro
      - ./data:/app/data
      - ./cache:/app/cache
    restart: unless-stopped
```

`config.py` 文件请参考 `server/config.example.py` 进行修改。

如果你要启用外部 OIDC / OAuth 登录：

- 必须启用 webhook 模式，也就是让容器里的 aiohttp 服务能够接收来自 Telegram 与身份提供商的 HTTP 回调。
- 需要在 `config.py` 中设置 `OIDC_LOGIN["base_url"]`，它应当指向用户浏览器和身份提供商都能访问到的公开基址。
- 需要把回调地址 `<OIDC_LOGIN["base_url"]><OIDC_LOGIN["callback_path"]>` 注册到你的 OIDC provider。
- `iedon` 模板会预填 discovery 地址、默认显示名，以及与 discovery 文档一致的默认 scope `dn42`。

> **插件说明**：插件通过 Git 仓库自动加载。在 `config.py` 的 `PLUGINS` 列表中指定插件 Git 地址和名称，启动时会自动 clone 到 `./data/plugins_repos/` 目录。Docker 镜像已内置 `git` 和 `nodejs/npm`，无需额外配置。如果插件包含 Web 前端（如 `email` 插件），启动时会自动执行 `npm install && npm run build`。插件数据存放在 `./data/plugins/` 下，由 `./data` volume 统一管理。

## Agent

Docker Compose 示例：

```yaml
version: "3.8"

services:
  agent:
    image: ghcr.io/bingxin666/dn42-bot/agent:latest
    container_name: dn42-agent
    dns:
      - 172.20.0.53
      - 1.1.1.1
    network_mode: host
    cap_add:
      - NET_ADMIN
      - SYS_ADMIN
    devices:
      - /dev/net/tun:/dev/net/tun
    restart: unless-stopped
    volumes:
      - /etc/wireguard:/etc/wireguard
      - /etc/bird/dn42_peers:/etc/bird/dn42_peers
      - /etc/bird/config:/etc/bird/config
      - /var/run/bird/bird.ctl:/var/run/bird/bird.ctl # 修改为你的 bird.ctl 路径
      - ./agent_config.json:/app/agent_config.json:ro
```
