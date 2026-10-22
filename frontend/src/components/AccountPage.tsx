import { useEffect, useMemo, useState } from 'react'
import {
  App as AntdApp,
  Button,
  Card,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Tooltip,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  DeleteOutlined,
  EditOutlined,
  LoginOutlined,
  PlusOutlined,
  ReloadOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { bindings, errText } from '../bindings'
import { useStore } from '../store'
import { AccountInput, AccountView } from '../types'
import { courseKeyOf, formatTime } from '../utils'

interface FormValues {
  username: string
  password: string
  remark: string
  enabled: boolean
  use_cookies: boolean
  cookies: string
  course_ids: string[]
}

const emptyForm: FormValues = {
  username: '',
  password: '',
  remark: '',
  enabled: true,
  use_cookies: false,
  cookies: '',
  course_ids: [],
}

export default function AccountPage() {
  const { message } = AntdApp.useApp()
  const {
    accounts,
    accountsLoading,
    reloadAccounts,
    courses,
    selectedAccounts,
    setSelectedAccounts,
  } = useStore()

  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<AccountView | null>(null)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState<string | null>(null)
  const [form] = Form.useForm<FormValues>()
  const useCookies = Form.useWatch('use_cookies', form)

  useEffect(() => {
    reloadAccounts().catch((e) => message.error(`加载账号失败: ${errText(e)}`))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const courseOptions = useMemo(() => {
    const out: Record<string, { label: string; value: string }[]> = {}
    accounts.forEach((a) => {
      const list = courses[a.id] ?? []
      const opts = list.map((c) => ({
        label: `${c.title || c.courseId} (${c.teacher || '未知教师'})`,
        value: courseKeyOf(c.courseId, c.clazzId),
      }))
      // 已保存但尚未拉取到课程时, 用原始 ID 兜底, 保证标签能正常显示
      const known = new Set(opts.map((o) => o.value))
      a.course_ids.forEach((k) => {
        if (!known.has(k)) opts.push({ label: k, value: k })
      })
      out[a.id] = opts
    })
    return out
  }, [accounts, courses])

  function openCreate() {
    setEditing(null)
    form.setFieldsValue({ ...emptyForm })
    setModalOpen(true)
  }

  function openEdit(acc: AccountView) {
    setEditing(acc)
    form.setFieldsValue({
      username: acc.username,
      password: '',
      remark: acc.remark,
      enabled: acc.enabled,
      use_cookies: acc.use_cookies,
      cookies: '',
      course_ids: [...acc.course_ids],
    })
    setModalOpen(true)
  }

  async function submit() {
    let values: FormValues
    try {
      values = await form.validateFields()
    } catch {
      return
    }
    const input: AccountInput = {
      id: editing?.id ?? '',
      username: values.username ?? '',
      password: values.password ?? '',
      remark: values.remark ?? '',
      enabled: !!values.enabled,
      use_cookies: !!values.use_cookies,
      cookies: values.cookies ?? '',
      course_ids: values.course_ids ?? [],
    }
    setSaving(true)
    try {
      if (editing) {
        await bindings.updateAccount(input)
        message.success('账号已更新')
      } else {
        await bindings.addAccount(input)
        message.success('账号已新增')
      }
      setModalOpen(false)
      await reloadAccounts()
    } catch (e) {
      message.error(errText(e))
    } finally {
      setSaving(false)
    }
  }

  async function remove(acc: AccountView) {
    try {
      await bindings.deleteAccount(acc.id)
      message.success(`已删除账号: ${acc.label}`)
      setSelectedAccounts((prev) => prev.filter((id) => id !== acc.id))
      await reloadAccounts()
    } catch (e) {
      message.error(errText(e))
    }
  }

  async function loginTest(acc: AccountView) {
    setTesting(acc.id)
    try {
      const res = await bindings.loginAccount(acc.id)
      if (res?.status) {
        message.success(
          `登录成功${res.realName ? ` · ${res.realName}` : ''}${res.msg ? ` (${res.msg})` : ''}`,
        )
      } else {
        message.warning(`登录失败: ${res?.msg || '未知原因'}`)
      }
      await reloadAccounts()
    } catch (e) {
      message.error(`登录测试失败: ${errText(e)}`)
    } finally {
      setTesting(null)
    }
  }

  async function batchLoginTest() {
    const targets = accounts.filter((a) => selectedAccounts.includes(a.id))
    if (!targets.length) {
      message.info('请先勾选要测试的账号')
      return
    }
    for (const acc of targets) {
      await loginTest(acc)
    }
  }

  const columns: ColumnsType<AccountView> = [
    {
      title: '标识',
      dataIndex: 'label',
      width: 140,
      render: (label: string, r) => (
        <Space size={4}>
          <span style={{ fontWeight: 500 }}>{label || r.id}</span>
          {!r.enabled && <Tag color="default">已禁用</Tag>}
        </Space>
      ),
    },
    { title: '手机号', dataIndex: 'username', width: 140, render: (v: string) => v || '—' },
    {
      title: '启用',
      dataIndex: 'enabled',
      width: 70,
      render: (v: boolean) => (v ? <Tag color="green">启用</Tag> : <Tag>停用</Tag>),
    },
    {
      title: '登录方式',
      key: 'auth',
      width: 150,
      render: (_, r) => (
        <Space size={4}>
          <Tag color={r.use_cookies ? 'blue' : 'default'}>
            {r.use_cookies ? 'Cookie 登录' : '账号密码'}
          </Tag>
          {r.has_password && <Tag color="geekblue">有密码</Tag>}
          {r.has_cookies && <Tag color="cyan">有 Cookie</Tag>}
        </Space>
      ),
    },
    {
      title: '上次状态',
      dataIndex: 'last_status',
      ellipsis: true,
      render: (v: string) =>
        v ? (
          <Tooltip title={v}>
            <span style={{ color: v.includes('异常') || v.includes('失败') ? '#ff4d4f' : undefined }}>
              {v}
            </span>
          </Tooltip>
        ) : (
          <span className="cx-hint">尚未登录</span>
        ),
    },
    {
      title: '上次登录',
      dataIndex: 'last_login',
      width: 160,
      render: (v: string) => <span className="cx-mono">{formatTime(v)}</span>,
    },
    {
      title: '已选课程',
      key: 'courseCount',
      width: 90,
      align: 'center',
      render: (_, r) => (r.course_ids?.length ? `${r.course_ids.length} 门` : '全部'),
    },
    {
      title: '操作',
      key: 'action',
      width: 220,
      fixed: 'right',
      render: (_, r) => (
        <Space size={2}>
          <Button
            type="link"
            size="small"
            icon={<LoginOutlined />}
            loading={testing === r.id}
            onClick={() => loginTest(r)}
          >
            登录测试
          </Button>
          <Button type="link" size="small" icon={<EditOutlined />} onClick={() => openEdit(r)}>
            编辑
          </Button>
          <Popconfirm
            title="确认删除该账号?"
            description="删除后该账号的本地密码与 Cookie 也会一并清除。"
            okText="删除"
            cancelText="取消"
            okButtonProps={{ danger: true }}
            onConfirm={() => remove(r)}
          >
            <Button type="link" size="small" danger icon={<DeleteOutlined />}>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  const modalCourseOptions = editing ? courseOptions[editing.id] ?? [] : []

  return (
    <div>
      <Card className="cx-card" size="small">
        <div className="cx-toolbar" style={{ marginBottom: 0 }}>
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            新增账号
          </Button>
          <Button
            icon={<ThunderboltOutlined />}
            onClick={batchLoginTest}
            disabled={!selectedAccounts.length}
          >
            批量登录测试 ({selectedAccounts.length})
          </Button>
          <Button
            icon={<ReloadOutlined />}
            loading={accountsLoading}
            onClick={() => reloadAccounts().catch((e) => message.error(errText(e)))}
          >
            刷新
          </Button>
          <div className="cx-toolbar-spacer" />
          <span className="cx-hint">共 {accounts.length} 个账号 · 已勾选 {selectedAccounts.length}</span>
        </div>
      </Card>

      <Card className="cx-card" size="small" styles={{ body: { padding: 0 } }}>
        <Table<AccountView>
          className="cx-table"
          rowKey="id"
          size="small"
          loading={accountsLoading}
          dataSource={accounts}
          columns={columns}
          scroll={{ x: 1180 }}
          rowSelection={{
            selectedRowKeys: selectedAccounts,
            onChange: (keys) => setSelectedAccounts(keys as string[]),
          }}
          pagination={{ pageSize: 12, showSizeChanger: true, size: 'small' }}
          locale={{ emptyText: '暂无账号，点击左上角“新增账号”添加' }}
        />
      </Card>

      <Modal
        title={editing ? `编辑账号 · ${editing.label}` : '新增账号'}
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={submit}
        confirmLoading={saving}
        okText="保存"
        cancelText="取消"
        width={620}
        destroyOnClose
      >
        <Form<FormValues>
          form={form}
          layout="vertical"
          initialValues={emptyForm}
          autoComplete="off"
          style={{ marginTop: 12 }}
        >
          <Form.Item
            label="手机号"
            name="username"
            rules={[
              ({ getFieldValue }) => ({
                validator(_, value: string) {
                  if (getFieldValue('use_cookies') || (value && value.trim())) {
                    return Promise.resolve()
                  }
                  return Promise.reject(new Error('手机号不能为空 (使用 Cookie 登录时可留空)'))
                },
              }),
            ]}
          >
            <Input placeholder="学习通登录手机号" allowClear />
          </Form.Item>

          <Form.Item
            label="密码"
            name="password"
            extra={editing ? '留空表示不修改原密码' : '使用 Cookie 登录时可留空'}
          >
            <Input.Password
              placeholder={editing ? '留空则不修改' : '学习通登录密码'}
              autoComplete="new-password"
            />
          </Form.Item>

          <Form.Item label="备注 (用于日志与进度展示)" name="remark">
            <Input placeholder="例如: 主账号" allowClear />
          </Form.Item>

          <Space size={32} align="start">
            <Form.Item label="启用" name="enabled" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item
              label="使用 Cookie 登录"
              name="use_cookies"
              valuePropName="checked"
              tooltip="开启后忽略账号密码, 直接使用下面的 Cookie 登录"
            >
              <Switch />
            </Form.Item>
          </Space>

          <Form.Item label="Cookie" name="cookies" extra="多个键值用分号分隔, 例如 UID=xxx; _uid=xxx;">
            <Input.TextArea
              rows={3}
              className="cx-mono"
              placeholder="UID=xxxxxx; _uid=xxxxxx;"
              disabled={!useCookies}
            />
          </Form.Item>

          <Form.Item
            label="指定课程 (courseId/clazzId)"
            name="course_ids"
            extra="留空表示该账号学习全部课程; 可直接从下拉选择, 也可手动输入后回车"
          >
            <Select
              mode="tags"
              allowClear
              placeholder="不选择则默认全部课程"
              options={modalCourseOptions}
              tokenSeparators={[',', ' ']}
              optionFilterProp="label"
            />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
