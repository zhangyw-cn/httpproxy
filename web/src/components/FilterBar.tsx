import type { Filters } from '../types'

interface Props {
  filters: Filters
  onChange: (f: Filters) => void
}

const STATUS_PRESETS = ['2', '3', '4', '5']

export default function FilterBar({ filters, onChange }: Props) {
  const set = (patch: Partial<Filters>) => onChange({ ...filters, ...patch })
  return (
    <div className="filterbar">
      <input
        className="input"
        placeholder="筛选主机"
        value={filters.host}
        onChange={(e) => set({ host: e.target.value })}
      />
      <input
        className="input"
        placeholder="筛选路径"
        value={filters.path}
        onChange={(e) => set({ path: e.target.value })}
      />
      <input
        className="input input-status"
        placeholder="状态码"
        title="1 位表示类别（2 → 2xx），2 位表示前缀（40 → 40x），3 位表示精确匹配"
        value={filters.status}
        onChange={(e) => set({ status: e.target.value.replace(/\D/g, '').slice(0, 3) })}
      />
      <div className="chips">
        <button
          className={`chip${filters.status === '' ? ' active' : ''}`}
          onClick={() => set({ status: '' })}
        >
          全部
        </button>
        {STATUS_PRESETS.map((s) => (
          <button
            key={s}
            className={`chip${filters.status === s ? ' active' : ''}`}
            onClick={() => set({ status: filters.status === s ? '' : s })}
          >
            {s}xx
          </button>
        ))}
      </div>
    </div>
  )
}
