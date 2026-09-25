# 前端交付与接口对接

当前交付范围为静态前端演示，保留原生 HTML/CSS/JS。真实鉴权、设备、数据库、消息发送由后端负责；不要把前端的演示账号当作授权边界。

## 入口和状态

- `index.html`：登录后的居民端；`login.html`：演示登录；`个人端.html`：兼容跳转。
- `管理端.html`：模拟监测、预警和档案；`home.html`：机构样例展示。
- `yangheng.html`：内容筛选、详情、收藏；`privacy.html`：实际本地存储和第三方AI说明。
- 演示指标/位置必须保留来源标记。接口加载失败不得回填随机数据冒充成功。
- 短信、邮件、推送未接入时保持禁用；不得仅弹成功提示。

## 健康数据适配接口

当前 `window.API` 由 `js/mock-api.js` 提供。对接真实接口时提供相同入口或集中替换适配层，视图不应分散拼接URL。

| 接口 | 前端使用的结果 | 失败行为 |
|---|---|---|
| `getPatients()` | 居民列表：id/name/gender/age/status/heartRate/bloodOxygen/temperature/systolic/diastolic/bloodSugar/admitted/place | reject，视图显示加载失败 |
| `getPatientDetail(id)` | 上述字段及medicalHistory、height/weight/chronic等；不存在返回null | reject，保留错误状态 |
| `getAlerts()` | 当前活动事件数组，包含已知晓但异常未恢复的事件 | reject，不伪造空列表 |
| `acknowledgeAlert(eventId)` | `{success:true,eventId,state:'acknowledged'}` | 失效事件返回`{success:false,reason}`；存储/网络失败reject |
| `getLocations()` | 带id/name/x/y/place/fenceStatus/status的模拟地图点 | 真定位须另行定义坐标系和采样时间 |

告警对象：`{eventId,id,name,type:'health'|'fence',state:'triggered'|'acknowledged',status:'danger',alertMsg,place}`。

`id`是居民ID，`eventId`是事件ID。一次持续异常的ID保持不变，数值或文案轻微变化不生成新事件。异常恢复后再次发生必须生成新ID。前端“已知晓”只进入处理中；没有真实处置结果时，不能由关闭弹窗/发送消息推导“已解决”。异常恢复由数据源决定。生产接口需补时间戳、操作者、处理结果及幂等语义。

## 浏览器存储契约

- `DataStore.set/saveChat/saveChats/remove`返回Promise；写入结果仅在事务完成后为true，失败为false或reject，调用者都必须处理。
- AI配置只有确认保存成功后才能关闭弹窗并更新会话配置；密钥不能退化为明文localStorage。
- 聊天采用每用户快照。IndexedDB失败时使用每用户localStorage备用快照，恢复后读取备用快照并在下一次成功保存时清理，全部存储失败时提示先导出。
- localStorage记录只能保证本机演示，不保证多设备、隐私账号隔离或并发事务。不要把真实患者信息打包到data目录。

## AI 对接

`js/config.js`集中提供`aiMode`和`aiEndpoint`。当前为用户自行填写密钥、浏览器直连DeepSeek。关闭健康上下文只阻止自动附加的模拟档案，不阻止聊天本身发送。若以后使用后端代理，应由后端持有服务密钥，前端不提交或内嵌机构密钥。

## 本地回归

```sh
python -m pip install -r requirements-test.txt
python -m playwright install chromium
python tests/check_static.py
python -m unittest discover -s tests -v
```

需要Node.js运行语法检查。浏览器测试会自行启动临时HTTP服务和独立上下文，不操作个人浏览器或线上数据，拦截外部AI请求。

GitHub Actions会在main推送及PR时运行上述检查。工作流成功不是医疗业务认证。GitHub Pages现有部署与测试可能并行；若团队要求失败禁止上线，需要把该检查设为保护规则，并将Pages发布串接到检查成功后。

## 维护与验收

新增页面/控件时测试正常、空、失败、存储受限和键盘路径。修改全局样式时回归320/390/768/1440px；新增异步写入必须测试失败返回。部署后对比发布提交并冒烟内容页、登录及关键菜单。回滚使用git revert生成新提交，保留历史。
