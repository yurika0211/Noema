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

---

## 10. 自己做：GPT 出图之后怎么绑 Live2D

你已经有一张（或几张）GPT 图。目标不是「提示词直接出 moc3」，而是：**定稿一张灯織 → 拆层 → Cubism 里绑 → 按第 2 节打包**。AI 可以辅助拆层和草稿绑定，六个表情和呼吸仍要你在编辑器里收。

官方编辑器以 [Live2D Cubism](https://www.live2d.com/) 为准。社区拆层/自动绑（[See-through](https://github.com/shitagaki-lab/see-through)、[image2live2d](https://github.com/Wzhang3912/image2live2d)、[psd2live](https://github.com/tsunehimatoi/psd2live)）能当草稿，导出若不能用**官方 Viewer** 打开，就不要交给工程。

### 10.1 定稿图（先过这一关再拆层）

用 GPT 时把第 1 节整段贴进提示，并加约束：

- 单个角色、半身或膝上、**透明或纯色背景**（剧场背景另出一张，不要把她画进书架）
- 正面略偏 3/4，头和身子完整，头发、开衫、衬衫领、发夹分得清
- 同一张脸反复出：保存成功的一张当「主参考」，之后只说「和这张同一人物、同一套衣服」
- 不要：第二个人、手挡住脸、刘海盖住双眼、Q 版、写实照片风

过关：打印或放大后，仍能看出长黑发、灰紫眼、四叶草发夹、米白衬衫、墨绿开衫。过不了就重出图，不要拿去绑。

另出（可用同一提示加「只有房间、没有人」）：

- `bg/library`：图书室窗与书架，**不要角色**

从定稿图再导出两张静图（透明底）：`still/hiori-bust.png`（对话半身）、`still/hiori-theater.png`（更大）。这两张必须和后面 Live2D **同一套外观**。

### 10.2 拆层

Live2D 要的是图层，不是一张扁图。任选：

**A. 手工（稳）**  
Photoshop / CSP：按部位建图层，大致：

- 后发、侧发、前发 / 刘海  
- 头、颈  
- 眼睛（白眼、瞳、高光、睫毛，左右分开更好）  
- 眉、鼻、嘴（唇、口腔）  
- 发夹  
- 衬衫、开衫（前片/袖可分）  
- 身体/下装  

导出 **PSD**（或按部位 PNG 文件夹）。图层名用英文：`hair_front`、`eye_l`、`clover_clip`。

**B. AI 拆层（快，必须检查）**  
用拆层工具把定稿图分成透明 PNG，再在 PS 里改名、补洞（转头时耳后、领口里面）。穿帮、破洞、左右眼贴反了，绑出来一定会坏。

### 10.3 进 Cubism Editor

1. 安装 Cubism Editor（5.x 即可），新建模型，导入 PSD。  
2. 网格：每层自动网格后再手修发丝、衣摆。  
3. 变形器：头、身体、头发用弯曲/旋转变形器，避免整层硬转。  
4. 参数至少：`ParamAngleX/Y/Z`（头）、`ParamEyeLOpen` / `ParamEyeROpen`、`ParamBreath`。发夹单独一层，避免跟着头皮平贴。  
5. 物理：头发、开衫下摆轻微晃动即可，不要整头乱甩。  
6. **表情**（第 3 节六个 id）：每个做成 Expression，文件名就用 id，例如 `smile.exp3.json`。在 Viewer 里逐个点，确认 smile 不是张大嘴、puzzled 不是生气。  
7. **呼吸 / 眨眼**：Idle 动画或眼睛参数自动眨眼。motion id 用 `idle_breath`、`idle_blink`（眨眼若做在参数里，在 README 写一句）。  
8. **不要做口型。** 第一期没有语音。  
9. 导出：**嵌入式文件** → `hiori.model3.json` + `.moc3` + 贴图 + physics。用 **Cubism Viewer** 打开，转头、眨眼、切六个表情都正常再往下。

自动绑工具若能出 `.moc3`：必须用官方 Viewer 打开并完成第 6–7 步。Viewer 打不开或表情对不上，当草稿，在 Editor 里重来。

### 10.4 按第 2 节收包

```
hiori-assets-0.1.0/
  LICENSE.txt
  README.txt          # Cubism 版本、预乘/非预乘 alpha、GPT 用了什么、拆层/绑定怎么做的
  live2d/             # Viewer 能开的那套
  still/              # 两张静图
  bg/library.jpg      # 有就放
```

`LICENSE.txt` 写清：图是 AI 生成、你有权用于 Noema 公开仓和 APK、如何署名。说不清就不要交给工程进 `main`。

`manifest.json` 可以交给工程代填哈希，但 **expression_ids / motion_ids 必须是第 3 节那些字符串**。

### 10.5 自己验收（交之前）

- [ ] Viewer 默认姿态就是灯織，不是另一个人  
- [ ] 六个 expression 能点、id 与第 3 节一致  
- [ ] 呼吸眨眼循环，没有空转嘴型  
- [ ] 静图和 Live2D 同一套衣服发型  
- [ ] LICENSE 允许公开 GitHub + APK  
- [ ] 没有把 SDK、密钥、别人的商业模型塞进 zip  

过了再发 `hiori-assets-0.1.0.zip`。工程只接能加载的包，不接「提示词和散图」。

### 10.6 常见翻车

| 现象 | 多半原因 |
| --- | --- |
| 转头露空洞 | 层没分、没补内轮廓 |
| 关动画换了一个人 | 静图不是同一张定稿 |
| 表情文件对不上 | 没用 `smile` 这种 id |
| Web 贴图发黑/发白 | 预乘 alpha 没写 |
| 不能进仓 | LICENSE 没写 AI 来源和分发权 |

