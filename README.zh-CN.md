# Local Web Navigator

[English README](./README.md)

Local Web Navigator 是一个用于扫描目标主机 Web 服务并生成导航页的轻量级应用。

它会自动扫描目标主机上的常见 Web 端口和其他端口，判断该端口是否提供网页服务；如果是，就提取网页标题和图标，展示在首页导航卡片中，并把结果记录下来。下次进入时会优先检查历史发现过的网页，已经失效的网页会自动从列表中移除。

## 功能特性

- 扫描目标主机上的 Web 服务
- 优先检查历史发现过的端口，再扫描其他端口
- 自动提取网页标题和 favicon
- 首页只显示扫描到的网页卡片
- 可在设置面板中修改扫描目标
- 页面底部显示小型悬浮扫描进度条
- 扫描完成后自动隐藏进度条
- 本地持久化保存历史站点和设置
- 自动移除已经离线的网页
- 支持 Linux Docker 部署
- 支持 GitHub Releases 自动构建

## 技术栈

- 后端：Go
- 前端：HTML、CSS、JavaScript
- 实时更新：SSE
- 存储：本地 JSON 文件

## 项目结构

```text
.
├─ main.go
├─ public/
│  ├─ index.html
│  ├─ app.js
│  └─ styles.css
├─ data/
├─ Dockerfile
├─ docker-compose.yml
├─ .github/
│  └─ workflows/
│     └─ release.yml
├─ README.md
├─ README.zh-CN.md
└─ LICENSE
```

## 本地运行

### 环境要求

- Go 1.25 或更高版本

### 编译

Windows PowerShell：

```powershell
$env:GOCACHE="$PWD\.gocache"
& "C:\Program Files\Go\bin\go.exe" build -o local-web-nav.exe .
```

Linux / macOS：

```bash
go build -o local-web-nav .
```

### 启动

Windows：

```powershell
.\local-web-nav.exe
```

Linux / macOS：

```bash
./local-web-nav
```

默认访问地址：

```text
http://127.0.0.1:3210
```

## 环境变量

- `PORT`
  服务监听端口，默认值：`3210`
- `DATA_DIR`
  数据目录，默认值：`./data`

示例：

```bash
PORT=8080 DATA_DIR=./data ./local-web-nav
```

## 数据文件

程序会在 `DATA_DIR` 下生成以下文件：

- `sites.json`
  已发现网页的历史记录
- `settings.json`
  当前扫描目标设置

## Docker 部署

### Linux Docker + compose

推荐在 Linux 宿主机上使用：

```bash
docker compose up -d --build
```

启动后访问：

```text
http://服务器IP:3210
```

### Linux Docker + docker run

```bash
docker build -t local-web-nav .

docker run -d \
  --name local-web-nav \
  --restart unless-stopped \
  --network host \
  -e PORT=3210 \
  -e DATA_DIR=/data \
  -v $(pwd)/data:/data \
  local-web-nav
```

## 为什么 Linux Docker 推荐使用 `host` 网络

这个项目需要扫描目标主机上的端口和网页服务。

如果容器使用默认的 `bridge` 网络模式，`127.0.0.1` 和 `localhost` 通常会指向容器本身，而不是宿主机。  
在 Linux Docker 中使用 `host` 网络模式时，容器看到的网络环境更接近宿主机本身，因此更适合这个项目的扫描逻辑。

## GitHub Releases 自动构建

仓库中已经包含 GitHub Actions 工作流：

`/.github/workflows/release.yml`

它会自动：

- 在推送版本标签时构建发布版本
- 自动创建 GitHub Release
- 自动上传多平台编译产物

### 当前包含的平台

- Linux amd64
- Linux arm64
- Windows amd64
- macOS amd64
- macOS arm64


## 使用说明

1. 打开首页
2. 程序会自动开始扫描
3. 首页只显示扫描到的网页卡片
4. 点击右上角设置按钮
5. 修改扫描目标，例如：
   - `192.168.1.10`
   - `localhost`
   - `127.0.0.1`
6. 点击“应用并扫描”

## 扫描逻辑

扫描顺序：

1. 历史发现过的端口
2. 常见 Web 端口
3. 其他剩余端口

网页识别条件：

- 响应头包含 `text/html` 或 `application/xhtml+xml`
- 或响应内容中包含 `<html`、`<title`、`<!doctype html`

网页名称来源：

1. `<title>`
2. `Server` 响应头
3. `IP:port`

## 注意事项

- 请只扫描你有权限管理的主机和网络
- 全端口扫描可能需要一定时间
- 首次扫描通常会比后续扫描更慢，因为历史记录还为空
- 在 Docker 中扫描 `localhost` 时，Linux 上推荐使用 `host` 网络模式


## License

MIT
