# LexiGo Android 源码与构建

Java 类位于 `cn.wordreview.app`，默认 APK 安装包名为 `cn.wordreview.app.lexigo.v213`。支持 Android 9 及以上的 ARM64 和 x86_64 设备。程序由 Java Activity、本地 WebView 和通过 JNI 加载的 Go 学习引擎构成。Android 专用布局在 `../assets/android.css`。系统文件选择、英文离线 TTS 和应用生命周期桥接位于 `src/main/java/`。

Android 1.4.1 由 NDK 编译 SQLite，经 Android libc 访问文件，修复 1.4.0 在 Android x86_64 上的原生启动崩溃。Windows 采用原有 SQLite 驱动，两端读写同样的表结构和完整学习数据文件。

## 构建

在 Linux x64 上安装 Go 1.25、JDK 17 或更新版本、Python 3，以及 Android SDK 平台 35、Build Tools 35.0.0 和 NDK 28。下面的路径需要换成自己的安装位置。

```bash
export ANDROID_SDK_ROOT=/path/to/android-sdk
export ANDROID_NDK_HOME="$ANDROID_SDK_ROOT/ndk/28.2.13676358"
export JAVA_HOME=/path/to/jdk
bash android/build-android.sh dist/LexiGo.apk
```

脚本会编译两个架构、打包资源、对齐 APK、签名并验证签名。首次运行会要求输入密码并生成本地签名库。也可通过 `WORDREVIEW_KEYSTORE` 和 `WORDREVIEW_KEY_ALIAS` 指向已有签名库。安装更新必须沿用同一包名和签名。自行签名的构建如果与已安装版本的签名不同，先通过应用导出数据，保管好文件后再卸载原版本、安装自签版本并导入。发行签名密钥由构建方保存，源码不含私钥。

## 安装和验证

可把签名后的 APK 发到手机，通过系统文件管理器安装。按系统提示为发起安装的应用允许安装未知应用。也可在连接已授权的测试设备后执行 `adb install -r dist/LexiGo.apk`。

2.1.3 使用独立包名和新的签名，可与 2.1.2 及更早版本同时安装。通过旧版「设置 → 导出数据 → 保存到手机」保存完整 JSON，再在 LexiGo 选择「设置 → 导入数据」迁移词库、备注和复习记录。后续覆盖升级需沿用已安装的 LexiGo 包名及签名。

Android 通过系统分享面板发送实际的 `application/json` 文件。系统只向接收应用授予临时读取权限。私有缓存中超过三天的共享文件会在下次启动或分享时清理。「导出数据 → 保存到手机」沿用系统文件选择器。
