# hzk-doc

基于 Go 的通用文档站，提供文档阅读、全文搜索和后台内容管理。文档保存在本地目录，前端使用 Vue 3、TypeScript 和 Vite，构建后嵌入 Go 可执行文件。

## 功能

- 文档目录、后台站点名称与主题设置、日夜模式、移动端导航、页内目录、代码高亮和复制，以及标题与全文搜索。
- Markdown、HTML、PDF、图片、Office、文本和音视频预览，支持下载原文件。
- 单管理员后台，可创建文件夹和文档、改名、编辑 Markdown/HTML/文本、上传文件、调整同级显示顺序。
- SQLite 保存文档的固定 ID 和删除状态；文件名不含 ID，后台改名、编辑和回收站还原均保留原分享地址。
- 删除的文件进入回收站，可还原、单项彻底删除或清空；删除文档时会把只被它引用的素材一起移入回收站。
- 上传与新建遇到同名文件会拒绝覆盖；编辑带版本校验，防止覆盖其他人的修改。
- 后台保存后立即更新目录与搜索；从文件系统修改内容时按检查间隔更新。浏览文档不会重命名或改写文件。
- 提供 Docker 镜像与 Compose 配置，文档目录与 SQLite 元数据通过挂载目录持久化。

## 构建与启动

构建环境需要 Go 1.27.3 及以上、Node.js 24.12 及以上、npm 和 make。以下命令从项目根目录执行：

```bash
make -C src build
./0_Release/dev/hzk-doc --docs ./src/docs
```

默认只监听本机回环地址，端口自动选择，实际监听地址显示在终端。`src/docs/` 提供通用使用示例；可以通过 `--docs` 指向自己的文档目录，目录必须已经存在。

构建后的程序不需要 PHP、Python、Node 服务或独立数据库服务。SQLite 已嵌入程序，首次启动会在文档目录创建 `.hzk-doc.sqlite`，该目录需要可写；同一个文档库只允许一个程序实例使用。Node.js 仅用于构建前端。生成文件保存在已忽略的 `0_Release/` 和 `dist/` 中，不提交 Git。

## Docker 部署

`src/Dockerfile` 为多阶段构建，前端与 Go 二进制在构建阶段完成，运行镜像基于 Alpine，以 UID 10001 的非 root 用户运行。默认监听 `0.0.0.0:8080`，文档目录为容器内 `/data/docs`。

本地构建镜像（Apple Silicon 需要显式指定目标平台）：

```bash
docker buildx build --platform linux/amd64 --provenance=false --load \
  --build-arg VERSION="$(cat VERSION)" \
  -t hzk-doc:latest -f src/Dockerfile src/
```

构建阶段需要访问 Go 和 npm 软件源；网络受限时追加 `--build-arg HTTP_PROXY=<代理地址> --build-arg HTTPS_PROXY=<代理地址>`。若基础镜像元数据解析失败，先用带代理的容器构建器，再执行同一条构建命令：

```bash
docker buildx create --name proxybuilder --driver docker-container \
  --driver-opt env.HTTP_PROXY=<代理地址> --driver-opt env.HTTPS_PROXY=<代理地址>
docker buildx build --builder proxybuilder ... # 其余参数同上
```

发布产物保存在 `0_Release/<版本号>/`，包含镜像归档 `hzk-doc-latest-linux-amd64.tar` 和 `docker-compose.yml`。服务器上导入并启动：

```bash
mkdir -p data/docs
docker load --input hzk-doc-latest-linux-amd64.tar
docker compose up -d
```

`./data` 挂载到容器内 `/data`，`.hzk-doc.sqlite`、`.hzk-doc-order.json` 和 `.hzk-doc-trash/` 都在其中，备份时必须整体保留。宿主机目录属主需为 10001。

启用后台时，在 Compose 文件同目录创建 `.env`（不要提交 Git），写入至少 12 个字符的密码后启动：

```bash
printf 'HZK_DOC_ADMIN_PASSWORD=你的管理员密码\n' > .env
chmod 600 .env
docker compose up -d
```

## 启用后台

默认不启用后台写入。设置至少 12 个字符的管理员密码后启动程序，在站点的 `/admin` 页面登录；不需要用户名。

可以在 Bash 或 Zsh 中隐藏输入密码，避免将明文密码写进命令历史：

```bash
printf '请输入管理员密码（至少 12 个字符）：'
read -rs HZK_DOC_ADMIN_PASSWORD
printf '\n'
export HZK_DOC_ADMIN_PASSWORD
./0_Release/dev/hzk-doc --docs ./src/docs
```

也可以将密码保存在公开文档目录之外的 UTF-8 文本文件中，例如 `data/admin-password`，文件中只写一行密码，不要加入 Git：

```bash
chmod 600 ./data/admin-password
./0_Release/dev/hzk-doc --docs ./data/docs --admin-password-file ./data/admin-password
```

指定 `--admin-password-file` 时以文件为准，否则读取 `HZK_DOC_ADMIN_PASSWORD`。文件末尾的换行会被忽略；文件无法读取、为空或密码不足 12 个字符时拒绝启动，不会退回环境变量。密码在启动时读取，不写入文档数据库。修改密码文件或环境变量后重启服务即可生效，原登录会话随重启失效。

后台允许编辑 Markdown、HTML 和纯文本。Office、PDF 和媒体文件通过上传维护，不提供浏览器内的 Office 编辑器。单个上传文件上限为 64 MiB，目录排序单独保存在文档根目录的 `.hzk-doc-order.json`，备份文档时应一起保留。

管理员登录后可在顶部“设置”中修改站点名称，并选择经典、青岚或黑白主题；名称和主题分别保存，访客刷新页面后即可看到更改。设置保存在 SQLite 数据库中，后台保存的名称优先于启动参数 `--name`；尚未保存名称时使用 `--name` 指定的名称。访客只能通过顶部图标切换日夜模式；未手动切换时跟随系统设置，手动选择保存在当前浏览器。黑白主题的日间背景为纯白，夜间背景为纯黑。

## 回收站

后台删除时，数据库保留文档记录与 ID，并记录删除时间；对应文件移入文档根目录下的 `.hzk-doc-trash/`，属于软删除，不会立即销毁。删除 Markdown 或 HTML 文档时，只被这份文档引用的图片、音视频、样式和字体文件会一起移入回收站；仍被其他文档引用的素材保持原位置。删除目录时也会保留其中被外部文档引用的素材及必要父目录，其余内容作为同一个回收站条目保存。

后台的回收站页面可以还原、勾选后彻底删除或清空。还原时按原路径放回并保留 ID；因共享素材而保留的目录允许合并还原，遇到同名文件则取消整批还原，不覆盖现有内容。删除后新建同名文档会分配新的 ID。彻底删除和清空才删除文件及对应数据库记录，无法撤销。

删除、还原和改名保存操作日志；中途失败会撤回已经完成的文件移动，程序异常退出后在下次启动时恢复。彻底删除中断后会继续完成已确认的删除。回收站中仍可还原的文档也参与共享素材保护。

软删除的文档不参与公开目录和搜索，原 ID 地址暂时返回不存在。备份前先停止服务，完整保存文档目录，包括 `.hzk-doc.sqlite`、`.hzk-doc-order.json` 和 `.hzk-doc-trash/`；不要单独删除数据库或手动清理回收站目录。

## 启动参数

| 参数 | 默认值 |
| --- | --- |
| `--docs` | `./docs` |
| `--admin-password-file` | 未指定；读取 `HZK_DOC_ADMIN_PASSWORD` |
| `--listen` | `127.0.0.1:0`，自动选择端口 |
| `--name` | `hzk-doc` |
| `--refresh` | `3s`，最小 `1s` |
| `--version` | 显示程序版本后退出 |
| `--migrate-ids` | 登记内部 ID，去除旧文件名 ID 后退出；默认关闭 |

需要局域网访问时可使用 `--listen 0.0.0.0:0`，再使用终端显示的端口与本机局域网地址访问。`0.0.0.0` 是监听地址，不是浏览器访问地址。

## 文档与素材

文件夹对应文档分类。隐藏文件与名为 `assets` 的文件夹（不区分大小写）不出现在公开目录中；`assets` 只存放素材，不能通过“新建文档”在其中创建公开文档，素材上传仍可正常使用。文档通过 `/?id=<文档ID>` 访问，ID 由数据库持久保存，不再依赖文件名或目录。新建和上传会分配新 ID，文件名只保留正常名称；旧式 `_YYYYMMDD-六位字母` 后缀会从新建和上传的文件名中去除。

后台“改名”会同时更新磁盘文件和数据库，保持 ID、分享链接与显示顺序。不要绕过后台直接改名或移动文件：外部路径变化不能可靠识别为同一文档，可能被登记为新文档。直接修改正文仍会按检查间隔更新搜索。

### 迁移旧文件名

原来通过文件名保存 ID 的文档库，先停止服务，再执行一次迁移：

```bash
./0_Release/dev/hzk-doc --docs ./data/docs --migrate-ids
```

迁移会登记已有 ID，并去除正常文档和回收站文档的旧 ID 后缀；已经登记的内部 ID 保持不变。若去除后缀后发生同名冲突，整批文件保持原样，不会覆盖内容。数据库保留旧相对路径的对应关系，已有文内链接仍可解析。普通启动和浏览请求不会自动改名。

Markdown 中的 `assets/...` 从文档根目录的统一素材目录读取，其他相对路径以当前文档所在目录为起点。文档间链接可以直接使用相对路径，站内导航会转换成可分享的文档地址。

Markdown 阅读页可以选择单独下载原文件，或下载包含引用素材的 ZIP。有素材时，压缩包内只有一个以文档名命名的文件夹，其中包含 Markdown 文件和同级的 `assets` 文件夹；引用的本地素材统一放在 `assets` 中，同名素材会自动改名，包内引用改为相对路径。没有可打包素材时，ZIP 内只有 Markdown 原文件，不创建目录。只收集允许公开访问且存在的本地文件，外部链接和其他 Markdown 文档不会打包。HTML 和 CSS 素材引用的本地资源也会一并收集。单个文档或文本素材最大为 64 MiB，文档与文本素材合计最大为 128 MiB，压缩包最多包含 10000 个文件。导出的 HTML 除资源路径外保留原始内容，单独打开时不再受站点的隔离预览保护。

Markdown 内容经过安全处理。HTML 使用隔离预览，文档内脚本和表单不可执行。Office 预览用于内容阅读，不保证与桌面软件的字体、动画、图表和分页完全一致；精确排版建议提供 PDF。表格预览显示每个工作表的前 200 行、30 列，完整内容可以下载。音视频编码需要浏览器支持。

全文搜索覆盖 Markdown、HTML 和文本。PDF 优先由 Go 提取文本层，失败时可以使用系统已有的 `pdftotext`；无法提取文本时只检索标题，不做 OCR。Office、图片和媒体文件按标题检索。

## 安全边界

管理员登录只保护后台写入，**公开阅读不需要登录**。不要把凭据或私密资料放入公开文档目录。需要限制读者范围时，在站点前配置访问控制；对外提供后台必须使用 HTTPS。

文件访问限制在配置的文档目录内，拒绝隐藏路径、符号链接和不支持的文件类型。文档目录不直接作为静态网站目录暴露，管理接口检查登录、请求来源和 CSRF token。

## 开发与验证

Go 模块和构建入口位于 `src/`，Vue 工程位于 `src/web/`。首次运行完整 Go 检查前需要构建前端，生成嵌入资源：

```bash
make -C src test
make -C src check
```

`test` 运行 Go 测试和前端逻辑测试；`check` 运行前端构建、类型检查和 Go 静态检查。两者都不会自动修复或格式化源码。

需要前端热更新时，先启动 Go 服务，将 `HZK_DOC_DEV_BACKEND` 设为其实际完整访问地址，再运行 `npm --prefix src/web run dev -- --port <空闲高位端口>`。Vite 通过该地址代理 API 请求，不固定后端端口；前端构建与成品运行不需要这个变量。

## 原项目与许可证

重写参考项目为 `/Users/hzk/Documents/SOMEWHILE/somewhile-git/somewhile-doc`，不包含原项目的企业品牌、私有文档、飞书流程和自动部署设置。

采用 Apache License 2.0，详见 `LICENSE`。
