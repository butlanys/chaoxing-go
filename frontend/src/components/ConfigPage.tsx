import { useEffect, useMemo, useState } from 'react'
import {
  App as AntdApp,
  AutoComplete,
  Button,
  Card,
  Divider,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Slider,
  Space,
  Switch,
  Table,
  Tag,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  DeleteOutlined,
  DownloadOutlined,
  FolderOpenOutlined,
  PlusOutlined,
  ReloadOutlined,
  SaveOutlined,
  UndoOutlined,
} from '@ant-design/icons'
import { bindings, errText } from '../bindings'
import { useStore } from '../store'
import { CommonConfig, Config, defaultConfig, normalizeConfig } from '../types'
import { NOTIFY_PROVIDERS, NOT_OPEN_ACTIONS, TIKU_PROVIDERS, toNum, toStr } from '../utils'

/** 有专属输入框的 tiku 键, 其余键走“自定义键值对”表格 */
const KNOWN_TIKU_KEYS = new Set([
  'provider',
  'check_llm_connection',
  'submit',
  'cover_rate',
  'delay',
  'tokens',
  'url',
  'endpoint',
  'key',
  'model',
  'min_interval_seconds',
  'http_proxy',
  'siliconflow_key',
  'siliconflow_model',
  'siliconflow_endpoint',
  'manual_mode_default',
  'manual_mode_separator',
])

function boolStr(v: unknown, def = false): boolean {
  if (typeof v === 'boolean') return v
  const s = toStr(v).trim().toLowerCase()
  if (s === 'true' || s === '1' || s === 'yes' || s === 'y') return true
  if (s === 'false' || s === '0' || s === 'no' || s === 'n') return false
  return def
}

function cloneConfig(c: Config): Config {
  // 先归一化: 后端 ini 缺节时 tiku/notification 会是 null, 直接展开会在渲染期抛错
  const n = normalizeConfig(c)
  return {
    common: { ...n.common, course_list: [...n.common.course_list] },
    tiku: { ...n.tiku },
    notification: { ...n.notification },
    path: n.path,
  }
}

export default function ConfigPage() {
  const { message } = AntdApp.useApp()
  const { config, setConfig } = useStore()

  const [draft, setDraft] = useState<Config>(() => cloneConfig(config))
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saveAsOpen, setSaveAsOpen] = useState(false)
  const [saveAsPath, setSaveAsPath] = useState('')
  const [extraKeyDraft, setExtraKeyDraft] = useState('')

  // 后端配置变化 (首次加载 / 重新加载 / 导入) 时同步草稿, 避免覆盖用户未保存的修改
  useEffect(() => {
    if (dirty) return
    setDraft(cloneConfig(config))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [config])

  const common = draft.common
  const tiku = draft.tiku
  const notif = draft.notification

  function patchCommon(patch: Partial<CommonConfig>) {
    setDirty(true)
    setDraft((prev) => ({ ...prev, common: { ...prev.common, ...patch } }))
  }

  function setTikuKey(key: string, value: string) {
    setDirty(true)
    setDraft((prev) => ({ ...prev, tiku: { ...prev.tiku, [key]: value } }))
  }

  function removeTikuKey(key: string) {
    setDirty(true)
    setDraft((prev) => {
      const next = { ...prev.tiku }
      delete next[key]
      return { ...prev, tiku: next }
    })
  }

  function renameTikuKey(oldKey: string, newKey: string) {
    const k = newKey.trim()
    if (!k || k === oldKey) return
    setDirty(true)
    setDraft((prev) => {
      const next = { ...prev.tiku }
      const val = next[oldKey] ?? ''
      delete next[oldKey]
      next[k] = val
      return { ...prev, tiku: next }
    })
  }

  function setNotifKey(key: string, value: string) {
    setDirty(true)
    setDraft((prev) => ({ ...prev, notification: { ...prev.notification, [key]: value } }))
  }

  function addExtraKey() {
    const k = extraKeyDraft.trim()
    if (!k) {
      message.info('请输入键名')
      return
    }
    if (KNOWN_TIKU_KEYS.has(k)) {
      message.warning(`“${k}” 已有专属输入框, 请直接在上方填写`)
      return
    }
    setTikuKey(k, '')
    setExtraKeyDraft('')
  }

  async function save() {
    setSaving(true)
    try {
      await bindings.saveConfig(cloneConfig(draft))
      setConfig(cloneConfig(draft))
      setDirty(false)
      message.success('配置已保存')
    } catch (e) {
      message.error(`保存配置失败: ${errText(e)}`)
    } finally {
      setSaving(false)
    }
  }

  async function reload() {
    try {
      const cfg = await bindings.loadConfig()
      if (cfg) {
        setConfig(cfg)
        setDraft(cloneConfig(cfg))
        setDirty(false)
        message.success('已重新加载配置')
      }
    } catch (e) {
      message.error(`加载配置失败: ${errText(e)}`)
    }
  }

  async function importFromFile() {
    try {
      const path = await bindings.pickConfigFile()
      if (!path) {
        message.info('已取消选择')
        return
      }
      const cfg = await bindings.loadConfigFile(path)
      if (cfg) {
        setConfig(cfg)
        setDraft(cloneConfig(cfg))
        setDirty(false)
        message.success(`已导入配置: ${path}`)
      }
    } catch (e) {
      message.error(`导入配置失败: ${errText(e)}`)
    }
  }

  function openSaveAs() {
    setSaveAsPath(draft.path || '~/.chaoxing-go/config.ini')
    setSaveAsOpen(true)
  }

  async function doSaveAs() {
    const path = saveAsPath.trim()
    if (!path) {
      message.warning('请输入保存路径')
      return
    }
    try {
      await bindings.saveConfigAs(path, cloneConfig(draft))
      setDraft((prev) => ({ ...prev, path }))
      setConfig({ ...cloneConfig(draft), path })
      setDirty(false)
      setSaveAsOpen(false)
      message.success(`配置已另存为: ${path}`)
    } catch (e) {
      message.error(`另存配置失败: ${errText(e)}`)
    }
  }

  function resetDefault() {
    const def = defaultConfig()
    def.path = draft.path
    setDraft(def)
    setDirty(true)
    message.info('已恢复默认值, 记得点击“保存配置”生效')
  }

  const extraKeys = useMemo(
    () => Object.keys(tiku).filter((k) => !KNOWN_TIKU_KEYS.has(k)),
    [tiku],
  )

  const extraColumns: ColumnsType<{ key: string; value: string }> = [
    {
      title: '键 (key)',
      dataIndex: 'key',
      width: 260,
      render: (v: string) => (
        <Input
          size="small"
          value={v}
          className="cx-mono"
          onChange={(e) => renameTikuKey(v, e.target.value)}
          onBlur={(e) => {
            const nv = e.target.value.trim()
            if (!nv) renameTikuKey(v, v)
          }}
        />
      ),
    },
    {
      title: '值 (value)',
      dataIndex: 'value',
      render: (_: string, r) => (
        <Input
          size="small"
          value={tiku[r.key] ?? ''}
          className="cx-mono"
          onChange={(e) => setTikuKey(r.key, e.target.value)}
        />
      ),
    },
    {
      title: '操作',
      key: 'op',
      width: 70,
      render: (_: unknown, r) => (
        <Popconfirm title={`删除键 ${r.key}?`} okText="删除" cancelText="取消" onConfirm={() => removeTikuKey(r.key)}>
          <Button type="link" size="small" danger icon={<DeleteOutlined />} />
        </Popconfirm>
      ),
    },
  ]

  const extraRows = extraKeys.map((k) => ({ key: k, value: tiku[k] ?? '' }))

  return (
    <div>
      <Card
        className="cx-card"
        size="small"
        title="配置文件"
        extra={
          <Space size={6}>
            {dirty && <Tag color="orange">有未保存的修改</Tag>}
            <Tag color={draft.path ? 'blue' : 'default'}>{draft.path || '尚未指定路径'}</Tag>
          </Space>
        }
      >
        <Space wrap>
          <Button type="primary" icon={<SaveOutlined />} loading={saving} onClick={save}>
            保存配置
          </Button>
          <Button icon={<FolderOpenOutlined />} onClick={importFromFile}>
            从文件导入
          </Button>
          <Button icon={<DownloadOutlined />} onClick={openSaveAs}>
            另存为…
          </Button>
          <Button icon={<ReloadOutlined />} onClick={reload}>
            重新加载
          </Button>
          <Popconfirm title="恢复为默认配置?" okText="恢复" cancelText="取消" onConfirm={resetDefault}>
            <Button icon={<UndoOutlined />} danger>
              恢复默认
            </Button>
          </Popconfirm>
        </Space>
      </Card>

      <Card className="cx-card" size="small" title="运行参数">
        <Form layout="vertical">
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fill, minmax(240px, 1fr))',
              columnGap: 16,
            }}
          >
            <Form.Item label="默认使用 Cookie 登录">
              <Switch checked={common.use_cookies} onChange={(v) => patchCommon({ use_cookies: v })} />
            </Form.Item>
            <Form.Item label="默认手机号">
              <Input
                value={common.username}
                placeholder="CLI 模式使用的默认账号"
                onChange={(e) => patchCommon({ username: e.target.value })}
              />
            </Form.Item>
            <Form.Item label="默认密码">
              <Input.Password
                value={common.password}
                placeholder="CLI 模式使用的默认密码"
                autoComplete="new-password"
                onChange={(e) => patchCommon({ password: e.target.value })}
              />
            </Form.Item>
            <Form.Item label="默认课程列表 (course_list)" tooltip="留空表示全部课程, 回车分隔">
              <Select
                mode="tags"
                value={common.course_list}
                placeholder="不填则学习全部课程"
                tokenSeparators={[',', ' ']}
                onChange={(v) => patchCommon({ course_list: v })}
                options={[]}
              />
            </Form.Item>
          </div>

          <Divider style={{ margin: '4px 0 14px' }} />

          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))',
              columnGap: 16,
            }}
          >
            <Form.Item label={`视频倍速 (speed): ${common.speed.toFixed(1)}x`} tooltip="1 ~ 2 倍速">
              <Slider
                min={1}
                max={2}
                step={0.1}
                value={toNum(common.speed, 1)}
                onChange={(v) => patchCommon({ speed: v })}
                marks={{ 1: '1x', 1.5: '1.5x', 2: '2x' }}
              />
            </Form.Item>
            <Form.Item label="并发章节数 (jobs)" tooltip="单账号同时处理的章节数量">
              <InputNumber
                min={1}
                max={32}
                style={{ width: '100%' }}
                value={common.jobs}
                onChange={(v) => patchCommon({ jobs: toNum(v, 4) })}
              />
            </Form.Item>
            <Form.Item label="遇到关闭章节的动作 (notopen_action)">
              <Select
                value={common.notopen_action}
                options={NOT_OPEN_ACTIONS}
                onChange={(v) => patchCommon({ notopen_action: v })}
              />
            </Form.Item>
            <Form.Item label="重试间隔 (retry_interval, 秒)">
              <InputNumber
                min={0}
                max={120}
                step={0.5}
                style={{ width: '100%' }}
                value={common.retry_interval}
                onChange={(v) => patchCommon({ retry_interval: toNum(v, 1) })}
              />
            </Form.Item>
            <Form.Item label="章节检测答错后自动重做 (work_redo_enabled)">
              <Switch
                checked={common.work_redo_enabled}
                onChange={(v) => patchCommon({ work_redo_enabled: v })}
              />
            </Form.Item>
            <Form.Item label="最大重做次数 (work_max_retries)">
              <InputNumber
                min={1}
                max={10}
                style={{ width: '100%' }}
                value={common.work_max_retries}
                onChange={(v) => patchCommon({ work_max_retries: toNum(v, 3) })}
              />
            </Form.Item>
            <Form.Item label="刷完后增加章节学习次数 (add_learning_count)">
              <Switch
                checked={common.add_learning_count}
                onChange={(v) => patchCommon({ add_learning_count: v })}
              />
            </Form.Item>
            <Form.Item label="目标学习次数 (target_count)">
              <InputNumber
                min={1}
                max={10000}
                style={{ width: '100%' }}
                value={common.target_count}
                disabled={!common.add_learning_count}
                onChange={(v) => patchCommon({ target_count: toNum(v, 100) })}
              />
            </Form.Item>
          </div>
        </Form>
      </Card>

      <Card className="cx-card" size="small" title="题库配置 (tiku)">
        <Form layout="vertical">
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fill, minmax(250px, 1fr))',
              columnGap: 16,
            }}
          >
            <Form.Item
              label="题库提供方 (provider)"
              tooltip="支持多个题库按顺序回退, 用英文逗号分隔, 例如 TikuGo,TikuYanxi"
            >
              <AutoComplete
                value={tiku.provider ?? ''}
                options={TIKU_PROVIDERS.map((p) => ({ value: p }))}
                placeholder="TikuYanxi / TikuGo,TikuLike / AI ..."
                onChange={(v) => setTikuKey('provider', v)}
                filterOption={(input, opt) =>
                  String(opt?.value ?? '').toLowerCase().includes(input.toLowerCase())
                }
              />
            </Form.Item>
            <Form.Item label="是否提交答题 (submit)">
              <Switch
                checked={boolStr(tiku.submit, false)}
                onChange={(v) => setTikuKey('submit', v ? 'true' : 'false')}
              />
            </Form.Item>
            <Form.Item label="最低题库覆盖率 (cover_rate)" tooltip="0 ~ 1, 默认 0.9">
              <InputNumber
                min={0}
                max={1}
                step={0.05}
                style={{ width: '100%' }}
                value={toNum(tiku.cover_rate, 0.9)}
                onChange={(v) => setTikuKey('cover_rate', String(toNum(v, 0.9)))}
              />
            </Form.Item>
            <Form.Item label="查询间隔 (delay, 秒)">
              <InputNumber
                min={0}
                max={60}
                step={0.5}
                style={{ width: '100%' }}
                value={toNum(tiku.delay, 1)}
                onChange={(v) => setTikuKey('delay', String(toNum(v, 1)))}
              />
            </Form.Item>
            <Form.Item label="题库 Token (tokens)" tooltip="多个 token 用英文逗号分隔">
              <Input
                value={tiku.tokens ?? ''}
                className="cx-mono"
                placeholder="token1,token2"
                onChange={(e) => setTikuKey('tokens', e.target.value)}
              />
            </Form.Item>
            <Form.Item label="检查大模型连接 (check_llm_connection)">
              <Switch
                checked={boolStr(tiku.check_llm_connection, true)}
                onChange={(v) => setTikuKey('check_llm_connection', v ? 'true' : 'false')}
              />
            </Form.Item>
            <Form.Item label="TikuAdapter 地址 (url)">
              <Input
                value={tiku.url ?? ''}
                className="cx-mono"
                placeholder="http://127.0.0.1:8080"
                onChange={(e) => setTikuKey('url', e.target.value)}
              />
            </Form.Item>
            <Form.Item label="AI Endpoint (endpoint)" tooltip="需带 /v1 路径, 例如 https://api.example.com/v1">
              <Input
                value={tiku.endpoint ?? ''}
                className="cx-mono"
                placeholder="https://api.example.com/v1"
                onChange={(e) => setTikuKey('endpoint', e.target.value)}
              />
            </Form.Item>
            <Form.Item label="AI Key (key)">
              <Input.Password
                value={tiku.key ?? ''}
                className="cx-mono"
                placeholder="sk-..."
                autoComplete="new-password"
                onChange={(e) => setTikuKey('key', e.target.value)}
              />
            </Form.Item>
            <Form.Item label="AI 模型 (model)">
              <Input
                value={tiku.model ?? ''}
                className="cx-mono"
                placeholder="gpt-4o-mini / deepseek-chat ..."
                onChange={(e) => setTikuKey('model', e.target.value)}
              />
            </Form.Item>
            <Form.Item label="请求最小间隔 (min_interval_seconds)">
              <InputNumber
                min={0}
                max={600}
                style={{ width: '100%' }}
                value={toNum(tiku.min_interval_seconds, 3)}
                onChange={(v) => setTikuKey('min_interval_seconds', String(toNum(v, 3)))}
              />
            </Form.Item>
            <Form.Item label="请求代理 (http_proxy)">
              <Input
                value={tiku.http_proxy ?? ''}
                className="cx-mono"
                placeholder="http://127.0.0.1:7890"
                onChange={(e) => setTikuKey('http_proxy', e.target.value)}
              />
            </Form.Item>
            <Form.Item label="硅基流动 Key (siliconflow_key)">
              <Input.Password
                value={tiku.siliconflow_key ?? ''}
                className="cx-mono"
                placeholder="sk-..."
                autoComplete="new-password"
                onChange={(e) => setTikuKey('siliconflow_key', e.target.value)}
              />
            </Form.Item>
            <Form.Item label="硅基流动模型 (siliconflow_model)">
              <Input
                value={tiku.siliconflow_model ?? ''}
                className="cx-mono"
                onChange={(e) => setTikuKey('siliconflow_model', e.target.value)}
              />
            </Form.Item>
            <Form.Item label="硅基流动 Endpoint (siliconflow_endpoint)">
              <Input
                value={tiku.siliconflow_endpoint ?? ''}
                className="cx-mono"
                onChange={(e) => setTikuKey('siliconflow_endpoint', e.target.value)}
              />
            </Form.Item>
            <Form.Item label="手动模式默认 (manual_mode_default)">
              <Select
                value={tiku.manual_mode_default || 'batch'}
                options={[
                  { value: 'batch', label: 'batch · 一次性输入全部' },
                  { value: 'single', label: 'single · 一题一输入' },
                ]}
                onChange={(v) => setTikuKey('manual_mode_default', v)}
              />
            </Form.Item>
            <Form.Item label="手动模式分隔符 (manual_mode_separator)">
              <Select
                value={tiku.manual_mode_separator || ';'}
                options={[
                  { value: ';', label: '分号 ;' },
                  { value: ',', label: '逗号 ,' },
                  { value: 'space', label: '空格 space' },
                  { value: '\\n', label: '换行 \\n' },
                ]}
                onChange={(v) => setTikuKey('manual_mode_separator', v)}
              />
            </Form.Item>
          </div>
        </Form>

        <Divider orientation="left" style={{ fontSize: 12, margin: '4px 0 12px' }}>
          其余键值对
        </Divider>
        <div className="cx-toolbar">
          <Input
            size="small"
            style={{ width: 220 }}
            placeholder="新键名, 如 likeapi_model"
            className="cx-mono"
            value={extraKeyDraft}
            onChange={(e) => setExtraKeyDraft(e.target.value)}
            onPressEnter={addExtraKey}
          />
          <Button size="small" icon={<PlusOutlined />} onClick={addExtraKey}>
            添加键
          </Button>
          <span className="cx-hint">
            共 {extraKeys.length} 个自定义键 · 保存时会与上方字段一并写入 [tiku] 节
          </span>
        </div>
        <Table<{ key: string; value: string }>
          className="cx-table"
          rowKey="key"
          size="small"
          dataSource={extraRows}
          columns={extraColumns}
          pagination={false}
          locale={{ emptyText: '暂无自定义键 (likeapi_* / go_* / true_list 等可在此维护)' }}
        />
      </Card>

      <Card className="cx-card" size="small" title="通知配置 (notification)">
        <Form layout="vertical">
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))',
              columnGap: 16,
            }}
          >
            <Form.Item label="通知提供方 (provider)">
              <Select
                value={notif.provider ?? ''}
                options={NOTIFY_PROVIDERS.map((p) => ({
                  value: p,
                  label: p === '' ? '(不启用)' : p,
                }))}
                onChange={(v) => setNotifKey('provider', v)}
              />
            </Form.Item>
            <Form.Item label="推送地址 (url)" tooltip="Server酱 / Qmsg / Bark / Telegram 的完整推送 URL">
              <Input
                value={notif.url ?? ''}
                className="cx-mono"
                placeholder="https://sctapi.ftqq.com/xxx.send"
                onChange={(e) => setNotifKey('url', e.target.value)}
              />
            </Form.Item>
            <Form.Item label="Telegram chat_id (tg_chat_id)">
              <Input
                value={notif.tg_chat_id ?? ''}
                className="cx-mono"
                placeholder="123456789"
                onChange={(e) => setNotifKey('tg_chat_id', e.target.value)}
              />
            </Form.Item>
          </div>
        </Form>
      </Card>

      <Modal
        title="配置另存为"
        open={saveAsOpen}
        onCancel={() => setSaveAsOpen(false)}
        onOk={doSaveAs}
        okText="保存"
        cancelText="取消"
      >
        <div className="cx-hint" style={{ marginBottom: 8 }}>
          输入目标 ini 文件的完整路径 (例如 /root/.chaoxing-go/config.ini)
        </div>
        <Input
          className="cx-mono"
          value={saveAsPath}
          onChange={(e) => setSaveAsPath(e.target.value)}
          onPressEnter={doSaveAs}
          placeholder="/path/to/config.ini"
        />
      </Modal>
    </div>
  )
}
