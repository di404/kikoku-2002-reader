# 鬼哭街 原版阅读器

在 Mac 上阅读 Nitroplus《鬼哭街》2002 原版（妹乃萌汉化，无语音）。程序直接读取游戏数据：中文横排，字由程序绘制，窗口约 1440p，字体可换，背景音乐和音效跟着对应的句子播放。

不运行 `KIKOKUCHS.exe`，也不需要 Wine、CrossOver 或 ONScripter。

## 游戏数据

默认目录是 `~/Games/kikoku-2002/`，第一次启动如果找不到，会让你选文件夹。

| 文件 | 用途 | 必需 |
|---|---|---|
| `script.chs` | 中文剧本（汉化补丁） | ✓ |
| `IMAGE.PK2` | CG、背景、立绘 | ✓ |
| `SOUND.PK2` | 音乐、音效 | ✓ |
| `SCRIPT.PK2` | 日文剧本。汉化补丁弄坏了部分资源名，用它对照修复 | 建议有 |
| `movie/*.mpg` | 过场动画 | 可选，需要 ffmpeg |

## 构建

需要 Go 1.26+、Xcode 命令行工具和 Wails v2：

```bash
./build.sh
```

生成 `build/bin/kikoku2002.app`，并加上本机签名（ad-hoc）。

## 操作

| 按键 | 作用 |
|---|---|
| 点击 / 空格 / 回车 / 滚轮向下 | 下一句 |
| ← / 滚轮向上 | 上一句 |
| 右键 / H | 隐藏文字框 |
| 按住 Ctrl | 快进 |
| F | 全屏 |
| Esc | 菜单：字体、字号、行距、文字框浓度、音量、章节、过场 |

第一次从头读时先播放开场动画，点击可跳过。设置和进度保存在 `~/Library/Application Support/kikoku2002-reader/settings.json`。过场第一次播放时转码一次，缓存在 `~/Library/Caches/kikoku2002-reader/movies`。

## 数据格式（和交接文档写的不一样）

- 这部原版**不是 NScripter**。`script.chs` 和 `*.PK2` 都是 Nitroplus 自家引擎的 ARCV 包：Shift-JIS 文件名表（zlib）、`INDX` 索引（绝对偏移、大小、DOS 时间）、`DATA`。每个条目要么原样存储（PNG/JPEG/WAV），要么是 `u24 长度 + 'Z' + zlib`。见 `internal/pk2`。
- 剧本 `脚本*.nps` 是 HTML 风格的标签语言：`<k>` 翻页，`<BOX TYPE=n>`、`<clear>`、`<background>`、`<bustup>`、`<LAYER>`、`<wipe>`、`<WHITEOUT>`、`<bgm>`、`<se>`，靠 `<a href>` 接到下一个文件。正文是 GBK，标签属性是 Shift-JIS。汉化后有些属性名坏了，`internal/nps` 会按位置去日文剧本里找对应标签来修复。
- 音效是 16 位 PCM WAV，音乐是 Microsoft ADPCM WAV，都由 `internal/audio` 解码，再用 oto 输出。

开发用工具：`go run ./cmd/pk2tool FILE [OUTDIR]` 列出或解出包内文件；`go run ./cmd/dumpbook [FROM N]` 打印编译后的阅读页。
`go build -tags desktop,production -o bin/kikoku2002-preview . && bin/kikoku2002-preview -preview :8091` 可以在浏览器里看阅读界面（地址后加 `#页号` 直接跳页）。

## 不做的

滤镜、噪点转场、原版菜单和存档、结尾的工作人员名单滚动、Windows 版。

## 版权声明

本仓库只包含阅读器程序源码(MIT 协议),**不包含**任何游戏数据(`script.chs`、`IMAGE.PK2`、`SOUND.PK2` 等)。请自行持有正版游戏及汉化补丁。《鬼哭街》版权归 Nitroplus 所有。
