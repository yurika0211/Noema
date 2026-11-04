# 灯織视觉资产准备说明

给画师 / Live2D 绑定师。工程侧按这份验收，不按聊天里的截图口头改。

对应 GitHub：[#33 Live2D 灯織](https://github.com/yurika0211/Noema/issues/33)。  
Web 样品（[#18](https://github.com/yurika0211/Noema/issues/18) / PR #32）现在用剪影占位，**不是**最终形象。没有本包之前，不要用别的角色模型顶上。

产品：Noema。角色：灯織（Hiori）。第一期只要**这一个**人。

---

## 1. 她是谁（不要改）

稳定外观，不能按用户一句话换成另一个人：

| 项 | 定稿 |
| --- | --- |
| 名字 | 灯織 / Hiori |
| 头发 | 长黑发，软、有体积，不是贴头皮的色块 |
| 眼睛 | 灰紫 |
| 发夹 | 绿色四叶草，别在一侧 |
| 衣服 | 米白衬衫 + 墨绿开衫（设计图里还有白领、深色下装） |
| 气质 | 安静、知性；亲近克制，不要幼态或夸张 Q 版当主资产 |

参考：产品设计图里的图书室半身（托腮、开衫、发夹）是**视觉基准**。剪影页只是开发占位。

---

## 2. 你要交什么（最小可开工包）

目录名建议 `hiori-assets-0.1.0/`。版本号只升、不复用。

```
hiori-assets-0.1.0/
  LICENSE.txt                 # 必给：谁能用、能否进公开仓和 APK、怎么署名
  README.txt                  # 可选：软件版本、绑定师、注意事项
  manifest.json               # 见第 5 节；没有表也可以先交表格，工程写成 json
  live2d/
    hiori.model3.json
    hiori.moc3
    textures/                 # atlas png 或 webp
    hiori.physics3.json       # 有物理就带
    hiori.cdi3.json           # 可选
    motions/                  # 见第 3 节
    expressions/              # 见第 3 节
  still/
    hiori-bust.png            # 对话回退：半身、透明底
    hiori-theater.png         # 剧场回退：更大半身或膝上、透明底
  bg/
    library.jpg               # 强烈建议：图书室静态背景，没有则剧场先纯色
```

**Live2D：** Cubism 4 或 5 导出，须能用官方 **Cubism Web Runtime** 和 **Android Runtime** 加载。交导出结果即可，不必交 `.cmo3` 源工程（源工程另存备份更好）。

**静图：** 关动画或 Live2D 加载失败时用。必须和 Live2D **同一套发型、发夹、衣服**，否则一关动画人就换了。

建议尺寸（可按绑定习惯微调，但要写进 README）：

| 文件 | 建议 |
| --- | --- |
| `hiori-bust` | 透明 PNG/WebP，约 1080×1920 或 512×860，半身 |
| `hiori-theater` | 透明 PNG/WebP，更大半身 / 膝上 |
| `library` | 1920×1080 或 3840×2160，JPG/WebP，图书室窗与书架，**不要**把角色画进背景 |

---

## 3. 动作与表情（第一期只要这些 id）

客户端只认 **manifest 里的 id**。模型临时名（如 `exp_01`）必须映射到下表。多了的动作第一期不会播。

### 表情 expression（必须）

| id | 何时用 | 注意 |
| --- | --- | --- |
| `neutral` | 待机 | 默认 |
| `thinking` | 用户已发送、还没有正文 | 不是「露出思考过程」 |
| `focused` | 正在跑真实任务 | 安静专注，不要忙乱 |
| `smile` | 轻松对话 | 克制，不要张大嘴 |
| `shy` | 亲近但收着 | 不要演成好感度 |
| `puzzled` | 没听清、或工具失败后解释 | 失败不要演成生气 |

### 循环 motion（必须）

| id | 说明 |
| --- | --- |
| `idle_breath` | 呼吸 |
| `idle_blink` | 眨眼（若做进物理/眼睛参数里，manifest 注明即可） |

### 第一期不要做

- 口型、 viseme、语音同步（另单，跟语音一起）
- 走动、坐下、翻书等「自己过日子」
- 关系受伤、哭、好感度专用动作
- 第二套衣服、多口套

---

## 4. 用在哪两个画面

工程已经有两个布局，资产要能套进去：

**对话**  
人在左侧或居中，**固定比例**（不要拉成细长条）。没有消息时人还在。半身即可。

**剧场**  
人叠在图书室背景上，对白在画面下沿。背景先静态一张。Auto / Skip / Log / Save、右侧建议句、能量条**不**跟本包一起做。

Web 和 Android 共用**同一份包**，不要出「网页一套、手机一套」。

---

## 5. manifest 要填的字段

工程会写成 `manifest.json`。你至少提供能填满这些格子的信息：

```json
{
  "schema_version": "noema.character-assets.v1",
  "id": "hiori",
  "version": "0.1.0",
  "display_name": "灯織",
  "license": "见 LICENSE.txt",
  "live2d": {
    "model": "live2d/hiori.model3.json",
    "runtime": "cubism4"
  },
  "stills": {
    "bust": "still/hiori-bust.png",
    "theater": "still/hiori-theater.png"
  },
  "backgrounds": {
    "library": "bg/library.jpg"
  },
  "expression_ids": ["neutral", "thinking", "focused", "smile", "shy", "puzzled"],
  "motion_ids": ["idle_breath", "idle_blink"],
  "files": [
    { "path": "live2d/hiori.moc3", "sha256": "（交文件后工程可代算）" }
  ]
}
```

`sha256` 可以交文件后由工程算。**id 列表不能空。**

---

## 6. 许可证（没有就不进公开仓）

`LICENSE.txt` 请用中文或中英写清：

- 著作权人 / 画师 / 绑定师名字（署名怎么写）
- 能否放进 GitHub 公开仓库 `yurika0211/Noema`
- 能否打进 Android APK、能否随 Web 页分发
- 能否改参数、裁切、做回退静帧
- 不能用于训练、不能转售等限制

没有书面授权，工程**不会**把 moc/贴图推进 `main`。

---

## 7. 技术约束（绑定师）

- 官方 Cubism runtime 能加载即可；不要依赖网页里 `eval`、不要运行时再下载任意 URL。
- 贴图尽量合 atlas，避免几十张散图。
- 参数命名稳定（头、眼、呼吸、发夹）；第一期客户端只切上表 id。
- 不要在模型里藏外部脚本。
- 预乘/非预乘 alpha 在 README 写一句，Web 和 Android 才能设对。

---

## 8. 验收（工程怎么算「能接」）

- [ ] `live2d/` 在官方 Web runtime 里能打开，默认 `neutral` + 呼吸眨眼
- [ ] 六个 expression id 都能切，没有未知文件
- [ ] `still/` 两张和 Live2D 同一套外观
- [ ] `LICENSE.txt` 允许公开仓 + APK
- [ ] 关闭动画时用静图仍能认出是灯織
- [ ] （建议）`bg/library` 一张，角色不画进背景

缺 Live2D、只有静图：继续 #18 高质量静图，**不开** #33。  
缺静图、只有 Live2D：可以接 #33，但必须能关动画（至少导出一张默认姿态静帧）。

---

## 9. 怎么交

整包 zip 或单独网盘，文件名带版本：`hiori-assets-0.1.0.zip`。  
不要把密钥、未授权的商业素材混进包里。

交来之后工程侧：核对外观 → 写 `manifest.json` 哈希 → 只在允许列表里加载 → Web 对话/剧场替换剪影。Android 同包或另开「本迭代仅 Web」说明。

问外观争议时，以本文件第 1 节和设计图图书室半身为准，不以当前网页剪影为准。
