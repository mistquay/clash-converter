# Clash Subscription Converter

仅支持 Clash 的订阅转换器。订阅请求、Header 透传、规则集下载缓存由 Go 实现，具体订阅转换逻辑由 JS 脚本提供。具体转换逻辑的开发（JS
脚本）不是本仓库的核心目标，故仅提供示例。

## 功能特性

- **配置灵活**：转换逻辑由 JS 脚本定义
- **多订阅合并**：支持合并多个订阅源的节点；单个来源失败时跳过，其他来源继续更新
- **服务端配置**：将订阅源和转换参数写入 YAML，客户端只需固定短链接
- **VLESS 节点**：支持将 `vless://` 分享链接与机场订阅混合使用
- **SS 与本地节点文件**：支持 `ss://` 分享链接及每行一条链接的本地文件，文件内容随订阅更新即时读取
- **流量统计**：自动解析和合并订阅流量信息
- **规则缓存**：规则集和模板文件自动缓存，减少网络请求
- **Web UI**：提供友好的前端界面，快速生成订阅链接

## 快速开始

### 环境要求

- Go 1.24+

### 编译运行

```bash
# 克隆仓库
git clone https://github.com/yourusername/clash-converter.git
cd clash-converter

# 编译
go build

# 运行
./clash-converter
```

服务将在 `http://localhost:8080` 启动。

### Docker 部署

#### 推荐：服务端配置文件 + 短订阅链接

将包含本次功能的项目代码放到服务器，在项目目录执行以下命令。这里通过本地源码构建镜像，**现有的远端 `latest` 镜像需要发布新版本后才具备这些功能**。

```bash
mkdir -p config data
cp config.example.yaml config/config.yaml
```

Windows PowerShell 对应命令：

```powershell
New-Item -ItemType Directory -Force config, data
Copy-Item config.example.yaml config/config.yaml
```

编辑 `config/config.yaml`，填写自己的访问令牌、机场订阅和节点链接：

```yaml
server:
  listen: ":8080"
  access_token: "换成你自己的随机字母数字令牌"
  db_path: "./data/database.db"
  cache_expire_sec: 86400

subscription:
  filename: "我的订阅"
  legacy_relay: false
  script: ""
  template: ""
  sources:
    - name: "机场A"
      url: "https://example.com/subscribe?token=你的机场令牌"
    - name: "自建节点"
      url: "vless://完整的原始分享链接"
    - name: "腾讯云 SS"
      file: "./ss.txt"
```

- 机场、SS、VLESS 和本地文件条目都可以添加多个，也可以只使用其中一种；删除不用的示例条目。
- `name` 可省略；填写时作为节点名称前缀，例如 `[自建节点] HK`。不填写时 SS/VLESS 使用链接 `#` 后的名称，没有名称则使用协议、服务器地址和端口。
- `script`、`template` 留空时直接使用程序内置示例，不需要再填写本服务的 `/example/...` URL。
- 每个来源必须在 `url` 和 `file` 中二选一。`url` 支持**返回 Clash YAML 的 HTTP(S) 订阅**、完整 `ss://` 或 `vless://` 链接；`file` 引用服务端本地节点文件。通用 Base64 订阅目前不支持。
- 原样粘贴节点链接，保留 `&`、`%2F` 等原始字符，不要使用 Markdown 的 `vless\://` 或 HTML 的 `&amp;` 转义形式。
- SS/VLESS 节点及本地节点文件参与正常的策略分组，不显示机场流量信息；只有这些节点时不创建订阅信息组。
- 更新时某个来源请求失败、返回非 200 状态、内容解析失败或本地节点文件不可读时，记录来源序号和错误并跳过，使用其余成功来源生成配置；流量统计也只包含成功来源。所有来源都失败时返回 HTTP 500，避免输出空配置。此行为同样适用于完整 URL 模式，失败来源会在下次更新时重新尝试。配置在启动时仍需通过校验。

**通过 Compose 启动：** 仓库已经提供 [compose.yaml](compose.yaml)。

```bash
docker compose up -d --build
docker compose logs --tail=100 clash-converter
curl http://127.0.0.1:8080/ping
```

`/ping` 返回 `pong` 表示 HTTP 服务正常。在 Clash Verge / Mihomo 客户端中添加远程订阅：

```text
http://服务器IP:8080/sub?token=你在access_token中填写的令牌
```

有反向代理和域名时，使用 `https://你的域名/sub?token=...`。如果令牌包含特殊字符，需要 URL 编码；使用随机字母数字字符串可以省去编码步骤。第一次更新订阅仍需要服务端访问机场和内置脚本引用的远程规则集。

**继续使用 `docker run`：** 在同一项目目录执行：

```bash
docker build -t clash-converter:local .
docker run -d \
  --name clash-converter \
  --restart unless-stopped \
  -p 8080:8080 \
  -v "$(pwd)/config:/app/config:ro" \
  -v "$(pwd)/data:/app/data" \
  clash-converter:local -config /app/config/config.yaml
```

如果已有容器占用 `8080` 或同名容器，先构建好新镜像，再停止并移除旧容器，使用上面的命令重建；挂载的 `data` 应沿用原来的宿主机数据目录。不要把旧 `ACCESS_TOKEN` 环境变量原样带入，除非你希望它覆盖文件中的令牌。

**修改配置与更新代码：**

```bash
# 修改 config/config.yaml 或本地脚本/模板后
docker compose restart clash-converter
# docker run 用户使用：docker restart clash-converter

# 更新项目代码后重新构建并替换 Compose 容器
docker compose up -d --build
```

修改 `config.yaml` 中的订阅源后，客户端短链接不变，重启容器后在客户端更新订阅即可。仅修改 `sources.file` 引用的节点文件内容时，无需重启，直接更新客户端订阅。`docker run` 用户更新程序时需要重新 `docker build` 并重建容器，仅 `docker restart` 不会切换到新镜像。

#### 本地 SS / VLESS 节点文件

在宿主机创建 `config/ss.txt`，每行粘贴一个原始分享链接。允许空行和以 `#` 开头的整行注释，链接自身的 `#节点名称` 会正常保留。例如：

```text
# 广州
ss://YWVzLTEyOC1nY206ZXhhbXBsZS1wYXNzd29yZA@node.example.com:8388#Guangzhou

# 也可以混合 VLESS
vless://00000000-0000-4000-8000-000000000001@node.example.com:443?security=tls&sni=node.example.com#MyVLESS
```

上面都是占位节点，需要替换为自己的完整链接。不要添加 Markdown 反斜杠、列表符号、引号或行末注释，不要将整个文件再编码为 Base64。文件使用 UTF-8；空文件、没有节点或包含无效链接时会明确报错，不会跳过坏行或继续使用旧节点。

在现有 `subscription.sources` 列表中追加一个条目：

```yaml
    - name: "腾讯云 SS"
      file: "./ss.txt"
```

路径相对于 **`config.yaml` 所在目录**解析。`/app/config/config.yaml` 中的 `./ss.txt` 对应 `/app/config/ss.txt`，也支持容器内绝对路径。已有 `./config:/app/config:ro` 目录挂载即可读取，宿主机仍可编辑该文件。

配置来源在启动时检查文件内容，之后每次 `/sub` 请求都会重新读取文件，不使用远程资源缓存。首次添加或修改 `file` 配置需要重启；以后增删、替换文件中的节点，保存后在客户端更新订阅即可，无需修改脚本或重启服务。来源 `name` 会作为文件中每个节点的名称前缀。

本地文件只能通过服务端配置的 `file` 字段引用；URL 参数 `sub` 不能读取本地文件。文件仅接受 SS/VLESS 分享链接，不接受嵌套文件、HTTP 订阅地址或 Clash YAML。

SS 支持 SIP002 的 Base64 userinfo、明文 URL 编码 userinfo，以及旧版整体 Base64 分享链接，兼容标准与 URL-safe Base64、有无填充。当前不支持 SS `plugin` 等查询参数，填写后明确报错；加密算法是否可用取决于客户端内核。本地文件示例见 [example/nodes.txt](example/nodes.txt)。

#### 自定义本地脚本和模板

在宿主机 `config` 目录放入 `script.js`、`template.yaml`，然后修改：

```yaml
subscription:
  script: "./script.js"
  template: "./template.yaml"
  # filename、legacy_relay、sources 等字段保留在同一个 subscription 下
```

脚本和模板的相对路径基于 `config.yaml` 所在目录；也可以填写容器内的绝对路径或 HTTP(S) URL。配置文件、本地脚本和本地模板在启动时加载，修改后重启；远程脚本和模板在每次订阅请求时获取。只有服务端文件配置可以引用本地文件，URL 参数不能读取服务端文件。

`server.db_path` 的相对路径基于程序工作目录，Docker 中为 `/app`。`server.listen` 修改后需同步调整 Docker 容器端口映射。`ACCESS_TOKEN`、`DB_PATH`、`CACHE_EXPIRE_SEC` 环境变量优先于配置文件，包括显式设置为空的环境变量；配置了订阅源时访问令牌不允许为空。

启动参数 `-config /path/config.yaml` 指定配置文件；未传时自动读取工作目录中的 `config.yaml`，文件不存在则保留原先仅使用环境变量和完整 URL 的用法。显式指定的文件不存在、YAML 字段拼错或节点参数不支持时会直接启动失败，并给出字段或来源序号，不会悄悄忽略。

真实 `config/` 目录及根目录的 `config.yaml`、`config.yml` 已加入 Git 和 Docker 构建忽略规则，配置通过只读挂载提供给容器。

#### VLESS 支持范围

- 传输：TCP（`tcp` / `raw`，也支持省略 `type`）、WebSocket（`ws`）、HTTPUpgrade（`httpupgrade`）、gRPC（`grpc`，`mode` 省略或为 `gun`）。
- 安全：`none`、TLS、Reality；支持 `sni` / `serverName`、`fp`、`alpn`、`allowInsecure` / `insecure`。默认不跳过证书校验。
- Reality：`pbk`、`sid`；需要 `sni` 或 `serverName`，未填写 `fp` 时使用 `chrome`。TCP + TLS/Reality 支持 `flow=xtls-rprx-vision`。
- WebSocket / HTTPUpgrade：`host`、`path`、`ed`、`eh`；gRPC：`serviceName`；UDP 封装：`packetEncoding=xudp/packetaddr/packet`。
- 目前只支持省略 `encryption` 或 `encryption=none`。XHTTP、KCP、TCP HTTP 伪装、gRPC multi、`spx`、未知参数及不适用当前传输方式的参数会明确报错，不会丢弃后继续生成。填写后需要使用支持相应协议的 Mihomo 客户端。

#### 原完整 URL 模式的部署方式

**使用预构建镜像：**

```bash
# 拉取镜像
docker pull ghcr.io/etnatker/clash-converter:latest

# 运行容器
docker run -d \
  --name clash-converter \
  -p 8080:8080 \
  -e ACCESS_TOKEN=your-secret-token \
  -v $(pwd)/data:/app/data \
  ghcr.io/etnatker/clash-converter:latest
```

**使用 Dockerfile 构建：**

```bash
# 构建镜像
docker build -t clash-converter .

# 运行容器
docker run -d \
  --name clash-converter \
  -p 8080:8080 \
  -e ACCESS_TOKEN=your-secret-token \
  -v $(pwd)/data:/app/data \
  clash-converter
```

**使用 docker-compose：**

```yaml
version: '3'
services:
  clash-converter:
    build: .
    # 或使用预构建
    # image: ghcr.io/etnatker/clash-converter:latest
    ports:
      - "8080:8080"
    environment:
      - ACCESS_TOKEN=your-secret-token
      - CACHE_EXPIRE_SEC=86400
    volumes:
      - ./data:/app/data
    restart: unless-stopped
```

启动：`docker-compose up -d`

### 环境变量

```bash
# 缓存过期时间（秒），默认 86400（24小时）
export CACHE_EXPIRE_SEC=86400

# 数据库路径，默认 ./data/database.db
export DB_PATH=./data/database.db

# 访问令牌（强烈建议设置）
export ACCESS_TOKEN=your-secret-token
```

## API 文档

### GET /sub

订阅转换接口，支持多订阅合并。

支持两种互相独立的模式：

- **服务端配置模式**：`/sub?token=...`，不携带 `sub`，使用配置文件中的所有转换参数。不允许通过 `script`、`template`、`subName`、`filename`、`legacyRelay` 覆盖服务端配置。
- **完整 URL 模式**：携带 `sub` 时，使用下面的 URL 参数，不与服务端订阅源或转换参数合并；鉴权仍使用服务端令牌。此模式的脚本和模板必须为 HTTP(S) URL。

**请求参数：**

| 参数       | 类型       | 必填 | 说明                                                                   |
|----------|----------|----|----------------------------------------------------------------------|
| sub      | string[] | 完整 URL 模式必填 | Clash 订阅链接或 VLESS 分享链接，可混合传入多个；整个值需 URL 编码 |
| subName  | string[] | 否  | 与 `sub` 按顺序对应的自定义名称；填写后会作为节点名称前缀                         |
| script   | string   | 完整 URL 模式必填 | JS 脚本 HTTP(S) URL |
| template | string   | 完整 URL 模式必填 | 模板 YAML HTTP(S) URL |
| token    | string   | 是  | 访问令牌                                                                 |
| filename | string   | 否  | 返回配置的文件名，系统会自动补充 `.yaml` 后缀                                  |
| legacyRelay | bool | 否 | 是否启用 legacy relay 兼容参数，支持 `true/false/1/0`，默认 `false`；此参数直接传递给 JS 脚本 |


**响应：**

- 成功：返回转换后的 Clash 配置（YAML 格式）
- 失败：返回错误信息

**响应头：**

- `Content-Disposition`: 合并后的订阅文件名（用`|`分隔的各订阅名）
- `Subscription-Userinfo`: 合并后的流量统计信息

### GET /ui

Web 界面，用于可视化生成订阅链接。

访问 `http://localhost:8080/ui` 即可使用。

**URL 参数预填：**

所有配置项都可以通过 URL 参数传入并预填写（用于保存至书签或分享）。
例如：`baseUrl`、`sub`、`subName`、`script`、`template`、`token`、`filename`、`legacyRelay`。

### GET /ping

健康检查接口。

**响应：** `pong`

## 前端 UI 使用

`/ui` 保留原来的完整 URL 生成方式，不读取或展示服务端私有配置。使用服务端配置时，直接在客户端填写 `/sub?token=...` 即可，无需再填写网页表单。

1. 访问 `http://localhost:8080/ui`
2. 填写以下配置：
    - **Base URL**：订阅服务的基础地址（默认为当前页面域名）
    - **订阅列表**：添加一个或多个订阅链接和自定义名称，支持排序；自定义名称会成为节点前缀
    - **Script URL**：JS 脚本地址
    - **Template URL**：模板文件地址
    - **Access Token**：访问令牌
    - **输出文件名**：可选，指定 Clash 客户端收到的 YAML 文件名
    - **Legacy Relay**：是否附带 `legacyRelay=1` 参数
3. 页面会生成两个链接：
    - **订阅链接**：用于 Clash 客户端订阅
    - **收藏链接**：包含当前配置的页面链接，可保存到书签

服务会内置并默认填写以下地址，无需额外托管示例文件：

- `http://localhost:8080/example/script.js`
- `http://localhost:8080/example/template.yaml`

部署后将 `http://localhost:8080` 替换为实际服务地址即可，也可以改填 GitHub Raw 等可由服务端访问的 URL。

## 订阅用量信息

转换后的配置会在最前面自动添加一个 **"📊 订阅信息"** 节点组，用于显示各订阅的用量信息。

**节点命名格式：**

```
订阅名称 | 已用 12.5 GB / 100.0 GB | 剩余 87.5 GB | 到期 2026-09-01
```

自定义名称优先于订阅响应中的文件名；两者都没有时使用 `订阅01`、`订阅02` 等缺省名称。未返回
`Subscription-Userinfo` 时会显示“流量信息不可用”。

## Template 模板文件

Template 是一个 YAML 格式的 Clash 配置文件，定义基础配置和策略组结构。程序会自动填充以下内容：

- **proxies**：订阅的所有节点（包括订阅信息假节点）
- **rules**：根据 JS 脚本定义的规则集生成的规则列表

模板中可以定义：

- 基础配置（port、mode、log-level 等）
- 策略组（proxy-groups）
- DNS 配置
- 其他 Clash 支持的配置项

**说明**：

- `proxies` 和 `rules` 字段会被覆盖
- JS 脚本的 `buildConfig(config, legacyRelay)` 可以进一步修改模板生成的配置

## JS 脚本

JS 脚本负责定义订阅转换的具体逻辑，包括规则集的下载和配置的最终调整。脚本在 Go
运行时中通过 [goja](https://github.com/dop251/goja) 引擎执行。

### 执行流程

1. Go 从订阅 URL 获取节点列表
2. 执行 JS 脚本，调用 `rulesets()` 函数获取规则集列表
3. Go 并发下载所有规则集（支持缓存）
4. Go 根据模板、节点和规则集构建基础配置
5. 执行 JS 的 `buildConfig(config, legacyRelay)` 函数（如果存在）进行最终调整
6. 返回最终配置

### 需要实现的函数

#### rulesets(callback)

用于定义需要下载的规则集。

- **参数**：`callback(tag, url, behavior)` - 规则集回调函数
    - `tag` (string): 规则标签，将作为规则的目标策略组
    - `url` (string): 规则集文件的 URL（支持缓存）
    - `behavior` (string，可选): `classical`、`domain` 或 `ipcidr`，默认 `classical`
- **返回值**：无

**示例：**

```javascript
function rulesets(callback) {
    // 定义直连规则
    callback('DIRECT', 'https://raw.githubusercontent.com/ACL4SSR/ACL4SSR/master/Clash/LocalAreaNetwork.list');
    callback('DIRECT', 'https://raw.githubusercontent.com/ACL4SSR/ACL4SSR/master/Clash/ChinaDomain.list');

    // 定义代理规则
    callback('PROXY', 'https://raw.githubusercontent.com/ACL4SSR/ACL4SSR/master/Clash/ProxyLite.list');
    callback('PROXY', 'https://raw.githubusercontent.com/ACL4SSR/ACL4SSR/master/Clash/Telegram.list');

    // 定义特定应用规则
    callback('Netflix', 'https://raw.githubusercontent.com/ACL4SSR/ACL4SSR/master/Clash/Ruleset/Netflix.list');

    // MetaCubeX 文本列表由服务端展开，最终配置不会包含 rule-providers
    callback('OpenAI', 'https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geosite/openai.list', 'domain');
    callback('DIRECT', 'https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/geoip/cn.list', 'ipcidr');
}
```

**规则处理**：

- 规则集文件中的规则会被解析并添加 `tag` 作为目标
- 例如：`DOMAIN,google.com` → `DOMAIN,google.com,PROXY`
- `domain` 会把普通域名和 `+.` 域名分别转换为 `DOMAIN`、`DOMAIN-SUFFIX`
- `ipcidr` 会根据地址族转换为 `IP-CIDR` 或 `IP-CIDR6`
- 支持的规则格式参考 Clash Meta 文档

#### buildConfig(config, legacyRelay)

用于在配置生成后进行最终调整。

- **参数**：
  - `config` (object): 完整的 Clash 配置对象（可修改）
  - `legacyRelay` (boolean): 来自 `/sub` 接口 `legacyRelay` 查询参数，未传时为 `false`
- **返回值**：无（直接修改 `config` 对象）

**config 结构示例：**

```javascript
{
    "proxies": [...],           // 节点列表（包含订阅信息假节点）
    "proxy-groups": [...],      // 策略组（包含订阅信息组）
    "rules": [...],             // 规则列表
    "mixed-port": 7890,         // 其他配置项...
    // ... 模板中的其他配置
}
```

**示例：**

```javascript
function buildConfig(config, legacyRelay) {
    // 修改端口
    config['mixed-port'] = 7890;
    config['allow-lan'] = true;

    if (legacyRelay) {
        log('启用 legacy relay 兼容模式');
    }

    // 添加自定义 DNS 配置
    config['dns'] = {
        enable: true,
        listen: '0.0.0.0:53',
        nameserver: ['223.5.5.5', '119.29.29.29']
    };

    // 添加自定义策略组
    config['proxy-groups'].push({
        name: 'Custom',
        type: 'select',
        proxies: ['DIRECT', 'PROXY']
    });

    // 在规则列表最后添加自定义规则
    config['rules'] = config['rules'].concat(['MATCH,PROXY']);
}
```

### 可用的内置函数

#### log(message)

输出日志到服务端控制台，用于调试。

- **参数**：`message` - 要输出的内容
- **返回值**：无

**示例：**

```javascript
log('开始处理规则集');
log('规则数量: ' + config.rules.length);
log({status: 'ok', count: 42});  // 支持对象
```

**日志输出**：日志会以 `[JS]` 前缀显示在服务端控制台。

### 完整示例

参见 `example/script.js` 和 `example/template.yaml`。

### 说明

- 规则集 URL 支持缓存，相同 URL 在缓存期内不会重复下载
- 规则集并发下载，但会保持调用 `callback` 的顺序
- 不支持 ES6+ 的高级特性（goja 兼容 ES5.1）
- 不支持 `console.log`，请使用 `log()` 函数

## 项目结构

```
clash-converter/
├── main.go              # 入口函数
├── api_controller.go    # HTTP 路由和处理器
├── subscription.go      # 订阅解析和合并
├── config_builder.go    # 配置构建逻辑
├── js_runner.go         # JS 脚本执行引擎
├── dao.go               # 数据库操作
├── logger.go            # 日志系统
├── utils.go             # 工具函数
├── ui.html              # 前端界面
└── data/                # 数据目录（自动创建）
    └── database.db      # SQLite 数据库
```

## 许可证

MIT License
