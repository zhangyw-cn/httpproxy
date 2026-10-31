import { rulesMark } from '../capture'
import type { ConfigValues } from '../types'

interface Props {
  file: ConfigValues | null
  failed: boolean
}

export default function RulesMark({ file, failed }: Props) {
  const mark = rulesMark(file, failed)
  return (
    <div className={mark.active ? 'rules active' : 'rules'}>
      <button type="button" className="rules-btn" aria-label="抓包规则">
        <svg
          width="16"
          height="16"
          viewBox="0 0 16 16"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinecap="round"
          aria-hidden="true"
        >
          <path d="M2 4h12" />
          <path d="M2 8h12" />
          <path d="M2 12h12" />
          <circle cx="6" cy="4" r="1.6" fill="var(--bg)" />
          <circle cx="11" cy="8" r="1.6" fill="var(--bg)" />
          <circle cx="5" cy="12" r="1.6" fill="var(--bg)" />
        </svg>
      </button>
      <div className="rules-pop">
        {mark.lines.map((line, index) => (
          <div key={index}>{line}</div>
        ))}
      </div>
    </div>
  )
}
