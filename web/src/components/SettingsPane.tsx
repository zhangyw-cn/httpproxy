import { useEffect, useRef, useState, type FormEvent } from 'react'
import { putConfig } from '../api'
import { limitNote } from '../settings'
import type { ConfigSnapshot } from '../types'

interface Props {
  snapshot: ConfigSnapshot | null
  loadError: boolean
  open: boolean
  onSaved: (snapshot: ConfigSnapshot) => void
}

export default function SettingsPane({ snapshot, loadError, open, onSaved }: Props) {
  const [proxy, setProxy] = useState(snapshot?.file.proxy ?? '')
  const [ui, setUi] = useState(snapshot?.file.ui ?? '')
  const [theme, setTheme] = useState(snapshot?.file.theme ?? 'dark')
  const [maxFlows, setMaxFlows] = useState(snapshot ? String(snapshot.file.max_flows) : '')
  const [maxBody, setMaxBody] = useState(snapshot ? String(snapshot.file.max_body) : '')
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')
  const savingRef = useRef(false)

  useEffect(() => {
    if (!snapshot) return
    setProxy(snapshot.file.proxy)
    setUi(snapshot.file.ui)
    setTheme(snapshot.file.theme)
    setMaxFlows(String(snapshot.file.max_flows))
    setMaxBody(String(snapshot.file.max_body))
  }, [snapshot?.file.proxy, snapshot?.file.ui, snapshot?.file.max_flows, snapshot?.file.max_body, snapshot?.file.theme])

  if (snapshot == null) {
    return (
      <div className="settings" hidden={!open}>
        {loadError ? <p className="settings-error">无法读取配置</p> : <p>正在读取配置</p>}
      </div>
    )
  }

  const flowsNote = limitNote(snapshot.file.max_flows, snapshot.runtime.max_flows)
  const bodyNote = limitNote(snapshot.file.max_body, snapshot.runtime.max_body)

  const onSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (savingRef.current) return
    savingRef.current = true
    setSaving(true)
    setSaveError('')
    try {
      const next = await putConfig({
        proxy,
        ui,
        max_flows: Number(maxFlows),
        max_body: Number(maxBody),
        theme,
      })
      onSaved(next)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : '无法写入配置')
    } finally {
      savingRef.current = false
      setSaving(false)
    }
  }

  return (
    <form className="settings" hidden={!open} onSubmit={(event) => void onSubmit(event)}>
      <h1>设置</h1>
      <div className="settings-row">
        <span className="settings-label">主题</span>
        <button
          type="button"
          className={theme === 'dark' ? 'btn seg-btn active' : 'btn seg-btn'}
          disabled={saving}
          onClick={() => setTheme('dark')}
        >
          黑色
        </button>
        <button
          type="button"
          className={theme === 'light' ? 'btn seg-btn active' : 'btn seg-btn'}
          disabled={saving}
          onClick={() => setTheme('light')}
        >
          浅色
        </button>
      </div>
      <div className="settings-row">
        <label className="settings-label" htmlFor="settings-proxy">代理地址</label>
        <input
          id="settings-proxy"
          className="input"
          disabled={saving}
          value={proxy}
          onChange={(event) => setProxy(event.target.value)}
        />
        <span className="settings-note">当前 {snapshot.runtime.proxy}</span>
        <span className="settings-note">重启后生效</span>
      </div>
      <div className="settings-row">
        <label className="settings-label" htmlFor="settings-ui">UI 地址</label>
        <input
          id="settings-ui"
          className="input"
          disabled={saving}
          value={ui}
          onChange={(event) => setUi(event.target.value)}
        />
        <span className="settings-note">当前 {snapshot.runtime.ui}</span>
        <span className="settings-note">重启后生效</span>
      </div>
      <div className="settings-row">
        <label className="settings-label" htmlFor="settings-max-flows">保留条数</label>
        <input
          id="settings-max-flows"
          className="input"
          disabled={saving}
          value={maxFlows}
          onChange={(event) => setMaxFlows(event.target.value)}
        />
        {flowsNote ? <span className="settings-note">{flowsNote}</span> : null}
      </div>
      <div className="settings-row">
        <label className="settings-label" htmlFor="settings-max-body">正文上限</label>
        <input
          id="settings-max-body"
          className="input"
          disabled={saving}
          value={maxBody}
          onChange={(event) => setMaxBody(event.target.value)}
        />
        {bodyNote ? <span className="settings-note">{bodyNote}</span> : null}
      </div>
      <div className="settings-row">
        <span className="settings-label" />
        <button className="btn" type="submit" disabled={saving}>保存</button>
        {saveError ? <p className="settings-error">{saveError}</p> : null}
      </div>
    </form>
  )
}
