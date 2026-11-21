import { useId, useState, type FormEvent } from 'react'
import { putCaptureRules } from '../api'
import { captureRulesPayload } from '../capture'
import type { ConfigSnapshot } from '../types'

interface Props {
  snapshot: ConfigSnapshot
  onSaved: (snapshot: ConfigSnapshot) => void
  onClose: () => void
}

/** Popover form for the three capture rules; saves through PUT /api/capture/rules. */
export default function CaptureRules({ snapshot, onSaved, onClose }: Props) {
  const [empty, setEmpty] = useState<'all' | 'none'>(snapshot.file.capture_empty)
  const [httpsMode, setHttpsMode] = useState<'decrypt' | 'tunnel'>(snapshot.file.capture_https)
  const [hostsText, setHostsText] = useState(snapshot.file.capture_hosts.join('\n'))
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')
  const hostsId = useId()

  const onSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (saving) return
    setSaving(true)
    setSaveError('')
    try {
      const next = await putCaptureRules(captureRulesPayload(empty, httpsMode, hostsText))
      onSaved(next)
      onClose()
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : '无法写入配置')
    } finally {
      setSaving(false)
    }
  }

  return (
    <form className="rules-editor" role="dialog" aria-label="抓包规则" onSubmit={(event) => void onSubmit(event)}>
      <h2>抓包规则</h2>
      <div className="settings-row">
        <span className="settings-label">空规则</span>
        <button
          type="button"
          className={empty === 'all' ? 'btn seg-btn active' : 'btn seg-btn'}
          disabled={saving}
          onClick={() => setEmpty('all')}
        >
          抓全部
        </button>
        <button
          type="button"
          className={empty === 'none' ? 'btn seg-btn active' : 'btn seg-btn'}
          disabled={saving}
          onClick={() => setEmpty('none')}
        >
          不抓
        </button>
      </div>
      <div className="settings-row">
        <span className="settings-label">未抓 HTTPS</span>
        <button
          type="button"
          className={httpsMode === 'decrypt' ? 'btn seg-btn active' : 'btn seg-btn'}
          disabled={saving}
          onClick={() => setHttpsMode('decrypt')}
        >
          解密
        </button>
        <button
          type="button"
          className={httpsMode === 'tunnel' ? 'btn seg-btn active' : 'btn seg-btn'}
          disabled={saving}
          onClick={() => setHttpsMode('tunnel')}
        >
          隧道
        </button>
      </div>
      <div className="settings-row rules-editor-hosts">
        <label className="settings-label" htmlFor={hostsId}>抓包域名</label>
        <div className="rules-editor-hosts-field">
          <textarea
            id={hostsId}
            className="input rules-hosts"
            disabled={saving}
            rows={3}
            value={hostsText}
            placeholder={'example.com\n*.example.com\nexample.com:8443'}
            onChange={(event) => setHostsText(event.target.value)}
          />
          <span className="settings-note">保存后立即生效</span>
        </div>
      </div>
      {snapshot.file.capture ? null : <p className="rules-note">抓包关闭时以上规则不生效</p>}
      <div className="settings-row">
        <span className="settings-label" />
        <button className="btn" type="submit" disabled={saving}>保存</button>
        <button className="btn" type="button" disabled={saving} onClick={onClose}>关闭</button>
      </div>
      {saveError ? <p className="settings-error">{saveError}</p> : null}
    </form>
  )
}
