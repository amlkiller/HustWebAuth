HustWebAuth
===========

锐捷 WEB 认证跨平台工具

Web认证
----------
当前很多学校使用了锐捷 Web 认证的方式来实现校园内有线网和无线网的网络认证和登录。

锐捷 Web 认证是一种对用户访问网络的权限进行控制的身份认证方法，这种认证方法不需要用户安装专用的客户端认证软件，使用**普通的浏览器访问**就可以进行身份认证。

未认证用户使用浏览器上网时，网络设备会强制浏览器访问特定站点，也就是Web认证服务器，通常称为Portal服务器。当用户需要访问认证服务器以外的其它网络资源时，就必须通过浏览器在Portal服务器上进行身份认证，只有认证通过后才可以使用网络资源。参考：[Web认证概述](https://image.ruijie.com.cn/Upload/Article/fd9117df-4b38-49fb-a6ac-a8b6cb43a130/RAC&RAP%20%E5%AE%9E%E6%96%BD%E4%B8%80%E6%9C%AC%E9%80%9A%EF%BC%88%E5%B0%8F%E7%9D%BF%E5%93%A5%EF%BC%89/RAC&RAP%20%E5%AE%9E%E6%96%BD%E4%B8%80%E6%9C%AC%E9%80%9A%EF%BC%88%E5%B0%8F%E7%9D%BF%E5%93%A5%EF%BC%89/8/1/Web%E8%AE%A4%E8%AF%81%E5%8E%9F%E7%90%86.html)

锐捷 WEB 认证跨平台工具的目的是方便用户**使用 Linux (如 Armbian/Ubuntu/Debian) 或配置 OpenWrt 路由器**时，在**不打开浏览器的前提**下，通过**命令行**直接实现 Web 认证。

核心特性
----------
- **跨平台运行**：纯静态编译，支持 Linux (x86_64, ARM, ARM64, MIPS, MIPSLE)、OpenWrt、Armbian、Windows、macOS 等。
- **单线多拨与网口绑定 (`-i, --iface`)**：支持为指定网口（如 `vwan1`、`macvlan0`）绑定出站流量，支持单线多拨多账号并发登录。
- **多账号轮转登录**：支持多账号备用池，登录失败自动无缝平滑切换下一个可用账号。
- **指数规避冷却机制 (Exponential Backoff)**：智能检测账号被实体终端（手机/电脑）挤下线或认证失败，依连续失败次数指数级延长冷却时间（$T_{\text{base}} \times 2^n$），彻底杜绝与实体设备互相挤占争抢的死循环。
- **守护进程与系统服务集成**：内置后台 Daemon 轮询探测，支持一键安装为 systemd 或 OpenWrt procd/init.d 系统服务开机自启。
- **MAC 无感认证绑定**：支持 `-r, --register` 自动将设备 MAC 地址绑定至锐捷 Portal，后续连接免密直连。

使用方法
-----------
1. 安装
    > 方式一: 使用 `go install` 命令安装
    >
    > ```bash
    > go install github.com/a76yyyy/HustWebAuth@latest
    > ```
    >
    > 方式二: 下载 `release` 可执行文件
    >
    > 1. 下载指定架构的[可执行文件](https://github.com/a76yyyy/HustWebAuth/releases)
    > 2. 重命名可执行文件为 `HustWebAuth` 或 `HustWebAuth.exe`
    > 3. 将文件权限修改为可执行权限, 如 `chmod +x HustWebAuth`
    > 4. **(建议)** 将可执行文件移动到 `/usr/local/bin` 目录下 或 添加到 `Path` 环境变量中

2. 命令行快速运行
    ```bash
    # 查看帮助
    HustWebAuth -h

    # 单账号一次性认证
    HustWebAuth -a account -p password

    # 多账号轮转认证 (以英文逗号分隔账号与密码)
    HustWebAuth -a "user1,user2" -p "pass1,pass2"

    # 单线多拨绑定指定网口 (如 OpenWrt 虚拟拨号口 vwan1)
    HustWebAuth -i vwan1 -a account -p password

    # 开启周期轮询保活模式 (每 5 分钟探测一次网络，掉线自动重连与轮转)
    HustWebAuth -a account -p password --cycle --cycleDuration 5m
    ```

3. 配置文件使用与持久化
    可使用 `-o, --save` 选项将当前命令行配置保存至默认配置文件 `$HOME/HustWebAuth.yaml`：
    ```bash
    HustWebAuth -a "user1,user2" -p "pass1,pass2" -i vwan1 --cooldown 10m --maxCooldown 2h -o
    ```

    **完整配置文件示例 (`$HOME/HustWebAuth.yaml`)：**
    ```yaml
    # 认证配置
    auth:
      account: "user1"               # 主账号
      password: "pass1"              # 主账号密码
      serviceType: "internet"        # 锐捷服务类型 (internet 或 local)
      encrypt: false                 # 密码是否加密传输
      rotation: true                 # 是否启用多账号轮转
      cooldown: 4m59s                # 初始基准冷却时长
      maxCooldown: 2h                # 最大封顶冷却时长
      accounts:                      # 多账号轮转列表 (优先于单个 account)
        - account: "user1"
          password: "pass1"
        - account: "user2"
          password: "pass2"

    # 网络与接口绑定
    net:
      iface: "vwan1"                 # 绑定的网卡或虚拟网口 (如 eth0, vwan1, macvlan0)

    # (可选) 单线多拨多网口集中配置
    # 当 net.iface 为空且配置了 interfaces 时，程序将为每个网口启动独立并发 Worker 协程
    # interfaces:
    #   - iface: "vwan1"
    #     accounts:
    #       - account: "user1"
    #         password: "pass1"
    #   - iface: "vwan2"
    #     accounts:
    #       - account: "user2"
    #         password: "pass2"

    # 网络连通性探测 (基于原生 SO_BINDTODEVICE 强设备绑定的 HTTP 204 检测)
    check:
      url: "http://connect.rom.miui.com/generate_204"  # 连通性探测端点 (HTTP 204)
      timeout: 5s                                      # 探测超时时长

    # 旧版 ICMP Ping 兼容配置 (可选)
    # ping:
    #   ip: "202.114.0.131"
    #   count: 3
    #   timeout: 3s
    #   privilege: true

    # 循环探测保活
    cycle:
      enable: true                   # 启用周期保活模式
      duration: 5m0s                 # 探测周期时长
      retry: 3                       # 失败重试次数 (-1 表示无限重试)

    # 日志设置
    log:
      dir: "/tmp/HustWebAuth"
      file: "HustWebAuth.log"
      append: true
      connected: false               # 网络正常在线时是否记录日志 (建议设为 false 防止日志过大)
      syslog: false
    ```

4. 系统服务安装与自启 (systemd / OpenWrt procd)
    ```bash
    # 默认服务安装
    HustWebAuth service install

    # 单线多拨场景：为特定网口安装独立命名的系统服务
    HustWebAuth service install --name HustWebAuth_vwan1 -i vwan1 -f /etc/HustWebAuth_vwan1.yaml
    HustWebAuth service install --name HustWebAuth_vwan2 -i vwan2 -f /etc/HustWebAuth_vwan2.yaml

    # 服务管控
    HustWebAuth service start [--name 服务名]
    HustWebAuth service status [--name 服务名]
    HustWebAuth service restart [--name 服务名]
    HustWebAuth service stop [--name 服务名]
    HustWebAuth service uninstall [--name 服务名]
    ```

Help 命令
==========
```bash
> HustWebAuth -h
HustWebAuth is a program used to implement Ruijie web authentication.

Usage:
  HustWebAuth [flags]
  HustWebAuth [command]

Available Commands:
  get         Get the login url from the redirect url
  help        Help about any command
  login       Hust web auth only once
  service     System service related commands

Flags:
  -a, --account string           Account(s) for authentication (comma-separated for multi-account)
      --checkTimeout duration    Timeout for connectivity check (default 5s)
      --checkURL string          URL endpoint for HTTP 204 connectivity check (default "http://connect.rom.miui.com/generate_204")
  -f, --config string            Config file (default is $HOME/HustWebAuth.yaml)
      --cooldown duration        Base cooldown duration for exponential backoff (default 4m59s)
  -c, --cycle                    Enable cycle mode
      --cycleDuration duration   Cycle duration (default 5m0s)
      --cycleRetry int           Cycle retry times, -1 means retry forever (default 3)
  -d, --daemon                   Enable daemon mode, not support windows
      --daemonPidFile string     Daemon pid file
  -e, --encrypt                  Password is encrypted or not (default false)
  -h, --help                     help for HustWebAuth
  -i, --iface string             Network interface or IP address to bind (e.g. eth0, vwan1, 10.0.0.2)
      --logAppend                Log file append mode. 
                                 NOTE: if logRandom is true, it will be ignored (default true)
      --logConnected             Enable logging of "The network is connected" (default true)
      --logDir string            Log Directory (default "/tmp/HustWebAuth")
  -l, --logFile string           Log file name (default means output to os.stdout)
      --logRandom                Log file name with random string.
                                 NOTE: If logFile includes a "*", the random string replaces the last "*".
                                  (default true)
      --maxCooldown duration     Max cooldown duration for exponential backoff (default 2h0m0s)
  -p, --password string          Password(s) for authentication (comma-separated)
      --pingCount int            ping count (deprecated) (default 3)
      --pingIP string            IP address to ping (deprecated, please use --checkURL) (default "202.114.0.131")
      --pingPrivilege            Sets the type of ping pinger will send (deprecated). (default true)
      --pingTimeout duration     Ping timeout (deprecated) (default 3s)
      --redirectURL string       Redirect URL (default "http://123.123.123.123")
      --rotation                 Enable multi-account rotation (default true)
  -o, --save                     Save config file
  -s, --serviceType string       Service type, options: [internet, local] (default "internet")
      --syslog                   Enable syslog, not support windows

Use "HustWebAuth [command] --help" for more information about a command.
```

Service Help 命令
=================
```bash
> HustWebAuth service -h
Use HustWebAuth as a system service: install, start, stop, uninstall, etc.

Usage:
  HustWebAuth service [flags]
  HustWebAuth service [command]

Available Commands:
  install     Install HustWebAuth service
  restart     Restart HustWebAuth service
  start       Start HustWebAuth service
  status      Get HustWebAuth service status
  stop        Stop HustWebAuth service
  uninstall   Uninstall HustWebAuth service from system

Flags:
  -h, --help          help for service
      --name string   Custom service name (default HustWebAuth or HustWebAuth_<iface>)

Use "HustWebAuth service [command] --help" for more information about a command.
```