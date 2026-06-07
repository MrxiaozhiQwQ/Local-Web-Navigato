# Local Web Navigator

一个用于扫描目标主机 Web 服务并生成导航页的本地网站。

它会自动扫描目标主机上的常见 Web 端口和其他端口，判断该端口是否提供网页服务；如果是，就提取网页标题和图标，展示在首页导航卡片中，并把结果记录下来。下次进入时会优先检查历史发现过的网页，失效的网页会自动从列表中移除。

## 功能特性

- 自动扫描目标主机上的 Web 服务
- 优先检查历史发现的网页，再扫描常见端口，最后补扫其他端口
- 自动提取网页标题和 favicon
- 首页只显示已发现的网页卡片
- 右上角设置按钮可修改扫描目标并重新扫描
- 底部悬浮进度条显示扫描状态，扫描完成后自动隐藏
- 历史记录持久化保存
- 已关闭或失效的网页会自动从首页移除
- 支持 Linux Docker 部署

## 技术栈

- 后端：Go
- 前端：原生 HTML / CSS / JavaScript
- 通信：SSE
- 存储：本地 JSON 文件

## 项目结构

```text
.
├─ main.go                # Go 后端，负责扫描、接口、静态资源服务
├─ public/
│  ├─ index.html          # 首页
│  ├─ app.js              # 前端交互逻辑
│  └─ styles.css          # 页面样式
├─ data/                  # 运行后生成，保存扫描历史和目标设置
├─ Dockerfile             # Docker 镜像构建文件
├─ docker-compose.yml     # Linux Docker 部署示例
└─ README.md
```

## 本地运行

### 1. 准备环境

- Go 1.25 或更高版本

### 2. 编译

Windows PowerShell:

```powershell
$env:GOCACHE="$PWD\.gocache"
& "C:\Program Files\Go\bin\go.exe" build -o local-web-nav.exe .
```

Linux / macOS:

```bash
go build -o local-web-nav .
```

### 3. 启动

Windows:

```powershell
.\local-web-nav.exe
```

Linux / macOS:

```bash
./local-web-nav
```

默认访问地址：

```text
http://127.0.0.1:3210
```

## 环境变量

支持以下环境变量：

- `PORT`
  站点监听端口，默认 `3210`
- `DATA_DIR`
  数据目录，默认 `./data`

示例：

```bash
PORT=8080 DATA_DIR=./data ./local-web-nav
```

## 数据文件

运行后会在 `DATA_DIR` 下生成：

- `sites.json`
  已发现网页的历史记录
- `settings.json`
  当前扫描目标设置

## Docker 部署

### 方式一：直接使用 docker compose

适合 Linux 宿主机。

```bash
docker compose up -d --build
```

当前 `docker-compose.yml` 使用的是：

- `network_mode: host`
- 数据卷挂载到 `./data`
- 自动重启

启动后访问：

```text
http://服务器IP:3210
```

### 方式二：使用 docker run

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

## 为什么 Linux Docker 推荐 host 网络

这个项目需要扫描目标主机上的端口和网页服务。

如果容器使用默认的 `bridge` 网络：

- `127.0.0.1`
- `localhost`

通常只指向容器自己，而不是宿主机。

在 Linux Docker 中使用 `host` 网络模式时，容器能更直接地看到宿主机网络，因此更适合这个项目的扫描逻辑。

## 使用说明

1. 打开首页
2. 首页会自动开始扫描
3. 主页只显示扫描到的网页卡片
4. 点击右上角设置按钮
5. 在设置中修改扫描目标，例如：
   - `192.168.1.10`
   - `localhost`
   - `127.0.0.1`
6. 点击“应用并扫描”

## 扫描逻辑说明

扫描顺序：

1. 优先扫描历史发现过的端口
2. 扫描常见 Web 端口
3. 扫描其余端口

网页识别方式：

- 响应头包含 `text/html` 或 `application/xhtml+xml`
- 或返回内容中包含 `<html`、`<title`、`<!doctype html`

页面名称来源：

1. `<title>`
2. `Server` 响应头
3. `IP:端口`

## 注意事项

- 请只扫描你自己有权限管理的主机和网络
- 全端口扫描会占用一定时间和网络资源
- 如果目标主机端口很多，首次扫描时间会比历史扫描更长
- 在 Docker 中扫描 `localhost` 时，推荐 Linux 宿主机使用 `host` 网络模式

