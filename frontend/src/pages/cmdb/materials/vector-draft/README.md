# 10 款机柜设备矢量素材 · 制作与评审历史

这批素材已获用户确认，并复制到 ../vectors/ 和 ../panels/ 发布。本目录保留制作历史；
后续草稿修改不会自动影响已发布素材。

本目录包含 20 张独立的透明背景 SVG 面板（每款正背面各一张）、三组 PNG 预览、
完整总览和离线 HTML 图库。发布使用的目录与桌面副本见上一级 README.md。

统一的是正交视角、边线、光照、端口及机械细节的表达；不统一产品本身的配色：

- HPE：黑色密孔 / 纵栅面罩、银色肩框、绿色品牌矩形、紫色电源卡扣；Gen10 与 Gen11 分开。
- Dell：银黑机身、15G 银色几何面罩、DELL EMC、橙色电源卡扣；R740 改用用户确认的 14G 银灰蜂窝面罩整机外观。
- Lenovo：黑色竖盘架、蜂窝空位、白色竖向 Lenovo 标识、银色背板与橙色电源卡扣。
- Huawei：黑色盘架、白色花瓣标识、蓝色 USB / 接口、绿色网卡区域、黄绿色电源卡扣。
- Inspur：蓝灰盘托与 12 个横向盘位、浪潮信息标识、银色背板和彩色接口。
- H3C：保留已评审画风及 Logo；上排网口卡扣朝上，下排朝下。

每张面板使用 `1200 × (120 × U数)` viewBox，带挂耳、透明底，不包含外链或嵌入位图。
它们是基于参考配置简化绘制的运维图形，并非机械制图或精确 BOM。
接口灯采用中性色；素材中的盘位和接口数量不写入 CMDB 配置，也不表示在线状态。

`index.html` 可离线打开并逐张查看 SVG。`review-hpe.png`、`review-dell.png`、
`review-other.png` 便于聊天评审。`review-all.svg` 是全部 10 款正背面对照。

各型号目录的 reference.json 保留原产品图来源与原件 SHA-256。
品牌标识来源见 `brand-marks/README.md`。H3C 的官方结构依据另见其目录说明。

重新生成其余九款：`python3 scripts/build-cmdb-vector-drafts.py`。
H3C：`python3 front/cmdb/src/materials/vector-draft/h3c-s5130s-28s-ei/build_sample.py`。
生成器只写本评审目录，不替换生产目录、数据库或已发布面板。
