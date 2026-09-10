<a name="readme-top"></a>

<div align="center">

<img src="docs/pictures/banner.png" alt="LitePan" width="100%">

# LitePan

一个面向个人与家庭媒体库的网盘聚合管理工具。

[![License][license-shield]][license-url]

</div>

> [!NOTE]
> 本项目 fork 自 [Ponphil/LitePan](https://github.com/Ponphil/LitePan)，在原项目基础上持续维护和整理。

## 项目简介

LitePan 使用 Go 编写，提供统一的网盘文件管理、媒体整理、STRM 生成、离线下载和网盘资源搜索能力。项目包含 Vue/TypeScript Web 界面，可通过 Docker Compose 快速部署。

## 主要功能

<table>
  <tr>
    <td width="50%" valign="top" align="center">
      <h3>网盘聚合管理</h3>
      <p align="left">统一管理多个网盘账号，在一个界面中浏览和操作文件。</p>
      <img src="docs/pictures/feature-browser.png" alt="网盘聚合管理" height="220">
    </td>
    <td width="50%" valign="top" align="center">
      <h3>跨盘传输</h3>
      <p align="left">优先使用秒传能力，不满足条件时自动上传。</p>
      <img src="docs/pictures/feature-crosstransfer.png" alt="跨盘传输" height="220">
    </td>
  </tr>
  <tr>
    <td width="50%" valign="top" align="center">
      <h3>STRM 媒体库</h3>
      <p align="left">生成 <code>.strm</code> 文件，对接 Emby、Jellyfin 等媒体服务器。</p>
      <img src="docs/pictures/feature-strm.png" alt="STRM 媒体库" height="220">
    </td>
    <td width="50%" valign="top" align="center">
      <h3>STRM 刮削</h3>
      <p align="left">生成 nfo 和海报信息，方便维护媒体库。</p>
      <img src="docs/pictures/feature-strm-scrape.png" alt="STRM 刮削" height="220">
    </td>
  </tr>
  <tr>
    <td width="50%" valign="top" align="center">
      <h3>媒体整理</h3>
      <p align="left">基于 TMDB 识别媒体信息，预览后整理文件和目录。</p>
      <img src="docs/pictures/feature-organize.png" alt="媒体整理" height="220">
    </td>
    <td width="50%" valign="top" align="center">
      <h3>自动联动</h3>
      <p align="left">串联整理、STRM、刮削和媒体库刷新流程。</p>
      <img src="docs/pictures/feature-automation.png" alt="自动联动" height="220">
    </td>
  </tr>
</table>

此外还支持：

- WebDAV 和 FUSE 本地挂载；
- 302 直链、缓存保持、命名对齐；
- HTTP、磁力链接和电驴链接离线下载；
- 115、夸克等网盘的分享链接转存；
- 基于 PanSou 的跨网盘资源搜索与一键转存。

## 离线下载与分享转存

“离线下载”同时支持链接任务、BT 种子和分享转存。分享转存直接调用目标网盘接口，不经过本地下载和重新上传。

| 网盘 | 普通离线下载 | 分享链接转存 | 认证要求 |
| --- | --- | --- | --- |
| 115 Open | HTTP / HTTPS / FTP / Magnet / ED2K / BT | `115.com`、`anxia.com`、`115cdn.com` | Open OAuth；分享转存还需要网页版 Cookie |
| 夸克网盘 | HTTP / HTTPS / Magnet | `pan.quark.cn` | 账号 Cookie |
| 123 云盘 Open | HTTP / HTTPS | 暂未支持 | Open API 凭据 |
| 光鸭云盘 | HTTP / HTTPS / FTP / Thunder / Magnet | 暂未支持 | 账号凭据 |
| 其他支持上传的网盘 | HTTP / HTTPS / Magnet | 暂未支持 | 对应账号凭据 |

分享链接只能转存到相同网盘类型的账号。例如，夸克分享链接需要选择已配置的夸克账号作为目标账号。

### 分享转存流程

1. 在文件浏览器中选择目标网盘账号和保存目录。
2. 打开“离线下载”，切换到“分享转存”。
3. 粘贴分享链接或完整分享文案，并按需填写提取码。
4. 解析分享链接，选择需要转存的文件或文件夹。
5. 确认保存位置后提交转存任务。

115 分享转存需要在对应的 115 Open 账号中配置当前账号的网页版 Cookie。Cookie 属于敏感凭据，请只保存在自己的 LitePan 实例中，并在 Cookie 失效后及时更新。

除手动提交外，PanSou 搜索结果和后台“影视搜索转存”测试列表也支持一键转存。分享链接走对应网盘的转存接口，磁力和电驴链接则交给支持离线下载的账号处理。

## PanSou 资源搜索

PanSou 资源搜索可以按片名聚合多个网盘平台的分享资源，并将搜索结果直接转存到已配置的网盘账号。

### 配置

1. 进入“设置 → PanSou 资源搜索”，启用功能。
2. 配置 PanSou 服务地址，默认地址为 `https://so.252035.xyz`。
3. 根据服务端要求填写 Basic Auth 或 API Token。
4. 选择需要搜索的平台，并在后台“增强工具 → 影视搜索转存”中测试连接。

PanSou 是第三方聚合服务，服务可用性和返回内容取决于上游。密码和 Token 等凭据应当仅保存在自己的实例中。

## 快速开始

### Docker Compose

```yaml
services:
  litepan:
    image: ghcr.io/q107580018/litepan:latest
    container_name: litepan
    restart: unless-stopped
    ports:
      - "5211:5211"
      # 内置 Magnet 的 TCP/uTP/DHT 监听端口；若在后台修改，需同步调整映射
      - "42069:42069/tcp"
      - "42069:42069/udp"
    environment:
      - TZ=Asia/Shanghai
    volumes:
      - ./data:/app/data
      - ./strm:/app/strm
      - ./mounts:/app/mounts:shared
      # 可选：将 FUSE 读缓存单独映射到更快的磁盘
      # - ./fuse_read_cache:/app/data/fuse_read_cache
    devices:
      - /dev/fuse:/dev/fuse
    pid: "host"
    privileged: true
```

启动服务：

```bash
docker compose up -d
```

然后访问 `http://你的IP:5211`。首次登录后请立即修改管理员密码。使用 FUSE 挂载时，宿主机需要提供 `/dev/fuse`，并保留示例中的相关权限配置。

镜像发布到 GitHub Container Registry：

```bash
docker pull ghcr.io/q107580018/litepan:latest
```

如需固定当前版本，可将 Compose 中的镜像改为 `ghcr.io/q107580018/litepan:v0.6.3`。推送到 `main` 或创建版本标签后，GitHub Actions 会构建并发布 amd64/arm64 镜像。

### 从源码运行

环境要求：Go 1.26.6、Node.js 和 npm。

```bash
# 构建前端依赖并生成 Web 资源
cd web
npm ci
npm run build
cd ..

# Go 测试和构建
make test
make build
```

常用命令：

```bash
make build-nofuse  # 不包含 FUSE 支持的构建
make lint          # Go 代码检查
make docker-build  # 构建本地 Docker 镜像
```

运行时默认使用以下目录和端口：

- HTTP：`5211`
- 数据目录：`./data`
- STRM 目录：`./strm`
- 挂载目录：`./mounts`

可使用以下参数或环境变量覆盖默认配置：

- 参数：`--listen`、`--data-dir`、`--strm-dir`
- 环境变量：`LITEPAN_LISTEN`、`LITEPAN_DATA_DIR`、`LITEPAN_STRM_DIR`、`LITEPAN_DB_PATH`、`LITEPAN_LOG_LEVEL`

## 贡献与反馈

欢迎通过本仓库的 Issue 反馈问题、提出建议或提交改进。提交代码前请先阅读 [AGENTS.md](./AGENTS.md) 了解目录结构、开发命令和代码规范。

外部贡献记录见 [ACKNOWLEDGEMENTS.md](./ACKNOWLEDGEMENTS.md)。

## 许可

本项目使用 [PolyForm Noncommercial 1.0.0](./LICENSE) 许可证，仅允许非商业用途。请同时遵守各网盘服务条款、第三方服务条款和当地法律法规。

第三方依赖及其许可信息见 [THIRD_PARTY_NOTICES.md](./THIRD_PARTY_NOTICES.md)。

[license-shield]: https://img.shields.io/badge/License-PolyForm%20NC-red?style=flat-square
[license-url]: ./LICENSE
