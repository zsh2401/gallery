<p align="center">
  <img src="https://img.shields.io/badge/go-1.25-00ADD8?style=flat-square&logo=go" alt="Go 1.25" />
  <img src="https://img.shields.io/badge/react-19-61DAFB?style=flat-square&logo=react" alt="React 19" />
  <img src="https://img.shields.io/badge/license-MIT-green?style=flat-square" alt="MIT" />
  <img src="https://img.shields.io/badge/postgres-optional-blue?style=flat-square&logo=postgresql" alt="PostgreSQL optional" />
</p>

<h1 align="center">📷 Seym's Gallery</h1>

<p align="center">
  <em>自托管的本地相册。指向一个文件夹，即刻浏览。</em>
</p>

<p align="center">
  <a href="./README.md">🇬🇧 English</a>
</p>

---

## ✨ 功能

- **🖥️ 零配置** — 只需 `imageRoot` 指向照片目录即可运行
- **📂 文件即结构** — 目录自动成为相册，保留嵌套层级
- **🖼️ 智能预览** — 自动生成缩略图，RAW 文件提取内嵌 JPEG
- **📸 EXIF 解析** — 相机、镜头、光圈、ISO、焦段、快门数、评分
- **🎨 复古拟物风** — 暖灰调、触感卡片、暗色/亮色模式、响应式布局
- **📱 朋友圈时间线** — 以社交动态流形式浏览相册，支持行内 Markdown
- **🔒 相册密码** — 目录下放 `ALBUM.yaml` 即可加密，无需登录系统
- **👍 匿名统计** — 设备指纹记录浏览 / 点赞，无需 cookie 或账号
- **🌍 中英双语** — 自动检测浏览器语言
- **⚡ 高性能** — LRU 缩略图缓存、ETag HTTP 缓存、懒加载

## 🚀 快速开始

```bash
# 生成示例图库
make sample-gallery

# 安装依赖
make setup

# 启动前后端
make dev
```

打开 **http://127.0.0.1:5173**，后端监听 `127.0.0.1:8080`。

### 手动启动

```bash
# 终端 1 — 后端
cd backend && go run . --config ../config.example.yaml

# 终端 2 — 前端
cd frontend && npm run dev
```

自定义后端地址：

```bash
cd frontend && VITE_API_BASE=http://127.0.0.1:8080 npm run dev
```

## 📁 项目结构

```
├── backend/            # Go 后端 — API、缩略图、EXIF、统计
│   ├── main.go         # 单文件二进制，<2500 行
│   ├── main_test.go
│   └── .air.toml       # 开发热重载
├── frontend/           # React SPA — Vite + TypeScript
│   └── src/
│       ├── App.tsx     # 主组件 & 全部 UI
│       ├── api.ts      # POST API 客户端，注入 deviceId
│       ├── deviceId.ts # FingerprintJS 匿名设备标识
│       ├── consent.ts  # 欧盟 Cookie / 版权同意
│       ├── password.ts # 相册密码令牌
│       └── reactions.ts# 客户端点赞/点踩状态
├── tools/              # 工具
│   └── make-sample-gallery.go
├── config.example.yaml # 参考配置
└── Makefile            # 顶层 dev 命令
```

## ⚙️ 配置

从示例创建配置：

```bash
cp config.example.yaml config.yaml
```

最小配置：

```yaml
imageRoot: /path/to/your/photos
```

### 统计后端

| 后端 | 场景 |
|------|------|
| `memory`（默认） | 单实例、开发调试 |
| `postgres` | 持久化、多实例部署 |

```yaml
stats:
  backend: postgres
  postgres:
    dsn: "postgres://user:pass@localhost:5432/gallery?sslmode=disable"
```

启动时自动建表，无需迁移。

### 相册密码

在任意相册目录下放置 `ALBUM.yaml`：

```yaml
password:
  value: "mysecret"
  hint: "我们的婚礼日期"   # 可选
readme: |
  ## 夏日之旅 2024
  
  海边的美好回忆。
```

- YAML 中的 `readme` 优先于 `README.md`
- 密码在服务端验证，令牌存在 `sessionStorage`

## 📡 API

所有业务接口为 `POST`，JSON body：

| 端点 | 用途 |
|------|------|
| `/api/list-albums` | 相册树及统计 |
| `/api/list-images` | 相册内照片列表 |
| `/api/get-image-detail` | 完整 EXIF + 元数据 |
| `/api/get-status` | 服务状态 |
| `/api/record-view` | 记录浏览 |
| `/api/react-item` | 点赞 / 点踩 |
| `/api/verify-album-password` | 解锁相册 |

媒体端点（GET）：

| 端点 | 响应 |
|------|------|
| `/media/thumb/{id}` | JPEG 缩略图 |
| `/media/original/{id}` | 原图 |
| `/media/raw/{id}` | RAW 文件下载 |

响应格式：

```json
{ "ok": true, "data": {} }
```

## 🔒 隐私

- **无账号** — [FingerprintJS](https://github.com/fingerprintjs/fingerprintjs) 匿名设备标识，仅用于去重
- **无 Cookie** — 偏好与同意记录在 `localStorage`
- **无追踪** — 统计数据仅为计数，不与身份关联
- **欧盟合规** — 首次访问显示 cookie 同意横幅

## 🛠️ 技术栈

| 层 | 技术 |
|----|------|
| 后端 | Go, `net/http`, `pgx/v5` |
| 前端 | React 19, TypeScript, Vite |
| 样式 | CSS 自定义属性，暗/亮主题 |
| 图片 | `golang.org/x/image`, EXIF via `goexif` |
| 配置 | YAML |
| 统计 | 内存 LRU 或 PostgreSQL |

## 📄 许可

MIT © 2026 [zsh2401](https://github.com/zsh2401)

---

<p align="center">
  <sub>为想拥有自己作品的摄影师而建。</sub>
</p>
