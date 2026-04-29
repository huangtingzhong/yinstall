# yasinstaller 项目说明

YashanDB 安装自动化工具：通过 SSH 在目标机上执行 OS 基线、数据库、YMP/YCM 等安装与清理。

---

## 一、项目组成

| 类型 | 说明 |
|------|------|
| **yasinstall** | Go 编译的主程序（CLI），支持多子命令，通过 SSH 在目标机执行安装 |
| **ymp_install_command.sh** | 辅助脚本：根据变量生成并打印 `yasinstall ymp` 的完整命令，便于复制执行 |
| **ymp_install_cmd.txt** | YMP 安装命令的文本模板与参数说明 |
| **installer.md** | 安装规范文档（需求、硬件、OS、安装步骤等） |
| **requirements.md** | 需求/规格文档 |

---

## 二、主程序 yasinstall

### 2.1 子命令一览

```text
yasinstall [command]
```

| 子命令 | 说明 |
|--------|------|
| **os** | 执行 OS 基线准备 |
| **db** | 安装 YashanDB 数据库（单机/YAC） |
| **standby** | 为现有集群添加备库 |
| **ycm** | 安装 YCM（YashanDB Cloud Manager） |
| **ymp** | 安装 YMP（YashanDB Migration Platform） |
| **clean** | 清理 YashanDB/YCM/YMP 安装 |

### 2.2 全局参数（所有子命令可用）

**目标与 SSH：**

- `--targets <IP或主机列表>`：目标主机（逗号分隔，必填，除非用 `--local`）
- `--ssh-user`：SSH 用户（默认 root）
- `--ssh-password` / `-P`：SSH 密码
- `--ssh-port`：SSH 端口（默认 22）
- `--ssh-auth password|key`：认证方式
- `--ssh-key-path`：私钥路径（key 认证时）
- `--local`：本机执行，不通过 SSH

**执行控制：**

- `--dry-run`：跳过各步骤的 Action/PostCheck（PreCheck 及 db 等命令里的全局预检查仍可能执行）
- `--precheck`：只做检查，不做变更
- `-s`/`--include-steps`、`-e`/`--exclude-steps`：只执行/跳过指定步骤
- `-f`/`--force-steps`：强制重新执行指定步骤（会删除已有资源）；`-F`/`--force` 表示对所有步骤强制

**路径与日志：**

- `-L`/`--local-software-dirs`：本地软件目录（默认 `./software,./pkg`）
- `-R`/`--remote-software-dir`：目标机软件目录（默认 `/data/yashan/soft`）
- `--log-dir`：日志目录（默认 `~/.yasinstall/logs`）

### 2.3 YMP 安装命令用法（yasinstall ymp）

**必填：**

- `--targets`：目标主机
- `--ymp-package`：YMP zip 路径
- `--ymp-instantclient-basic`：Oracle InstantClient Basic zip 路径
- `--ymp-db-package`：YashanDB 包路径（嵌入式库用）

**常用可选：**

- `--skip-os`：是否跳过 OS 基线（默认 true，即跳过）
- `--ymp-user` / `--ymp-user-password`：YMP 系统用户及密码
- `--ymp-install-dir`：安装目录（默认 `/opt/ymp`）
- `--ymp-port`：Web 端口（默认 8090，db=port+1, yasom=port+3, yasagent=port+4）
- `--ymp-jdk-enable`：是否安装 JDK（默认不安装，仅校验已有 JDK）
- `--ymp-jdk-package` / `--ymp-jdk-version`：JDK 包路径与版本（启用 JDK 安装时）
- `--ymp-instantclient-sqlplus`：Oracle SQLPlus zip（可选）
- `--ymp-oracle-env-file`：Oracle 环境变量文件路径
- `--ymp-deps-packages`：依赖包列表（默认 `libaio lsof`）
- `--ymp-cleanup`：失败时是否允许清理（慎用）

**示例：**

```bash
./yasinstall ymp \
  --targets 10.10.10.135 \
  --ssh-user root \
  --ssh-password 'your-password' \
  --ymp-package "/path/to/ymp.zip" \
  --ymp-instantclient-basic "/path/to/instantclient-basic.zip" \
  --ymp-db-package "/path/to/yashandb-package" \
  --ymp-user ymp \
  --ymp-install-dir /opt/ymp \
  --ymp-port 8090 \
  --skip-os
```

### 2.4 清理命令（yasinstall clean）

```bash
# 清理 YMP 示例
yasinstall clean -t ymp --targets 10.10.10.125 --ymp-home /opt/ymp
```

---

## 三、脚本 ymp_install_command.sh

### 3.1 作用

- 在脚本内填写**必填路径**和**可选参数**（目标机、SSH、YMP 用户、端口、JDK 等）。
- 运行后**生成并打印**对应的 `./yasinstall ymp ...` 完整命令，方便检查后复制执行，避免手写一长串参数。

### 3.2 使用步骤

1. **编辑脚本，填写必填变量（第 12–18 行）：**
   - `YMP_PACKAGE`：YMP 软件包路径
   - `INSTANTCLIENT_BASIC`：Oracle InstantClient Basic 包路径
   - `DB_PACKAGE`：YashanDB 包路径（嵌入式库）

2. **按需修改可选变量：**
   - `SKIP_OS`：是否跳过 OS 基线（true/false）
   - `YMP_USER` / `YMP_USER_PASSWORD`：YMP 用户与密码
   - `YMP_INSTALL_DIR`、`YMP_PORT`：安装目录与端口
   - `JDK_ENABLE`、`JDK_VERSION`、`JDK_PACKAGE`：是否安装 JDK 及包路径
   - `INSTANTCLIENT_SQLPLUS`、`ORACLE_ENV_FILE`、`DEPS_PACKAGES` 等

3. **修改目标与 SSH（第 71–73 行）：**  
   脚本内写死了 `--targets 10.10.10.135`、`--ssh-user root`、`--ssh-password 'huangyihan'`，使用前请改成你的目标机和账号密码。

4. **运行脚本：**
   ```bash
   cd /path/to/yasinstaller
   chmod +x ymp_install_command.sh
   ./ymp_install_command.sh
   ```
   脚本会检查必填变量是否为空，然后**在终端打印**完整命令；若需执行，复制输出的命令再运行即可。

### 3.3 注意事项

- 脚本**不会自动执行**安装，只做参数拼接与输出。
- 密码等敏感信息不要提交到版本库，建议用环境变量或临时修改后执行。
- 软件包路径可以是本地路径；yasinstall 会按 `--local-software-dirs` / `--remote-software-dir` 处理上传与使用。

---

## 四、快速参考

| 需求 | 操作 |
|------|------|
| 查看所有子命令 | `./yasinstall --help` |
| 查看 YMP 参数说明 | `./yasinstall ymp --help` |
| 只生成 YMP 安装命令（不执行） | 编辑并运行 `./ymp_install_command.sh`，复制输出命令 |
| 实际执行 YMP 安装 | 使用 `./yasinstall ymp ...`（或脚本输出的命令） |
| 安装规范与环境要求 | 查阅 `installer.md` |

---

*文档根据当前代码与 `--help` 输出整理，若二进制或参数有更新，请以 `./yasinstall --help` 与 `./yasinstall ymp --help` 为准。*
# yinstall
