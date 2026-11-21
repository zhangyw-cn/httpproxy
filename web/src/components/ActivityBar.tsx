import type { ReactNode } from 'react'
import type { View } from '../types'

interface Props {
  view: View
  onSelect: (view: View) => void
}

interface Item {
  view: View
  label: string
  icon: ReactNode
}

function HomeIcon() {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 20 20"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M3 8l7-5 7 5v6a1 1 0 0 1-1 1h-3v-4H7v4H4a1 1 0 0 1-1-1z" />
    </svg>
  )
}

function CaptureIcon() {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M9 6h12" />
      <path d="M9 12h12" />
      <path d="M9 18h12" />
      <circle cx="4" cy="6" r="1.4" fill="currentColor" stroke="none" />
      <circle cx="4" cy="12" r="1.4" fill="currentColor" stroke="none" />
      <circle cx="4" cy="18" r="1.4" fill="currentColor" stroke="none" />
    </svg>
  )
}

function HostsIcon() {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <rect x="3" y="4" width="18" height="7" rx="1.5" />
      <rect x="3" y="13" width="18" height="7" rx="1.5" />
      <circle cx="7" cy="7.5" r="1" fill="currentColor" stroke="none" />
      <circle cx="7" cy="16.5" r="1" fill="currentColor" stroke="none" />
    </svg>
  )
}

function SettingsIcon() {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
      <circle cx="12" cy="12" r="3" />
    </svg>
  )
}

const items: Item[] = [
  { view: 'home', label: '首页', icon: <HomeIcon /> },
  { view: 'capture', label: '抓包', icon: <CaptureIcon /> },
  { view: 'hosts', label: 'Hosts', icon: <HostsIcon /> },
]

export default function ActivityBar({ view, onSelect }: Props) {
  return (
    <nav className="activity" aria-label="侧栏">
      {items.map((item) => (
        <button
          key={item.view}
          type="button"
          className={view === item.view ? 'activity-btn active' : 'activity-btn'}
          aria-label={item.label}
          title={item.label}
          aria-pressed={view === item.view}
          onClick={() => onSelect(item.view)}
        >
          {item.icon}
        </button>
      ))}
      <div className="activity-spacer" />
      <button
        type="button"
        className={view === 'settings' ? 'activity-btn active' : 'activity-btn'}
        aria-label="设置"
        title="设置"
        aria-pressed={view === 'settings'}
        onClick={() => onSelect('settings')}
      >
        <SettingsIcon />
      </button>
    </nav>
  )
}
