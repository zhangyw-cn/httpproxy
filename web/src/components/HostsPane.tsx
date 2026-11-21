import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import {
  activateHost,
  createHost,
  deleteHost,
  getConfig,
  getHost,
  listHosts,
  renameHost,
  saveHost,
} from '../api'
import { warningText } from '../overview'
import type { ConfigSnapshot, HostProfileInfo, HostsList } from '../types'

interface Props {
  writeLocked: boolean
  beginWrite: () => boolean
  endWrite: () => void
  onConfigChanged: (snapshot: ConfigSnapshot) => void
}

const DISCARD_CONFIRM = '未保存的修改会丢失，继续？'
const SAVED_FLASH_MS = 1500

/** The server matches profile names case-insensitively; keep the list's spelling. */
function sameName(a: string, b: string): boolean {
  return a.toLowerCase() === b.toLowerCase()
}

/** Hosts profile list + content editor; activation changes config.yaml. */
export default function HostsPane({ writeLocked, beginWrite, endWrite, onConfigChanged }: Props) {
  const [list, setList] = useState<HostsList | null>(null)
  const [listError, setListError] = useState('')
  const [selected, setSelected] = useState('')
  const [content, setContent] = useState('')
  const [savedContent, setSavedContent] = useState('')
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState(false)
  const [detailError, setDetailError] = useState('')
  const [actionError, setActionError] = useState('')
  const [savedFlash, setSavedFlash] = useState(false)
  const [warningsOpen, setWarningsOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [newName, setNewName] = useState('')
  const [createError, setCreateError] = useState('')
  const [renaming, setRenaming] = useState(false)
  const [renameName, setRenameName] = useState('')
  const [renameError, setRenameError] = useState('')
  const savedTimer = useRef<number | undefined>(undefined)
  const detailRequest = useRef(0)

  const profile = list?.profiles.find((p) => p.name === selected)
  const isActive = selected !== '' && sameName(selected, list?.active ?? '')
  const dirty = selected !== '' && content !== savedContent
  const locked = busy || writeLocked
  const warnings = profile?.warnings ?? []

  const loadContent = useCallback(async (name: string, initialError: string) => {
    const request = ++detailRequest.current
    setLoading(true)
    setDetailError(initialError)
    try {
      const detail = await getHost(name)
      if (request !== detailRequest.current) return
      setContent(detail.content)
      setSavedContent(detail.content)
    } catch (err) {
      if (request !== detailRequest.current) return
      setContent('')
      setSavedContent('')
      setDetailError(err instanceof Error ? err.message : '无法读取 Hosts 配置')
    } finally {
      if (request === detailRequest.current) setLoading(false)
    }
  }, [])

  const refreshList = useCallback(async (): Promise<HostsList | null> => {
    try {
      const next = await listHosts()
      setList(next)
      setListError('')
      return next
    } catch (err) {
      setListError(err instanceof Error ? err.message : '无法读取 Hosts 配置')
      return null
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const next = await listHosts()
        if (cancelled) return
        setList(next)
        setListError('')
        if (next.active === '') return
        const activeProfile = next.profiles.find((p) => sameName(p.name, next.active))
        if (!activeProfile) return
        setSelected(activeProfile.name)
        void loadContent(activeProfile.name, activeProfile.error)
      } catch (err) {
        if (!cancelled) setListError(err instanceof Error ? err.message : '无法读取 Hosts 配置')
      }
    })()
    return () => {
      cancelled = true
      detailRequest.current++
    }
  }, [loadContent])

  useEffect(() => () => window.clearTimeout(savedTimer.current), [])

  const confirmDiscard = (): boolean => !dirty || window.confirm(DISCARD_CONFIRM)

  const refreshConfig = async (fallback: string) => {
    try {
      onConfigChanged(await getConfig())
    } catch {
      setActionError(fallback)
    }
  }

  const selectProfile = (next: HostProfileInfo) => {
    if (next.name === selected || locked) return
    if (!confirmDiscard()) return
    setSelected(next.name)
    setContent('')
    setSavedContent('')
    setWarningsOpen(false)
    setSavedFlash(false)
    setRenaming(false)
    setRenameError('')
    setActionError('')
    void loadContent(next.name, next.error)
  }

  const startCreate = () => {
    if (locked) return
    setCreating(true)
    setNewName('')
    setCreateError('')
    setRenaming(false)
    setRenameError('')
  }

  const submitCreate = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const name = newName.trim()
    if (name === '' || locked) return
    if (!confirmDiscard()) return
    setBusy(true)
    setCreateError('')
    setActionError('')
    try {
      await createHost(name)
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : '无法创建 Hosts 配置')
      setBusy(false)
      return
    }
    setCreating(false)
    setNewName('')
    const next = await refreshList()
    if (!next) {
      setList((cur) =>
        cur ? { ...cur, profiles: [...cur.profiles, { name, rules: 0, warnings: [], error: '' }] } : cur,
      )
    }
    setSelected(name)
    setContent('')
    setSavedContent('')
    setDetailError('')
    setWarningsOpen(false)
    setBusy(false)
  }

  const startRename = () => {
    if (locked || selected === '') return
    setRenameName(selected)
    setRenameError('')
    setRenaming(true)
  }

  const submitRename = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const name = renameName.trim()
    if (name === '' || locked) return
    if (name === selected) {
      setRenaming(false)
      setRenameError('')
      return
    }
    const wasActive = isActive
    const oldName = selected
    const lock = wasActive
    if (lock && !beginWrite()) return
    setBusy(true)
    setRenameError('')
    setActionError('')
    let renamed = false
    try {
      await renameHost(oldName, name)
      renamed = true
    } catch (err) {
      setRenameError(err instanceof Error ? err.message : '无法重命名 Hosts 配置')
    } finally {
      if (lock) endWrite()
    }
    if (renamed) {
      setRenaming(false)
      setSelected(name)
      setList((cur) =>
        cur && {
          ...cur,
          active: wasActive ? name : cur.active,
          profiles: cur.profiles.map((p) => (p.name === oldName ? { ...p, name } : p)),
        },
      )
      await refreshList()
      if (wasActive) await refreshConfig('已重命名 Hosts 配置，但无法刷新配置状态')
    }
    setBusy(false)
  }

  const onSetActive = async () => {
    if (selected === '' || locked) return
    const target = isActive ? '' : selected
    if (!beginWrite()) return
    setBusy(true)
    setActionError('')
    try {
      const next = await activateHost(target)
      setList(next)
      setListError('')
      await refreshConfig('已更新 Hosts 配置，但无法刷新配置状态')
    } catch (err) {
      setActionError(
        err instanceof Error ? err.message : target === '' ? '无法停用 Hosts 配置' : '无法启用 Hosts 配置',
      )
    } finally {
      endWrite()
      setBusy(false)
    }
  }

  const onDelete = async () => {
    if (selected === '' || isActive || locked) return
    if (!window.confirm(`删除「${selected}」后无法恢复，继续？`)) return
    const name = selected
    setBusy(true)
    setActionError('')
    try {
      await deleteHost(name)
      setList((cur) => cur && { ...cur, profiles: cur.profiles.filter((p) => p.name !== name) })
      setSelected('')
      setContent('')
      setSavedContent('')
      setDetailError('')
      setWarningsOpen(false)
      setRenaming(false)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : '无法删除 Hosts 配置')
    } finally {
      setBusy(false)
    }
  }

  const onSave = async () => {
    if (selected === '' || locked || !dirty) return
    const name = selected
    const text = content
    const activeNow = isActive
    setBusy(true)
    setDetailError('')
    setActionError('')
    try {
      const result = await saveHost(name, text)
      setSavedContent(text)
      setWarningsOpen(false)
      setSavedFlash(true)
      window.clearTimeout(savedTimer.current)
      savedTimer.current = window.setTimeout(() => setSavedFlash(false), SAVED_FLASH_MS)
      setList((cur) =>
        cur
          ? {
              ...cur,
              active_error: activeNow ? '' : cur.active_error,
              profiles: cur.profiles.map((p) =>
                p.name === name ? { ...p, rules: result.rules, warnings: result.warnings, error: '' } : p,
              ),
            }
          : cur,
      )
      if (activeNow) await refreshConfig('已保存 Hosts 配置，但无法刷新配置状态')
    } catch (err) {
      setDetailError(err instanceof Error ? err.message : '无法保存 Hosts 配置')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="hosts">
      <div className="hosts-head">
        <h1>Hosts</h1>
        <button className="btn hosts-new" type="button" disabled={locked || creating} onClick={startCreate}>
          新建配置
        </button>
      </div>

      {list?.active_error ? (
        <p className="notice" title={list.active_error}>
          当前启用的 Hosts 配置有误，仍在使用上一次有效的规则
        </p>
      ) : null}
      {listError ? <p className="notice">{listError}</p> : null}
      {actionError ? <p className="notice">{actionError}</p> : null}

      {list == null ? (
        listError ? null : <p className="hosts-loading">正在读取 Hosts 配置</p>
      ) : list.profiles.length === 0 && !creating ? (
        <div className="hosts-empty">
          <p>还没有 Hosts 配置</p>
          <button className="btn" type="button" disabled={locked} onClick={startCreate}>
            新建配置
          </button>
        </div>
      ) : (
        <div className="hosts-body">
          <div className="hosts-list">
            <h2 className="hosts-list-head">配置</h2>
            {creating ? (
              <form className="hosts-create" onSubmit={(event) => void submitCreate(event)}>
                <input
                  className="input"
                  autoFocus
                  aria-label="新配置名"
                  placeholder="配置名"
                  disabled={locked}
                  value={newName}
                  onChange={(event) => setNewName(event.target.value)}
                />
                <div className="hosts-create-actions">
                  <button className="btn" type="submit" disabled={locked || newName.trim() === ''}>
                    创建
                  </button>
                  <button
                    className="btn"
                    type="button"
                    disabled={locked}
                    onClick={() => {
                      setCreating(false)
                      setNewName('')
                      setCreateError('')
                    }}
                  >
                    取消
                  </button>
                </div>
                {createError ? <p className="hosts-error">{createError}</p> : null}
              </form>
            ) : null}
            {list.profiles.map((item) => {
              const itemActive = sameName(item.name, list.active)
              const itemSelected = item.name === selected
              return (
                <button
                  key={item.name}
                  className={itemSelected ? 'hosts-item active' : 'hosts-item'}
                  type="button"
                  title={item.error || item.name}
                  aria-current={itemSelected ? 'true' : undefined}
                  disabled={locked}
                  onClick={() => selectProfile(item)}
                >
                  <span className={itemActive ? 'hosts-dot on' : 'hosts-dot'} aria-hidden="true" />
                  <span className="hosts-item-name">{item.name}</span>
                  <span className="hosts-item-count">{item.rules} 条</span>
                  {item.error ? <span className="hosts-item-error">有错误</span> : null}
                </button>
              )
            })}
          </div>

          <div className="hosts-main">
            {profile == null ? (
              <div className="hosts-empty">
                <p>选择左侧的配置进行编辑</p>
              </div>
            ) : (
              <>
                <div className="hosts-editor-head">
                  {renaming ? (
                    <form className="hosts-rename" onSubmit={(event) => void submitRename(event)}>
                      <input
                        className="input"
                        autoFocus
                        aria-label="配置名"
                        disabled={locked}
                        value={renameName}
                        onChange={(event) => setRenameName(event.target.value)}
                      />
                      <button className="btn" type="submit" disabled={locked || renameName.trim() === ''}>
                        确定
                      </button>
                      <button
                        className="btn"
                        type="button"
                        disabled={locked}
                        onClick={() => {
                          setRenaming(false)
                          setRenameError('')
                        }}
                      >
                        取消
                      </button>
                      {renameError ? <span className="hosts-error">{renameError}</span> : null}
                    </form>
                  ) : (
                    <>
                      <h2 title={profile.name}>{profile.name}</h2>
                      {isActive ? (
                        <span className="hosts-active">
                          <span className="hosts-dot on" aria-hidden="true" />
                          当前启用
                        </span>
                      ) : null}
                      <div className="hosts-actions">
                        <button className="btn" type="button" disabled={locked} onClick={() => void onSetActive()}>
                          {isActive ? '停用' : '启用'}
                        </button>
                        <button className="btn" type="button" disabled={locked} onClick={startRename}>
                          重命名
                        </button>
                        <button
                          className="btn"
                          type="button"
                          disabled={locked || isActive}
                          title={isActive ? '当前启用的 Hosts 配置不能删除' : ''}
                          onClick={() => void onDelete()}
                        >
                          删除
                        </button>
                      </div>
                    </>
                  )}
                </div>

                <textarea
                  className="hosts-textarea"
                  aria-label="Hosts 配置内容"
                  placeholder="每行一条：IP 域名，例如 127.0.0.1 example.com"
                  spellCheck={false}
                  disabled={loading}
                  readOnly={busy}
                  value={content}
                  onChange={(event) => setContent(event.target.value)}
                />

                <div className="hosts-status">
                  {loading ? <span>正在读取</span> : <span>{profile.rules} 条规则</span>}
                  {warnings.length > 0 ? (
                    <button
                      className="hosts-warn-toggle"
                      type="button"
                      aria-expanded={warningsOpen}
                      onClick={() => setWarningsOpen((open) => !open)}
                    >
                      · {warnings.length} 行重复已忽略
                    </button>
                  ) : null}
                  {savedFlash ? <span className="hosts-saved">已保存</span> : null}
                  <button
                    className="btn"
                    type="button"
                    disabled={locked || loading || !dirty}
                    onClick={() => void onSave()}
                  >
                    保存
                  </button>
                </div>

                {warningsOpen && warnings.length > 0 ? (
                  <ul className="hosts-warnings">
                    {warnings.slice(0, 3).map((warning, index) => (
                      <li key={`${warning.domain}-${warning.ignored_line}-${index}`}>{warningText(warning)}</li>
                    ))}
                    {warnings.length > 3 ? <li>还有 {warnings.length - 3} 条</li> : null}
                  </ul>
                ) : null}

                {detailError ? <p className="hosts-error">{detailError}</p> : null}
              </>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
