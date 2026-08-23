# cuda-docs-cli

[English](README.en.md) | 简体中文

`cuda-docs-cli` 把 NVIDIA CUDA 文档服务变成一个普通的命令行工具。你不需要安装、配置或运行任何 MCP 客户端。

```console
$ cuda-docs search "如何减少 shared memory bank conflict"
...
```

> [!IMPORTANT]
> NVIDIA 目前只为这个服务公开了远程 MCP HTTP 接口，并且要求 OAuth 登录。本工具在单个 Go 二进制内部处理 OAuth、JSON-RPC 和会话协议；这些实现细节不会暴露成 MCP 配置或常驻服务。

本项目与 NVIDIA 无隶属或背书关系。CUDA、Nsight 和 NVIDIA 是 NVIDIA Corporation 的商标。

## 功能

- `search`：自动发现服务端的文档搜索工具并查询。
- `tools`：查看 NVIDIA 当前开放的文档能力及参数 schema。
- `call`：调用未来新增或无法自动映射的文档工具。
- `login/logout`：浏览器 OAuth 登录、自动刷新 token、安全清理凭证。
- `--json`：输出适合脚本和 `jq` 处理的结构化结果。
- 单文件 Go 二进制，无 Python、Node.js 或 MCP 客户端依赖。

本工具只查询文档，不采集或分析 GPU 性能数据。

## 安装

### 从源码构建

需要 Go 1.22 或更高版本：

```bash
git clone https://github.com/detailyang/cuda-docs-cli.git
cd cuda-docs-cli
make build
install -m 0755 bin/cuda-docs ~/.local/bin/cuda-docs
```

仓库发布后也可以使用：

```bash
go install github.com/detailyang/cuda-docs-cli/cmd/cuda-docs@latest
```

发布页中的压缩包包含 Linux 和 macOS 的 amd64/arm64 二进制。发布构建使用 `CGO_ENABLED=0`、`netgo` 和 `osusergo`：Linux 产物为完全静态链接；macOS 产物为无 cgo 的单文件程序，仅链接 macOS 自带系统框架。两者都不需要 Go 运行环境或额外安装共享库。

## 快速开始

首次使用需要在浏览器登录 NVIDIA：

```bash
cuda-docs login
```

无 GUI 或 SSH 环境可以只打印登录链接；如果 8765 端口被占用，可以换端口：

```bash
cuda-docs login --no-browser --port 9876
```

然后查询文档：

```bash
cuda-docs search "cuda graph launch overhead"
cuda-docs search --json "coalesced global memory access" | jq .
```

## 命令

```text
cuda-docs login [--port 8765] [--no-browser]
cuda-docs logout
cuda-docs search [--json] <query>
cuda-docs tools [--json]
cuda-docs call [--args JSON] [--json] <tool-name>
cuda-docs version
```

查看服务端原始能力：

```bash
cuda-docs tools --json
```

手动调用某个工具：

```bash
cuda-docs call --args '{"query":"CUDA streams"}' TOOL_NAME
```

Go 标准 `flag` 语法要求选项出现在查询文本或工具名之前。

## 配置与隐私

凭证默认保存在操作系统用户配置目录：

- Linux：`$XDG_CONFIG_HOME/cuda-docs-cli/credentials.json`，未设置时通常是 `~/.config/...`
- macOS：`~/Library/Application Support/cuda-docs-cli/credentials.json`

凭证文件采用原子写入，并在 Unix 上设置为 `0600`。查询内容会发送到 NVIDIA 文档服务；本工具没有遥测、数据库或第三方代理。

可用环境变量：

| 变量 | 用途 |
| --- | --- |
| `CUDA_DOCS_CONFIG_DIR` | 覆盖凭证目录，适合 CI 或隔离测试 |
| `CUDA_DOCS_ENDPOINT` | 覆盖远程文档端点，主要用于开发测试 |

## 开发

```bash
make fmt
make test
make vet
make build
```

更多资料：

- [架构与安全边界](docs/architecture.md)
- [认证流程](docs/authentication.md)
- [故障排查](docs/troubleshooting.md)
- [贡献指南](CONTRIBUTING.md)
- [安全策略](SECURITY.md)

## 许可证

[MIT](LICENSE)
