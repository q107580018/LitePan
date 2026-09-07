<a name="readme-top"></a>

<div align="center">

<img src="docs/pictures/banner.png" alt="LitePan" width="100%">

<br>

<a href="https://www.litepan.top"><img src="https://img.shields.io/badge/官网文档-www.litepan.top-6C63FF?style=for-the-badge&labelColor=1B1B2F" alt="官网文档"></a>
&nbsp;
<a href="https://space.bilibili.com/1501989416"><img src="https://img.shields.io/badge/Bilibili-交流与演示-00A1D6?style=for-the-badge&logo=bilibili&logoColor=white&labelColor=1B1B2F" alt="Bilibili"></a>
&nbsp;
<a href="https://hub.docker.com/r/ponphil/litepan"><img src="https://img.shields.io/badge/Docker-ponphil%2Flitepan-2496ED?style=for-the-badge&logo=docker&logoColor=white&labelColor=1B1B2F" alt="Docker"></a>


[![docker-pulls][docker-pulls-shield]][docker-url]
[![version][version-shield]][docker-url]
[![license][license-shield]][license-url]

</div>

<br>

> [!CAUTION]
> 当前仓库是正在开发中的 **Go 版 LitePan**，首次发布可能问题较多，请谨慎测试。
> Python 旧版已归档至 [LitePan-old](https://github.com/Ponphil/LitePan-old)。


<br>

## ▎ 功能简述

<table>
  <tr>
    <td width="50%" valign="top" align="center">
      <h3>多网盘聚合</h3>
      <p align="left">多账号统一管理，一个界面看完。</p>
      <img src="docs/pictures/feature-browser.png" alt="多网盘聚合" height="220">
    </td>
    <td width="50%" valign="top" align="center">
      <h3>跨盘秒传</h3>
      <p align="left">能秒传就秒传，否则自动上传。</p>
      <img src="docs/pictures/feature-crosstransfer.png" alt="跨盘秒传" height="220">
    </td>
  </tr>
  <tr>
    <td width="50%" valign="top" align="center">
      <h3>STRM 直连播放</h3>
      <p align="left">生成 <code>.strm</code>，对接 Emby / Jellyfin。</p>
      <img src="docs/pictures/feature-strm.png" alt="STRM 直连播放" height="220">
    </td>
    <td width="50%" valign="top" align="center">
      <h3>STRM 刮削</h3>
      <p align="left">写 nfo / 海报，海报墙可追更。</p>
      <img src="docs/pictures/feature-strm-scrape.png" alt="STRM 刮削" height="220">
    </td>
  </tr>
  <tr>
    <td width="50%" valign="top" align="center">
      <h3>目录整理</h3>
      <p align="left">TMDB 识别，预览后再归档。</p>
      <img src="docs/pictures/feature-organize.png" alt="目录整理" height="220">
    </td>
    <td width="50%" valign="top" align="center">
      <h3>自动联动</h3>
      <p align="left">整理、STRM、刮削、刷库串起来。</p>
      <img src="docs/pictures/feature-automation.png" alt="自动联动" height="220">
    </td>
  </tr>
</table>

## ▎ 挂载与更多功能

支持 WebDAV 与 FUSE 本地挂载，另有 302 直链、缓存保持、命名对齐、HTTP/磁力离线下载、115/夸克分享链接转存，以及跨网盘平台的 PanSou 资源搜索与搜索结果一键转存等能力。

## ▎ 离线下载与分享转存

LitePan 的“离线下载”入口同时提供链接任务、BT 种子和分享转存。分享转存会直接调用对应网盘的转存接口，不经过本地下载和重新上传。

### 支持范围

| 网盘 | 普通离线下载 | 分享链接转存 | 认证要求 |
| --- | --- | --- | --- |
| 115 网盘 Open | HTTP / HTTPS / FTP / Magnet / ED2K / BT | `115.com`、`anxia.com`、`115cdn.com` | Open OAuth；分享转存还需网页版 Cookie |
| 夸克网盘 | 使用 LitePan 内置下载器处理 HTTP / HTTPS / Magnet | `pan.quark.cn` | 账号 Cookie |
| 123 云盘 Open | HTTP / HTTPS | 暂未支持 | Open API 凭据 |
| 光鸭云盘 | HTTP / HTTPS / FTP / Thunder / Magnet | 暂未支持 | 账号凭据 |
| 其他支持上传的网盘 | 使用 LitePan 内置下载器处理 HTTP / HTTPS / Magnet | 暂未支持 | 对应账号凭据 |

分享链接只能转存到相同网盘类型的当前账号。例如，夸克分享链接需要先在文件浏览器中选择夸克账号。

### 使用流程

1. 在文件浏览器中选择目标网盘账号，进入准备保存文件的目录。
2. 点击工具栏中的“离线下载”，切换到“分享转存”。
3. 粘贴分享链接或完整分享文案；提取码可单独填写，也可从“提取码、访问码、密码、`pwd`、`password`、`passcode`”等文本自动识别。
4. 点击“解析分享链接”。LitePan 会读取分享根目录并默认勾选全部项目。
5. 调整需要转存的文件或文件夹，确认保存位置后点击“转存已选内容”。
6. 115 转存成功后立即刷新目标目录；夸克异步转存会先进入任务列表，并由离线任务轮询更新最终状态。

当前分享预览展示分享根目录中的文件和文件夹。选中一个文件夹时，由网盘服务端递归转存其中内容；暂不提供在分享目录内部逐层浏览的界面。

### 115 分享 Cookie

`115_Open` 的常规文件操作和原生离线下载使用 OAuth Token，但 `webapi.115.com` 分享接收接口使用网页版 Cookie。使用 115 分享转存前，请编辑对应的 115 Open 账号，在“网页版 Cookie（分享转存）”中填写当前账号的完整网页版 Cookie。

Cookie 属于敏感凭据，请仅保存在自己的 LitePan 实例中。Cookie 失效后需要重新登录 115 网页版并更新账号配置。

### 接口与状态

分享转存采用两阶段接口：

```text
POST /api/files/offline-download/share/prepare
POST /api/files/offline-download/share
```

`prepare` 只向前端返回短期 `preparation_id` 和可选择的文件列表。夸克 `stoken`、`share_fid_token` 与 115 分享参数保存在服务端内存中，绑定解析时使用的账号，并在 30 分钟后过期。转存任务记录只保存规范化分享 URL，不保存提取码或完整分享文案。

除手动粘贴外，前台 PanSou 搜索结果与后台「影视搜索转存」测试列表均提供「转存」按钮：选择目录后，分享链接会整份转存，磁力 / 电驴链接则交给支持离线下载的账号执行。

分享转存完成后会进入现有离线任务记录，并触发目录缓存失效、文件列表刷新和“离线下载完成”自动联动。空选择、过期 preparation、账号不匹配和重复提交都会返回校验错误。

---

## ▎ 资源搜索（PanSou）

基于 PanSou 系列 API 的网盘资源聚合搜索：一次输入片名，即可跨多个已配置网盘平台检索分享链接，找到后可在结果右侧一键转存到自己的网盘目录，也可复制到离线下载 / 分享转存中手动提交。

### 开启与配置

1. 进入后台「设置 → PanSou 资源搜索」，启用功能。
2. 配置 PanSou 服务地址（默认 `https://so.252035.xyz`）。若上游需要鉴权，可填写 Basic Auth 用户名 / 密码，或 API Token。
3. 按需配置搜索平台范围（115、夸克、百度、阿里云盘、迅雷、天翼、UC、PikPak、磁力、ED2K 等）。仅 PanSou 认可的平台代号会被保留，避免“有配置却搜不到”。
4. 可在后台「增强工具 → 影视搜索转存」中直接测试关键词与平台组合。

### 使用方式

- 启用后，前台首页显示资源搜索入口；文件浏览器中也可随时打开 PanSou 搜索面板（需要管理员身份登录）。
- 输入片名搜索，结果按网盘平台展示分享链接与提取码。
- **一键转存**：每条结果右侧都有「转存」按钮，点击后在弹窗中选择目标网盘账号与保存目录（可新建文件夹），确认后整份资源直接转存到自己的网盘：
  - 分享链接（夸克 / 115 等）走对应网盘的分享转存接口，不经过本地下载与重新上传；
  - 磁力、电驴等链接会自动切换到支持离线下载的账号执行；
  - 站点没有可接收该资源的账号时按钮会置灰并说明原因（跨平台分享链接需要先绑定对应平台的网盘账号）。
- 后台「增强工具 → 影视搜索转存」的连通性测试结果同样提供「转存」入口，可先行验证一键转存链路。
- 复制目标链接后，也可在对应网盘账号的「离线下载 → 分享转存」中手动提交；两种方式共用同一套转存任务与进度追踪。

> [!NOTE]
> PanSou 为第三方聚合服务，可用性与返回内容取决于其上游服务。密码 / Token 属于敏感凭据，请仅保存在自己的 LitePan 实例中。

---

## ▎ 快速开始

**Docker Compose 部署** · 镜像标签：`Beta`或指定`v0.6.0`

```yaml
services:
  litepan:
    image: ponphil/litepan:beta
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

      # 可选：将 FUSE 读缓存单独映射，建议放到更快的磁盘
      # - ./fuse_read_cache:/app/data/fuse_read_cache
    devices:
      - /dev/fuse:/dev/fuse
    pid: "host"
    privileged: true
    # 没有代理环境的，可以在下方配置tmdb的hosts
    # extra_hosts:
      # - "api.themoviedb.org:这里填写对应的ip"
      # - "image.tmdb.org:这里填写对应的ip"
    # 注意：也可以在程序内「目录整理 → TMDB 设置」填写反代主域名（自动补 /3 与 /t/p），与 hosts 二选一即可
```

打开 `http://你的IP:5211`，默认管理员密码均为admin。  
需要 FUSE 时请确保宿主机具备 `/dev/fuse` 权限。

> [!WARNING]
> **不要用 `ponphil/litepan:latest` 部署本仓库对应的 Go 版。**  
> `latest` 仍是 Python 旧版镜像。若你需要旧版程序与 Compose 脚本，请前往归档仓库：[LitePan-old](https://github.com/Ponphil/LitePan-old)。

### 从本仓库的 GHCR 拉取自建镜像

本仓库 push 到 `main` 后会自动构建并发布多架构（amd64/arm64）镜像到 GitHub Container Registry。直接拉取：

```bash
docker pull ghcr.io/q107580018/litepan:latest
```

将上方 Compose 中的 `image: ponphil/litepan:beta` 换成 `ghcr.io/q107580018/litepan:latest` 即可使用本仓库对应代码的镜像（新功能以本仓库为准，如 115 / 夸克分享转存、PanSou 资源搜索）。

手动触发构建：在仓库 **Actions → docker-publish → Run workflow**。

## ▎ 支持

<table>
  <tr>
    <td width="50%" valign="top">
      <h3>支持 LitePan</h3>
      <p>如果这个项目对你有帮助，欢迎点右上角 <strong>Star</strong>，也欢迎自愿赞赏。</p>
      <img src="docs/pictures/wechat-tip.png" alt="微信赞赏" width="260">
    </td>
    <td width="50%" valign="top">
      <h3>赞助致谢</h3>
      <p>感谢每一位支持 LitePan 的朋友。</p>
      <p>完整致谢名单见官方网站：</p>
      <p>
        <a href="https://www.litepan.top/sponsor.html">https://www.litepan.top/sponsor.html</a>
      </p>
    </td>
  </tr>
</table>

## ▎ 反馈

交流请到 <a href="https://space.bilibili.com/1501989416">B 站主页</a>。  
暂不接受公开 PR；有维护意愿请私信。
外部贡献致谢见 [ACKNOWLEDGEMENTS.md](./ACKNOWLEDGEMENTS.md)。

---

## ▎ 许可

[PolyForm Noncommercial 1.0.0](./LICENSE) — 个人学习与非商业使用，**禁止商用**。  
第三方依赖见 [THIRD_PARTY_NOTICES.md](./THIRD_PARTY_NOTICES.md)。请遵守各网盘服务条款与当地法规。

[docker-pulls-shield]: https://img.shields.io/docker/pulls/ponphil/litepan?logo=docker&logoColor=white&style=flat-square
[version-shield]: https://img.shields.io/badge/Version-v0.6.0-6C63FF?style=flat-square
[license-shield]: https://img.shields.io/badge/License-PolyForm%20NC-red?style=flat-square
[docker-url]: https://hub.docker.com/r/ponphil/litepan
[license-url]: ./LICENSE
