# 背词本 Android 源码与构建

应用包名为 `cn.wordreview.app`。支持 Android 9 及以上的 ARM64 和 x86_64 设备。程序由 Java Activity、本地 WebView 和通过 JNI 加载的 Go 学习引擎构成。Android 专用布局在 `../assets/android.css`。系统文件选择、英文离线 TTS 和应用生命周期桥接位于 `src/main/java/`。

Android 1.4.1 由 NDK 编译 SQLite，经 Android libc 访问文件，修复 1.4.0 在 Android x86_64 上的原生启动崩溃。Windows 采用原有 SQLite 驱动，两端读写同样的表结构和完整学习数据文件。

## 构建

在 Linux x64 上安装 Go 1.25、JDK 17 或更新版本、Python 3，以及 Android SDK 平台 35、Build Tools 35.0.0 和 NDK 28。下面的路径需要换成自己的安装位置。

```bash
export ANDROID_SDK_ROOT=/path/to/android-sdk
export ANDROID_NDK_HOME="$ANDROID_SDK_ROOT/ndk/28.2.13676358"
export JAVA_HOME=/path/to/jdk
bash android/build-android.sh dist/WordReview.apk
```

脚本会编译两个架构、打包资源、对齐 APK、签名并验证签名。首次运行会要求输入密码并生成本地签名库。也可通过 `WORDREVIEW_KEYSTORE` 和 `WORDREVIEW_KEY_ALIAS` 指向已有签名库。安装更新必须沿用同一包名和签名。自行签名的构建如果与已安装版本的签名不同，先通过应用导出数据，保管好文件后再卸载原版本、安装自签版本并导入。发行签名密钥由构建方保存，源码不含私钥。

## 安装和验证

可把签名后的 APK 发到手机，通过系统文件管理器安装。按系统提示为发起安装的应用允许安装未知应用。也可在连接已授权的测试设备后执行 `adb install -r dist/WordReview.apk`。

随交付包提供的 1.9.0 APK 与 1.8.0、1.7.0、1.6.0 同签名、同包名（`cn.wordreview.app.v16`）。可在手机上直接覆盖升级并保留数据。若当前在用 1.5.1 或更早的旧签名版本，请先导出完整学习数据、确认 JSON 文件已保存，再切换到新版并导入。构建脚本使用 `WORDREVIEW_ANDROID_PACKAGE` 指定包名，使用 `WORDREVIEW_KEYSTORE` 配置自己的签名文件。
