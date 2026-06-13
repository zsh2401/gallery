# Seym's

前后端分离的本地图片相册。后端扫描 `imageRoot`，实时读取目录结构、解析 EXIF、生成内存缩略图；Web 前端通过一层 POST API 和媒体 URL 浏览照片。浏览大图时加载原图，合集和图片列表使用后端生成的缩略图。

## Quick Start

```bash
make sample-gallery
make setup
make dev
```

打开 `http://127.0.0.1:5173`。默认后端监听 `127.0.0.1:8080`，配置来自 `config.example.yaml`。

## Development

```bash
# 同时启动前后端；退出时会结束两个子 dev 进程
make dev

# 只启动后端，Air 会监听 Go/YAML 变化并自动重启；pretty 日志带 ANSI 彩色
make -C backend dev

# 只启动前端，Vite 会 HMR
make -C frontend dev

# 构建和类型/后端测试
make build
make test
```

后端启动参数：

```bash
cd backend
go run . --config ../config.example.yaml
```

前端可通过环境变量指定后端：

```bash
cd frontend
VITE_API_BASE=http://127.0.0.1:8080 npm run dev
```

## Config

`--config` 指向 YAML 文件，最小配置：

```yaml
imageRoot: /path/to/photos
```

常用配置见 `config.example.yaml`。第一版只实现内存缓存；如果配置其他 cache backend，后端会记录 warning 并回退到 memory。

日志配置：

```yaml
logging:
  level: debug
  format: pretty # pretty/text 彩色输出；json 输出结构化 JSON
```

## API Rules

业务 API 全部是一层路径并且只接受 POST：

- `POST /api/list-albums`
- `POST /api/list-images`
- `POST /api/get-image-detail`
- `POST /api/get-status`
- `POST /api/record-view`
- `POST /api/react-item`

响应统一为：

```json
{ "ok": true, "data": {} }
```

图片资源供浏览器和小程序组件加载，使用 GET：

- `GET /media/thumb/{imageId}`
- `GET /media/original/{imageId}`
- `GET /media/raw/{imageId}`

## Image Behavior

- `imageRoot` 下每个目录都是合集。
- 每个合集只包含该目录直接文件，子目录会成为独立合集；前端按文件浏览器方式保留层级。
- 合集时间字段为 `firstTakenAt`，取该合集整棵子树里最早一张照片的拍摄时间。
- 同级合集排序按 `firstTakenAt` 倒序，较新的合集在前；时间相同按名称排序。
- 同目录同 basename 文件归为同一张照片。
- 展示优先级：JPEG/JPG、PNG、WebP；RAW-only 会尝试抽取内嵌 JPEG 预览。
- 不可预览文件不会出现在用户 UI，只写日志。
- RAW 文件如果与可展示照片同名，会在详情页提供下载。
- 合集封面从整个子树平均采样最多 9 张缩略图，类似微信朋友圈九宫格。
- 如果合集目录里有 `README.md` 或 `readme.md`，首页时间线和合集顶部会渲染该说明。
- 详情页顶部提供原图下载；如果有 RAW，也提供 RAW 下载。
- API/前端不会暴露本地磁盘路径，图片关联文件只显示文件名。
- EXIF 字符串会去掉外层引号；摘要显示相机、镜头、曝光、`F2.8` 风格光圈、ISO、焦段、评分和可解析的 Sony 快门数。
- 合集和图片都有浏览量、点赞数和点踩数。当前实现是进程内内存统计，适合单机调试；需要持久化或多实例时可替换为 Redis。

## Cache Strategy

- 后端：缩略图生成后进入内存 LRU，key 包含图片 ID、目标尺寸、文件大小和修改时间。
- HTTP：缩略图响应带 `ETag`、`Cache-Control: private, max-age=3600, stale-while-revalidate=86400`，浏览器可用 `If-None-Match` 命中 `304`。
- 前端：不再自己保存图片二进制，依赖浏览器 HTTP 缓存和图片解码缓存；应用状态只保存 API 返回的元数据和统计数。

## Frontend Behavior

- 桌面端：左侧默认展开菜单栏，右侧显示当前合集的合集/图片网格；点击图片后进入图片浏览器，中央显示原图，右侧 EXIF 独立滚动，底部显示类似 Lightroom 的图片横条。
- 移动端：菜单默认收起，顶部按钮展开；首页直接是朋友圈式时间线流，进入合集后显示缩略图网格，详情页使用普通页面滚动。
- 主题支持 `auto`、`light`、`dark`，`auto` 跟随系统暗黑模式；主题和中英文语言选择会写入 `localStorage`。
- 样式是白灰色复古拟物风格，包含暗黑模式和拟物滚动条。
- 顶部和详情页展示版权声明：除非允许，否则禁止商用；并展示“某网络平台用户盗用图片并出版，最终赔偿 4500 元”的案例。
