说明及变更记录

```
此版本为安装规范的文字版本，后续计划将此版本中的规范全部写成脚本，实现一键安装数据库，简化安装的复杂度、提升安装的效率与安装的质量。
20251231：初始版本发布，第一个版本难免有错误或者考虑不周全的地方。如有任何建议，欢迎大家随时联系黄彦、黄廷忠。 
20250118：进行版本重构，简化内容，同时将非必要的操作放到附录中。
20250119：Oracle Linux 8、UOS 20、麒麟V10平台下已经验证
20250120：添加YAC SCANDNS模式及依赖的内容章节
```

# 1 需求准备阶段


## 1.1 服务器配置要求

**存储要求：** 在生产环境中，如果数据文件存放在服务器本地，要求使用NVME磁盘，以获得最佳IO性能；如果存放在集中式存储中，存储要求至少是SSD磁盘，确保满足数据库的IO性能需求。

**说明：** NVME磁盘相比传统SATA SSD具有更低的延迟和更高的IOPS，特别适合数据库这种IO密集型应用。集中式存储需要支持多路径，确保高可用性。

| 产品  | 部署架构 | 服务器数量 | 功能最低配置          | 性能最低配置          | 共享磁盘类型                  |
| --- | ---- | ----- | --------------- | --------------- | ----------------------- |
| 数据库 | 1-1  | 2台    | CPU：MEM：NETWORK | CPU：MEM：NETWORK | TYPE:NVME/SSD，IOPS/MBPS |
| YMP |      |       |                 |                 |                         |
| YCM |      |       |                 |                 |                         |
|     |      |       |                 |                 |                         |
|     |      |       |                 |                 |                         |
|     |      |       |                 |                 |                         |
|     |      |       |                 |                 |                         |

## 1.2 明确版本要求

**版本兼容性检查：** 每次安装前，必须确认所有YashanDB相关产品（数据库、YMP、YCM）和操作系统的版本号，确认产品与操作系统之间的兼容性。**重要性：** 版本不兼容可能导致安装失败、功能异常或性能问题。建议在安装前查阅官方兼容性矩阵文档，确保所有组件版本匹配。

| 产品  | 产品版本 | 操作系统 |
| --- | ---- | ---- |
| 数据库 |      |      |
| YMP |      |      |
| YCM |      |      |

## 1.3 网络要求

**网络隔离原则：** 在生产系统中，要求业务网络与主备同步网络、集群网络独立分开，避免网络拥塞影响数据库性能和高可用性。**带宽要求：** 所有网络带宽建议万兆（10Gbps），集群心跳网络万兆是必备条件，确保集群节点间通信的实时性和可靠性。**说明：** 网络隔离可以避免业务流量影响主备同步和集群心跳，提高系统稳定性。

| 架构    | 业务网络 | 主备同步网络    | 集群心跳网络 |
| ----- | ---- | --------- | ------ |
| 单机架构  | 最低千兆 |           |        |
| 主从架构  | 最低千兆 | 最低千兆，建议万兆 |        |
| YAC架构 | 最低千兆 | 最低千兆，建议万兆 | 万兆     |



# 2 服务硬件配置

## 2.1 性能模式调整

**性能模式说明：** 为了获得最佳数据库性能，需要在BIOS/UEFI中调整CPU和电源管理相关设置。这些设置会影响CPU频率、功耗管理等，关闭节能功能可以确保CPU始终运行在最高性能状态。

关闭如下的功能：

```
Cstate
Pstate
EIST
Power saving
```

确认以下功能开启：

```
Automatic Power on After Power Loss: Always on
Hyper-threading
Hardware prefetcher
Turbo Mode
Energy performance：最大 performance
```

## 2.2 CPU Prefetching

**CPU预读功能说明：** CPU预读（Prefetching）是CPU的一种优化技术，通过预测内存访问模式提前加载数据到缓存。但在某些数据库场景中，预读可能会带来缓存污染，反而影响性能。

### 2.2.1 ARM平台

目前在测试中发现禁用CPU预读功能后性能会有大约百分之几的性能提升，建议禁用CPU预读功能。**配置方法：** 可以通过BIOS设置或内核参数禁用CPU预读功能，具体方法需要参考服务器厂商文档。

### 2.2.2 X86平台

目前X86平台暂时还没有测试数据，建议保持默认设置。如果后续有测试数据表明禁用预读可以提升性能，再考虑调整。


## 2.3 NUMA功能

**NUMA说明：** NUMA（Non-Uniform Memory Access）是一种多处理器架构，不同CPU核心访问不同内存区域的速度不同。NUMA的开启或关闭会影响数据库的内存分配策略和性能。

### 2.3.1 需要开启NUMA的服务器

**开启NUMA的原因：** 某些CPU架构（如ARM、海光）在开启NUMA后可以更好地利用多核性能，提高数据库并发处理能力。

后续大家可以共同确认哪些型号的CPU需要开启NUMA，同时根据多个项目经验后，再形成统一的规范。

```
ARM和海光的CPU需要开启NUMA
```

### 2.3.2 需要关闭NUMA的服务器

**关闭NUMA的原因：** 某些CPU架构（如Intel）在关闭NUMA后可以避免跨NUMA节点访问内存带来的性能损失，特别是在单节点数据库场景中。

后续大家可以共同确认哪些型号的CPU需要关闭NUMA，同时根据多个项目经验后，再形成统一的规范。

```
INTEL需要关闭NUMA
```
## 2.4 磁盘镜像

**RAID配置要求：** 操作系统磁盘必须采用RAID方式，比如RAID1（镜像）或者RAID 5与RAID 6（带校验的条带化），确保操作系统的高可用性。**Write-back模式：** 必须开启Write-back模式，提升写的IO性能。**注意：** Write-back模式在系统断电时可能有数据丢失风险，需要配合UPS等设备使用。
## 2.5 网络连线与绑定

**网卡配置要求：** 
- 单机环境最少需要2张网卡，主备环境最少需要2张网卡，每张网卡最少2个网口
- 集群+主从环境最少需要2张网卡，每张网卡最少2个网口

**网卡绑定原则：** 操作系统网卡需要采用绑定技术，必须是不同网卡之间的网口进行绑定，绑定的网口连接到不同的交换机，实现链路冗余。

**绑定模式选择：** 绑定模式选择主从（mode 1，active-backup）或者负载均衡（mode 4，802.3ad），其中mode 4需要交换机支持802.3ad（LACP）协议。

**建议：** 生产环境建议使用mode 1（主从模式），简单可靠；如果对带宽要求高且交换机支持，可以使用mode 4（负载均衡模式）。

## 2.6 网络VLAN划分

**VLAN隔离原则：** 主备同步网络在独立的VLAN中，集群模式私有网络在独立的VLAN中，实现网络隔离，避免网络拥塞和广播风暴。

**交换机配置：** 需要注意在交换机中配置相应的VLAN，确保不同VLAN之间的路由和访问控制正确配置。

## 2.7 磁盘规划
### 2.7.1 操作系统分区

```
/home    100G
/        50G
swap     32G
```
### 2.7.2 YAC共享磁盘

**共享磁盘说明：** YAC共享集群需要使用共享存储，所有节点都可以访问相同的磁盘，实现数据共享。

#### 2.7.2.1 YAC系统磁盘

SYSTEM磁盘统一采用5G，一共3个。

**说明：** SYSTEM磁盘用于存储集群的元数据和配置信息，3个磁盘提供冗余保护，确保集群元数据的高可用性。

#### 2.7.2.2 数据磁盘

数据磁盘每个2T，总容量根据业务需求而定。

**说明：** 数据磁盘用于存储数据库的数据文件，建议根据业务数据量、增长趋势、备份保留策略等因素综合考虑容量需求。建议预留30-50%的容量用于数据增长和临时文件。

### 2.7.3 单机环境

单机环境数据文件存放在单独的lv中，容量需要根据业务数据文件容量、归档日志容量、临时文件等综合考虑容量需求。**容量规划建议：** 建议预留足够的空间用于数据增长、归档日志、备份文件等，通常建议预留50-100%的额外空间。

**注意：** 如果数据文件存放在独立的磁盘中，利用磁盘创建新的VG，在新的VG中创建lv，必须确保数据文件位于独立的lv中，避免与操作系统或其他应用共享磁盘空间，影响数据库性能。

```
/data       根据业务数据量和归档等需求
```

## 2.8 操作系统安装

**安装模式：** 系统安装采用**最小化安装模式（不要安装图形界面等非必要的组件）**，减少系统资源占用，提高安全性和性能。**磁盘管理：** 操作系统采用LVM的磁盘管理模式，便于后续的磁盘扩容和管理。**ISO文件：** 将ISO文件上传到/root目录下，用于配置本地YUM源。



# 3 操作系统

## 3.1 IP地址配置

**IP地址检查：** 确认IP地址配置是否正确，包括IP地址、子网掩码、网关等。**网络连通性：** 在YAC环境中，需要确认服务器之间的业务网络是否在同一网段、心跳网络是否在同一网段，确保节点间可以正常通信。主备环境中需要确认主备同步网络是否在同一网段，确保主备同步正常。

**配置方法：** IP地址修改、网络名修改、网卡绑定可参考附录。

## 3.2 主机名配置

**主机名要求：** 所有生产服务器必须配置主机名，不能采用默认的localhost名字。

**命名规范：** 主机名配置时建议与数据库名字或者系统名字挂钩，便于理解和管理。在YAC环境中，不同节点后面用1、2等数字来区分，比如acct1、acct2。

**YAC环境主机名要求：** 在YAC环境中，对主机名还有如下要求：
* 名称由字母、数字以及下划线组成，且必须以字母开头，长度为[4,64]个字符。
* 同一个YashanDB共享集群中的主机名不能相同。
* 建议每台服务器上只运行一个实例，若一台服务器需运行多个实例则要求将主机名称设置为[3,63]个字符。

```shell
hostnamectl set-hostname htz01
```


## 3.3 用户创建

**用户ID规划：** 默认使用-g 701和-g 702（组ID），-u为701（用户ID），如果对应的id已经存在，自行修改对应的值。

**说明：** 统一的用户ID和组ID便于在多台服务器间保持一致性，特别是在集群环境中，建议所有节点使用相同的用户ID和组ID。

### 3.3.1 创建用户

```
/usr/sbin/groupadd -g 701 yashan
/usr/sbin/groupadd -g 702 YASDBA
/usr/sbin/useradd -u 701 -g yashan  -G YASDBA -m  yashan  -s /bin/bash
echo 'aaBB11@@33$$'|passwd yashan --stdin
```

### 3.3.2 配置sudo权限

**sudo权限说明：** 配置sudo权限后，yashan用户可以在不需要root密码的情况下执行管理员命令，便于数据库的安装和管理。

**注意：** 这里修改/etc/sudoers的权限为400，防止文件权限过大，Linux操作系统认为有风险，会去掉所有配置用户的sudo超级权限。

**安全建议：** 在生产环境中，建议根据实际需求配置更细粒度的sudo权限，而不是ALL权限。

```shell
chmod +w /etc/sudoers
echo "yashan  ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers
chmod -w /etc/sudoers
chmod 400 /etc/sudoers
```

## 3.4 时区配置

**时区要求：** 时区统一配置为上海时区（Asia/Shanghai），确保数据库时间与业务系统时间一致。

**说明：** 时区配置会影响数据库的时间函数、日志时间戳等，建议所有服务器（包括数据库服务器和应用服务器）使用相同的时区。

```
timedatectl set-timezone "Asia/Shanghai"
```
## 3.5 内核参数配置

### 3.5.1 常规推荐参数配置

以下是YashanDB官方文档中要求配置的内核参数值。**参数说明：**
- kernel.shmmax：操作系统最大共享内存段的值，此值必须大于主机上YashanDB的SGA的值，建议配置为物理内存的80%
- kernel.shmall：系统可分配的共享内存页总数
- vm.swappiness：控制swap使用倾向，设置为0表示尽可能不使用swap
- vm.max_map_count：进程可用的最大内存映射区域数，对于大型数据库很重要

**配置原则：** 这些参数需要根据实际硬件配置和数据库规模调整，建议在配置前查阅YashanDB官方文档获取最新推荐值。

```shell
cat > /etc/sysctl.d/yashandb.conf << 'HTZ' 
# YashanDB matching parameters
vm.swappiness = 0
net.ipv4.ip_local_port_range=32768 60999
vm.max_map_count=2000000
net.core.somaxconn=32768
kernel.shmall = 3774873
kernel.shmmni = 4096
kernel.shmmax = 246960619520
#tunning parameters
fs.aio-max-nr = 6194304
vm.dirty_ratio=20
vm.dirty_background_ratio=3
vm.dirty_writeback_centisecs=100
vm.dirty_expire_centisecs=500
vm.min_free_kbytes=524288
net.core.netdev_max_backlog = 30000
net.core.netdev_budget = 600
vm.oom-kill = 0
vm.overcommit_memory = 2
HTZ
sysctl --system
```

### 3.5.2 大页配置

**大页说明：** 大页（HugePages）可以减少页表项数量，降低TLB（Translation Lookaside Buffer）缺失率，提高内存访问性能。

**配置建议：** 大页参数非必须参数，但对于大型数据库（特别是内存使用量超过64G的场景），建议开启大页功能。如需开启大页，参考附录《大页配置》。

## 3.6 用户资源限制配置

### 3.6.1 常规资源限制

```
cat >> /etc/security/limits.conf << 'HTZ'
yashan soft nofile  1048576
yashan hard nofile  1048576
yashan soft nproc   1048576
yashan hard nproc   1048576
yashan soft rss     unlimited
yashan hard rss     unlimited
yashan soft stack   8192
yashan hard stack   8192
yashan soft core    unlimited
yashan hard core    unlimited
yashan soft memlock -1
yashan hard memlock -1
HTZ
```

## 3.7 内核启动参数

**内核参数说明：** 以下配置用于禁用透明大页、配置LANG和配置IO elevator。这些参数需要在内核启动时设置，修改后需要重启系统生效。

**参数说明：**
- transparent_hugepage=never：禁用透明大页，避免透明大页带来的性能抖动
- elevator=deadline：设置IO调度器为deadline，适合数据库这种随机IO场景
- LANG=en_US.UTF-8：设置系统语言环境，确保字符编码正确


```
grubby --update-kernel=ALL --args="transparent_hugepage=never  elevator=deadline LANG=en_US.UTF-8"
```

## 3.8 禁用服务

**服务管理说明：** 禁用不必要的系统服务可以减少系统资源占用，提高安全性和性能。

**禁用原则：** 禁用服务非必选项，建议根据实际需求决定。如果需要禁用服务，参考附录《禁用服务》。

**注意：** 禁用服务前需要确认该服务不被其他应用依赖，避免影响系统功能。

## 3.9 安装软件包

**软件包安装说明：** 安装软件前先确认服务器YUM资源库是否可用，如果无可用资源库，可以通过ISO来配置本地资源库。

**配置本地源的优势：** 本地源可以避免网络问题，提高安装速度，特别适合内网环境。

详细配置命令如下。

### 3.9.1 挂载ISO文件

```
mount -t iso9660 /dev/cdrom /media
```
### 3.9.2 配置YUM配置文件

#### 3.9.2.1 RHEL7、Kylin V10

```
cat >  /etc/yum.repos.d/local.repo << 'HTZ'
[local]
name = Enterprise Linux 7 DVD
baseurl=file:///media
gpgcheck=0
enabled=1
HTZ
```

#### 3.9.2.2 RHEL8、UOS V20

```
cat >  /etc/yum.repos.d/local.repo << 'HTZ'
[local-baseos]
name=DVD for RHEL - BaseOS
baseurl=file:///media/BaseOS
enabled=1
gpgcheck=0

[local-appstream]
name=DVD for RHEL - AppStream
baseurl=file:///media/AppStream
enabled=1
gpgcheck=0
HTZ
```

### 3.9.3 数据库依赖软件

**依赖软件说明：** 安装YashanDB需要依赖以下软件包，这些软件包提供了数据库运行所需的基础库和工具。**安装顺序：** 建议先安装依赖软件包，再安装YashanDB数据库软件，避免依赖问题。

安装YashanDB需要依赖的软件包：

#### 3.9.3.1 通用命令

```
yum -y install libzstd zlib lz4 openssl openssl-devel 
```
#### 3.9.3.2 RHEL7

```
yum -y install --disablerepo=\* --enablerepo=local libzstd zlib lz4 openssl openssl-devel 
```

#### 3.9.3.3 RHEL8以后版本

```
 yum -y install --disablerepo=\* --enablerepo=local-baseos --enablerepo=local-appstream libzstd zlib lz4 openssl openssl-devel 
```



#### 3.9.3.4 常用工具包

常用工具包非必须安装项，如需安装，请参考附录《常用工具包》

## 3.10 chrony配置

**时间同步说明：** 时间同步对于数据库集群非常重要，时间偏差可能导致数据不一致、主备切换失败等问题。**配置要求：** 所有数据库服务器必须配置相同的时间服务器，确保时间同步。时钟服务器客户端配置，将ntp.aliyun.com替换成客户具体的NTP服务器的地址。

```
cp /etc/chrony.conf /etc/chrony.conf.bak_$(date +%F)

cat > /etc/chrony.conf << 'HTZ'
# Aliyun NTP server (IP)
server ntp.aliyun.com iburst
allow 0.0.0.0/0
makestep 1.0 3
driftfile /var/lib/chrony/drift
rtcsync
logdir /var/log/chrony
HTZ


systemctl restart chronyd && systemctl enable chronyd
chronyc makestep && chronyc tracking
```



## 3.11 防火墙配置

### 3.11.1 关闭防火墙版本

**防火墙管理说明：** 如果客户对防火墙无特殊要求，可以在操作系统上关闭防火墙，简化配置。**安全建议：** 如果客户要求开启防火墙，需要配置相应的端口规则，确保数据库服务可以正常访问。
```
systemctl stop firewalld
systemctl disable firewalld
```
### 3.11.2 开启防火墙版本

**防火墙配置说明：** 如果客户要求开启防火墙，需要根据部署架构开放相应的端口。不同部署架构使用的端口不同，需要仔细配置，避免遗漏导致服务无法访问。



```
# 1. 查看防火墙端口开放情况
firewall-cmd --zone=public --list-ports

# 2. 按部署形态选择端口列表（建议复制下面模板修改后执行）

# 单机/主备（Yashan协议）
PORTS="1675 1676 1688 1689"

# 单机（MySQL协议额外端口）
# PORTS="${PORTS} 1690"

# 共享集群（YAC）
# PORTS="1675 1676 1688 1689 1788 1690"

# 分布式集群（DN端口按DN数量从1700开始递增，此处只放起始端口）
# PORTS="1675 1676 1688 1689 1788 1700"

# 可视化部署Web服务（如需要）
# PORTS="${PORTS} 9001"

for p in $PORTS; do
  firewall-cmd --zone=public --add-port=${p}/tcp --permanent
done

# 3. 重新载入防火墙配置
firewall-cmd --reload

# 4. 验证端口是否已添加（以1688为例）
firewall-cmd --zone=public --query-port=1688/tcp

# 5. 查看所有已开放的端口
firewall-cmd --zone=public --list-ports
```




## 3.12 目录准备

建议将软件安装在独立的分区上，在单机环境，数据文件一定要放到高性能磁盘中。

### 3.12.1 独立磁盘模式
独立磁盘模式即YashanDB使用独立的磁盘，可以来自服务器的本地磁盘，也可以来自存储（需提前配置多路径软件）。
生产环境中如采用本地独立磁盘，磁盘需要提前做RAID，保持磁盘有冗余。如果硬件不支持磁盘冗余，可以利用操作系统软RAID来实现。在操作系统识别到多个独立磁盘后，建议采用LVM方式管理磁盘，并且LV需要采用==条带化==，条带化可以将IO打散到多个物理磁盘上，提升IO性能。

下面以3个nvme的磁盘为例，说明如何创建LVM条带化逻辑卷：

**注意：** 在执行pvcreate之前，务必先确认磁盘是否已经创建PV，如果已经创建请勿重复使用，避免数据丢失。

```
# 查看当前PV状态
pvs 

# 创建物理卷（PV），注意：确保这些磁盘未被使用
pvcreate /dev/nvme0n2 
pvcreate /dev/nvme0n3 
pvcreate /dev/nvme0n4
vgcreate yashandbdg /dev/nvme0n2 /dev/nvme0n3 /dev/nvme0n4
# -i 指定pv的个数，也就是物理磁盘的个数；-I指定条带化的大小，默认单位是KB（-I的值不能低于数据库单次IO的最大值）；-L 指定lv的大小，可以是100G、100%FREE（全部剩余空间）；-n指定lv的名字。
lvcreate -i 3 -I 32768 -L 100%FREE -n yashanlv1 yashandbdg
# 格式化为文件系统，17T以上只支持xfs格式
mkfs.xfs /dev/yashandbdg/yashanlv1
mkdir /data
mount /dev/yashandbdg/yashanlv1 /data
mkdir -p /data/yashan/yasdb_data  && chown -R yashan:yashan /data/yashan/yasdb_data
mkdir -p /data/yashan/yasdb_home  && chown -R yashan:yashan /data/yashan/yasdb_home
# 配置自动挂载
echo "/dev/yashandbdg/yashanlv1 /data    xfs     defaults        0 0" >> /etc/fstab
```

### 3.12.2 非独立磁盘模式

非独立磁盘模式适用于测试环境或对性能要求不高的场景，数据文件与操作系统共享同一磁盘。直接创建对应目录即可，但需要注意磁盘空间规划，确保有足够的空间用于数据库数据文件和日志文件。

```
mkdir -p /data/yashan/yasdb_data  && chown -R yashan:yashan /data/yashan/yasdb_data
mkdir -p /data/yashan/yasdb_home  && chown -R yashan:yashan /data/yashan/yasdb_home
```



## 3.13 YAC模式配置

**YAC模式说明：** YAC（YashanDB Active Cluster）是YashanDB的共享存储集群模式，多个节点共享同一套存储，实现高可用和负载均衡。YAC模式需要特殊的存储和网络配置。

### 3.13.1 多路径软件配置

**多路径软件说明：** 在使用共享存储的环境中，需要配置多路径软件，实现存储路径的冗余和负载均衡。正常情况多路径软件由存储工程师或者主机工程师配置。
**配置原则：** 配置多路径软件时，建议采用多路径默认路径（/dev/mapper/）和自定义多路径磁盘的名字，便于管理和识别。多路径磁盘的全路径规则如下：

```
/dev/mapper/sys1
/dev/mapper/sys2
/dev/mapper/sys3
/dev/mapper/data01
/dev/mapper/arch01
```
磁盘的名字可以与YFS中磁盘名直接联系起来，可以防止后续YFS中添加磁盘时出现误操作，比如：sys1、sys2、sys3为集群系统磁盘，data01是数据磁盘的名字，arch01是归档日志磁盘的名字。

#### 3.13.1.1 共享磁盘的WWID号

**WWID说明：** WWID（World Wide Identifier）是存储设备在全球范围内的唯一标识符，用于识别共享磁盘。**识别方法：** 在YAC所有的节点上执行下面的操作，获取每个节点的WWID号，当两台服务器获取到相同的WWID号时，表示磁盘为共享磁盘。**注意：** 只有所有节点都能看到相同WWID的磁盘，才能作为YAC的共享磁盘使用。

```
for i in `lsblk|grep disk|awk '{print $1 "."$4}'`
do
 echo $i && /lib/udev/scsi_id --whitelisted --replace-whitespace --device=/dev/`echo $i|awk -F. '{print $1}'`
done
```

#### 3.13.1.2 配置多路径
本小结的配置为基础模版，生产环境的配置建议跟甲方的主机工程师和存储工程师沟通后再进行多路径配置。
**配置说明：** 将获取到的共享磁盘添加到多路径软件中，通过alias指定友好的磁盘名称，便于后续使用和管理。**配置要求：** 所有节点的多路径配置必须一致，确保磁盘名称在所有节点上相同。
```
cat > /etc/multipath.conf << 'HTZ'
defaults {
        user_friendly_names yes
        find_multipaths off
        reservation_key file
}


multipaths {
       multipath {
               wwid                    36000c29da9d8ef72048cb8a895009185
               alias                   sys1
       }
       multipath {
               wwid                    36000c295742008f65f783e6c706ca955
               alias                   data1
       }
}
HTZ
```

#### 3.13.1.3 启动服务

**服务管理：** 开启多路径软件服务器自动随操作系统启动，确保系统重启后多路径功能自动生效。

**验证方法：** 启动服务后，可以通过`multipath -ll`命令查看多路径磁盘状态，确认配置是否正确。

```
systemctl enable multipathd
systemctl start multipathd
```

#### 3.13.1.4 确认多路径磁盘是否生效

**验证步骤：** 执行multipath命令来确认多路径磁盘是否生效，同时确认YAC多个节点多路径磁盘名称是一致的。**重要性：** 磁盘名称一致性是YAC集群正常运行的前提，如果节点间磁盘名称不一致，会导致集群配置失败或数据不一致。

```
multipath -ll
```



### 3.13.2 磁盘权限配置

**权限配置说明：** 磁盘权限建议采用udev的方式来配置，udev是Linux的设备管理器，可以在设备添加或变更时自动设置权限。**配置原则：** UDEV配置建议在满足需求的前提下，尽可能简单，避免复杂的规则导致配置错误。

#### 3.13.2.1 多路径环境

多路径场景中，建议磁盘名字直接用多路径磁盘。
```
cat > /etc/udev/rules.d/99-yashandb-permissions.rules << 'HTZ' 
ACTION=="add|change",ENV{DM_NAME}=="data*|sys*|arch*", OPTIONS:="nowatch",OWNER:="yashan", GROUP:="YASDBA", MODE:="0666"
HTZ
```


#### 3.13.2.2 块设备
虽然udev支持块设备默认配置，但还是建议先配置多路径名字，再利用多路径名字配置udev。

```
cat > /etc/udev/rules.d/99-yashandb-permissions.rules << 'HTZ' 
SUBSYSTEM=="block",ACTION=="add|change", KERNEL=="nvme0n2|nvme0n3|nvme0n4", OPTIONS:="nowatch",OWNER:="yashan", GROUP:="YASDBA", MODE:="0666"
HTZ
```

#### 3.13.2.3 触发udev配置生效

**配置生效：** 手动加载udev配置文件，触发配置文件中change的配置内容，使权限配置立即生效。**验证方法：** 执行命令后，可以通过`ls -l /dev/dm*`命令查看磁盘权限是否已正确设置。

```
udevadm control --reload-rules
/sbin/udevadm trigger --type=devices --action=change
ls -l /dev/dm*
```

## 3.14 重启操作系统

**重启说明：** 上面修改完成后，需要重启操作系统，让所有配置生效。**需要重启的配置：** 内核参数、内核启动参数、udev规则、网卡配置等都需要重启后才能生效。**建议：** 重启前建议检查所有配置是否正确，避免重启后发现问题需要再次重启。

## 3.15 操作系统压测

**压测目的：** 配置完成后，需要对操作系统进行CPU、MEM、DISK等进行高负载压测，确认在主机层面能达到多少性能数据，为数据库性能调优提供参考。**压测内容：** 压测内容参考《TPCC》中服务器压测部分。**压测建议：** 建议在数据库安装前完成压测，了解硬件性能基线，便于后续性能问题排查。



# 4 数据库安装

## 4.1 单机安装

### 4.1.1 创建目录

创建目录需要根据第3章中操作系统的《目录准备》这节内容，如果已经创建，直接跳过。

**目录说明：**
- yasdb_home：数据库软件安装目录
- yasdb_data：数据库数据文件目录
- log：数据库日志文件目录
- soft：软件包存放目录

**注意：** 目录创建后需要确保yashan用户拥有相应的权限，否则会影响后续的数据库安装和运行。

```
sudo mkdir -p /data/yashan/yasdb_home 
sudo mkdir -p /data/yashan/yasdb_data
sudo mkdir -p /data/yashan/log
sudo chown -R yashan:yashan /data/yashan
sudo mkdir -p /data/yashan/soft
```

### 4.1.2 解压软件

解压软件，这里只需要在执行安装的节点上面

安装软件建议放到/data/yashan/soft目录下面，按实际版本修改下面的包名。

```
sudo mkdir -p /home/yashan/install 
sudo tar -zxf /data/yashan/soft/yashan/soft/yashandb/23.4.6.100/yashandb-23.4.6.100-linux-x86_64.tar.gz  -C /home/yashan/install
sudo chown -R yashan:yashan /home/yashan/install 
```



### 4.1.3 生成配置文件

**配置文件说明：** yasboot在生成配置文件时，默认内存百分比为80%，此参数需要根据实际环境进行调整。**内存配置原则：** 如果主机内存小于256G，建议配置为70%，避免内存过度分配导致系统不稳定；如果主机内存大于256G，可以保留默认值80%，但需要综合考虑操作系统和其他应用的内存需求。

**参数说明：** 
- --cluster：集群名称，建议使用有意义的名称，便于管理
- --begin-port：起始端口号，数据库会从此端口开始分配端口，需要确保端口范围未被占用

**重点：** --cluster和--begin-port这里为默认值，如需自定义，可以修改参数值。

在YashanDB安装用户下执行下面命令：

```
cd /home/yashan/install && /home/yashan/install/bin/yasboot package se gen --cluster yashandb --recommend-param \
-u yashan -p 'aaBB11@@33$$' --ip 10.10.10.150 --port 22 \
--install-path /data/yashan/yasdb_home  \
--data-path /data/yashan/yasdb_data \
--log-path /data/yashan/log \
--begin-port 1688  \
--memory-limit 70 \
--node 1
```

如需 **MySQL 模式**（例如需要生成 MySQL 协议/兼容相关配置），在生成配置时增加参数：`--mode mysql`。\n+\n+使用 `yinstall` 安装时，对应参数为：`yinstall db --db-mode mysql ...`（默认不传表示不指定 mode）。\n+
这里会生成hosts.toml和yashandb.toml文件，需要注意，在新版本中hosts.toml中的密码是加密密码。

### 4.1.4 安装软件

YashanDB数据库软件依赖SSL库文件，在部分操作系统中需要自定义SSL库文件。**判断是否需要指定SSL依赖包：** 如果操作系统自带的SSL库版本不兼容或缺失，需要在安装时指定SSL依赖包。通常UOS V20和麒麟V10不需要指定，其他操作系统需要根据实际情况判断。

#### 4.1.4.1 不指定SSL依赖

UOS V20和麒麟V10都不需要指定依赖的SSL。

```
cd /home/yashan/install && /home/yashan/install/bin/yasboot package install -t /home/yashan/install/hosts.toml
```

安装完成后，会自动启动yasom和yasagent两个进程，om进程是全局共享的。

#### 4.1.4.2 指定SSL依赖包

```
cd /home/yashan/install && /home/yashan/install/bin/yasboot package install -t /home/yashan/install/hosts.toml --deps /data/yashan/soft/yashandb-deps-23.4.4.100-linux-aarch64.tar.gz
```

### 4.1.5 创建数据库

**创建前检查：** 在创建数据库前，需要确认两个重要内容，这些配置在数据库创建后无法修改，需要谨慎选择。

**1. 数据库字符集（CHARACTER_SET）**

参数默认值为UTF8，建议规范是保持与源端数据库字符集一致，避免字符集转换带来的性能损失和数据问题。**字符集映射规则：**
- Oracle:ZHS16GBK  → YashanDB:GB18030
- Oracle:AL32UTF8   → YashanDB:UTF8
- Dameng:GB18030 → YashanDB:GB18030

针对新开发系统或者没有对应规则的字符集场景中，联系组长，内部确认字符集的配置。

**2. 原生字段类型（USE_NATIVE_TYPE）**

参数默认值为true。**配置原则：**
- 如源库是Oracle并且需要实现零代码修改时，建议统一采用false配置，使用Oracle兼容的数据类型
- 其它所有的场景保持默认true的配置，使用YashanDB原生数据类型，获得更好的性能





#### 4.1.5.1 修改字符集

yashandb.toml配置文件中的CHARACTER_SET为字符集配置信息，如需自定义字符集，请手动修改。

```
grep CHARACTER_SET yashandb.toml
    CHARACTER_SET = "utf8"
```

**重要限制：** YashanDB暂时不支持修改字符集，只能在创建实例时配置，因此创建前必须确认字符集选择正确。

#### 4.1.5.2 修改原生字段类型

USE_NATIVE_TYPE参数默认不在yashandb.toml中，如需修改，直接在yashandb.toml中添加参数，参数需要添加在[group.node.config]下面。

```
USE_NATIVE_TYPE = false
```

修改后的值内容：

```
   [group.node.config]
      ARCH_CLEAN_IGNORE_MODE = "BACKUP"
      CGROUP_ROOT_DIR = "/sys/fs/cgroup"
      LISTEN_ADDR = "10.10.10.150:1688"
      REPLICATION_ADDR = "10.10.10.150:1689"
      RUN_LOG_FILE_PATH = "/data/yashan/log"
      RUN_LOG_LEVEL = "INFO"
      SLOW_LOG_FILE_PATH = "/data/yashan/log"
      USE_NATIVE_TYPE = false
```



#### 4.1.5.3 创建数据库

在安装用户yashan下面执行下面命令，下面命令会创建数据库，如果是主从，还会自动搭建主从关系。

```
cd /home/yashan/install && /home/yashan/install/bin/yasboot cluster deploy -t yashandb.toml -p  'Yashan1!'
```

创建完成后，可以查下下面的命令：

```
cd /home/yashan/install && /home/yashan/install/bin/yasboot cluster status -c yashandb -d
```

确认输出中主库 `database_role` 为 `primary`、备库为 `standby`，且 `database_status` 为 `normal` 即可。


### 4.1.6 配置环境变量

#### 4.1.6.1 默认环境

需要在所有节点上执行下面命令，下面文件yashandb关键字也是集群的名字，如果yasboot命令自定义了集群的名字，这里文件需要做对应修改。

```
cat ~/.yasboot/yashandb_yasdb_home/conf/yashandb.bashrc >> ~/.bashrc
source ~/.bashrc && which yasboot
```

确认yasboot能找到即可，同时确认yasql / as sysdba能正常登录数据库。

```
yasql / as sysdba
```


## 4.2 YAC安装

在进行集群安装前，需要先对集群中每台服务器的操作系统进行配置，详细参考第3章内容。本章只给出在主机环境配置完成后，集群安装需要进行的单独操作或命令。

### 4.2.1 私有网卡地址配置

**私有网络说明：** 在集群环境中，建议节点之间通信配置专用的私有网卡和私有网段，实现网络隔离，避免业务流量影响集群内部通信。**网卡绑定：** 私有网卡也建议采用网卡绑定，提供链路冗余，确保集群通信的可靠性。

**配置方法：** 网卡的配置与网卡的绑定查看附录。

### 4.2.2 创建目录

创建目录需要根据第3步中操作系统的《目录准备》这节内容，如果已经创建，直接跳过。

```
mkdir -p /data/yashan/yasdb_home 
mkdir -p /data/yashan/yasdb_data
mkdir -p /data/yashan/log
chown -R yashan:yashan /data/yashan
mkdir -p /data/yashan/soft
```

### 4.2.3 解压软件

解压软件，这里只需要在执行安装的节点上面

安装软件建议放到/data/yashan/soft目录下面，按实际版本修改下面的包名。

```
sudo mkdir -p /home/yashan/install 
sudo tar -zxvf /data/yashan/soft/yashan/soft/yashandb/23.4.6.100/yashandb-23.4.6.100-linux-x86_64.tar.gz  -C /home/yashan/install
sudo chown -R yashan:yashan /home/yashan/install 
```



### 4.2.4 生成配置文件

**配置文件说明：** 命令会在当前目录下生成hosts.toml和yashandb.toml两个配置文件。**文件内容：**
- hosts.toml：包含所有节点的连接信息（IP、端口、用户名、密码等）
- yashandb.toml：包含数据库集群的配置信息（路径、参数、网络等）

**配置检查：** 生成配置文件后，建议先检查配置文件内容是否正确，特别是IP地址、端口、路径、磁盘设备等关键信息，避免配置错误导致安装失败。

#### 4.2.4.1 VIP模式

在VIP模式中，每台服务器需要分配VIP地址，VIP地址与业务网段相同，并且需要确保VIP地址没有被使用。**VIP模式说明：** VIP（Virtual IP）模式是YAC集群的一种访问方式，客户端通过VIP连接到集群，VIP会在主节点上，当主节点故障时，VIP会自动切换到其他节点，实现高可用。

```
cd /home/yashan/install/ && /home/yashan/install/bin/yasboot package ce gen --cluster yashandb \
-u yashan -p 'aaBB11@@33$$' --ip 10.10.10.150,10.10.10.151 --port 22 \
--install-path /data/yashan/yasdb_home \
--data-path /data/yashan/yasdb_data \
--log-path /data/yashan/log \
--begin-port 1688  \
--node 2 \
--data /dev/mapper/data1 \
--disk-found-path /dev/mapper \
--system-data /dev/mapper/sys1 \
--inter-cidr 10.10.10.0/24 \
--public-network 10.10.10.0/24/ens192 \
--vips 10.10.10.153/24,10.10.10.154/24
```

#### 4.2.4.2 SCAN模式

SCAN（Single Client Access Name）模式是YAC集群的另一种访问方式，客户端通过SCAN名称连接到集群，通过DNS解析到各个数据库节点。
**SCAN模式说明：** SCAN模式需要配置DNS或hosts文件解析SCAN名称到多个IP地址，相比VIP模式，SCAN模式提供了更好的负载均衡能力。服务器添加DNS域名解析配置参考附录。

```
nslookup yashan.cdhtz.com
```
确认YAC所有节点都可以解析域名，如不能解析，参考附录《**添加DNS服务器**》

```
cd /home/yashan/install/ && /home/yashan/install/bin/yasboot package ce gen --cluster yashandb \
-u yashan -p 'aaBB11@@33$$' --ip 10.10.10.150,10.10.10.151 --port 22 \
--install-path /data/yashan/yasdb_home \
--data-path /data/yashan/yasdb_data \
--log-path /data/yashan/log \
--begin-port 1688  \
--node 2 \
--data /dev/mapper/data1 \
--disk-found-path /dev/mapper \
--system-data /dev/mapper/sys1 \
--inter-cidr 10.10.10.0/24 \
--public-network 10.10.10.0/24/ens192 \
--vips 10.10.10.153/24,10.10.10.154/24 \
--scanname yashan.cdhtz.com
```



### 4.2.5 安装软件

YashanDB数据库软件依赖SSL库文件，在部分操作系统中需要自定义SSL库文件。**判断是否需要指定SSL依赖包：** 如果操作系统自带的SSL库版本不兼容或缺失，需要在安装时指定SSL依赖包。通常UOS V20和麒麟V10不需要指定，其他操作系统需要根据实际情况判断。

#### 4.2.5.1 不指定SSL依赖包

```
cd /home/yashan/install && /home/yashan/install/bin/yasboot package install -t /home/yashan/install/hosts.toml
```

安装完成后，会自动启动yasom和yasagent两个进程，om进程是全局共享的。

#### 4.2.5.2 指定SSL依赖包

```
cd /home/yashan/install && /home/yashan/install/bin/yasboot package install -t ~/hosts.toml -i /data/yashan/soft/yashan/soft/yashandb/23.4.6.100/yashandb-23.4.6.100-linux-x86_64.tar.gz --deps /data/yashan/soft/yashan/soft/yashandb/23.4.6.100/yashandb-deps-23.4.4.100-linux-x86_64.tar.gz
```

### 4.2.6 创建数据库

**创建前检查：** 在创建数据库前，需要确认两个重要内容，这些配置在数据库创建后无法修改，需要谨慎选择。

**1. 数据库字符集（CHARACTER_SET）**

参数默认值为UTF8，建议规范是保持与源端数据库字符集一致，避免字符集转换带来的性能损失和数据问题。**字符集映射规则：**
- Oracle:ZHS16GBK → YashanDB:GB18030
- Oracle:AL32UTF8 → YashanDB:UTF8
- Dameng:GB18030 → YashanDB:GB18030

针对新开发系统或者没有对应规则的字符集场景中，联系组长，内部确认字符集的配置。

**2. 原生字段类型（USE_NATIVE_TYPE）**

参数默认值为true。**配置原则：**
- 如源库是Oracle并且需要实现零代码修改时，建议统一采用false配置，使用Oracle兼容的数据类型
- 其它所有的场景保持默认true的配置，使用YashanDB原生数据类型，获得更好的性能





#### 4.2.6.1 配置YFS参数

YFS（YashanDB File System）是YashanDB的共享文件系统，用于YAC集群中节点之间的数据共享。下面配置为生产环境推荐配置，如果POC或者测试环境可以不用配置，使用默认值即可。**注意：** 这些参数会影响集群的性能和稳定性，建议根据实际业务需求调整。

```
sed -i 's/au_size.*/au_size = "32M"/' /home/yashan/install/yashandb.toml
sed -i 's/REDO_FILE_SIZE.*/REDO_FILE_SIZE = "1G"/' /home/yashan/install/yashandb.toml
sed -i 's/REDO_FILE_NUM.*/REDO_FILE_NUM= 6/' /home/yashan/install/yashandb.toml
sed -i 's/SHM_POOL_SIZE .*/SHM_POOL_SIZE = "2G"/' /home/yashan/install/yashandb.toml
sed -i 's/ABC/    MAXINSTANCES = 64/' /home/yashan/install/yashandb.toml
```



#### 4.2.6.2 修改字符集

yashandb.toml配置文件中的CHARACTER_SET为字符集配置信息，如需自定义字符集，请手动修改。

```
grep CHARACTER_SET yashandb.toml
    CHARACTER_SET = "utf8"
```

**重要限制：** YashanDB暂时不支持修改字符集，只能在创建实例时配置，因此创建前必须确认字符集选择正确。



#### 4.2.6.3 修改原生字段类型

USE_NATIVE_TYPE参数默认不在yashandb.toml中，如需修改，直接在yashandb.toml中添加参数，参数需要添加在[group.node.config]下面。

```
USE_NATIVE_TYPE = false
```



#### 4.2.6.4 创建数据库

在安装用户yashan下执行下面命令，此命令会创建数据库集群。**说明：** 在YAC环境中，此命令会在所有节点上创建数据库实例，并自动配置集群关系。如果是主从环境，还会自动搭建主从关系。

```
/home/yashan/install/bin/yasboot cluster deploy -t /home/yashan/install/yashandb.toml -p  'Yashan1!'
```

**注意：** 创建数据库过程可能需要较长时间，特别是大数据量的场景。可以通过查看日志文件监控创建进度。

创建完成后，可以执行下面的命令查看集群状态：

```
/home/yashan/install/bin/yasboot cluster status -c yashandb -d
```

确认输出中主库 `database_role` 为 `primary`、备库为 `standby`，且 `database_status` 为 `normal` 即可。





### 4.2.7 配置环境变量



需要在所有节点上执行下面命令，下面文件yashandb关键字是集群的名字，如果yasboot命令自定义了集群的名字，这里文件需要做对应修改。ce-1-1名字在每个节点不一样，请手动替换成对应路径的名字。

```
cat ~/.yasboot/yashandb_yasdb_home/conf/yashandb.bashrc >> ~/.bashrc
cat >> ~/.bashrc <<'HTZ'
export YASCS_HOME=/data/yashan/yasdb_data/ycs/ce-1-1
HTZ
source ~/.bashrc && which yasboot
```

确认yasboot能找到即可，同时确认yasql / as sysdba能正常登录数据库。

```
yasql / as sysdba
```



### 4.2.8 查看集群状态

```
yasboot cluster status -c yashandb -d 
```

确认各节点 `instance_status` 为 `open`，且 `database_status` 为 `normal` 即可。**状态说明：** `instance_status` 为 `open` 表示实例已打开并可以接受连接；`database_status` 为 `normal` 表示数据库运行正常。如果状态异常，需要查看日志文件排查问题。



# 5 实例优化（可选）

实例优化将对数据库参数、数据文件、日志等进行优化，使得数据库运行的效率更高、稳定性更好。本章节对POC项目、临时测试的项目为可选项，对生产上线的项目为必选项。本章节中配置的值是已经上线项目中的配置，可以借鉴参考，但请不要完全复制使用，需要根据每一套系统硬件配置、业务负载等信息进行综合考虑后再配置。

## 5.1 参数优化

### 5.1.1 内存参数

内存参数的修改需要根据生产环境物理内存、业务并发、数据量等因素综合考虑。**内存配置原则：** 

以下参数配置来自实际项目经验，仅供参考，需要根据实际情况调整。

来自UCIS项目
```
alter system set data_buffer_size = 30g        scope=spfile;
alter system set vm_buffer_size = 6g		   scope=spfile;
alter system set work_area_pool_size = 256m    scope=spfile;
alter system set share_pool_size = 8g          scope=spfile;
alter system set lock_pool_size = 512m         scope=spfile;
```

来自某YAC项目
```
alter system set work_area_heap_size = 1m      scope=spfile;
alter system set work_area_stack_size = 2m     scope=spfile;
```
### 5.1.2 REDO参数

REDO日志用于记录数据库的所有变更操作，是数据库恢复的关键。**REDO参数配置原则：**
- redo_buffer_size：REDO缓冲区大小，建议根据事务量配置，过小会导致频繁刷新，过大可能浪费内存
- redo_buffer_parts：REDO缓冲区分区数，建议配置为8或16，可以提高并发写入性能

以下参数配置来自UCIS项目，仅供参考：
```
alter system set redo_buffer_size = 64m        scope=spfile;
alter system set redo_buffer_parts = 8         scope=spfile;
```

### 5.1.3 UNDO与归档参数

UNDO表空间用于存储事务回滚信息，归档日志用于数据恢复和主备同步。**参数配置说明：**
- checkpoint_interval：检查点间隔，影响恢复时间，建议根据业务容忍度配置
- undo_retention：UNDO保留时间，建议根据最长查询时间配置
- arch_clean_upper_threshold：归档清理阈值，建议根据磁盘空间配置

以下参数配置来自UCIS项目，仅供参考：
```
alter system set  checkpoint_interval = 256m           scope=spfile;
alter system set  undo_shrink_interval = 10800         scope=spfile;
alter system set  undo_retention = 7200                scope=spfile;
alter system set  arch_clean_upper_threshold = 50g     scope=spfile;
alter system set  arch_clean_ignore_mode=backup        scope=spfile;
```

### 5.1.4 其它参数

来自UCIS项目
```
alter system set  unified_auditing = true             scope=spfile;
alter system set  startup_rollback_parallelism = 16   scope=spfile;
alter system set  open_cursors = 1000                 scope=spfile;
alter system set  slow_log_time_threshold = 1000      scope=spfile;
alter system set  recovery_parallelism = 16           scope=spfile;
alter system set  max_sessions = 2000                 scope=spfile;
alter system set  max_parallel_workers = 44           scope=spfile;
alter system set  DATE_FORMAT='yyyy-mm-dd hh24:mi:ss' scope=spfile;
```

### 5.1.5 锁相关参数

```
alter system set transaction_lock_timeout = 5 scope=spfile;
alter system set ddl_lock_timeout = 30        scope=spfile;
```
### 5.1.6 特殊参数

来自某YAC项目
```
alter system set stream_pool_size = 50 scope=spfile;
```

## 5.2 数据文件优化

**UNDO表空间配置：** UNDO表空间用于存储事务回滚信息，建议根据业务实际需要配置大小。**配置原则：** 每个表空间的数据文件个数建议配置在2个以上，可以提高IO并发性能。UNDO表空间大小建议为数据库总大小的10-20%。

**TEMP表空间配置：** TEMP表空间用于存储临时数据（如排序、哈希等操作），建议根据业务查询特点配置。**配置原则：** 每个表空间的数据文件个数建议配置在2个以上，可以提高IO并发性能。TEMP表空间大小建议根据最大查询的内存需求配置。

**重要限制：** YashanDB的单个数据文件最大大小为512G，所以针对UNDO、TEMP表空间的数据文件一定要限制每个数据文件的最大大小，避免单个文件过大导致管理困难。



## 5.3 日志优化

**REDO日志文件大小配置原则：** 生产环境中REDO日志文件大小配置原则是尽可能在5~10分钟切换一次日志，这样既可以减少日志切换对数据库带来的影响，也能保证日志尽早归档，防止日志损坏带来的数据丢失。**计算公式：** 日志文件大小 = (平均每秒日志生成量 × 600秒) / 日志文件数量

**REDO日志文件个数配置原则：** 日志文件个数的配置原则是在业务高峰期不能出现全部日志文件用完而导致数据库无法切换日志的情况，这需要根据每秒生成的日志大小、IO性能、日志文件大小来核算。**建议：** 一般的业务系统，每个实例建议配置6个日志文件；高并发系统建议配置8-10个日志文件。

单机环境
```
ALTER DATABASE ADD LOGFILE '?/dbfiles/redo05' SIZE 2G;
```

YAC环境需要给每个实例添加日志文件
```
ALTER DATABASE ADD LOGFILE THREAD 2 '?/dbfiles/redo05' SIZE 2G;
ALTER DATABASE ADD LOGFILE THREAD 1 '?/dbfiles/redo05' SIZE 2G;
```

对于不满足要求的日志文件（如文件太小或数量不足），需要先删除旧的日志文件，然后添加新的日志文件。**注意：** 删除日志文件前需要确保数据库处于正常状态，且归档日志已备份。

## 5.4 Profile优化

**说明：** Profile用于管理用户密码策略和资源限制。默认failed_login_attempts的参数值过低，很容易就导致账户被锁，影响业务的运行。**建议：** 生产环境建议修改为不限制（UNLIMITED），但需要配合其他安全措施（如强密码策略、网络访问控制等）来保证安全性。

```
alter profile default limit failed_login_attempts UNLIMITED;
```


## 5.5 审计优化

**审计功能说明：** YashanDB支持统一审计功能，可以记录数据库的各种操作，包括登录、DDL、DML等。审计功能需要根据每一个项目的安全要求确认是否开启。**注意事项：** 开启审计功能会增加系统开销，需要合理配置审计策略，避免审计日志过多影响性能。同时需要定期清理审计日志，避免占用过多磁盘空间。

以下是一个实际项目的审计配置示例，来自UCIS项目，仅供参考：

#### 5.5.1.1 开启和创建审计策略
```
alter system set UNIFIED_AUDITING=true;
CREATE AUDIT POLICY UP1 PRIVILEGES CREATE ANY TABLE, CREATE TABLE, ALTER ANY TABLE, DROP ANY TABLE, GRANT ANY PRIVILEGE, GRANT ANY OBJECT PRIVILEGE, GRANT ANY ROLE, CREATE USER, ALTER USER, DROP USER, DROP ANY ROLE, AUDIT SYSTEM;
CREATE AUDIT POLICY UP2 ACTIONS DROP TABLE, DROP ROLE, CREATE AUDIT POLICY, ALTER AUDIT POLICY, DROP AUDIT POLICY, AUDIT, NOAUDIT;
CREATE AUDIT POLICY UP3 ACTIONS LOGON, LOGOFF;
AUDIT POLICY UP3 BY SYS;
AUDIT POLICY UP1;
AUDIT POLICY UP2;
```


#### 5.5.1.2 配置审计清理策略

```
BEGIN
DBMS_SCHEDULER.CREATE_JOB (
'update_audit_archive_time',
'PLSQL_BLOCK',
'BEGIN DBMS_AUDIT_MGMT.SET_LAST_ARCHIVE_TIMESTAMP(DBMS_AUDIT_MGMT.AUDIT_TRAIL_UNIFIED, sysdate-270);END;' ,
0,
SYSDATE,
'sysdate+1',
NULL,
'DEFAULT_JOB_CLASS',
TRUE,
FALSE,
'update audit archive time');
END;
/
BEGIN
DBMS_AUDIT_MGMT.CREATE_PURGE_JOB (
DBMS_AUDIT_MGMT.AUDIT_TRAIL_UNIFIED,
SYSDATE + 5/24,
'sysdate + 1',
'audit_job',
TRUE);
END;
/
```



# 6 添加备库

## 6.1 单机扩容

**扩容说明：** 主备扩容就是在当前的环境中再增加一个备库，实现主备高可用架构。**扩容步骤：** 此步骤包含两个过程：
1. 节点扩容：在新服务器上安装YashanDB软件，加入集群
2. 实例扩容：在新节点上创建备库实例，与主库建立主备关系

**扩容前提：** 扩容前需要确保新节点的操作系统配置已完成，参考第3章内容。

### 6.1.1 操作系统准备

操作系统环境配置详细参考第3章内容。

### 6.1.2 确认主库运行状态

**状态检查：** 在当前正常运行的节点上执行下面命令，查看当前的架构和运行状态，确认主库运行正常，为扩容做准备。

```
yasboot cluster status -c yashandb  -d
```

### 6.1.3 生成扩容配置文件

**配置文件生成：** 在当前主库环境中运行下面命令，生成扩容所需的配置文件。

**参数说明：**
- --ip：被添加节点的IP地址，如果多个服务器，用逗号间隔
- --node：被添加节点的个数

**注意：** 确保新节点的IP地址正确，且新节点可以通过SSH访问。

```
cd /home/yashan/install 
yasboot config node gen \
-c yashandb -u yashan -p 'aaBB11@@33$$' \
--ip 10.10.10.151 --port 22 \
--install-path /data/yashan/yasdb_home \
--data-path /data/yashan/yasdb_data \
--log-path /data/yashan/log \
--node 1
```

会生成两个配置文件，分别是：
- hosts_add.toml：包含新增节点的连接信息
- yashandb_add.toml：包含新增节点的数据库配置信息

**注意：** 生成配置文件后，建议先检查配置文件内容是否正确，特别是IP地址、端口、路径等关键信息。

### 6.1.4 安装软件

**软件安装：** 在当前主库环境中运行下面命令，将YashanDB软件安装到新节点。

```
cd /home/yashan/install  && yasboot host add -c yashandb -t /home/yashan/install/hosts_add.toml
```

**SSL依赖处理：**

如果存在SSL依赖文件，在添加节点时需要加上`--deps`参数来指定SSL依赖包路径，解决SSL库兼容性问题。

```
cd /home/yashan/install && yasboot host add -c yashandb -t /home/yashan/install/hosts_add.toml --deps /tmp/yashandb-deps-23.4.4.100-linux-x86-64.tar.gz
```

**验证安装：** 添加完成后，可以通过下面命令来查看每个节点的信息，确认所有节点的yasagent进程都正常运行。

```
yasboot process yasagent status -c yashandb
```

### 6.1.5 添加备库实例

**实例扩容：** 扩容实例前需要先生成配置文件（参考6.1.3节），然后执行实例扩容命令。

**扩容过程说明：** 扩容备库实例会自动同步数据，虽然命令返回成功了，但是后台还在继续做数据备份和还原。**扩容时间：** 扩容过程包括数据备份、传输、还原等步骤，对于大数据量的数据库，这个过程可能需要较长时间。可以通过查看日志文件监控扩容进度。

```
cd /home/yashan/install  && yasboot node add -c yashandb -t /home/yashan/install/yashandb_add.toml 
```

**重要说明：** 扩容备库实例会自动同步数据，虽然命令返回成功了，但是后台还在继续做数据备份和还原。**扩容过程说明：** 扩容过程包括数据备份、传输、还原等步骤，对于大数据量的数据库，这个过程可能需要较长时间。可以通过查看日志文件监控扩容进度。扩容过程中执行的核心命令如下：

1，主库上面重建远程实例

```
BUILD DATABASE TO REMOTE ('10.10.10.141:1688');
```

2，主库修改对应的参数

```
ALTER SYSTEM SET ARCHIVE_DEST_1='10.10.10.141:1688' scope=both;
ALTER SYSTEM SET DB_BUCKET_NAME_CONVERT='/data/yashan/yasdb_data/db-1-2','/data/yashan/yasdb_data/db-1-1' scope=both;
ALTER SYSTEM SET REDO_FILE_NAME_CONVERT='/data/yashan/yasdb_data/db-1-2','/data/yashan/yasdb_data/db-1-1' scope=both;
ALTER SYSTEM SET DB_FILE_NAME_CONVERT='/data/yashan/yasdb_data/db-1-2','/data/yashan/yasdb_data/db-1-1' scope=both;
```

**故障处理：** 在扩容的过程中如果有报错，导致扩容中断，需要先清理已创建的资源，然后重新扩容。清理命令如下（注意：此操作会删除已创建的备库实例，请谨慎操作）：

```
yasboot node remove -c yashandb -n 1-2 --clean
```



### 6.1.6 配置环境变量

需要在所有节点上执行下面命令，下面文件yashandb关键字也是集群的名字，如果yasboot命令自定义了集群的名字，这里文件需要做对应修改。

```
cat ~/.yasboot/yashandb_yasdb_home/conf/yashandb.bashrc > ~/.bashrc
source ~/.bashrc && which yasboot
```

确认yasboot能找到即可，同时确认yasql / as sysdba能正常登录数据库。

```
yasql / as sysdba
```





# 7 开机自启

## 7.1 数据库开启自启

开机自启是通过monit进程来实现的。**monit说明：** monit是YashanDB的监控进程，负责监控数据库进程状态，当进程异常退出时会自动重启。通过systemd服务配置monit脚本，可以实现数据库开机自动启动。

### 7.1.1 配置启停脚本

下面的自动启停脚本已经实现单机和YAC同一个脚本。**脚本说明：** 脚本会定期检查monit和ycsrootagent进程状态，如果进程异常退出会自动重启。如果是RAC（分布式集群）环境，需要修改YCSROOTAGENT_AUTOSTART为TRUE，启用ycsrootagent的自动启动功能。

```
sudo sh -c "cat  > /usr/local/bin/yashan_monit.sh" <<'HTZ'
#!/bin/bash
if [ $# -eq 0 ]; then
    echo "Usage: $0 <PORT> or $0 bashrc"
    echo "Example: $0 1788"
    echo "Example: $0 bashrc"
    exit 1
fi

MONIT_AUTOSTART="true"                
YCSROOTAGENT_AUTOSTART="false"
YASDB_USER=yashan                              
INTERVAL=3                              
PORT=$1

ENV_FILE="/home/${YASDB_USER}/.${PORT}"
if [ -f "$ENV_FILE" ]; then
    source "$ENV_FILE"
fi

if [ -z "$YASDB_HOME" ]; then
    echo "$(date) Error: YASDB_HOME is not set. Please check $ENV_FILE"
    exit 1
fi

export LD_LIBRARY_PATH=$YASDB_HOME/lib

MONITRC_FILE="$YASDB_HOME/om/monit/monitrc"
if [ -f "$MONITRC_FILE" ]; then
    CURRENT_OWNER=$(stat -c '%U' "$MONITRC_FILE" 2>/dev/null || stat -f '%Su' "$MONITRC_FILE" 2>/dev/null)
    if [ "$CURRENT_OWNER" != "$YASDB_USER" ]; then
        echo "$(date) Fixing monitrc owner: $CURRENT_OWNER -> $YASDB_USER"
        chown $YASDB_USER "$MONITRC_FILE"
    fi
    
    CURRENT_PERM=$(stat -c '%a' "$MONITRC_FILE" 2>/dev/null)
    if [ -z "$CURRENT_PERM" ]; then
        PERM_STR=$(ls -ld "$MONITRC_FILE" 2>/dev/null | awk '{print $1}')
        if [ -z "$PERM_STR" ]; then
            CURRENT_PERM="000"
        else
            CURRENT_PERM="000"
        fi
    fi
    if [ "$CURRENT_PERM" != "700" ]; then
        echo "$(date) Fixing monitrc permissions: $CURRENT_PERM -> 700"
        chmod 0700 "$MONITRC_FILE"
    fi
else
    echo "$(date) Warning: $MONITRC_FILE not found"
    exit 1
fi

while true; do    
    if [ "$MONIT_AUTOSTART" = "true" ]; then
        if ! pgrep -a monit | grep "$YASDB_HOME" > /dev/null; then
            echo "$(date) monit abnormal, try restart..." 
            su - $YASDB_USER -c "source /home/${YASDB_USER}/.${PORT} && $YASDB_HOME/om/bin/monit -c $YASDB_HOME/om/monit/monitrc" &
        fi
    fi
    if [ "$YCSROOTAGENT_AUTOSTART" = "true" ]; then
       if [ -n "$YASCS_HOME" ]; then
         if ! pgrep -a ycsrootagent | grep "$YASCS_HOME" > /dev/null; then
           echo "$(date) ycsrootagent abnormal, try restart..."
           su - $YASDB_USER -c "source /home/${YASDB_USER}/.${PORT} &&  sudo env LD_LIBRARY_PATH=$YASDB_HOME/lib $YASDB_HOME/bin/ycsrootagent start -H $YASCS_HOME" & 
         fi
       fi
    fi
    sleep "$INTERVAL"
done
HTZ

sudo chmod +x /usr/local/bin/yashan_monit.sh
```

### 7.1.2 配置启停服务

**端口配置说明：** 如果有自定义端口，下面脚本中yashan_monit.sh bashrc的bashrc需要替换成具体的端口号。脚本会根据端口号查找对应的环境变量文件（如/home/yashan/.1688），加载相应的环境变量。


```
sudo sh -c "cat > /etc/systemd/system/yashan_monit.service" << 'HTZ'

[Unit]
Description=Yashan Monitor
After=network.target

[Service]
ExecStart=/usr/local/bin/yashan_monit.sh bashrc
Type=simple
Restart=always
StandardOutput=syslog
StandardError=syslog

[Install]
WantedBy=multi-user.target
HTZ

```

启用服务

```
sudo systemctl daemon-reload
sudo systemctl enable yashan_monit
sudo systemctl start yashan_monit
```

查看服务的状态：

```
sudo systemctl status yashan_monit
```



## 7.2 YCM开启自启

### 7.2.1 创建YCM启动服务

YCM启动服务目前只实现了服务器启动、关闭功能，后续考虑进程监控功能。

```
sudo sh -c "cat > /etc/systemd/system/ycm.service" << 'HTZ'
[Unit]
Description=Yashan Cloud Manager  Service
After=network.target

[Service]
Type=forking

ExecStart=/opt/ycm/ycm/scripts/yasadm ycm start
ExecStop=/opt/ycm/ycm/scripts/yasadm ycm stop
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
HTZ
```



### 7.2.2 启动服务

```
sudo systemctl daemon-reload
sudo systemctl enable ycm
sudo systemctl start ycm
sudo systemctl status ycm
```



## 7.3 YMP开机自启

YMP服务为迁移过程中临时使用，暂时未考虑开机自启。



# 8 数据库运维

## 8.1 用户管理

**用户管理原则：** 数据库用户管理需要遵循最小权限原则，只授予用户必要的权限，降低安全风险。

### 8.1.1 创建业务用户

**密码策略：** 创建业务用户时，密码建议大于8位，并且包含大小写及特殊字符，提高密码强度。**权限原则：** 用户权限建议只授予connect、resource、alter session、create view权限，不要授予dba权限。**DBA权限说明：** 如果需要授权dba权限，需要向甲方说明dba权限可能会带来数据泄露、误操作等风险，如甲方确认风险，经甲方同意后再授权dba权限。
```
create user htz identified by "Yashan1!";
grant connect to htz;
grant resource to htz;
grant alter session to htz;
grant create view to htz;
```



# 9 YCM安装

**YCM说明：** YCM（YashanDB Cloud Manager）是YashanDB的云管理平台，提供数据库的监控、管理、运维等功能。

**架构组成：** YCM软件主要包含两部分内容：
- YCP应用软件：提供Web界面和API接口
- 数据库：存储YCM的元数据和监控数据，支持YashanDB和sqlite两种

**数据库选择：** 如果需要使用YashanDB作为YCM的后端数据库，请提前安装YashanDB数据库，具体参数参考前面安装文档。使用YashanDB可以获得更好的性能和可扩展性。

## 9.1 操作系统配置

**配置说明：** YCM的操作系统配置与数据库安装类似，但有一些特殊要求。操作系统配置详细参考第3章操作系统配置内容，下面只列出YCM特有的配置项。

- **创建安装用户**

YCM支持自定义安装用户，如非特殊原因，建议安装在yashan用户下。
```
/usr/sbin/groupadd -g 701 yashan
/usr/sbin/groupadd -g 702 YASDBA
/usr/sbin/useradd -u 701 -g yashan  -G YASDBA -m  yashan  -s /bin/bash
echo 'aaBB11@@33$$'|passwd yashan --stdin
```
- **配置用户的sudo**

注意：这里修改了/etc/sudoers的权限为400，防止文件权限过大，Linux操作系统认为有风险，会去掉所有的配置用户的超级sudo超级权限。
```
chmod +w /etc/sudoers  
echo "yashan  ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers  
chmod -w /etc/sudoers  
chmod 400 /etc/sudoers
```
- **禁用防火墙**

如果客户对防火墙无特殊要求，可以在操作系统上关闭防火墙
```
systemctl stop firewalld  
systemctl disable firewalld
```
- **安装libnsl及常用工具包软件**

  ```
  yum -y install --disablerepo=\* --enablerepo=local   libnsl
  ```

- **时区配置**
时区统一配置为上海
```
timedatectl set-timezone "Asia/Shanghai"
```
- **配置umask**

```
echo "umask 022" >> /home/yashan/.bashrc
```

- **配置时间同步**

时钟服务器客户端配置，将203.167.6.88替换成客户具体的NTP服务器的地址。

```
cp /etc/chrony.conf /etc/chrony.conf.bak_$(date +%F)  
​  
cat > /etc/chrony.conf << 'HTZ'  
# 7 Aliyun NTP server (IP)  
server 203.167.6.88 iburst  
allow 0.0.0.0/0  
makestep 1.0 3  
driftfile /var/lib/chrony/drift  
rtcsync  
logdir /var/log/chrony  
HTZ  
​  
​  
systemctl restart chronyd && systemctl enable chronyd  
chronyc makestep && chronyc tracking
```

- 确认安装目录

默认建议安装在/opt目录下，用户也可自定义安装目录。


## 9.2 安装YCM软件

### 9.2.1 解压软件

在yashan用户下面执行

```
sudo tar -zxf /data/yashan/soft/yashan/soft/ycm/yashandb-cloud-manager-23.5.3.2-linux-x86_64.tar.gz  -C /opt
sudo chown -R yashan:yashan /opt/ycm
```


### 9.2.2 安装

**配置文件说明：** 配置文件的路径默认在/opt/ycm/etc/deploy.yml，用户可以自行修改。**配置内容：** deploy.yml包含YCM的数据库连接信息、端口配置、路径配置等，需要根据实际环境修改。

### 9.2.3 端口检查

**端口说明：** YCM默认会用到如下端口信息，如果主机配置了防火墙功能，需要开通对应端口的访问。**端口用途：**
- ycm_port：YCM Web服务端口
- prometheus_port：Prometheus监控数据端口
- loki_http_port/loki_grpc_port：Loki日志服务端口
- yasdb_exporter_port：数据库指标导出端口

```
    ycm_port: 9060
    prometheus_port: 9061
    loki_http_port: 9062
    loki_grpc_port: 9063
    yasdb_exporter_port: 9064
```
端口号的配置信息已经写入在deploy.yml中，如果用户需要修改，可以自行修改，但是一定需要确认端口号没有被提前占用，下面是检查端口号的命令：
```
for p in $(grep -E 'ycm_port|prometheus_port|loki_http_port|loki_grpc_port|yasdb_exporter_port' \
    /opt/ycm/etc/deploy.yml | awk -F: '{print $2}' | tr -d ' '); do
    info=$(ss -tulnp | grep -w ":$p")
    if [ -n "$info" ]; then
        echo "$p USED $info"
    else
        echo "$p FREE"
    fi
done
```

也可以用下面的信息：

```
grep -E 'ycm_port|prometheus_port|loki_http_port|loki_grpc_port|yasdb_exporter_port' \
    /opt/ycm/etc/deploy.yml | awk -F: '{print $2}' | tr -d ' '|xargs -I {} netstat -anp |grep {}
```

### 9.2.4 安装软件

**安装方式选择：** YCM支持两种数据库后端，根据选择的数据库类型执行不同的安装命令。

- **sqlite3数据库**

  **适用场景：** sqlite方式适合测试环境或小规模部署，配置简单，无需额外数据库。**配置说明：** sqlite方式暂时不需要修改deploy.yml内容，使用默认配置即可。

  yashan用户执行下面的命令：

  ```bash
  sudo /opt/ycm/ycm-init deploy --conf /opt/ycm/etc/deploy.yml
  ```

  **日志查看：** 对应的日志输出可以参考第11章日志记录章节。

- **yashan数据库**

  **适用场景：** 使用YashanDB作为后端数据库适合生产环境，可以获得更好的性能和可扩展性。

  **配置修改：** 需要修改deploy.yml中yashan数据库配置的信息，如下：

  ```
  dbconfig:
      driver: "yashandb"  # 后端为yashanDB时，必须指定为yashandb
      url: "192.168.18.177:1688,192.168.18.178:1688,192.168.18.179:1688"   # 包括数据库的所有主备实例及监听端口 
      libPath: "/home/yashan/yashandb-client/lib" # 使用产品内置客户端无需配置，或者配置为自行安装的客户端lib路径   
  ```

  **安装命令：** 执行下面的安装命令，需要提供YashanDB的管理员用户名和密码。

  ```
  sudo /opt/ycm/ycm-init deploy --conf /opt/ycm/etc/deploy.yml --username yasman --password password
  ```

## 9.3 访问页面

**访问地址：** 安装完成后，可以通过浏览器访问YCM的Web界面。

```
http://10.10.10.150:9060
```

**登录信息：** 默认的用户名为admin，密码为admin。**安全建议：** 首次登录需要修改密码，建议使用强密码，提高安全性。

#### 9.3.1.1 服务确认

- 端口确认

  ```
  sudo netstat -anp|grep 9060  
  ```

- 服务进程确认

  ```
  ps -ef|grep "/opt/ycm"
  ```

##### 9.3.1.1.1 服务的重启

```
/opt/ycm/ycm/scripts/yasadm ycm stop
/opt/ycm/ycm/scripts/yasadm ycm start
```



# 10 YMP安装

## 10.1 YMP架构介绍
YMP由YMP应用和YMP对应的数据库构成。**架构说明：** YMP在迁移过程中，会在YMP内置数据库中创建需要迁移的对象元数据信息，用于管理迁移任务和存储迁移结果。**数据库选择建议：** 建议安装YMP时，YMP内置数据库与YMP迁移目标数据库保持一致（都使用YashanDB），这样可以避免字符集、数据类型等兼容性问题，提高迁移效率。

### 10.1.1 迁移原理

**迁移流程说明：** YMP的迁移过程分为多个阶段，了解迁移原理有助于优化迁移策略和排查问题。

**迁移步骤：**
1. 首先创建除索引、约束、和触发器的所有对象DDL（表、视图、序列、存储过程等）
2. 然后抽取数据，写到$YMP_HOME/tmp/大写用户名_大写表名/uuid.CSV文件，最后对每个uuid.csv文件通过yasldr导入到目标库
3. 最后创建索引、约束、和触发器
4. 迁移任务的配置存储在YMP_DEFAULT.MIGRATION_TASK表中，迁移结果存储在YMP_DEFAULT.MIGRATION_TASK_OBJECT表中

**优化建议：** 对于大数据量的迁移，建议调整并行度、批次大小等参数，提高迁移效率。

## 10.2 迁移用户准备

### 10.2.1 Oracle源端权限配置

详细的权限配置参考[官方文档](https://doc.yashandb.com/ymp/23.5/zh/Full-Migration/Oracle-Migration-to-YashanDB/Preparation-Before-Migration/Data-Source-Configuration.html)。下面给出满足源端所有场景需要的权限，将username替换为YMP登录Oracle的用户。

```
GRANT CREATE SESSION TO username;
GRANT SELECT ON DBA_OBJECTS TO username;
GRANT SELECT ON DBA_EXTENTS TO username;
GRANT SELECT ON DBA_SEGMENTS TO username;
GRANT SELECT ON DBA_LOBS TO username;
GRANT SELECT ANY TABLE TO username; 
GRANT SELECT ANY SEQUENCE TO username; 
GRANT SELECT ON DBA_TAB_COLS TO username;
GRANT SELECT ON DBA_SCHEDULER_JOBS TO username; 
GRANT SELECT ON DBA_MVIEWS TO username;
GRANT SELECT ON V_$PARAMETER TO username;
GRANT SELECT ON V_$SESSION TO username;
GRANT SELECT ON DBA_TABLES TO username;
GRANT SELECT ON DBA_TAB_COLUMNS TO username;
GRANT SELECT ON DBA_CONS_COLUMNS TO username;
GRANT SELECT ON DBA_CONSTRAINTS TO username;
```
## 10.3 YashanDB目标端权限配置

详细的权限配置参考[官方文档](https://doc.yashandb.com/ymp/23.5/zh/Full-Migration/Oracle-Migration-to-YashanDB/Preparation-Before-Migration/Data-Source-Configuration.html)。

```
grant  dba to username
```
## 10.4 操作系统准备

**配置说明：** 操作系统配置见第3章，这里只给出YMP需要单独配置的内容。YMP作为迁移工具，需要额外的Java环境和Oracle客户端库。

- 创建用户

  ```
  useradd ymp
  echo 'aaBB11@@33$$'|passwd ymp --stdin
  ```

- 资源限制

  ```
  echo "ymp soft nproc 65536
  ymp hard nproc 65536" >> /etc/security/limits.conf
  ```

- 安装软件

  ```
  yum -y install libaio lsof
  ```

- **安装JDK**

  **JDK要求：** YMP基于Java开发，需要安装JDK。直接去Oracle官方网站下载即可，目前支持的JDK版本有JDK8、JDK11、JDK17。**架构限制：** 如果是ARM架构，只支持JDK 11和17。

  ```
  rpm -ivh /data/yashan/soft/oracleinstall/jdk/jdk-11.0.28_linux-x64_bin.rpm 
  ```

- **安装目录规划**

  **目录选择：** YMP支持自定义安装目录，官方文档默认安装在用户的家目录中。**空间要求：** 在安装目录规划时，需要提供足够的空间用于软件安装和迁移过程中临时文件的生成（CSV文件、日志文件等），所以建议选择剩余容量大的目录空间。**建议：** 对于大数据量迁移，建议预留至少数据量2-3倍的空间用于临时文件。

## 10.5 安装YMP软件

**软件准备：** 上传软件到指定目录中，如果安装软件属主是root用户，需要将属主修改为YMP用户，确保YMP用户有权限访问和安装软件。

```
chown ymp:ymp /data/yashan/soft/yashan/soft/ymp/yashan-migrate-platform-23.5.3.2-linux-x86-64.zip 
chown ymp:ymp /data/yashan/soft/yashan/soft/ymp/instantclient-basic-linux.x64-19.29.0.0.0dbru.zip
chown ymp:ymp /data/yashan/soft/yashan/soft/yashandb/23.4.6.100/yashandb-23.4.6.100-linux-x86_64.tar.gz
```

### 10.5.1 清理环境

**清理说明：** 如果安装失败，需要手动清理下面文件，然后重新安装。**注意：** 清理前需要确保YMP进程已停止，避免文件被占用导致清理失败。
```
rm -rf /opt/ymp/yashan-migrate-platform/db/*
rm -rf  /home/ymp/.yasboot/ymp.env
rm -rf  /opt/ymp/yashan-migrate-platform
rm -rf  /opt/ymp/instantclient_19_10
```

如果删除时报进程正在使用，请手动kill对应的进程。

### 10.5.2 默认安装

**安装方式：** 默认安装会将YMP安装到用户的家目录中，适合测试环境。**解压软件：**

```
unzip /data/yashan/soft/yashan/soft/ymp/yashan-migrate-platform-23.5.3.2-linux-x86-64.zip -d ~
unzip /data/yashan/soft/yashan/soft/ymp/instantclient-basic-linux.x64-19.29.0.0.0dbru.zip -d ~
```

在ymp用户下执行安装命令：

```
sh ~/yashan-migrate-platform/bin/ymp.sh install --db /data/yashan/soft/yashan/soft/yashandb/23.4.6.100/yashandb-23.4.6.100-linux-x86_64.tar.gz --path ~/instantclient_19_29
```

### 10.5.3 自定义安装路径

**自定义安装：** 生产环境建议使用自定义安装路径，便于管理和维护。下面是自定义安装到/opt/ymp目录下的示例。
```
unzip /tmp/yashan-migrate-platform-23.4.11.2-linux-aarch64.zip -d /opt/ymp
unzip /tmp/instantclient-basic-linux.arm64-19.10.0.0.0dbru-2.zip  -d /opt/ymp
sh /opt/ymp/yashan-migrate-platform/bin/ymp.sh install --db /tmp/yashandb-23.4.4.100-linux-aarch64.tar.gz --path /opt/ymp/instantclient_19_10"
```


## 10.6 确认YMP服务是否可用

**服务验证：** 通过查看YMP进程和YMP端口号是否存在来确认YMP服务是否启动。**验证方法：** 如果进程和端口都存在，说明服务已正常启动；如果不存在，需要查看日志文件排查问题。
```
netstat -anp|grep 8090
ps -ef|grep ymp
```

如果服务不可用，可以在安装目录下执行ymp.sh来启动或者关闭服务

```
./bin/ymp.sh stop
./bin/ymp.sh start
```

## 10.7 部署sqlplus，远程登录数据库

**sqlplus说明：** sqlplus是Oracle的命令行工具，YMP在迁移过程中可能需要使用sqlplus连接Oracle数据库。**部署步骤：** 上面已经解压了Oracle instant客户端，下面是解压sqlplus的软件包，并配置环境变量。

```
unzip -qo /data/yashan/soft/yashan/soft/ymp/instantclient-sqlplus-linux.x64-19.29.0.0.0dbru.zip  -d ~

配置LIBRARY和PATH环境变量：
cat > /home/ymp/.oracle << 'HTZ'
export PATH=/home/ymp/instantclient_19_29:$PATH
export LD_LIBRARY_PATH=/home/ymp/instantclient_19_29:$LD_LIBRARY_PATH
HTZ
```



# 11 附录

**附录说明：** 本章节提供安装过程中可能用到的详细配置方法和参考信息，包括网络配置、系统优化等。



## 11.1 网卡绑定

**绑定技术选择：** RHEL7版本中建议采用BOND技术，8及后续的版本建议采用TEAM的技术。**绑定模式：** 主备的绑定模式，参考第2章《服务器硬件配置》，命名建议公网用bond0、集群私网用bond1、主备用bond2。**配置前准备：** 在进行网卡绑定前，需要先确认绑定前的物理网口的名字和绑定后的逻辑名字。下面是已物理网卡口名eth0和eth1、绑定后的逻辑名字bond0为例。

### 11.1.1 bond绑定技术

采用主备绑定技术，物理网卡的名字为eth0和eth1，绑定后的名字为bond0

```
echo "DEVICE=bond0
IPADDR=192.168.111.30
NETMASK=255.255.255.0
GATEWAY=192.168.111.1
ONBOOT=yes
BOOTPROTO=none
USERCTL=no
NM_CONTROLLED=no
IPV6INIT=no
BONDING_OPTS="mode=active-backup miimon=100 downdelay=5000 updelay=5000 num_grat_arp=100"">/etc/sysconfig/network-scripts/ifcfg-bond0

echo "DEVICE=eth0
BOOTPROTO=none
ONBOOT=yes
MASTER=bond0
SLAVE=yes
USERCTL=no
IPV6INIT=no
HOTPLUG=no
CONNECTED_MODE=yes
NM_CONTROLLED=no">/etc/sysconfig/network-scripts/ifcfg-eth0

echo "DEVICE=eth1
BOOTPROTO=none
ONBOOT=yes
MASTER=bond0
SLAVE=yes
USERCTL=no
IPV6INIT=no
HOTPLUG=no
CONNECTED_MODE=yes
NM_CONTROLLED=no">/etc/sysconfig/network-scripts/ifcfg-eth1
```



### 11.1.2 team技术

RHEL8以后版本建议使用TEAM，模式采用主从模式。例子物理网卡为eth2、eth3，聚合后的名字为team0

```
nmcli connection add type team ifname team0 con-name team0 config '{"runner": {"name": "activebackup"}, "link_watch": {"name": "ethtool", "delay_up": 5000, "delay_down": 5000}, "notify_peers": {"count": 100}}'
nmcli c add type team-slave master team0 ifname eth2 con-name eth2
nmcli c add type team-slave master team0 ifname eth3 con-name eth3 
nmcli c modify team0 ipv4.addresses 192.168.1.100/24
nmcli c modify team0 ipv4.gateway 192.168.1.1
nmcli c modify team0 ipv4.method manual
nmcli c modify team0 connection.autoconnect yes
nmcli c modify eth2 connection.autoconnect yes
nmcli c modify eth3 connection.autoconnect yes
nmcli c up team0
nmcli c up eth2
nmcli c up eth3

```

## 11.2 禁用服务

将部分不需要的服务禁用掉，禁用过程中会提示部分不存在的报错，直接忽略即可。
```
systemctl stop tuned.service
systemctl stop firewalld.service
systemctl stop postfix.service
systemctl stop avahi-daemon.socket
systemctl stop avahi-daemon.service
systemctl stop atd.service
systemctl stop bluetooth.service
systemctl stop wpa_supplicant.service
systemctl stop accounts-daemon.service
systemctl stop atd.service cups.service
systemctl stop postfix.service
systemctl stop ModemManager.service
systemctl stop debug-shell.service
systemctl stop rtkit-daemon.service
systemctl stop rpcbind.service
systemctl stop rngd.service
systemctl stop upower.service
systemctl stop rhsmcertd.service
systemctl stop rtkit-daemon.service
systemctl stop ModemManager.service
systemctl stop mcelog.service
systemctl stop colord.service
systemctl stop gdm.service
systemctl stop libstoragemgmt.service
systemctl stop ksmtuned.service
systemctl stop brltty.service
systemctl stop avahi-dnsconfd.service
systemctl stop firewalld

systemctl disable tuned.service 
systemctl disable ktune.service
systemctl disable firewalld.service
systemctl disable postfix.service
systemctl disable avahi-daemon.socket
systemctl disable avahi-daemon.service
systemctl disable atd.service
systemctl disable bluetooth.service
systemctl disable wpa_supplicant.service
systemctl disable accounts-daemon.service
systemctl disable atd.service cups.service
systemctl disable postfix.service
systemctl disable ModemManager.service
systemctl disable debug-shell.service
systemctl disable rtkit-daemon.service
systemctl disable rpcbind.service
systemctl disable rngd.service
systemctl disable upower.service
systemctl disable rhsmcertd.service
systemctl disable rtkit-daemon.service
systemctl disable ModemManager.service
systemctl disable mcelog.service
systemctl disable colord.service
systemctl disable gdm.service
systemctl disable libstoragemgmt.service
systemctl disable ksmtuned.service
systemctl disable brltty.service
systemctl disable avahi-dnsconfd.service
systemctl disable firewalld
```

## 11.3 修改网卡名

**修改场景：** 某些情况可能因为特殊原因需要修改网卡的物理名字（如统一命名规范、避免名称冲突等），可以采用如下的方法来修改名字。**获取MAC地址：** 通过命令`ip a`来获得当前网卡的MAC地址，如下面：00:0c:29:9c:f0:cd，ens192为重新定义后的名字。

```
cat >> /etc/udev/rules.d/70-persistent-net.rules << 'HTZ'
SUBSYSTEM=="net", ACTION=="add|change",  ATTR{address}=="00:0c:29:9c:f0:cd", NAME="ens192"
HTZ
```

网卡修改需要重启服务器才会生效，服务器重启后需要重新配置IP地址。



## 11.4 新建以太网

**配置场景：** 如果是全新服务器，需要我们自己配置IP地址，可以采用下面方法配置。**配置示例：** 利用物理网卡ens256新建一个以太网口，并配置IP地址为192.168.1.100，网关为192.168.1.1，开启网卡自动启动功能。

```
nmcli c add type ethernet con-name ens256 ifname ens256 ipv4.method manual ipv4.addresses 192.168.1.100/24 ipv4.gateway 192.168.1.1 connection.autoconnect yes
#启动网卡，使配置生效
nmcli c up ens256
```

### 11.4.1 修改IP地址

修改已有网卡网卡的IP地址

```
nmcli c modify ens256 -ipv4.address 10.10.234.128/24
nmcli c up ens256
```

### 11.4.2 启用网卡自动连接

网口默认不自动连接，需要手动开启自动连接功能

```
nmcli c modify ens256 connection.autoconnect yes
nmcli c up ens256
```



## 11.5 大页配置

**大页说明：** 建议都开启大页功能，大页可以减少页表项数量，降低TLB缺失率，提高内存访问性能。**配置原则：** 大页的值根据OLAP和OLTP分别配置不同的值，OLTP场景建议配置更大的大页数量。**架构差异：** 大页配置中需要注意，在X86下面每页是2M，在ARM下面每页是512M，查询每页的大小，可以通过Hugepagesize来确认：

```
cat /proc/meminfo|grep Hugepagesize
```

配置大页的参数。

```
echo "vm.nr_hugepages = 具体的值
vm.nr_overcommit_hugepages=0">> /etc/sysctl.d/yashandb.conf && sysctl --system
```

## 11.6 常用工具包

**工具包说明：** 常用工具包包括系统监控、网络诊断、性能分析等工具，便于数据库运维和问题排查。**安装注意：** 常用工具包安装过程中可能会有部分安装包存在失败的现象，此现象是正常的，因为不同操作系统之间软件包有差异。如果关键工具包安装失败，需要手动安装或寻找替代方案。

### 11.6.1 RHEL7、Kylin v10
安装过程中可能会提示部分软件不存在，可以忽略。
```
for pkg in zip bind-utils sysstat telnet iotop openssh-clients net-tools unzip libvncserver tigervnc-server device-mapper-multipath dstat lsof psmisc redhat-lsb-core parted xhost strace showmount expect tcl sysfsutils gdisk rsync lvm2 qperf chrony tmux bpftrace perf; do 
  echo "Installing $pkg..."; 
  yum -y install --disablerepo=\* --enablerepo=local $pkg || echo "Failed to install $pkg"; 
done
```

### 11.6.2 RHEL8、UOS V20
安装过程中可能会提示部分软件不存在，可以忽略。
```
for pkg in zip bind-utils sysstat telnet iotop openssh-clients net-tools unzip libvncserver tigervnc-server device-mapper-multipath dstat lsof psmisc redhat-lsb-core parted xhost strace showmount expect tcl sysfsutils gdisk rsync lvm2 qperf chrony tmux bpftrace perf; do 
  echo "Installing $pkg..."; 
  yum -y install --disablerepo=\* --enablerepo=local-baseos --enablerepo=local-appstream $pkg || echo "Failed to install $pkg"; 
done
```

## 11.7 添加DNS服务器

将网卡ipv4.dns的值修改成DNS服务器的IP地址。
```
sudo nmcli c mod ens192 ipv4.dns 10.10.10.1
sudo nmcli c up ens192
```

确认可以解析是否正常。
```
[root@kylin01 ~]# nslookup yashan.cdhtz.com
Server:         10.10.10.1
Address:        10.10.10.1#53

Non-authoritative answer:
Name:   yashan.cdhtz.com
Address: 10.10.10.153
Name:   yashan.cdhtz.com
Address: 10.10.10.152
Name:   yashan.cdhtz.com
Address: 10.10.10.154
```
## 11.8 开启防火墙

**防火墙配置说明：** 如果客户要求开启防火墙，需要在数据库所有的节点上配置相应的端口规则。**配置原则：** 根据部署架构开放相应的端口，避免遗漏导致服务无法访问。**安全建议：** 建议只开放必要的端口，提高安全性。

```
# 7 查看防火墙端口开放情况
firewall-cmd --zone=public --list-ports

# 8 添加端口到防火墙（以1688为例，其他端口操作方法相同）
# 9 单机部署 - Yashan模式
firewall-cmd --zone=public --add-port=1688/tcp --permanent
firewall-cmd --zone=public --add-port=1689/tcp --permanent  # 主备复制链路
firewall-cmd --zone=public --add-port=1675/tcp --permanent  # yasom
firewall-cmd --zone=public --add-port=1676/tcp --permanent  # yasagent

# 10 单机部署 - MySQL模式（需要额外添加）
firewall-cmd --zone=public --add-port=1690/tcp --permanent  # MySQL协议监听端口

# 11 共享集群部署
firewall-cmd --zone=public --add-port=1688/tcp --permanent  # 数据库监听
firewall-cmd --zone=public --add-port=1689/tcp --permanent  # 实例间通信
firewall-cmd --zone=public --add-port=1788/tcp --permanent  # YCS实例间通信
firewall-cmd --zone=public --add-port=1690/tcp --permanent  # 主备复制链路
firewall-cmd --zone=public --add-port=1675/tcp --permanent  # yasom
firewall-cmd --zone=public --add-port=1676/tcp --permanent  # yasagent

# 12 分布式集群部署（根据实际DN数量调整）
firewall-cmd --zone=public --add-port=1688/tcp --permanent  # 数据库监听
firewall-cmd --zone=public --add-port=1689/tcp --permanent  # 实例间通信
firewall-cmd --zone=public --add-port=1788/tcp --permanent  # YCS实例间通信
firewall-cmd --zone=public --add-port=1700/tcp --permanent  # CN与NVMe数据盘监听（起始）
firewall-cmd --zone=public --add-port=1675/tcp --permanent  # yasom
firewall-cmd --zone=public --add-port=1676/tcp --permanent  # yasagent
# 13 注意：根据DN数量，1700端口会按+1递增，系统盘监听端口为1700+DN数量

# 14 存算一体分布式集群部署
firewall-cmd --zone=public --add-port=1678/tcp --permanent  # MN监听
firewall-cmd --zone=public --add-port=1688/tcp --permanent  # CN监听
firewall-cmd --zone=public --add-port=1698/tcp --permanent  # DN监听
firewall-cmd --zone=public --add-port=1679/tcp --permanent  # MN跨组通信
firewall-cmd --zone=public --add-port=1689/tcp --permanent  # CN跨组通信
firewall-cmd --zone=public --add-port=1699/tcp --permanent  # DN跨组通信
firewall-cmd --zone=public --add-port=1680/tcp --permanent  # MN同组内通信
firewall-cmd --zone=public --add-port=1690/tcp --permanent  # CN同组内通信
firewall-cmd --zone=public --add-port=1700/tcp --permanent  # DN同组内通信
firewall-cmd --zone=public --add-port=1675/tcp --permanent  # yasom
firewall-cmd --zone=public --add-port=1676/tcp --permanent  # yasagent

# 15 可视化部署Web服务（如需要）
firewall-cmd --zone=public --add-port=9001/tcp --permanent

# 16 重新载入防火墙配置
firewall-cmd --reload

# 17 验证端口是否已添加（以1688为例）
firewall-cmd --zone=public --query-port=1688/tcp

# 18 查看所有已开放的端口
firewall-cmd --zone=public --list-ports
```

## 11.9 YMP参数优化

**优化说明：** YMP的参数优化直接影响迁移效率和成功率，需要根据数据量、网络带宽、源库性能等因素综合考虑。

#### 11.9.1.1 DTS迁移

**DTS迁移说明：** 如果使用DTS迁移，如果**为了极致的性能，全链路采用DTS迁移方式以及字符集GBK进行迁移**，目前DTS基于C语言，且避免了字符集转换，因此目前速度快于JDBC方式。

**优化建议：**
- **务必**适当增加拆分粒度和并行度，提高迁移速度
- **同时迁移的表数量不宜过小，比如10~20**，避免大表迁移占用并发数，导致其他表迁移不得不等待
- 容错配置设置为“**无限制**”，也可以保持默认的行数容错设置
  

-- 以7TB数据规模的迁移举例：

-- 修改 application.properties

```
ymp_memory= 8G
ymp_direct_memory= 2
export.tool= dts
export.table.splitNum= 10
export.table.splitCount= 10
export.lobTable.splitCount= 10
migration.character_set= GBK
migration.national_character_set= GBK
migration.parallel.execute= 20
migration.parallel.createIndexUseParallel= true
migration.parallel.index= 16
import.load.options=BATCH_SIZE=2048,MODE=BATCH,SENDERS=7,CHARACTER_SET= GBK
import.load.statement=DEGREE_OF_PARALLELISM= 16
```

#### 11.9.1.2 JDBC迁移

**JDBC迁移说明：** **但是如果不是为了极致的性能，则推荐用JDBC迁移方式以及字符集为UTF8方式进行迁移**，JDBC方式兼容性更好，支持更多的数据类型和功能。

**优化建议：**
- **务必**适当增加拆分粒度和并行度，提高迁移速度
- **同时迁移的表数量不宜过小，比如10~20**，避免大表迁移占用并发数，导致其他表迁移不得不等待
- 容错配置设置为“**无限制**”，也可以保持默认的行数容错设置

```
migration.character_set= UTF8
migration.national_character_set= UTF8
import.load.options=BATCH_SIZE=2048,MODE=BATCH,SENDERS=7,CHARACTER_SET= **UTF8**
```

#### 11.9.1.3 LOB字段的优化

**LOB迁移说明：** LOB（Large Object）字段包括BLOB、CLOB等大对象类型，迁移时需要特殊处理。**迁移方式：** 如果使用JAVA迁移，一般用于LOB迁移，因为LOB字段通常较大，需要流式处理。

```
ymp_memory= 24G
ymp_direct_memory= 4
export.tool= jdbc
import.load.options=BATCH_SIZE=2048,MODE=BATCH,SENDERS=7,CHARACTER_SET= UTF8
import.load.statement=DEGREE_OF_PARALLELISM= 16
```

**大表LOB优化：** 如果存在大表，且存在有LOB字段，由于LOB表抽取远慢于普通表，**则必须增加抽取的并发（export.table.splitNum）**，例如以下就采用100的抽取并发，提高LOB字段的抽取速度。

```
export.table.splitNum= 100
export.table.splitCount= 20
export.lobTable.splitCount= 20
export.csv.exportRowsEveryFile= 1000000
export.jdbc.thresholdForSplittingFileLines= 1000000
migration.max-file-rows= 1000000
```



## 11.10 YMP管理

**管理说明：** YMP的管理包括内置数据库管理、参数配置、任务监控等，了解这些内容有助于YMP的日常运维和问题排查。

#### 11.10.1.1 YMP内置库登录

**环境变量配置：** 查找YMP的环境变量配置文件，该文件包含连接内置数据库所需的环境变量。
```
 find / -name ymp.bashrc
```
应用环境变量文件：
```
source 查找到的文件
```
登录数据库，目前YMP内置数据库没有开启免密登录。
```
yasql sys/Ymppw602.
```

#### 11.10.1.2 YMP内置库连接信息配置

**配置文件说明：** conf/application.properties中配置YMP连接内置数据库的信息，这些信息用于YMP应用连接内置数据库。配置示例如下：
```
# 19 cat application.properties 
spring.datasource.url=jdbc:yasdb://127.0.0.1:8091/yashan
spring.datasource.username=YMP_DEFAULT
spring.datasource.password=4cd9LJ8WBbjAoT9bqT3nNw==
spring.datasource.largePoolSize=64M
spring.datasource.cursorPoolSize=64M
spring.datasource.defaultTableType=HEAP
spring.datasource.openCursors=3000
spring.datasource.sharePoolSize=2G
spring.datasource.dateFormat=yyyy-mm-dd hh24:mi:ss
spring.datasource.ddlLockTimeout=2
```
#### 11.10.1.3 YMP内置库配置信息

**配置信息说明：** YMP内置数据库的配置信息存储在conf/db.properties文件中，包括数据库密码、端口、字符集等。查看配置信息：
```
cat conf/db.properties

YASDB_PASSWORD=4cd9LJ8WBbjAoT9bqT3nNw==
YASDB_PORT=8091
YASDB_CHARACTER_SET=UTF8
```

#### 11.10.1.4 并发线程配置

**配置文件：** conf/application.properties

**参数说明：** 以下参数用于控制YMP的并发处理能力，需要根据源库性能和网络带宽调整。

| 参数名                       | 默认值 | 参数说明                                   |
| ------------------------- | --- | -------------------------------------- |
| assessment.ddlCount       | 20  | 评估任务单个会话获取DDL的数量，如果Oracle性能较差，则需要降低该值。 |
| assessment.maxThreadCount | 20  | 评估任务最多同时拥有的会话数，如果Oracle性能较差，则需要降低该值。   |

**调整建议：** 在一些特殊的情况下需要手动调整线程数，比如获取DDL失败，但是Oracle源端执行正常，可能是并发过高导致源库压力过大，需要降低并发数。

### 11.10.2 手动查询不兼容对象

**查询方法说明：** YMP页面上的兼容性每次需要手动点，相对比较麻烦，可以利用下面的SQL语句，直接在数据库查询出不兼容对象信息。**使用步骤：** 通过dbeaver对YMP评估库执行以下SQL语句，把结果导出到HTML文件里，然后用浏览器打开这个HTML文件页面，就能够在HTML文件页面CTRL+F快速查找任何对象的不兼容详情。
```
SELECT
	SCHEMA_NAME,
	OBJECT_TYPE,
	OBJECT_NAME,
	OBJECT_STATUS,
	replace(LISTAGG(rn || ') ' || YAS_ERROR2, '@@@@@') WITHIN GROUP(ORDER BY rn), '@@@@@', ';' || chr(10)) AS YAS_ERROR
FROM
	(
	SELECT
		SCHEMA_NAME,
		OBJECT_TYPE,
		OBJECT_NAME,
		OBJECT_STATUS,
		YAS_ERROR2,
		ROW_NUMBER() OVER(PARTITION BY SCHEMA_NAME, OBJECT_TYPE, OBJECT_NAME ORDER BY min_YAS_ERROR_LINE) rn
	FROM
		(
		SELECT
			SCHEMA_NAME,
			OBJECT_TYPE,
			OBJECT_NAME,
			OBJECT_STATUS,
			YAS_ERROR2,
			min(YAS_ERROR_LINE) AS min_YAS_ERROR_LINE
		FROM
			(
			SELECT
				oar.SCHEMA_NAME,
				oar.OBJECT_TYPE,
				oar.OBJECT_NAME,
				oar.OBJECT_STATUS,
				oei.YAS_ERROR_LINE,
				oei.YAS_ERROR_CODE || ' [' || to_char(oei.YAS_ERROR_MESSAGE) || ']' AS YAS_ERROR2
			FROM
				YMP_DEFAULT.OBJECT_ASSESSMENT_RESULT oar,
				YMP_DEFAULT.OBJ_ERROR_INFO oei
			WHERE
				oar.task_id = ? -- 需要在YMP任务详情通过URL获取，例如http://xxx.xxx.xxx.xxx:8090/#/task/detail?id=17&... 则task_id=17
				-- AND oar.SCHEMA_NAME = 'UCIS'
				AND oar.RESULT_TYPE = 1
				AND oar.TASK_ID = oei.TASK_ID
				AND oar.id = oei.OBJECT_ID
				AND oei.YAS_ERROR_CODE != 'YAS-04253')
		GROUP BY
			SCHEMA_NAME,
			OBJECT_TYPE,
			OBJECT_NAME,
			OBJECT_STATUS,
			YAS_ERROR2))
GROUP BY
	SCHEMA_NAME,
	OBJECT_TYPE,
	OBJECT_NAME,
	OBJECT_STATUS
ORDER BY
	SCHEMA_NAME,
	OBJECT_TYPE,
	OBJECT_NAME,
	OBJECT_STATUS;
```

